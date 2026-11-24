#!/usr/bin/env python3
"""Move native Hermes Lite conversations between isolated workspace mounts.

Run with the source or target Hermes installation's Python environment. The
caller binds workspace mounts to SQL owner/instance records and transports the
capture stream inside the trial's authenticated encryption.
"""

import argparse
import contextlib
import errno
import hashlib
from http.server import BaseHTTPRequestHandler
import json
import os
from pathlib import Path
import re
import socket
import socketserver
import sqlite3
import stat
import sys
import tempfile
import time
from urllib.parse import parse_qs


FORMAT = "clawmanager-trial-hermes-history.v1"
MAX_PAYLOAD = 24 * 1024 * 1024
MAX_SNAPSHOT = 1024 * 1024 * 1024
MAX_SESSION_ID = 256
MAX_SESSION_SELECTION = 64
INVENTORY_PAGE = 100
SESSION_FIELDS = (
    "id", "source", "title", "model", "parent_session_id", "started_at",
    "ended_at", "end_reason", "archived",
)
MESSAGE_FIELDS = (
    "role", "content", "timestamp", "tool_calls", "tool_call_id", "tool_name",
    "finish_reason", "reasoning", "reasoning_content", "reasoning_details",
    "codex_reasoning_items", "codex_message_items", "display_kind",
)
STATE_FILES = ("state.db", "state.db-wal", "state.db-shm", "state.db-journal")


class HistoryError(Exception):
    pass


def state_directory(root, user_id, instance_id):
    root = root.absolute()
    target = root / "hermes" / f"user-{user_id}" / f"instance-{instance_id}" / "home" / ".hermes"
    if target.resolve(strict=True) != target or not target.is_dir():
        raise HistoryError("workspace directory does not match the instance layout")
    return target


def check_state_files(directory, *, empty):
    present = set()
    for name in STATE_FILES:
        try:
            info = (directory / name).lstat()
        except FileNotFoundError:
            continue
        if empty or not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise HistoryError("unsafe or existing Hermes state file")
        present.add(name)
    if not empty and "state.db" not in present:
        raise HistoryError("Hermes state database is missing")


def freeze_sqlite(source, target):
    deadline = time.monotonic() + 60

    def progress(_status, _remaining, total_pages):
        if time.monotonic() > deadline or total_pages * page_size > MAX_SNAPSHOT:
            raise HistoryError("Hermes state snapshot exceeded its limits")

    with contextlib.closing(sqlite3.connect(source.as_uri() + "?mode=ro", uri=True)) as reader:
        page_size = reader.execute("PRAGMA page_size").fetchone()[0]
        pages = reader.execute("PRAGMA page_count").fetchone()[0]
        if pages * page_size > MAX_SNAPSHOT:
            raise HistoryError("Hermes state snapshot exceeded its limits")
        with contextlib.closing(sqlite3.connect(target)) as copy:
            reader.backup(copy, pages=128, progress=progress)


def encode(value):
    return json.dumps(value, ensure_ascii=False, allow_nan=False, sort_keys=True,
                      separators=(",", ":")).encode("utf-8")


def project(session):
    metadata = {name: session.get(name) for name in SESSION_FIELDS}
    metadata["archived"] = bool(metadata["archived"])
    metadata["messages"] = [
        {name: message.get(name) for name in MESSAGE_FIELDS}
        for message in session["messages"]
    ]
    return metadata


def capture(session_db, workspace, scratch, session_ids):
    check_state_files(workspace, empty=False)
    with tempfile.TemporaryDirectory(prefix="hermes-history-", dir=scratch) as temporary:
        snapshot = Path(temporary) / "snapshot.db"
        freeze_sqlite(workspace / "state.db", snapshot)
        with contextlib.closing(session_db(db_path=snapshot, read_only=True)) as database:
            rows = {}
            captured_bytes = 0
            for requested in session_ids:
                lineage = database.get_compression_lineage(requested)
                if requested not in lineage:
                    raise HistoryError("requested session is missing from the snapshot")
                for identifier in lineage:
                    while identifier and identifier not in rows:
                        original = database.export_session(identifier)
                        if original is None:
                            raise HistoryError("session parent is missing from the snapshot")
                        row = project(original)
                        captured_bytes += len(encode(row)) + 1
                        if captured_bytes > MAX_PAYLOAD:
                            raise HistoryError("history payload exceeds the trial size limit")
                        rows[identifier] = row
                        identifier = row["parent_session_id"]
            return [rows[identifier] for identifier in sorted(rows)]


def valid_session_id(identifier):
    return (isinstance(identifier, str) and 0 < len(identifier.encode("utf-8")) <= MAX_SESSION_ID
            and identifier == identifier.strip() and not any(ord(ch) < 32 or ord(ch) == 127 for ch in identifier))


def source_inventory(workspace, after):
    check_state_files(workspace, empty=False)
    uri = (workspace / "state.db").as_uri() + "?mode=ro"
    with contextlib.closing(sqlite3.connect(uri, uri=True, timeout=5)) as database:
        rows = database.execute(
            "SELECT id, substr(title,1,160) FROM sessions "
            "WHERE id > ? AND length(CAST(id AS BLOB)) <= ? ORDER BY id LIMIT ?",
            (after, MAX_SESSION_ID, INVENTORY_PAGE + 1),
        ).fetchall()
    if any(not valid_session_id(identifier) for identifier, _title in rows):
        raise HistoryError("history inventory contains an invalid session ID")
    return {
        "sessions": [{"id": identifier, "title": title or ""} for identifier, title in rows[:INVENTORY_PAGE]],
        "next": rows[INVENTORY_PAGE - 1][0] if len(rows) > INVENTORY_PAGE else "",
    }


def capture_payload(session_db, workspace, scratch, user_id, instance_id, ids):
    payload = {"format": FORMAT, "user_id": user_id, "instance_id": instance_id,
               "sessions": capture(session_db, workspace, scratch, ids)}
    sessions = verify_payload(payload, user_id, instance_id)
    payload["transcript_sha256"] = receipt(sessions)["transcript_sha256"]
    encoded = encode(payload)
    if len(encoded) > MAX_PAYLOAD:
        raise HistoryError("history payload exceeds the trial size limit")
    return encoded


def verify_payload(payload, user_id, instance_id):
    if (not isinstance(payload, dict) or payload.get("format") != FORMAT or
            payload.get("user_id") != user_id or payload.get("instance_id") != instance_id):
        raise HistoryError("history does not match the target instance")
    sessions = payload.get("sessions")
    if not isinstance(sessions, list) or not sessions:
        raise HistoryError("history payload has no sessions")
    seen = {}
    for row in sessions:
        if not isinstance(row, dict) or row.keys() != set(SESSION_FIELDS) | {"messages"}:
            raise HistoryError("invalid session row")
        identifier = row["id"]
        if not isinstance(identifier, str) or not identifier or identifier != identifier.strip() or identifier in seen:
            raise HistoryError("invalid or duplicate session ID")
        parent = row["parent_session_id"]
        if parent is not None and (not isinstance(parent, str) or not parent):
            raise HistoryError("invalid session parent")
        if not isinstance(row["messages"], list) or any(
                not isinstance(message, dict) or message.keys() != set(MESSAGE_FIELDS) or
                not isinstance(message["role"], str) or not message["role"]
                for message in row["messages"]):
            raise HistoryError("invalid message row")
        seen[identifier] = parent
    complete = set()
    for identifier in seen:
        lineage = set()
        while identifier is not None and identifier not in complete:
            if identifier in lineage or identifier not in seen:
                raise HistoryError("invalid or incomplete session lineage")
            lineage.add(identifier)
            identifier = seen[identifier]
        complete.update(lineage)
    return sorted(sessions, key=lambda row: row["id"])


def transcript(sessions):
    # Local SQLite message row IDs are allocated anew by Hermes during import.
    return [[row["id"], row["title"], row["parent_session_id"],
             [[message["role"], message["content"], message["timestamp"]]
              for message in row["messages"]]] for row in sessions]


def receipt(sessions):
    return {"sessions": len(sessions), "messages": sum(len(row["messages"]) for row in sessions),
            "transcript_sha256": hashlib.sha256(encode(transcript(sessions))).hexdigest()}


def readback(session_db, candidate, sessions):
    with contextlib.closing(sqlite3.connect(candidate.as_uri() + "?mode=ro", uri=True)) as database:
        identifiers = [row[0] for row in database.execute("SELECT id FROM sessions ORDER BY id")]
    if identifiers != [session["id"] for session in sessions]:
        raise HistoryError("restored session inventory differs")
    with contextlib.closing(session_db(db_path=candidate, read_only=True)) as reader:
        restored = [reader.export_session(session["id"]) for session in sessions]
    if any(row is None for row in restored) or receipt(list(map(project, restored))) != receipt(sessions):
        raise HistoryError("independent transcript readback differs")
    return receipt(sessions)


def restore(session_db, workspace, sessions, *, publish=True):
    check_state_files(workspace, empty=True)
    with tempfile.TemporaryDirectory(prefix=".trial-history-", dir=workspace) as temporary:
        candidate = Path(temporary) / "state.db"
        with contextlib.closing(session_db(db_path=candidate)) as database:
            result = database.import_sessions(sessions)
            if (not result.get("ok") or result.get("imported") != len(sessions) or
                    result.get("skipped") or result.get("detached")):
                raise HistoryError("Hermes did not import the complete history")
        with contextlib.closing(sqlite3.connect(candidate)) as mode:
            if mode.execute("PRAGMA journal_mode=DELETE").fetchone()[0] != "delete":
                raise HistoryError("restored history still requires the WAL")
        verified = readback(session_db, candidate, sessions)
        check_state_files(workspace, empty=True)
        owner = workspace.stat()
        os.chmod(candidate, 0o600)
        with candidate.open("r+b") as content:
            current = os.fstat(content.fileno())
            if (current.st_uid, current.st_gid) != (owner.st_uid, owner.st_gid):
                os.fchown(content.fileno(), owner.st_uid, owner.st_gid)
            os.fsync(content.fileno())
        # The staging directory and target share a filesystem. Link publishes
        # the completed database atomically and rejects a concurrent creator.
        destination = workspace / "state.db" if publish else Path(temporary) / "publication-probe"
        os.link(candidate, destination)
    return verified


def apply_history(session_db, workspace, operation, raw, user_id, instance_id):
    if len(raw) > MAX_PAYLOAD:
        raise HistoryError("history payload exceeds the trial size limit")
    payload = json.loads(raw)
    sessions = verify_payload(payload, user_id, instance_id)
    if payload.get("transcript_sha256") != receipt(sessions)["transcript_sha256"]:
        raise HistoryError("history transcript digest differs")
    if operation == "verify" or (operation == "preflight" and os.path.lexists(workspace / "state.db")):
        check_state_files(workspace, empty=False)
        return readback(session_db, workspace / "state.db", sessions)
    return restore(session_db, workspace, sessions, publish=operation != "preflight")


class HistoryRequest(BaseHTTPRequestHandler):
    def log_message(self, _format, *args):
        pass

    def reply(self, status, value):
        self.reply_raw(status, encode(value))

    def reply_raw(self, status, body):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if (self.path == "/healthz" and
                self.headers.get("X-Trial-Workspace-Root") == str(self.server.workspace_root)):
            self.reply(200, {"ready": True})
        else:
            self.reply(404, {"error": "unknown history route"})

    def do_POST(self):
        route = re.fullmatch(r"/(restore|verify|preflight)/([1-9][0-9]{0,18})/([1-9][0-9]{0,18})", self.path)
        length = self.headers.get_all("Content-Length")
        root = self.server.workspace_root
        if (route is None or not length or len(length) != 1 or not re.fullmatch(r"[0-9]{1,8}", length[0]) or
                "Transfer-Encoding" in self.headers or self.headers.get("Content-Type") != "application/json" or
                self.headers.get("X-Trial-Workspace-Root") != str(root)):
            self.reply(400, {"error": "invalid history request"})
            return
        size = int(length[0])
        if size < 1 or size > MAX_PAYLOAD:
            self.reply(413, {"error": "history payload outside size limits"})
            return
        try:
            raw = self.rfile.read(size)
            if len(raw) != size:
                raise HistoryError("incomplete history payload")
            operation, owner, instance = route.groups()
            user_id, instance_id = int(owner), int(instance)
            workspace = state_directory(root, user_id, instance_id)
            result = apply_history(self.server.session_db, workspace, operation, raw, user_id, instance_id)
        except Exception:
            # Exceptions from SQLite and Hermes may contain private content.
            self.reply(422, {"error": "Hermes history could not be verified or restored"})
            return
        self.reply(200, result)


class HistorySourceRequest(HistoryRequest):
    def authorized_path(self, operation):
        path = f"/{operation}/{self.server.source_user}/{self.server.source_instance}"
        return (self.path.partition("?")[0] == path and
                self.headers.get_all("X-Trial-Workspace-Root") == [str(self.server.workspace_root)])

    def workspace(self):
        return state_directory(self.server.workspace_root, self.server.source_user, self.server.source_instance)

    def do_GET(self):
        if not self.authorized_path("inventory"):
            self.reply(404, {"error": "unknown history source route"})
            return
        try:
            query = parse_qs(self.path.partition("?")[2], keep_blank_values=True, strict_parsing=True,
                             max_num_fields=1, errors="strict")
            if query and (set(query) != {"after"} or not valid_session_id(query["after"][0])):
                raise ValueError("invalid cursor")
        except ValueError:
            self.reply(400, {"error": "invalid history source cursor"})
            return
        try:
            result = source_inventory(self.workspace(), query.get("after", [""])[0])
        except Exception:
            self.reply(422, {"error": "Hermes source inventory is unavailable"})
            return
        self.reply(200, result)

    def do_POST(self):
        if not self.authorized_path("capture") or "?" in self.path:
            self.reply(404, {"error": "unknown history source route"})
            return
        length = self.headers.get_all("Content-Length")
        if (not length or len(length) != 1 or not re.fullmatch(r"[0-9]{1,5}", length[0]) or
                "Transfer-Encoding" in self.headers or self.headers.get("Content-Type") != "application/json" or
                not 0 < int(length[0]) <= 65536):
            self.reply(400, {"error": "invalid history capture request"})
            return
        try:
            size = int(length[0])
            raw = self.rfile.read(size)
            ids = json.loads(raw)
            if (len(raw) != size or not isinstance(ids, list) or not 0 < len(ids) <= MAX_SESSION_SELECTION or
                    any(not valid_session_id(identifier) for identifier in ids) or len(set(ids)) != len(ids)):
                raise HistoryError("invalid history session selection")
            payload = capture_payload(self.server.session_db, self.workspace(), self.server.scratch,
                                      self.server.source_user, self.server.source_instance, ids)
        except Exception:
            self.reply(422, {"error": "Hermes source sessions could not be captured"})
            return
        self.reply_raw(200, payload)


def reclaim_socket(path):
    try:
        existing = path.lstat()
    except FileNotFoundError:
        return
    if not stat.S_ISSOCK(existing.st_mode):
        raise HistoryError("history socket path is already occupied")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(1)
        try:
            connection.connect(str(path))
        except OSError as error:
            if error.errno != errno.ECONNREFUSED or not os.path.samestat(existing, path.lstat()):
                raise HistoryError("history socket cannot be reclaimed") from None
        else:
            raise HistoryError("history adapter is already listening")
    path.unlink()


def canonical_socket_path(path):
    if not path.is_absolute():
        raise HistoryError("invalid history adapter socket mount")
    # Kubernetes manifests commonly use /var/run. Some Hermes images alias
    # that directory to /run. Resolve only this platform alias so symlinks
    # inside the shared socket mount are still rejected.
    alias, destination = Path("/var/run"), Path("/run")
    if path.is_relative_to(alias) and alias.is_symlink() and alias.resolve(strict=True) == destination:
        path = destination / path.relative_to(alias)
    if path.parent.resolve(strict=True) != path.parent:
        raise HistoryError("invalid history adapter socket mount")
    return path


class HistoryServer(socketserver.TCPServer):
    address_family = getattr(socket, "AF_UNIX", 0)

    def get_request(self):
        connection, address = super().get_request()
        connection.settimeout(10)
        return connection, address

    def handle_error(self, _request, _client_address):
        # A caller may disconnect before SQLite finishes the import.
        pass


def serve(session_db, root, socket_path, *, source=None):
    if not hasattr(socket, "AF_UNIX"):
        raise HistoryError("Unix domain sockets are unavailable")
    methods = ("export_session", "get_compression_lineage") if source else ("export_session", "import_sessions")
    if not all(callable(getattr(session_db, name, None)) for name in methods):
        raise HistoryError("Hermes native session portability is unavailable")
    if not root.is_absolute() or root.resolve(strict=True) != root or not root.is_dir() or root == root.parent:
        raise HistoryError("invalid history adapter mounts")
    socket_path = canonical_socket_path(socket_path)
    if source:
        user_id, instance_id, scratch = source
        if user_id <= 0 or instance_id <= 0 or not scratch.is_absolute() or scratch.resolve(strict=True) != scratch or not scratch.is_dir():
            raise HistoryError("invalid source adapter binding or snapshot directory")
    reclaim_socket(socket_path)
    mask = os.umask(0o007)
    try:
        server = HistoryServer(str(socket_path), HistorySourceRequest if source else HistoryRequest)
    finally:
        os.umask(mask)
    with server:
        identity = socket_path.lstat()
        os.chmod(socket_path, 0o660)
        server.workspace_root = root
        server.session_db = session_db
        if source:
            server.source_user, server.source_instance, server.scratch = source
        try:
            server.serve_forever()
        finally:
            if os.path.samestat(identity, socket_path.lstat()):
                socket_path.unlink()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--workspace-root", type=Path, required=True)
    common.add_argument("--user-id", type=int, required=True)
    common.add_argument("--instance-id", type=int, required=True)
    modes = parser.add_subparsers(dest="operation", required=True)
    source = modes.add_parser("capture", parents=[common])
    source.add_argument("--snapshot-dir", type=Path, required=True)
    source.add_argument("--session-id", action="append", required=True)
    modes.add_parser("restore", parents=[common])
    modes.add_parser("verify", parents=[common])
    adapter = modes.add_parser("serve")
    adapter.add_argument("--workspace-root", type=Path, required=True)
    adapter.add_argument("--socket", type=Path, required=True)
    source_adapter = modes.add_parser("serve-source", parents=[common])
    source_adapter.add_argument("--socket", type=Path, required=True)
    source_adapter.add_argument("--snapshot-dir", type=Path, required=True)
    args = parser.parse_args()
    try:
        from hermes_state import SessionDB

        if args.operation == "serve":
            serve(SessionDB, args.workspace_root, args.socket)
            return 0
        if args.operation == "serve-source":
            serve(SessionDB, args.workspace_root, args.socket, source=(args.user_id, args.instance_id, args.snapshot_dir))
            return 0
        if args.user_id <= 0 or args.instance_id <= 0:
            raise HistoryError("invalid workspace identity")
        workspace = state_directory(args.workspace_root, args.user_id, args.instance_id)
        if args.operation == "capture":
            sys.stdout.buffer.write(capture_payload(SessionDB, workspace, args.snapshot_dir,
                                                    args.user_id, args.instance_id, args.session_id))
        else:
            raw = sys.stdin.buffer.read(MAX_PAYLOAD + 1)
            result = apply_history(SessionDB, workspace, args.operation, raw, args.user_id, args.instance_id)
            print(json.dumps(result, sort_keys=True))
        return 0
    except HistoryError as error:
        print(str(error), file=sys.stderr)
        return 1
    except Exception:
        # Runtime exceptions can include session IDs, paths or message text.
        print("Hermes history operation failed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
