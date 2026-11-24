#!/usr/bin/env python3
"""Exercise isolated Kubernetes install and upgrade fixtures through the CM API."""

import argparse
import json
import os
from pathlib import Path
import secrets
import ssl
import subprocess
import time
from urllib.error import HTTPError
from urllib.parse import urljoin, urlsplit
from urllib.request import Request, urlopen

RUNTIMES = ("openclaw", "hermes", "opencode", "deepseek-harness")
MODEL = "installation-smoke"
FILENAME = "installation-smoke.txt"
CONTENT = b"clawmanager installation workspace\n"
INSTANCE_BUSY = "instance operation already in progress; wait for it to finish"


class API:
    def __init__(self, origin):
        self.origin = origin.rstrip("/")
        # The isolated cluster generates its own certificate. Chrome tests
        # below exercise the normal certificate confirmation separately.
        self.context = ssl._create_unverified_context()
        self.token = None

    def request(self, method, path, body=None, expected=(200,), token=None, raw=False, content_type="application/json"):
        headers = {"Content-Type": content_type}
        if credential := token or self.token:
            headers["Authorization"] = f"Bearer {credential}"
        payload = json.dumps(body).encode() if isinstance(body, dict) else body
        try:
            response = urlopen(Request(self.origin + "/api/v1" + path, data=payload, headers=headers, method=method),
                               context=self.context, timeout=30)
        except HTTPError as error:
            response = error
        with response:
            status = response.status
            content = response.read()
        if status not in expected:
            try:
                message = json.loads(content).get("error")
            except ValueError:
                message = None
            raise RuntimeError(f"{method} {path} returned {status}: {message or 'unexpected response'}")
        if raw:
            return status, content
        result = json.loads(content or b"{}")
        return status, result.get("data", result)

    def call(self, method, path, body=None, **kwargs):
        return self.request(method, path, body, **kwargs)[1]

    def admin_login(self, password):
        login = self.call("POST", "/auth/login", {"username": "admin", "password": password})
        self.token = login["access_token"]
        if self.call("GET", "/auth/me")["role"] != "admin":
            raise RuntimeError("smoke tests require an isolated administrator")


def wait_for_runtime_registration(api, timeout=120):
    deadline = time.monotonic() + timeout
    missing = set(RUNTIMES)
    while time.monotonic() < deadline:
        pods = api.call("GET", "/admin/runtime-pods")["pods"]
        registered = {pod["runtime_type"] for pod in pods
                      if pod["agent_reported"] and pod.get("pod_uid") and pod["state"] == "ready"
                      and "container-slots-v1" in pod["capabilities"]}
        missing = set(RUNTIMES) - registered
        if not missing:
            return
        time.sleep(3)
    raise RuntimeError("Runtime containers did not register with CM: " + ", ".join(sorted(missing)))


def wait_for_state(api, instance_id, desired, timeout=150):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        status, reply = api.request("GET", f"/instances/{instance_id}", expected=(200, 404))
        last = "absent" if status == 404 else reply["instance"]["status"]
        if last == desired:
            return
        if last == "error":
            # The list API redacts credentials in runtime errors.
            items = api.call("GET", "/instances?status=error")["instances"]
            failed = next((item for item in items if item["id"] == instance_id), {})
            reason = " ".join(filter(None, (failed.get("runtime_error_code"), failed.get("runtime_error_message"))))
            name = reply["instance"]["type"]
            raise RuntimeError(f"{name} instance {instance_id} reached error, expected {desired}: "
                               f"{reason or 'no runtime failure details reported'}")
        if last == "absent" and desired != "absent":
            raise RuntimeError(f"instance {instance_id} reached {last}, expected {desired}")
        time.sleep(3)
    raise RuntimeError(f"instance {instance_id} stayed {last}, expected {desired}")


def upload(api, instance_id):
    boundary = "CM" + secrets.token_hex(12)
    header = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{FILENAME}\"\r\n"
              "Content-Type: text/plain\r\n\r\n")
    payload = header.encode() + CONTENT + f"\r\n--{boundary}--\r\n".encode()
    path = f"/instances/{instance_id}/workspace/upload"
    deadline = time.monotonic() + 30
    while True:
        status, reply = api.request("POST", path, payload, expected=(201, 409),
                                    content_type=f"multipart/form-data; boundary={boundary}")
        if status == 201:
            return
        error = reply.get("error")
        if error != INSTANCE_BUSY or time.monotonic() >= deadline:
            raise RuntimeError(f"POST {path} returned {status}: {error or 'unexpected response'}")
        time.sleep(1)


def assert_workspace(api, instance_id):
    path = f"/instances/{instance_id}/workspace/download?path={FILENAME}"
    if api.call("GET", path, raw=True) != CONTENT:
        raise RuntimeError(f"instance {instance_id} did not retain the workspace file")


def seed(api):
    api.call("PUT", "/admin/models", {
        "display_name": MODEL, "provider_type": "local", "protocol_type": "openai-compatible",
        "model_group": MODEL, "provider_model_name": MODEL, "base_url": "http://provider.invalid/v1",
        "is_active": True, "is_secure": True,
    }, expected=(200, 201))
    instances = {}
    for kind in RUNTIMES:
        instance = api.call("POST", "/instances", {
            "type": kind, "runtime_type": "gateway", "instance_mode": "lite",
            "name": f"installation-smoke-{kind}", "cpu_cores": 1, "memory_gb": 2, "disk_gb": 5,
            "os_type": kind, "os_version": "latest",
        }, expected=(201,))
        instance_id = instance["id"]
        instances[kind] = instance_id
        # Finish provisioning each Runtime before the next scale-out on the single-node cluster.
        wait_for_state(api, instance_id, "running")
        upload(api, instance_id)
        assert_workspace(api, instance_id)
        print(f"{kind}: created and wrote workspace", flush=True)
    return instances


def verify_gateway_key(api):
    result = api.call("POST", "/admin/gateway-keys", {
        "name": MODEL, "owner_user_id": api.call("GET", "/auth/me")["id"],
        "allowed_model_groups": [MODEL],
    }, expected=(201,))
    identifier = result["key"]["id"]
    if not isinstance(identifier, int) or identifier <= 0:
        raise RuntimeError("gateway key creation did not return its database ID")
    credential = result["token"]
    try:
        if MODEL not in {item["id"] for item in api.call("GET", "/gateway/llm/models", token=credential)}:
            raise RuntimeError("gateway key could not access its model")
    finally:
        api.call("POST", f"/admin/gateway-keys/{identifier}/revoke", {"reason": "install smoke complete"})
    api.call("GET", "/gateway/llm/models", token=credential, expected=(401, 403))
    print("gateway key: created, used and revoked with returned ID", flush=True)


def verify_browser(api, instances):
    host = urlsplit(api.origin).hostname
    urls = {}
    for kind in ("opencode", "deepseek-harness"):
        access = api.call("POST", f"/instances/{instances[kind]}/access")
        url = urlsplit(urljoin(api.origin, access["access_url"]))
        if (access.get("browser_origin_mode") != "chrome-single-host" or url.hostname != host
                or url.scheme != "https" or url.port == urlsplit(api.origin).port):
            raise RuntimeError(f"{kind} requires a dedicated same-host HTTPS browser origin")
        urls[kind] = url.geturl()
    subprocess.run(["node", str(Path(__file__).with_name("kubernetes-install-browser.mjs"))],
                   input=json.dumps({"manager": api.origin, "runtimes": urls}), text=True, check=True)


def verify(api, instances):
    if set(instances) != set(RUNTIMES):
        raise ValueError("the fixture must cover all four Lite Runtime types")
    verify_gateway_key(api)
    for kind, instance_id in instances.items():
        item = api.call("GET", f"/instances/{instance_id}")["instance"]
        if item["name"] != f"installation-smoke-{kind}" or item["type"] != kind:
            raise ValueError("fixture instance identity changed")
        wait_for_state(api, instance_id, "running")
        assert_workspace(api, instance_id)
    verify_browser(api, instances)
    for kind, instance_id in instances.items():
        api.call("POST", f"/instances/{instance_id}/stop", expected=(200, 202))
        wait_for_state(api, instance_id, "stopped")
        api.call("POST", f"/instances/{instance_id}/start", expected=(200, 202))
        wait_for_state(api, instance_id, "running")
        assert_workspace(api, instance_id)
        api.call("DELETE", f"/instances/{instance_id}", expected=(202,))
        wait_for_state(api, instance_id, "absent")
        print(f"{kind}: stopped, restarted, retained workspace and deleted", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=("fresh", "seed-upgrade", "verify-upgrade"))
    parser.add_argument("--origin", required=True)
    parser.add_argument("--fixture", type=Path)
    args = parser.parse_args()
    if args.phase != "fresh" and args.fixture is None:
        parser.error("upgrade phases require --fixture")
    api = API(args.origin)
    api.admin_login(os.environ["CM_SMOKE_PASSWORD"])
    wait_for_runtime_registration(api)
    if args.phase == "verify-upgrade":
        verify(api, json.loads(args.fixture.read_text()))
    else:
        instances = seed(api)
        if args.phase == "seed-upgrade":
            args.fixture.write_text(json.dumps(instances) + "\n")
        else:
            verify(api, instances)


if __name__ == "__main__":
    main()
