import concurrent.futures
import importlib.util
from pathlib import Path
import tempfile
import unittest
import urllib.error
import urllib.parse
import base64
import threading
import http.client

spec = importlib.util.spec_from_file_location("session_entry", Path(__file__).with_name("opencode-pro-session.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class SessionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.state = Path(self.temp.name) / "state"
        self.project = Path(self.temp.name) / "workspace" / "project"
        self.sessions = {}
        self.creates = 0
        self.lose_response = False
        self.pages = None

    def session(self, session_id, project=None, updated=1, archived=0, parent=""):
        return {"id": session_id, "location": {"directory": str(project or self.project)},
                "time": {"updated": updated, "archived": archived}, "parentID": parent}

    def entry(self, project=None):
        entry = module.SessionEntry("http://127.0.0.1:4096", project or self.project, self.state)
        def api(path, body=None):
            if body is not None:
                self.creates += 1
                session = self.session(body["id"], body["location"]["directory"])
                self.sessions[body["id"]] = session
                if self.lose_response:
                    raise urllib.error.URLError("response lost after commit")
                return {"data": session}
            if path.startswith("/api/session?"):
                if self.pages:
                    return self.pages[1 if "cursor=" in path else 0]
                return {"data": list(self.sessions.values()), "cursor": {"next": None}}
            session_id = path.rsplit("/", 1)[1]
            if session_id not in self.sessions:
                raise urllib.error.HTTPError(path, 404, "missing", {}, None)
            return {"data": self.sessions[session_id]}
        entry.api = api
        return entry

    def test_first_boot_and_restart_share_session_and_v2_route(self):
        first = self.entry().url()
        self.assertIn("/server/aHR0cDovLzEyNy4wLjAuMTo0MDk2/session/ses_", first)
        self.assertEqual(first, self.entry().url())
        self.assertEqual(1, self.creates)

    def test_existing_latest_project_session_reused_across_pages(self):
        self.pages = [{"data": [self.session("ses_other", self.project.parent / "other", 99)], "cursor": {"next": "next"}},
                      {"data": [self.session("ses_existing", updated=20)], "cursor": {"next": None}}]
        self.assertEqual("ses_existing", self.entry().resolve())
        self.assertEqual(0, self.creates)

    def test_latest_conversation_preferred_over_starter(self):
        starter = self.entry().resolve()
        self.sessions["ses_latest"] = self.session("ses_latest", updated=99)
        self.assertEqual("ses_latest", self.entry().resolve())
        self.assertIn(starter, self.sessions)
        self.assertEqual(1, self.creates)

    def test_archived_starter_not_recreated(self):
        starter = self.entry().resolve()
        self.sessions[starter]["time"]["archived"] = 3
        self.assertIsNone(self.entry().resolve())
        self.assertEqual(1, self.creates)

    def test_switch_project_and_switch_back(self):
        first = self.entry().resolve()
        other = self.entry(self.project.parent / "other").resolve()
        self.assertNotEqual(first, other)
        self.assertEqual(first, self.entry().resolve())
        self.assertEqual(2, self.creates)

    def test_committed_create_recovers_lost_response(self):
        self.lose_response = True
        first = self.entry().resolve()
        self.assertEqual(first, self.entry().resolve())
        self.assertEqual(1, self.creates)

    def test_children_and_archived_sessions_not_adopted(self):
        self.sessions["ses_child"] = self.session("ses_child", parent="ses_parent")
        self.sessions["ses_archived"] = self.session("ses_archived", archived=1)
        result = self.entry().resolve()
        self.assertNotIn(result, ("ses_child", "ses_archived"))
        self.assertEqual(1, self.creates)

    def test_concurrent_seed_is_atomic_and_instance_specific(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            ids = list(pool.map(lambda _: self.entry().initial_id(), range(16)))
        self.assertEqual(1, len(set(ids)))
        other = module.SessionEntry("http://127.0.0.1:4096", self.project, Path(self.temp.name) / "other-state")
        self.assertNotEqual(ids[0], other.initial_id())

    def test_browser_pairing_uses_session_token_without_master_password(self):
        entry = self.entry()
        api = entry.api
        def pairing(path, body=None):
            if path == "/api/pair":
                self.assertEqual({}, body)
                return {"code": "single-use-code"}
            if path == "/auth/connect/single-use-code":
                self.assertIsNone(body)
                return {"token": "signed-browser-session"}
            return api(path, body)
        entry.api = pairing
        session = entry.browser_session()
        self.assertEqual("signed-browser-session", session["token"])
        self.assertNotIn("?", session["url"])
        self.assertEqual(1, self.creates)

    def test_loopback_handoff_delivers_native_cookie_and_preserves_route(self):
        target = "http://127.0.0.1:4096/server/local/session/ses_existing"
        server, url = module.browser_bootstrap({"url": target, "token": "signed.session-token"})
        self.addCleanup(server.server_close)
        def serve():
            while not server.completed:
                server.handle_request()
        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        parsed = urllib.parse.urlsplit(url)
        connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=5)
        self.addCleanup(connection.close)
        connection.request("GET", "/wrong-code")
        denied = connection.getresponse()
        denied.read()
        self.assertEqual(404, denied.status)
        self.assertIsNone(denied.getheader("Set-Cookie"))
        connection.request("GET", parsed.path)
        response = connection.getresponse()
        response.read()
        self.assertEqual(302, response.status)
        self.assertEqual(target, response.getheader("Location"))
        cookie = response.getheader("Set-Cookie")
        self.assertIn("opencode_session_4096=signed.session-token", cookie)
        self.assertIn("HttpOnly", cookie)
        self.assertIn("SameSite=Lax", cookie)
        self.assertEqual("no-store", response.getheader("Cache-Control"))
        thread.join(5)
        self.assertFalse(thread.is_alive())

    def test_loopback_handoff_rejects_external_targets_and_cookie_injection(self):
        with self.assertRaises(ValueError):
            module.browser_bootstrap({"url": "http://example.com/session", "token": "token"})
        with self.assertRaises(ValueError):
            module.browser_bootstrap({"url": "http://127.0.0.1:4096/", "token": "token\r\nInjected: value"})


if __name__ == "__main__":
    unittest.main()
