#!/usr/bin/env python3
"""Submit a CM-managed immutable Runtime release; never touch Kubernetes or SQL."""
import argparse
import fcntl
import http.client
import json
import os
from pathlib import Path
import re
import ssl
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

ROLLOUTS = "/admin/runtime-rollouts"
SCHEDULING = "/admin/runtime-scheduling"


class Stop(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def emit(**values):
    if "status" in values and values["status"] not in ("pending", "processing", "starting", "running", "completed", "finished", "blocked", "preflight_passed", "preflight_blocked", "error", "failed", "cancelled", "needs_attention"):
        values["status"] = "unknown"
    print(json.dumps(values, sort_keys=True), flush=True)


def save(path, state):
    descriptor, temporary = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump(state, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


class API:
    def __init__(self, args, token):
        self.args = args
        self.deadline = time.monotonic() + args.timeout
        self.opener = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=args.ca_file)))
        self.token = token

    def request(self, path, body=None):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise Stop("timeout; resume with the same state file")
        request = urllib.request.Request(self.args.api_url + path, data=None if body is None else json.dumps(body).encode(), headers={"Authorization": "Bearer " + self.token, "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=min(remaining, self.args.request_timeout)) as response:
                document = json.load(response)
        except urllib.error.HTTPError as error:
            # Do not echo server bodies or URL/exception strings: they may contain secrets.
            raise Stop("HTTP %d; stopped without retrying submission" % error.code) from None
        except (OSError, ValueError, http.client.HTTPException):
            raise Stop("network/response error; do not resubmit; resume known ID or inspect --history") from None
        if not isinstance(document, dict) or document.get("success") is not True or not isinstance(document.get("data"), dict):
            raise Stop("invalid API envelope; stopped without retrying submission")
        return document["data"]

    def pause(self):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise Stop("timeout; resume with the same state file")
        time.sleep(min(self.args.poll_interval, remaining))


def rollout_identity(rollout, args, operation_id=None):
    if not isinstance(rollout, dict) or type(rollout.get("id")) is not int or rollout["id"] <= 0:
        raise Stop("invalid rollout identity")
    if operation_id is not None and rollout["id"] != operation_id:
        raise Stop("rollout ID mismatch")
    if rollout.get("runtime_type") != args.runtime_type or rollout.get("target_image_ref") != args.target_image:
        raise Stop("rollout runtime/image identity mismatch")
    return rollout["id"]


def preparation_identity(operation, args, operation_id=None):
    if not isinstance(operation, dict) or not isinstance(operation.get("id"), str) or not re.fullmatch(r"[A-Za-z0-9_.:-]{1,200}", operation["id"]):
        raise Stop("invalid preparation identity")
    if operation_id is not None and operation["id"] != operation_id:
        raise Stop("preparation ID mismatch")
    if operation.get("kind") != "rollout_wake" or operation.get("runtime_type") != args.runtime_type:
        raise Stop("preparation kind/runtime identity mismatch")
    return operation["id"]


def preparation(api, args, state, path):
    if not state.get("preparation_id"):
        if state["stage"] != "new":
            raise Stop("submission outcome unknown; inspect --history and recover --preparation-id; never delete state to retry")
        state["stage"] = "preparing"
        save(path, state)
        operation = api.request(ROLLOUTS + "/prepare", {"runtime_type": args.runtime_type}).get("operation")
        state["preparation_id"] = preparation_identity(operation, args)
        save(path, state)
    while True:
        items = api.request(SCHEDULING).get("items", [])
        operation = next((item for item in items if item.get("id") == state["preparation_id"]), None)
        preparation_identity(operation, args, state["preparation_id"])
        status = operation.get("status")
        emit(preparation_id=state["preparation_id"], status=status)
        if status == "completed":
            state["stage"] = "prepared"
            save(path, state)
            return
        if status not in ("pending", "processing", "starting"):
            raise Stop("preparation blocked/failed/cancelled or unknown status; inspect CM history")
        api.pause()


def poll(api, args, state):
    while True:
        rollout = api.request(ROLLOUTS + "/" + str(state["operation_id"])).get("rollout")
        rollout_identity(rollout, args, state["operation_id"])
        status = rollout.get("status")
        emit(operation_id=state["operation_id"], status=status)
        if status == "finished":
            return
        if status not in ("pending", "running"):
            raise Stop("rollout blocked/failed/cancelled or unknown status; inspect CM history")
        api.pause()


def deliver(api, args, state, path):
    if args.operation_id:
        if state.get("operation_id") not in (None, args.operation_id):
            raise Stop("operation ID differs from saved state")
        rollout = api.request(ROLLOUTS + "/" + str(args.operation_id)).get("rollout")
        rollout_identity(rollout, args, args.operation_id)
        state.update(operation_id=args.operation_id, stage="observing")
        save(path, state)
    if state.get("operation_id") and state["stage"] != "preflight_ready":
        poll(api, args, state)
        return
    if args.preparation_id:
        if state.get("preparation_id") not in (None, args.preparation_id):
            raise Stop("preparation ID differs from saved state")
        # Recover only the initial preparation response. Later unknown POST
        # outcomes must retain their stage so this flag cannot replay them.
        if state["stage"] in ("new", "preparing"):
            state.update(preparation_id=args.preparation_id, stage="preparing")
            save(path, state)
    if state["stage"] in ("new", "preparing"):
        preparation(api, args, state, path)
    if state["stage"] == "prepared" and args.runtime_type == "openclaw":
        state["stage"] = "preflighting"
        save(path, state)
        result = api.request(ROLLOUTS + "/preflight", {"target_image_ref": args.target_image, "batch_size": args.batch_size, "max_unavailable": 0, "auto_rollback": True, "preparation_id": state["preparation_id"]})
        if result.get("rollout"):
            state["operation_id"] = rollout_identity(result["rollout"], args)
        state["stage"] = "blocked"
        save(path, state)
        if result.get("passed") is not True or result.get("blockers"):
            emit(operation_id=state.get("operation_id"), status="preflight_blocked")
            raise Stop("OpenClaw preflight blocked; inspect CM history")
        if result.get("target_image_ref") != args.target_image:
            raise Stop("preflight target identity mismatch")
        strategy = result.get("strategy")
        if strategy == "openclaw_8plus_data_safe":
            proof = result.get("rollout", {}).get("preflight_id")
            if not isinstance(proof, str) or not proof or not state.get("operation_id"):
                raise Stop("data-safe preflight lacks durable proof")
            state["preflight_id"] = proof
        elif strategy != "legacy_rolling":
            raise Stop("unknown OpenClaw upgrade strategy")
        emit(strategy=strategy, empty_pool_reset=result.get("empty_pool_reset") is True)
        state["stage"] = "preflight_ready"
        save(path, state)
    if state["stage"] not in ("prepared", "preflight_ready"):
        raise Stop("submission outcome unknown or blocked; inspect --history; never delete state to retry")
    body = {"runtime_type": args.runtime_type, "target_image_ref": args.target_image, "batch_size": args.batch_size, "max_unavailable": 0 if state.get("preflight_id") else 1, "auto_rollback": True, "preparation_id": state["preparation_id"]}
    if state.get("preflight_id"):
        body["preflight_id"] = state["preflight_id"]
    state["stage"] = "submitting"
    save(path, state)
    rollout = api.request(ROLLOUTS, body).get("rollout")
    state["operation_id"] = rollout_identity(rollout, args, state.get("operation_id"))
    state["stage"] = "observing"
    save(path, state)
    emit(operation_id=state["operation_id"], status=rollout.get("status"))
    poll(api, args, state)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api-url", required=True, help="CM API root, e.g. https://cm.example/api/v1")
    parser.add_argument("--runtime-type", required=True, choices=("openclaw", "hermes", "deepseek-harness", "opencode"))
    parser.add_argument("--target-image", help="immutable repository@sha256:<64 lowercase hex>")
    parser.add_argument("--state-file", type=Path, default=Path("runtime-upgrade-state.json"))
    parser.add_argument("--token-file", type=Path, help="otherwise CM_TOKEN or CM_TOKEN_FILE")
    parser.add_argument("--ca-file", help="trusted CA bundle for private CM TLS")
    parser.add_argument("--timeout", type=float, default=1800)
    parser.add_argument("--request-timeout", type=float, default=30)
    parser.add_argument("--poll-interval", type=float, default=3)
    parser.add_argument("--batch-size", type=int, default=1)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--operation-id", type=int, help="observe existing rollout only, never submit")
    mode.add_argument("--preparation-id", help="recover known rollout_wake preparation ID")
    mode.add_argument("--history", action="store_true", help="read-only IDs/statuses for recovery")
    args = parser.parse_args()
    args.api_url = args.api_url.rstrip("/")
    url = urllib.parse.urlsplit(args.api_url)
    if url.username or url.password or url.query or url.fragment or not url.hostname or (url.scheme != "https" and not (url.scheme == "http" and url.hostname in ("127.0.0.1", "::1", "localhost"))):
        raise Stop("use an HTTPS API URL without credentials/query/fragment; HTTP allowed only on loopback")
    if args.runtime_type == "opencode" and not args.history and not args.operation_id:
        raise Stop("OpenCode safe upgrade is not supported by CM; refusing unsafe rolling upgrade")
    if not args.history and (not args.target_image or not re.fullmatch(r"[^\s@]+@sha256:[0-9a-f]{64}", args.target_image)):
        raise Stop("target image must use an immutable repository@sha256 digest")
    if any(not (0 < value < float("inf")) for value in (args.timeout, args.request_timeout, args.poll_interval)) or args.batch_size <= 0 or (args.operation_id is not None and args.operation_id <= 0):
        raise Stop("timeouts, interval, batch size and operation ID must be positive")
    token_file = args.token_file or os.environ.get("CM_TOKEN_FILE")
    token = Path(token_file).read_text().strip() if token_file else os.environ.get("CM_TOKEN", "").strip()
    if not token or any(character.isspace() for character in token):
        raise Stop("provide a valid admin token via CM_TOKEN, CM_TOKEN_FILE or --token-file")
    api = API(args, token)
    if args.history:
        for item in api.request(ROLLOUTS + "?" + urllib.parse.urlencode({"runtime_type": args.runtime_type})).get("items", []):
            emit(operation_id=item.get("id"), status=item.get("status"))
        for item in api.request(SCHEDULING).get("items", []):
            if item.get("kind") == "rollout_wake" and item.get("runtime_type") == args.runtime_type:
                emit(preparation_id=item.get("id"), status=item.get("status"))
        return
    path = args.state_file.absolute()
    with open(str(path) + ".lock", "a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise Stop("another process owns this state file") from None
        identity = {"api_url": args.api_url, "runtime_type": args.runtime_type, "target_image": args.target_image, "batch_size": args.batch_size}
        if path.exists():
            state = json.loads(path.read_text())
            if not isinstance(state, dict) or any(state.get(key) != value for key, value in identity.items()):
                raise Stop("saved state identity/options mismatch")
        else:
            state = dict(identity, stage="new")
            save(path, state)
        deliver(api, args, state, path)


if __name__ == "__main__":
    try:
        main()
    except Stop as error:
        print("error: " + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        print("error: invalid local state, credentials, TLS configuration or API response; stopped", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("error: interrupted; resume with the same state file", file=sys.stderr)
        sys.exit(130)
