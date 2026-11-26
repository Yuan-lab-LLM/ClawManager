import importlib.util
import json
import os
from pathlib import Path
import socket
import sqlite3
import tempfile
import threading
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "trial_hermes_history", Path(__file__).with_name("system-backup-trial-hermes-history.py")
)
history = importlib.util.module_from_spec(spec)
spec.loader.exec_module(history)


def socket_request(address, workspace, method, route, body=b""):
    headers = (f"{method} {route} HTTP/1.0\r\nHost: hermes-history\r\n"
               f"X-Trial-Workspace-Root: {workspace}\r\nContent-Type: application/json\r\n"
               f"Content-Length: {len(body)}\r\n\r\n").encode()
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(3)
        connection.connect(address)
        connection.sendall(headers + body)
        with connection.makefile("rb") as response:
            status = int(response.readline().split()[1])
            for header in response:
                if header == b"\r\n":
                    break
            return status, json.loads(response.read())


class SQLiteFixture:
    """Small stand-in for the Hermes portable API, backed by real SQLite."""

    def __init__(self, *, db_path, read_only=False):
        uri = db_path.as_uri() + "?mode=ro" if read_only else db_path.as_uri()
        self.connection = sqlite3.connect(uri, uri=True)
        self.connection.row_factory = sqlite3.Row
        if not read_only:
            self.connection.executescript(
                "PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0;"
                "CREATE TABLE sessions(id TEXT PRIMARY KEY, title TEXT, parent_session_id TEXT);"
                "CREATE TABLE messages(id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, "
                "content TEXT, timestamp REAL);"
            )

    def close(self):
        self.connection.close()

    def get_compression_lineage(self, identifier):
        return [identifier] if self.export_session(identifier) is not None else []

    def export_session(self, identifier):
        session = self.connection.execute("SELECT * FROM sessions WHERE id=?", (identifier,)).fetchone()
        if session is None:
            return None
        messages = self.connection.execute(
            "SELECT role, content, timestamp FROM messages WHERE session_id=? ORDER BY id", (identifier,)
        )
        return dict(session, messages=[dict(row) for row in messages])

    def import_sessions(self, sessions):
        with self.connection:
            for row in sessions:
                self.connection.execute("INSERT INTO sessions VALUES(?,?,?)",
                                        (row["id"], row["title"], row["parent_session_id"]))
                self.connection.executemany(
                    "INSERT INTO messages(session_id, role, content, timestamp) VALUES(?,?,?,?)",
                    [(row["id"], message["role"], message["content"], message["timestamp"])
                     for message in row["messages"]],
                )
        return {"ok": True, "imported": len(sessions), "skipped": 0, "detached": 0}


class TrialHermesHistoryTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="trial-hermes-")
        self.addCleanup(temporary.cleanup)
        self.base = Path(temporary.name)
        self.source = self.workspace("source")
        self.native = SQLiteFixture(db_path=self.source / "state.db")
        self.addCleanup(self.native.close)
        self.native.connection.executescript(
            "PRAGMA wal_checkpoint(TRUNCATE);"
            "BEGIN; INSERT INTO sessions VALUES('parent','Original title',NULL),"
            "('child',NULL,'parent'),('unrelated',NULL,NULL);"
            "INSERT INTO messages VALUES(1,'parent','user','original question',1),"
            "(2,'child','assistant','original reply',2); COMMIT;"
        )

    def workspace(self, name):
        root = self.base / name
        (root / "hermes/user-7/instance-13/home/.hermes").mkdir(parents=True)
        return history.state_directory(root, 7, 13)

    def capture(self):
        rows = history.capture(SQLiteFixture, self.source, self.base, ["child"])
        return {"format": history.FORMAT, "user_id": 7, "instance_id": 13, "sessions": rows}

    def test_wal_contents_and_parent_replayed_to_two_empty_targets(self):
        self.assertGreater((self.source / "state.db-wal").stat().st_size, 0)
        sessions = history.verify_payload(self.capture(), 7, 13)
        self.assertEqual([row["id"] for row in sessions], ["child", "parent"])
        first = history.restore(SQLiteFixture, self.workspace("first"), sessions)
        second = history.restore(SQLiteFixture, self.workspace("second"), sessions)
        self.assertEqual(first, second)
        self.assertEqual((first["sessions"], first["messages"]), (2, 2))

    def test_rejects_wrong_instance_and_cyclic_history(self):
        payload = self.capture()
        with self.assertRaises(history.HistoryError):
            history.verify_payload(payload, 8, 13)
        payload["sessions"][1]["parent_session_id"] = "child"
        with self.assertRaises(history.HistoryError):
            history.verify_payload(payload, 7, 13)

    @unittest.skipUnless(os.name == "posix", "requires POSIX filesystem symlinks")
    def test_socket_mount_rejects_unregistered_symlink_parents(self):
        mount = self.base / "socket-dir"
        mount.mkdir()
        alias = self.base / "socket-alias"
        alias.symlink_to(mount, target_is_directory=True)
        self.assertEqual(history.canonical_socket_path(mount / "history.sock"), mount / "history.sock")
        with self.assertRaises(history.HistoryError):
            history.canonical_socket_path(alias / "history.sock")

    def test_refuses_existing_state(self):
        target = self.workspace("existing")
        existing = target / "state.db"
        existing.write_bytes(b"existing")
        with self.assertRaises(history.HistoryError):
            history.restore(SQLiteFixture, target, self.capture()["sessions"])
        self.assertEqual(existing.read_bytes(), b"existing")

    @unittest.skipUnless(hasattr(os, "geteuid") and os.geteuid() == 0, "requires root on Linux")
    def test_publishes_state_for_the_destination_workspace_owner(self):
        target = self.workspace("workspace-owner")
        os.chown(target, 200013, 200014)

        history.restore(SQLiteFixture, target, self.capture()["sessions"])

        state = (target / "state.db").stat()
        self.assertEqual((state.st_uid, state.st_gid, state.st_mode & 0o777), (200013, 200014, 0o600))

    def test_mismatched_readback_is_not_published(self):
        class BrokenImport(SQLiteFixture):
            def import_sessions(self, sessions):
                receipt = super().import_sessions(sessions)
                with self.connection:
                    self.connection.execute("UPDATE messages SET content='different'")
                return receipt

        target = self.workspace("mismatch")
        with self.assertRaises(history.HistoryError):
            history.restore(BrokenImport, target, self.capture()["sessions"])
        self.assertFalse((target / "state.db").exists())

    @unittest.skipUnless(hasattr(socket, "AF_UNIX"), "requires Unix domain sockets")
    def test_local_adapter_transports_only_matching_instance(self):
        target = self.workspace("socket")
        root = self.base / "socket"
        address = str(self.base / "history.sock")
        payload = self.capture()
        payload["transcript_sha256"] = history.receipt(payload["sessions"])["transcript_sha256"]
        encoded = history.encode(payload)

        def send(method, path, *, workspace=root, body=b""):
            return socket_request(address, workspace, method, path, body)

        with history.HistoryServer(address, history.HistoryRequest) as server:
            server.workspace_root = root
            server.session_db = SQLiteFixture
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                self.assertEqual(send("GET", "/healthz"), (200, {"ready": True}))
                self.assertEqual(send("POST", "/restore/7/13", workspace=self.base, body=encoded)[0], 400)
                self.assertEqual(send("POST", "/restore/7/14", body=encoded)[0], 422)
                self.assertFalse((target / "state.db").exists())
                expected = history.receipt(payload["sessions"])
                self.assertEqual(send("POST", "/preflight/7/13", body=encoded), (200, expected))
                self.assertEqual(list(target.iterdir()), [])
                self.assertEqual(send("POST", "/restore/7/13", body=encoded), (200, expected))
                self.assertEqual(send("POST", "/restore/7/13", body=encoded)[0], 422)
                self.assertEqual(send("POST", "/verify/7/13", body=encoded), (200, expected))
                before = (target / "state.db").read_bytes()
                self.assertEqual(send("POST", "/preflight/7/13", body=encoded), (200, expected))
                self.assertEqual((target / "state.db").read_bytes(), before)
            finally:
                server.shutdown()
                thread.join()

    @unittest.skipUnless(hasattr(socket, "AF_UNIX"), "requires Unix domain sockets")
    def test_source_inventory_and_capture_use_the_pinned_workspace(self):
        address = str(self.base / "src.sock")
        root = self.base / "source"
        before = (self.source / "state.db-wal").read_bytes()

        def send(method, path, body=b""):
            return socket_request(address, root, method, path, body)

        with patch.object(history, "INVENTORY_PAGE", 2), history.HistoryServer(address, history.HistorySourceRequest) as server:
            server.workspace_root, server.scratch = root, self.base
            server.source_user, server.source_instance, server.session_db = 7, 13, SQLiteFixture
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                self.assertEqual(send("GET", "/inventory/7/14")[0], 404)
                self.assertEqual(send("GET", "/inventory/7/13?after=parent&after=child")[0], 400)
                status, page = send("GET", "/inventory/7/13")
                self.assertEqual(status, 200)
                self.assertEqual(page, {"sessions": [{"id": "child", "title": ""},
                                                     {"id": "parent", "title": "Original title"}], "next": "parent"})
                self.assertEqual(send("GET", "/inventory/7/13?after=parent"),
                                 (200, {"sessions": [{"id": "unrelated", "title": ""}], "next": ""}))
                self.assertEqual(send("POST", "/restore/7/13", b"[]")[0], 404)
                self.assertEqual(send("POST", "/capture/8/13", b'["child"]')[0], 404)
                self.assertEqual(send("POST", "/capture/7/13", b'["child","child"]')[0], 422)
                self.assertEqual(send("POST", "/capture/7/13", b'["deleted"]')[0], 422)
                status, payload = send("POST", "/capture/7/13", b'["child"]')
                self.assertEqual(status, 200)
                target = self.workspace("source-round-trip")
                receipt = history.apply_history(SQLiteFixture, target, "restore", history.encode(payload), 7, 13)
                self.assertEqual((receipt["sessions"], receipt["messages"]), (2, 2))
                self.assertEqual([row["id"] for row in payload["sessions"]], ["child", "parent"])
                self.assertEqual((self.source / "state.db-wal").read_bytes(), before)
            finally:
                server.shutdown()
                thread.join()


if __name__ == "__main__":
    unittest.main()
