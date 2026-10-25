#!/usr/bin/env python3
"""Generate a short-lived IEI SSO URL for the configured instance owner."""

from __future__ import annotations

import base64
import binascii
import json
import os
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from email.utils import parseaddr
from pathlib import Path
from urllib.parse import urlencode, urlparse

try:
    from cryptography.hazmat.primitives import padding
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    from dotenv import load_dotenv
except ImportError as exc:
    raise SystemExit(
        "Missing dependencies. Run: "
        "python -m pip install -r examples/requirements-northbound.txt"
    ) from exc


ENV_FILE = Path(__file__).resolve().with_name(".env")
load_dotenv(dotenv_path=ENV_FILE, override=False)


def required_value(*names: str) -> str:
    for name in names:
        value = os.getenv(name, "").strip()
        if value:
            return value
    raise ValueError(f"Set {names[0]} in {ENV_FILE}")


def read_secret_value(*fields: str) -> bytes:
    namespace = os.getenv("IEISYSTEM_K8S_NAMESPACE", "clawmanager-system").strip()
    secret = os.getenv("IEISYSTEM_K8S_SECRET", "clawmanager-iei-sso").strip()
    kubeconfig = os.getenv("IEISYSTEM_KUBECONFIG", "").strip()

    command = ["kubectl"]
    if kubeconfig:
        command.extend(["--kubeconfig", kubeconfig])
    command.extend(
        [
            "-n",
            namespace,
            "get",
            "secret",
            secret,
            "-o",
            "json",
        ]
    )
    try:
        result = subprocess.run(
            command,
            check=True,
            capture_output=True,
            text=True,
            timeout=20,
        )
    except FileNotFoundError as exc:
        raise ValueError("kubectl was not found in PATH") from exc
    except subprocess.CalledProcessError as exc:
        message = exc.stderr.strip() or "kubectl returned an error"
        raise ValueError(
            f"Unable to read {field} from Kubernetes Secret: {message}"
        ) from exc
    except subprocess.TimeoutExpired as exc:
        raise ValueError("Timed out while reading the Kubernetes Secret") from exc

    try:
        data = json.loads(result.stdout).get("data", {})
    except (TypeError, json.JSONDecodeError) as exc:
        raise ValueError("Kubernetes Secret response is not valid JSON") from exc

    for field in fields:
        encoded = data.get(field)
        if not encoded:
            continue
        try:
            return base64.b64decode(encoded, validate=True)
        except (ValueError, binascii.Error) as exc:
            raise ValueError(
                f"Kubernetes Secret field {field} is not valid Base64"
            ) from exc
    raise ValueError(
        f"Kubernetes Secret does not contain any of: {', '.join(fields)}"
    )


def load_key_material() -> tuple[bytes, bytes]:
    configured_key = os.getenv("IEISYSTEM_SSO_KEY", "")
    key = (
        configured_key.encode("utf-8")
        if configured_key
        else read_secret_value("aes-key", "IEISYSTEM_SSO_KEY")
    )

    configured_iv = os.getenv("IEISYSTEM_SSO_IV", "")
    iv = (
        configured_iv.encode("utf-8")
        if configured_iv
        else read_secret_value("aes-iv", "IEISYSTEM_SSO_IV")
    )

    if len(key) != 16:
        raise ValueError("IEISYSTEM_SSO_KEY must contain exactly 16 UTF-8 bytes")
    if len(iv) != 16:
        raise ValueError("IEISYSTEM_SSO_IV must contain exactly 16 UTF-8 bytes")
    return key, iv


def generate_url() -> str:
    owner = required_value("IEISYSTEM_OWNER_EMAIL", "NORTHBOUND_OWNER").lower()
    local_part, separator, domain = owner.rpartition("@")
    if (
        not separator
        or not local_part
        or not domain
        or "." not in domain
        or parseaddr(owner)[1] != owner
    ):
        raise ValueError(
            "NORTHBOUND_OWNER must be the IEI login email, for example "
            "user@example.com"
        )

    portal_base_url = required_value(
        "IEISYSTEM_PORTAL_BASE_URL", "CLAWMANAGER_PUBLIC_BASE_URL"
    ).rstrip("/")
    parsed_url = urlparse(portal_base_url)
    if parsed_url.scheme != "https" or not parsed_url.netloc:
        raise ValueError("IEISYSTEM_PORTAL_BASE_URL must be an absolute HTTPS URL")

    key, iv = load_key_material()
    china_standard_time = timezone(timedelta(hours=8))
    timestamp = datetime.now(china_standard_time).strftime("%Y-%m-%d %H:%M:%S")
    plaintext = f"{owner}+{timestamp}".encode("utf-8")

    padder = padding.PKCS7(algorithms.AES.block_size).padder()
    padded = padder.update(plaintext) + padder.finalize()
    encryptor = Cipher(algorithms.AES(key), modes.CBC(iv)).encryptor()
    ciphertext = encryptor.update(padded) + encryptor.finalize()
    token = base64.b64encode(ciphertext).decode("ascii")

    return (
        f"{portal_base_url}/ieisystem/list-instances?"
        f"{urlencode({'token': token})}"
    )


def main() -> int:
    try:
        print(generate_url())
        return 0
    except ValueError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
