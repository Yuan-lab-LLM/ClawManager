#!/usr/bin/env python3
"""Carry the rendered dedicated origin into an existing-tenant Core patch."""
import argparse
from pathlib import Path
from urllib.parse import urlsplit

SETTING = "CLAWMANAGER_OPENCODE_PUBLIC_URL_TEMPLATE"


def merge(manifest, patch, namespace):
    apps = [d for d in manifest if isinstance(d, dict) and d.get("kind") == "Deployment"
            and d.get("metadata", {}).get("name") == "clawmanager-app"
            and d["metadata"].get("namespace") == namespace]
    if len(apps) != 1:
        raise ValueError("Expected exactly one tenant Core deployment")
    containers = apps[0]["spec"]["template"]["spec"]["containers"]
    source = next(c for c in containers if c["name"] == "clawmanager-app")
    origins = [e for e in source.get("env", []) if e["name"] == SETTING]
    if len(origins) != 1 or not isinstance(origins[0].get("value"), str):
        raise ValueError("Rendered OpenCode origin is missing or ambiguous")
    value = origins[0]["value"]
    url = urlsplit(value)
    if url.scheme != "https" or "{instance_id}" not in (url.hostname or "") or url.username or url.password or url.query or url.fragment or url.path not in ("", "/"):
        raise ValueError("Expected a dedicated HTTPS instance origin template")
    target = next(c for c in patch["spec"]["template"]["spec"]["containers"] if c["name"] == "clawmanager-app")
    env = target.setdefault("env", [])
    env[:] = [e for e in env if e["name"] != SETTING]
    env.append({"name": SETTING, "value": value})
    return patch


if __name__ == "__main__":
    import yaml
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest")
    parser.add_argument("patch")
    parser.add_argument("namespace")
    args = parser.parse_args()
    result = merge(list(yaml.safe_load_all(Path(args.manifest).read_text())), yaml.safe_load(Path(args.patch).read_text()), args.namespace)
    Path(args.patch).write_text(yaml.safe_dump(result, sort_keys=False))
    print("Dedicated OpenCode origin preserved in tenant Core patch")
