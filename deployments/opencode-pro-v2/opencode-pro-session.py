#!/usr/bin/env python3
"""Resolve a persistent Pro project's native OpenCode v2 browser entry."""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import subprocess
from http.server import BaseHTTPRequestHandler, HTTPServer


class SessionEntry:
    def __init__(self, origin, project, state_dir, authorization=None):
        self.origin = origin
        self.project = str(Path(project).resolve())
        self.state_dir = Path(state_dir)
        self.authorization = authorization
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def api(self, path, body=None):
        headers = {"Accept": "application/json"}
        if self.authorization:
            headers["Authorization"] = self.authorization
        if body is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.origin + path, headers=headers,
                                         data=None if body is None else json.dumps(body).encode())
        with self.http.open(request, timeout=5) as response:
            if response.status != 200 or "application/json" not in response.headers.get("Content-Type", ""):
                raise ValueError("OpenCode returned an invalid API response")
            return json.load(response)

    def valid(self, session):
        directory = session.get("location", {}).get("directory", "")
        return (bool(re.fullmatch(r"ses_[A-Za-z0-9_-]+", session.get("id", "")))
                and os.path.isabs(directory) and str(Path(directory).resolve()) == self.project
                and not session.get("parentID"))

    def active(self, session):
        return self.valid(session) and not session.get("time", {}).get("archived")

    def initial_id(self):
        self.state_dir.mkdir(parents=True, exist_ok=True)
        seed_file = self.state_dir / "pro-session-seed"
        if not seed_file.exists():
            temporary = self.state_dir / (".pro-seed-" + uuid.uuid4().hex)
            try:
                with temporary.open("x") as stream:
                    stream.write(uuid.uuid4().hex)
                temporary.chmod(0o600)
                try:
                    # Publish complete contents atomically, without replacing a seed.
                    os.link(str(temporary), str(seed_file))
                except FileExistsError:
                    pass
            finally:
                temporary.unlink()
        seed = seed_file.read_text().strip()
        if not re.fullmatch(r"[a-f0-9]{32}", seed):
            raise ValueError("Invalid persistent Pro session seed")
        return "ses_" + hashlib.sha256((seed + "\n" + self.project).encode()).hexdigest()[:32]

    def resolve(self):
        session_id = self.initial_id()
        try:
            saved = self.api("/api/session/" + session_id)["data"]
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise
            saved = None
        if saved is not None and not self.valid(saved):
            raise ValueError("Saved Pro session belongs to a different project")

        # Prefer the most recently used conversation of the selected project.
        # Never pull conversations from another project or revive archived ones.
        sessions = [saved] if saved is not None and self.active(saved) else []
        cursor, seen = "", set()
        while True:
            page = self.api("/api/session?" + urllib.parse.urlencode({"limit": 100, **({"cursor": cursor} if cursor else {})}))
            sessions.extend(session for session in page["data"] if self.active(session))
            cursor = (page.get("cursor") or {}).get("next")
            if not cursor:
                break
            if cursor in seen:
                raise ValueError("OpenCode returned a repeated session cursor")
            seen.add(cursor)
        if sessions:
            return max(sessions, key=lambda session: session.get("time", {}).get("updated", 0))["id"]
        if saved is not None:
            # Archiving the starter is an explicit user action. Leave it archived.
            return None
        payload = {"id": session_id, "title": "开始使用", "location": {"directory": self.project},
                   "metadata": {"clawmanager.pro.bootstrap": session_id[4:]}}
        try:
            created = self.api("/api/session", payload)["data"]
        except (urllib.error.URLError, TimeoutError, ValueError):
            # Recover a committed create whose response was lost, without a new ID.
            created = self.api("/api/session/" + session_id)["data"]
        if created.get("id") != session_id or not self.active(created):
            raise ValueError("Invalid Pro starter session response")
        return session_id

    def url(self):
        key = base64.urlsafe_b64encode(self.origin.encode()).decode().rstrip("=")
        session_id = self.resolve()
        return self.origin + "/server/" + key + ("/session/" + session_id if session_id else "")

    def browser_session(self):
        url = self.url()
        # Use native, revocable browser credentials instead of putting the
        # instance's gateway key into Chromium's command line or browser history.
        code = self.api("/api/pair", {})["code"]
        token = self.api("/auth/connect/" + urllib.parse.quote(code, safe=""))["token"]
        return {"url": url, "token": token}


def browser_bootstrap(session):
    """Deliver the native HttpOnly cookie once, then redirect to the project.

    V2's auth_token URL alone does not persist authentication across refreshes.
    Cookies are host scoped, so a one-use loopback listener can deliver the same
    signed cookie as native /auth/connect, while retaining the project route.
    No credential is included in the browser URL or process arguments.
    """
    target = urllib.parse.urlsplit(session["url"])
    if target.scheme != "http" or target.hostname != "127.0.0.1" or not target.port or target.query or target.fragment:
        raise ValueError("Invalid Pro browser target")
    token = session["token"]
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", token):
        raise ValueError("Invalid native browser session")
    nonce = "/" + uuid.uuid4().hex

    class Handler(BaseHTTPRequestHandler):
        timeout = 5

        def log_message(self, *_):
            pass

        def do_GET(self):
            if self.path != nonce or self.headers.get("Host") != "127.0.0.1:" + str(self.server.server_port):
                self.send_error(404)
                return
            self.send_response(302)
            self.send_header("Location", session["url"])
            self.send_header("Set-Cookie", "opencode_session_" + str(target.port) + "=" + token + "; Path=/; HttpOnly; SameSite=Lax; Max-Age=2592000")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Referrer-Policy", "no-referrer")
            self.send_header("Content-Length", "0")
            self.end_headers()
            self.server.completed = True

    server = HTTPServer(("127.0.0.1", 0), Handler)
    server.completed = False
    server.timeout = 1
    return server, "http://127.0.0.1:" + str(server.server_port) + nonce


def launch_browser_bootstrap(session):
    process = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "--browser-bootstrap"],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                               start_new_session=True, close_fds=True, text=True)
    process.stdin.write(json.dumps(session))
    process.stdin.close()
    url = process.stdout.readline().strip()
    process.stdout.close()
    if not re.fullmatch(r"http://127\.0\.0\.1:[0-9]+/[a-f0-9]{32}", url):
        raise RuntimeError("Could not start native browser authentication")
    return url


def main():
    port = int(os.environ.get("OPENCODE_WEB_PORT", "4096"))
    if not 1 <= port <= 65535:
        raise ValueError("Invalid OpenCode web port")
    workspace = Path(os.environ.get("CLAWMANAGER_WORKSPACE_PATH", os.environ.get("CLAWMANAGER_PROJECT_PATH", "/config/workspace"))).resolve()
    project = Path(os.environ.get("CLAWMANAGER_DEFAULT_PROJECT_PATH", str(workspace / "project"))).resolve()
    if workspace == Path("/") or (project != workspace and workspace not in project.parents):
        raise ValueError("OpenCode project must stay within its workspace")
    origin = "http://127.0.0.1:" + str(port)
    password = os.environ.get("OPENCODE_PASSWORD") or os.environ.get("OPENCODE_SERVER_PASSWORD", "")
    if not password:
        raise ValueError("OpenCode Pro v2 requires configured server credentials")
    # V2's native server fixes the username to opencode.
    authorization = "Basic " + base64.b64encode(("opencode:" + password).encode()).decode()
    entry = SessionEntry(origin, project, os.environ.get("OPENCODE_CONFIG_DIR", "/config/.opencode"), authorization)
    # Wait for the actual JSON API, not the SPA fallback or the desktop port.
    for attempt in range(90):
        try:
            info = entry.api("/api/info")
            if not re.fullmatch(r"(?:opencode v)?2\..+", str(info.get("version", ""))):
                raise ValueError("OpenCode Pro requires a v2 server")
            break
        except (urllib.error.URLError, TimeoutError, ValueError):
            if attempt == 89:
                raise RuntimeError("OpenCode Pro v2 API did not become ready") from None
            time.sleep(1)
    print(launch_browser_bootstrap(entry.browser_session()))


if __name__ == "__main__":
    try:
        if sys.argv[1:] == ["--browser-bootstrap"]:
            session = json.loads(sys.stdin.read(65536))
            server, url = browser_bootstrap(session)
            print(url, flush=True)
            deadline = time.monotonic() + 120
            try:
                while not server.completed and time.monotonic() < deadline:
                    server.handle_request()
            finally:
                server.server_close()
        else:
            main()
    except Exception as error:
        # Neither provider credentials nor HTTP response bodies belong in logs.
        print("opencode-pro-session: " + type(error).__name__ + ": initialization failed", file=sys.stderr)
        sys.exit(1)
