#!/usr/bin/env python3

"""Render the offline manifests from images/images.lock.

The source manifests remain human-readable defaults. Only the copies staged
inside a release bundle are rewritten, so images.lock is the single image map
that a release builder needs to edit.
"""

from pathlib import Path
import re
import sys


PLATFORM_DEPLOYMENTS = {
    "mysql": "platform.mysql",
    "minio": "platform.minio",
    "clawmanager-team-redis": "platform.redis",
    "skill-scanner": "platform.skill-scanner",
    "clawmanager-app": "platform.clawmanager",
}

RUNTIME_POOLS = {
    "openclaw-runtime": "runtime.openclaw.lite",
    "hermes-runtime": "runtime.hermes.lite",
}

RUNTIME_CATALOG = {
    "runtime.openclaw.pro": ("openclaw", "desktop", "OpenClaw Pro"),
    # The bootstrap ConfigMap creates an ENUM('desktop', 'shell') first. The
    # application migration later converts Lite rows from shell to gateway.
    "runtime.openclaw.lite": ("openclaw", "shell", "OpenClaw Lite"),
    "runtime.hermes.pro": ("hermes", "desktop", "Hermes Pro"),
    "runtime.hermes.lite": ("hermes", "shell", "Hermes Lite"),
    "runtime.dsh.pro": ("dsh", "desktop", "DeepSeek Harness Pro"),
    "runtime.dsh.lite": ("dsh", "shell", "DeepSeek Harness Lite"),
    "runtime.opencode.pro": ("opencode", "desktop", "OpenCode Pro"),
    "runtime.opencode.lite": ("opencode", "shell", "OpenCode Lite"),
    "runtime.workbuddy.internal": (
        "workbuddy",
        "desktop",
        "Workbuddy Internal",
    ),
}


def load_image_lock(path: Path) -> dict[str, str]:
    images: dict[str, str] = {}
    for line_number, raw_line in enumerate(
        path.read_text(encoding="utf-8-sig").splitlines(), start=1
    ):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        fields = line.split("|")
        if len(fields) != 2:
            raise SystemExit(f"{path}:{line_number}: expected key|image")
        key, image = (field.strip() for field in fields)
        if not re.fullmatch(r"[a-z0-9][a-z0-9._-]*", key):
            raise SystemExit(f"{path}:{line_number}: invalid image key: {key}")
        if key in images:
            raise SystemExit(f"{path}:{line_number}: duplicate image key: {key}")
        if not image and not key.startswith("runtime."):
            raise SystemExit(
                f"{path}:{line_number}: only runtime.* values may be empty"
            )
        if image:
            if re.search(r"[\s'\"`]", image):
                raise SystemExit(
                    f"{path}:{line_number}: unsafe or invalid image reference: {image}"
                )
            if not re.fullmatch(r"[^\s]+:[^\s/:]+", image):
                raise SystemExit(
                    f"{path}:{line_number}: image must have an explicit tag: {image}"
                )
        images[key] = image
    return images


def yaml_documents(text: str) -> list[str]:
    return re.split(r"(?m)^---\s*\n", text)


def document_identity(document: str) -> tuple[str | None, str | None]:
    kind_match = re.search(r"(?m)^kind:\s*([^\s#]+)\s*$", document)
    metadata_match = re.search(
        r"(?m)^metadata:\s*\n(?:(?:  |\t).*\n)*?^  name:\s*([^\s#]+)\s*$",
        document,
    )
    return (
        kind_match.group(1) if kind_match else None,
        metadata_match.group(1) if metadata_match else None,
    )


def replace_deployment_image(document: str, image: str, name: str) -> str:
    rendered, count = re.subn(
        r"(?m)^(\s{6,}image:)\s*[^\s#]+\s*$",
        rf"\1 {image}",
        document,
        count=1,
    )
    if count != 1:
        raise SystemExit(f"Deployment/{name}: expected exactly one container image")
    return rendered


def replace_runtime_image_env(document: str, image: str, name: str) -> str:
    rendered, count = re.subn(
        r"(?m)^(\s*- name: CLAWMANAGER_RUNTIME_IMAGE_REF\s*\n"
        r"\s*value:)\s*[^\s#]+\s*$",
        rf"\1 {image}",
        document,
        count=1,
    )
    if count != 1:
        raise SystemExit(
            f"Deployment/{name}: missing CLAWMANAGER_RUNTIME_IMAGE_REF"
        )
    return rendered


def render_runtime_sql(images: dict[str, str]) -> str:
    lines = ["  099_set_offline_runtime_images.sql: |", "    USE clawmanager;"]
    active_count = 0
    for key, (instance_type, runtime_type, display_name) in RUNTIME_CATALOG.items():
        image = images.get(key, "")
        if not image:
            continue
        active_count += 1
        lines.extend(
            [
                "",
                "    UPDATE system_image_settings",
                f"    SET image = '{image}', display_name = '{display_name}'",
                f"    WHERE instance_type = '{instance_type}'",
                f"      AND runtime_type = '{runtime_type}';",
                "",
                "    INSERT INTO system_image_settings "
                "(instance_type, runtime_type, display_name, image)",
                f"    SELECT '{instance_type}', '{runtime_type}', "
                f"'{display_name}', '{image}'",
                "    WHERE NOT EXISTS (",
                "      SELECT 1 FROM system_image_settings",
                f"      WHERE instance_type = '{instance_type}'",
                f"        AND runtime_type = '{runtime_type}'",
                "    );",
            ]
        )
    if active_count == 0:
        lines.extend(["", "    SELECT 1;"])
    return "\n".join(lines) + "\n"


def render_manifest(path: Path, images: dict[str, str]) -> None:
    text = path.read_text(encoding="utf-8-sig")
    text = text.replace("\r\n", "\n").replace("\r", "\n")

    sql_pattern = re.compile(
        r"(?ms)^  099_set_offline_runtime_images\.sql: \|\n.*?(?=^---\s*$)"
    )
    text, sql_count = sql_pattern.subn(render_runtime_sql(images), text)
    if sql_count != 1:
        raise SystemExit(f"{path}: expected one 099 runtime image SQL block")

    rendered_documents: list[str] = []
    seen_platform: set[str] = set()
    seen_runtime_deployments: set[str] = set()
    for document in yaml_documents(text):
        if not document.strip():
            continue
        kind, name = document_identity(document)

        runtime_key = RUNTIME_POOLS.get(name or "")
        if runtime_key and not images.get(runtime_key, ""):
            # The service and deployment form one pre-warmed Lite runtime pool.
            # An empty lock value means the entire pool is absent from this release.
            continue

        if kind == "Deployment" and name in PLATFORM_DEPLOYMENTS:
            key = PLATFORM_DEPLOYMENTS[name]
            image = images.get(key, "")
            if not image:
                raise SystemExit(f"{path}: missing required lock value: {key}")
            document = replace_deployment_image(document, image, name)
            seen_platform.add(name)

        if kind == "Deployment" and name in RUNTIME_POOLS:
            key = RUNTIME_POOLS[name]
            image = images[key]
            document = replace_deployment_image(document, image, name)
            document = replace_runtime_image_env(document, image, name)
            seen_runtime_deployments.add(name)

        document = re.sub(
            r"(?m)^(\s*imagePullPolicy:)\s*Always\s*$",
            r"\1 IfNotPresent",
            document,
        )
        rendered_documents.append(document.rstrip("\n"))

    missing_platform = set(PLATFORM_DEPLOYMENTS) - seen_platform
    if missing_platform:
        raise SystemExit(
            f"{path}: missing platform Deployment(s): "
            + ", ".join(sorted(missing_platform))
        )
    for name, key in RUNTIME_POOLS.items():
        if images.get(key, "") and name not in seen_runtime_deployments:
            raise SystemExit(f"{path}: missing runtime Deployment/{name}")

    rendered_text = "\n---\n".join(rendered_documents) + "\n"
    with path.open("w", encoding="utf-8", newline="\n") as manifest:
        manifest.write(rendered_text)


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: render-bundle-manifests.py BUNDLE_ROOT")
    root = Path(sys.argv[1])
    images = load_image_lock(root / "images" / "images.lock")
    unknown_runtime_keys = {
        key for key in images if key.startswith("runtime.")
    } - set(RUNTIME_CATALOG)
    if unknown_runtime_keys:
        raise SystemExit(
            "unsupported runtime image key(s): "
            + ", ".join(sorted(unknown_runtime_keys))
        )
    for name in ("clawmanager-single-node.yaml", "clawmanager-cluster.yaml"):
        render_manifest(root / "manifests" / name, images)


if __name__ == "__main__":
    main()
