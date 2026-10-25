#!/usr/bin/env python3
"""ClawManager Northbound API demo.

Requires Python 3.10+, ``cryptography``, and ``python-dotenv``. Configuration is
loaded from ``examples/.env``. Credentials are encrypted
locally as a compact JWE; only the ciphertext is sent to the login endpoint.
"""

from __future__ import annotations

import argparse
import base64
import email.utils
import json
import os
import secrets
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
from typing import Any

try:
    from dotenv import load_dotenv
except ImportError:  # Allow ``help`` to work before dependencies are installed.
    load_dotenv = None

try:
    from cryptography.hazmat.primitives import hashes
    from cryptography.hazmat.primitives.asymmetric import padding, rsa
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
except ImportError:  # Allow ``help`` to work before dependencies are installed.
    hashes = None
    padding = None
    rsa = None
    AESGCM = None


ENV_FILE = Path(__file__).resolve().parent / ".env"
if load_dotenv is not None:
    load_dotenv(dotenv_path=ENV_FILE, override=False)


API_PREFIX = "/api/northbound/v1"


class NorthboundAPIError(RuntimeError):
    """An error response or transport failure from the Northbound API."""

    def __init__(
        self,
        message: str,
        *,
        status: int | None = None,
        code: str | None = None,
        request_id: str | None = None,
    ) -> None:
        super().__init__(message)
        self.status = status
        self.code = code
        self.request_id = request_id


def env_bool(name: str, fallback: bool = False) -> bool:
    value = os.getenv(name)
    if value is None or value == "":
        return fallback
    return value.lower() in {"1", "true", "yes", "on"}


def env_positive_int(name: str, fallback: int) -> int:
    try:
        value = int(os.getenv(name, ""))
    except ValueError:
        return fallback
    return value if value > 0 else fallback


def instance_collection_path(instance_type: str = "", instance_mode: str = "") -> str:
    """Resolve the collection without exposing mode in the request body."""
    normalized_type = instance_type.strip().lower()
    normalized_mode = (instance_mode or os.getenv("NORTHBOUND_INSTANCE_MODE", "lite")).strip().lower()
    if normalized_type == "workbuddy":
        return "/lite-instances"
    if normalized_mode == "pro":
        return "/pro-instances"
    if normalized_mode != "lite":
        raise ValueError("NORTHBOUND_INSTANCE_MODE must be lite or pro")
    return "/lite-instances"


def required_positive_int(name: str) -> int:
    try:
        value = int(os.getenv(name, ""))
    except ValueError as exc:
        raise ValueError(f"{name} must be a positive integer") from exc
    if value <= 0:
        raise ValueError(f"{name} must be a positive integer")
    return value


def required_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise ValueError(f"{name} is required")
    return value


def b64url_encode(value: bytes) -> str:
    return base64.urlsafe_b64encode(value).rstrip(b"=").decode("ascii")


def b64url_decode(value: str) -> bytes:
    padding_length = (-len(value)) % 4
    return base64.urlsafe_b64decode(value + ("=" * padding_length))


def parse_json(raw: bytes) -> Any:
    if not raw:
        return None
    try:
        return json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return {"message": raw.decode("utf-8", errors="replace")}


class NorthboundClient:
    def __init__(self, base_url: str, public_base_url: str = "") -> None:
        parsed = urllib.parse.urlparse(base_url)
        if parsed.scheme.lower() != "https" or not parsed.netloc:
            raise ValueError("NORTHBOUND_BASE_URL must be an absolute HTTPS URL")
        self.base_url = base_url.rstrip("/")
        self.public_base_url = public_base_url.rstrip("/")
        self.timeout = env_positive_int("NORTHBOUND_HTTP_TIMEOUT_SECONDS", 30)
        ca_file = os.getenv("NORTHBOUND_CA_FILE", "").strip()
        if ca_file:
            ca_path = Path(ca_file).expanduser()
            if not ca_path.is_absolute():
                ca_path = ENV_FILE.parent / ca_path
            ca_file = str(ca_path.resolve())
        self.ssl_context = ssl.create_default_context(cafile=ca_file or None)
        self.tokens: dict[str, Any] | None = None

    def request(
        self,
        method: str,
        path: str,
        *,
        body: Any = None,
        headers: dict[str, str] | None = None,
    ) -> tuple[Any, Any]:
        request_headers = {"Accept": "application/json", **(headers or {})}
        payload = None
        if body is not None:
            payload = json.dumps(body, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
            request_headers["Content-Type"] = "application/json"
        request = urllib.request.Request(
            self.base_url + path,
            data=payload,
            headers=request_headers,
            method=method,
        )
        try:
            with urllib.request.urlopen(
                request,
                context=self.ssl_context,
                timeout=self.timeout,
            ) as response:
                return parse_json(response.read()), response.headers
        except urllib.error.HTTPError as exc:
            response_body = parse_json(exc.read())
            request_id = None
            code = None
            message = exc.reason
            if isinstance(response_body, dict):
                request_id = response_body.get("request_id")
                code = response_body.get("code")
                message = response_body.get("message") or message
            request_id = request_id or exc.headers.get("X-Request-ID") or "unknown"
            raise NorthboundAPIError(
                f"{exc.code} {code or 'HTTP_ERROR'}: {message} (request_id={request_id})",
                status=exc.code,
                code=code,
                request_id=request_id,
            ) from exc
        except urllib.error.URLError as exc:
            raise NorthboundAPIError(f"Unable to reach Northbound API: {exc.reason}") from exc

    def login(self, username: str, password: str) -> dict[str, Any]:
        challenge, response_headers = self.request(
            "POST", f"{API_PREFIX}/auth/challenge"
        )
        issued_at = http_date_epoch(response_headers)
        credential_jwe = encrypt_credential(
            challenge, username, password, issued_at=issued_at
        )
        tokens, _ = self.request(
            "POST",
            f"{API_PREFIX}/auth/login",
            body={
                "challenge_id": challenge["challenge_id"],
                "credential_jwe": credential_jwe,
            },
        )
        self.tokens = tokens
        return tokens

    def authenticated_request(
        self,
        method: str,
        path: str,
        *,
        body: Any = None,
        headers: dict[str, str] | None = None,
    ) -> tuple[Any, Any]:
        if not self.tokens or not self.tokens.get("access_token"):
            raise RuntimeError("Client is not logged in")
        request_headers = {
            "Authorization": f"Bearer {self.tokens['access_token']}",
            **(headers or {}),
        }
        return self.request(method, API_PREFIX + path, body=body, headers=request_headers)

    def absolute_share_url(self, share_path: str) -> str | None:
        if not self.public_base_url or not share_path.startswith("/"):
            return None
        return urllib.parse.urljoin(self.public_base_url + "/", share_path.lstrip("/"))


def http_date_epoch(headers: Any) -> int:
    raw_date = headers.get("Date") if headers is not None else None
    if not raw_date:
        raise ValueError("Challenge response is missing the HTTPS Date header")
    try:
        parsed = email.utils.parsedate_to_datetime(raw_date)
    except (TypeError, ValueError) as exc:
        raise ValueError("Challenge response contains an invalid HTTPS Date header") from exc
    if parsed.tzinfo is None:
        raise ValueError("Challenge response HTTPS Date header has no timezone")
    issued_at = int(parsed.timestamp())
    if issued_at <= 0:
        raise ValueError("Challenge response HTTPS Date header is out of range")
    return issued_at


def encrypt_credential(
    challenge: dict[str, Any],
    username: str,
    password: str,
    *,
    issued_at: int,
) -> str:
    if AESGCM is None or hashes is None or padding is None or rsa is None:
        raise RuntimeError(
            "Missing dependency 'cryptography'. Run: "
            "python -m pip install -r examples/requirements-northbound.txt"
        )

    encryption = challenge.get("encryption") or {}
    if encryption.get("alg") != "RSA-OAEP-256" or encryption.get("enc") != "A256GCM":
        raise ValueError("Server returned an unsupported JWE challenge")
    key_id = encryption.get("kid")
    jwk = encryption.get("public_jwk") or {}
    if not key_id or jwk.get("kty") != "RSA" or not jwk.get("n") or not jwk.get("e"):
        raise ValueError("Server returned an invalid JWE public key")
    if jwk.get("kid") and jwk["kid"] != key_id:
        raise ValueError("JWE challenge key IDs do not match")

    modulus = int.from_bytes(b64url_decode(jwk["n"]), "big")
    exponent = int.from_bytes(b64url_decode(jwk["e"]), "big")
    public_key = rsa.RSAPublicNumbers(exponent, modulus).public_key()

    protected_header = b64url_encode(
        json.dumps(
            {"kid": key_id, "alg": "RSA-OAEP-256", "enc": "A256GCM"},
            separators=(",", ":"),
        ).encode("utf-8")
    )
    content_encryption_key = bytearray(secrets.token_bytes(32))
    plaintext = bytearray(
        json.dumps(
            {
                "username": username,
                "password": password,
                "challenge_id": challenge["challenge_id"],
                "nonce": challenge["nonce"],
                "client_nonce": secrets.token_urlsafe(32),
                "issued_at": issued_at,
            },
            separators=(",", ":"),
            ensure_ascii=False,
        ).encode("utf-8")
    )
    try:
        encrypted_key = public_key.encrypt(
            bytes(content_encryption_key),
            padding.OAEP(
                mgf=padding.MGF1(algorithm=hashes.SHA256()),
                algorithm=hashes.SHA256(),
                label=None,
            ),
        )
        iv = secrets.token_bytes(12)
        sealed = AESGCM(bytes(content_encryption_key)).encrypt(
            iv,
            bytes(plaintext),
            protected_header.encode("ascii"),
        )
        ciphertext, tag = sealed[:-16], sealed[-16:]
        return ".".join(
            [
                protected_header,
                b64url_encode(encrypted_key),
                b64url_encode(iv),
                b64url_encode(ciphertext),
                b64url_encode(tag),
            ]
        )
    finally:
        for index in range(len(content_encryption_key)):
            content_encryption_key[index] = 0
        for index in range(len(plaintext)):
            plaintext[index] = 0


def print_result(client: NorthboundClient, value: Any, *, show_secrets: bool = False) -> None:
    def prepare(item: Any) -> Any:
        if isinstance(item, list):
            return [prepare(entry) for entry in item]
        if not isinstance(item, dict):
            return item
        result = {key: prepare(entry) for key, entry in item.items()}
        share_path = result.get("share_url")
        if isinstance(share_path, str):
            absolute = client.absolute_share_url(share_path)
            if absolute:
                result["share_url_absolute"] = absolute
        if result.get("password") and not show_secrets:
            result["password"] = "[REDACTED]"
        return result

    result = prepare(json.loads(json.dumps(value)))
    print(json.dumps(result, indent=2, ensure_ascii=False))


def share_link_password_request() -> dict[str, Any]:
    expires_mode = (
        os.getenv("NORTHBOUND_SHARELINK_EXPIRES_MODE", "preset").strip().lower()
        or "preset"
    )
    if expires_mode not in {"preset", "custom", "permanent"}:
        raise ValueError(
            "NORTHBOUND_SHARELINK_EXPIRES_MODE must be preset, custom, or permanent"
        )
    workspace_access = (
        os.getenv("NORTHBOUND_SHARELINK_WORKSPACE_ACCESS", "none").strip().lower()
        or "none"
    )
    if workspace_access not in {"none", "read", "write"}:
        raise ValueError(
            "NORTHBOUND_SHARELINK_WORKSPACE_ACCESS must be none, read, or write"
        )
    payload: dict[str, Any] = {
        "expires_mode": expires_mode,
        "workspace_access": workspace_access,
    }
    if expires_mode == "preset":
        preset = (
            os.getenv("NORTHBOUND_SHARELINK_EXPIRES_PRESET", "24h").strip().lower()
            or "24h"
        )
        if preset not in {"1h", "24h", "7d", "30d"}:
            raise ValueError(
                "NORTHBOUND_SHARELINK_EXPIRES_PRESET must be 1h, 24h, 7d, or 30d"
            )
        payload["expires_preset"] = preset
    elif expires_mode == "custom":
        payload["expires_at"] = required_env("NORTHBOUND_SHARELINK_EXPIRES_AT")
    return payload


def enable_password_share_link(
    client: NorthboundClient, instance_id: int
) -> dict[str, Any]:
    result, _ = client.authenticated_request(
        "POST",
        f"/lite-instances/{instance_id}/external-access/password",
        body=share_link_password_request(),
    )
    return result


def wait_for_operation(client: NorthboundClient, operation_id: str) -> dict[str, Any]:
    interval = env_positive_int("NORTHBOUND_POLL_INTERVAL_MS", 2000) / 1000
    timeout = env_positive_int("NORTHBOUND_POLL_TIMEOUT_MS", 180000) / 1000
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        operation, _ = client.authenticated_request(
            "GET",
            f"/operations/{urllib.parse.quote(operation_id, safe='')}",
        )
        if operation.get("status") in {"succeeded", "failed"}:
            return operation
        time.sleep(interval)
    raise TimeoutError(f"Timed out waiting for operation {operation_id}")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="ClawManager Northbound API demo")
    parser.add_argument(
        "command",
        nargs="?",
        choices=[
            "me",
            "create",
            "list",
            "get",
            "operation",
            "restart-instance",
            "reset-instance",
            "delete-instance",
            "enable-password",
            "reset-url",
            "reset-password",
            "refresh",
            "logout",
        ],
        help="Northbound API operation to run",
    )
    return parser


def run(command: str) -> None:
    if load_dotenv is None:
        raise RuntimeError(
            "Missing dependency 'python-dotenv'. Run: "
            "python -m pip install -r examples/requirements-northbound.txt"
        )
    auto_enable_share_link = command == "create" and env_bool(
        "NORTHBOUND_ENABLE_SHARELINK"
    )
    creates_share_link_password = (
        command in {"enable-password", "reset-password"} or auto_enable_share_link
    )
    if creates_share_link_password and not env_bool("NORTHBOUND_SHOW_SECRETS"):
        raise ValueError(
            "Refusing to generate and discard a ShareLink password. Set "
            "NORTHBOUND_SHOW_SECRETS=true in a secure interactive terminal."
        )
    if auto_enable_share_link and not env_bool("NORTHBOUND_WAIT_CREATE", True):
        raise ValueError(
            "NORTHBOUND_ENABLE_SHARELINK=true requires NORTHBOUND_WAIT_CREATE=true"
        )

    username = required_env("NORTHBOUND_USERNAME")
    password = required_env("NORTHBOUND_PASSWORD")
    client = NorthboundClient(
        os.getenv("NORTHBOUND_BASE_URL", "https://localhost:38443"),
        os.getenv("CLAWMANAGER_PUBLIC_BASE_URL", ""),
    )
    tokens = client.login(username, password)

    if command == "me":
        result, _ = client.authenticated_request("GET", "/auth/me")
        print_result(client, result)
        return

    if command == "create":
        owner = required_env("NORTHBOUND_OWNER")
        instance_type = os.getenv("NORTHBOUND_INSTANCE_TYPE", "openclaw").strip().lower()
        allowed_types = {
            "openclaw",
            "hermes",
            "opencode",
            "deepseek-harness",
        }
        if instance_type not in allowed_types:
            raise ValueError(
                "NORTHBOUND_INSTANCE_TYPE must be openclaw, hermes, opencode, "
                "or deepseek-harness"
            )
        instance_name = os.getenv("NORTHBOUND_INSTANCE_NAME", "").strip()
        payload: dict[str, Any] = {
            "name": instance_name or f"api-{instance_type}-{int(time.time() * 1000)}",
            "owner": owner,
            "type": instance_type,
        }
        if os.getenv("NORTHBOUND_DESCRIPTION"):
            payload["description"] = os.environ["NORTHBOUND_DESCRIPTION"]
        if os.getenv("NORTHBOUND_INSTANCE_ALIAS", "").strip():
            payload["alias"] = os.environ["NORTHBOUND_INSTANCE_ALIAS"].strip()
        idempotency_key = os.getenv(
            "NORTHBOUND_IDEMPOTENCY_KEY", f"demo-{uuid.uuid4()}"
        )
        operation, headers = client.authenticated_request(
            "POST",
            instance_collection_path(instance_type),
            body=payload,
            headers={"Idempotency-Key": idempotency_key},
        )
        result: dict[str, Any] = {
            "operation": operation,
            "idempotent_replayed": headers.get("Idempotent-Replayed") == "true",
        }
        if env_bool("NORTHBOUND_WAIT_CREATE", True):
            final_operation = wait_for_operation(client, operation["operation_id"])
            result["final_operation"] = final_operation
            if auto_enable_share_link and final_operation.get("status") == "succeeded":
                instance_id = final_operation.get("instance_id")
                if not isinstance(instance_id, int) or instance_id <= 0:
                    raise RuntimeError(
                        "Succeeded create operation did not return a valid instance_id"
                    )
                result["share_link"] = enable_password_share_link(client, instance_id)
        print_result(client, result, show_secrets=auto_enable_share_link)
        return

    if command == "list":
        owner = required_env("NORTHBOUND_OWNER")
        query = urllib.parse.urlencode(
            {
                "owner": owner,
                "page": env_positive_int("NORTHBOUND_PAGE", 1),
                "limit": env_positive_int("NORTHBOUND_LIMIT", 20),
            }
        )
        result, _ = client.authenticated_request(
            "GET", f"{instance_collection_path()}?{query}"
        )
        print_result(client, result)
        return

    if command == "get":
        instance_id = required_positive_int("NORTHBOUND_INSTANCE_ID")
        result, _ = client.authenticated_request(
            "GET", f"{instance_collection_path()}/{instance_id}"
        )
        print_result(client, result)
        return

    if command == "operation":
        operation_id = required_env("NORTHBOUND_OPERATION_ID")
        encoded_id = urllib.parse.quote(operation_id, safe="")
        result, _ = client.authenticated_request("GET", f"/operations/{encoded_id}")
        print_result(client, result)
        return

    if command in {"restart-instance", "reset-instance", "delete-instance"}:
        instance_id = required_positive_int("NORTHBOUND_INSTANCE_ID")
        action = command.removesuffix("-instance")
        idempotency_key = os.getenv(
            "NORTHBOUND_IDEMPOTENCY_KEY", f"demo-{action}-{uuid.uuid4()}"
        )
        body = {"confirm_data_loss": True} if action in {"reset", "delete"} else {}
        operation, headers = client.authenticated_request(
            "POST",
            f"{instance_collection_path()}/{instance_id}/{action}",
            body=body,
            headers={"Idempotency-Key": idempotency_key},
        )
        result = {
            "operation": operation,
            "idempotent_replayed": headers.get("Idempotent-Replayed") == "true",
            "final_operation": wait_for_operation(client, operation["operation_id"]),
        }
        print_result(client, result)
        return

    if command == "enable-password":
        instance_id = required_positive_int("NORTHBOUND_INSTANCE_ID")
        result = enable_password_share_link(client, instance_id)
        print_result(client, result, show_secrets=True)
        return

    if command == "reset-url":
        instance_id = required_positive_int("NORTHBOUND_INSTANCE_ID")
        result, _ = client.authenticated_request(
            "POST",
            f"/lite-instances/{instance_id}/external-access/share-link/reset",
            body={},
        )
        print_result(client, result)
        return

    if command == "reset-password":
        instance_id = required_positive_int("NORTHBOUND_INSTANCE_ID")
        result, _ = client.authenticated_request(
            "POST",
            f"/lite-instances/{instance_id}/external-access/password/reset",
            body={},
        )
        print_result(client, result, show_secrets=True)
        return

    if command == "refresh":
        refreshed, _ = client.request(
            "POST",
            f"{API_PREFIX}/auth/refresh",
            body={"refresh_token": tokens["refresh_token"]},
        )
        print_result(
            client,
            {
                "token_type": refreshed["token_type"],
                "expires_in": refreshed["expires_in"],
                "refresh_expires_in": refreshed["refresh_expires_in"],
                "scopes": refreshed["scopes"],
                "session_id": refreshed["session_id"],
                "tokens_redacted": True,
            },
        )
        return

    if command == "logout":
        client.authenticated_request("POST", "/auth/logout")
        print_result(client, {"logged_out": True, "session_id": tokens["session_id"]})
        return

    raise ValueError(f"Unsupported command: {command}")


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        return 0
    try:
        run(args.command)
        return 0
    except (NorthboundAPIError, RuntimeError, TimeoutError, ValueError, KeyError) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
