"""Only synthetic loopback requests; no imports of live test_order utilities."""
import base64
import hashlib
import http.cookiejar
import io
from email.message import Message
import importlib.util
import json
import threading
import unittest
import urllib.error
import urllib.request
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("mock_ui", Path(__file__).with_name("mock-ui.py"))
MOCK = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MOCK)
PASSWORD = "offline-password-only"
SALT = b"offline-salt-123"
ENCODED = "pbkdf2-sha256$600000$" + base64.b64encode(SALT).decode().rstrip("=") + "$" + base64.b64encode(hashlib.pbkdf2_hmac("sha256", PASSWORD.encode(), SALT, 600000)).decode().rstrip("=")


class MockUITest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = MOCK.MockServer(0, ENCODED)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.origin = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def setUp(self):
        self.server.fixture = MOCK.Fixture(ENCODED)
        self.client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.csrf = ""

    def call(self, path, data=None, key="offline-key"):
        request = urllib.request.Request(self.origin + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={"Origin": self.origin, "Content-Type": "application/json", "X-CSRF-Token": self.csrf, "Idempotency-Key": key})
        try:
            response = self.client.open(request, timeout=3)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            body = response.read().decode()
            try:
                value = json.loads(body)
            except ValueError:
                value = body
            if isinstance(value, dict) and "csrfToken" in value:
                self.csrf = value["csrfToken"]
            return response.status, value

    def login(self):
        self.call("/api/access")
        self.assertEqual(self.call("/api/tester/login", {"password": PASSWORD})[0], 200)

    def test_guest_owner_expiry_modes_and_no_secret_response(self):
        self.call("/api/access")
        payload = {"phone": "89990000000", "deliveryType": "pickup", "items": [{"productId": "beini-grill-04", "amount": 1}], "expectedPrices": [1250]}
        self.assertEqual(self.call("/api/order", payload)[0], 403)
        self.assertEqual(self.call("/__mock__")[1]["creates"], 0)
        self.assertEqual(self.call("/api/tester/login", {"password": "wrong"})[0], 401)
        self.login()
        self.assertEqual(self.call("/api/order", payload)[0], 200)
        self.assertEqual(self.call("/api/order", payload)[0], 200)
        self.assertEqual(self.call("/__mock__")[1]["creates"], 1)
        self.call("/__mock__", {"mode": "closed", "expireSessions": True})
        self.assertEqual(self.call("/api/order", payload)[0], 403)
        self.assertEqual(self.call("/api/order/status")[1]["status"], "pending")
        self.call("/api/access")
        self.call("/__mock__", {"mode": "public", "status": "confirmed"})
        self.assertEqual(self.call("/api/order", payload)[1]["status"], "confirmed")
        self.assertTrue(self.call("/api/order/status")[1]["mock"])
        state = self.call("/__mock__")[1]
        self.assertEqual(state["creates"], 1)
        for secret in (PASSWORD, ENCODED, "89990000000", "sessions", "secret"):
            self.assertNotIn(secret, json.dumps(state))

    def test_csrf_logout_menu_hidden_and_private_files(self):
        self.assertEqual(self.call("/api/tester/login", {"password": PASSWORD})[0], 403)
        self.login()
        self.assertTrue(self.call("/api/access")[1]["authenticated"])
        self.assertEqual(self.call("/api/tester/logout", {})[0], 200)
        self.assertFalse(self.call("/api/access")[1]["authenticated"])
        menu = self.call("/api/menu")[1]
        self.assertEqual(len(menu["items"]), 251)
        self.assertFalse(any(i.get("hidden") for i in menu["items"]))
        for path in ("/iiko_config.json", "/.git/config", "/docs/product.md", "/assets/../scripts/mock-ui.py"):
            self.assertEqual(self.call(path)[0], 404)

    def test_rejected_post_drains_bounded_body(self):
        # Closing a socket with unread request bytes can reset the connection
        # on Windows before the client receives the intended 403 response.
        request = object.__new__(MOCK.Handler)
        body = b'{"password":"offline-only"}'
        request.rfile = io.BytesIO(body)
        request.headers = Message()
        request.headers["Content-Length"] = str(len(body))
        request.headers["Content-Type"] = "application/json"
        request.headers["Host"] = "127.0.0.1:8765"
        request.headers["Origin"] = "http://127.0.0.1:8765"
        request.csrf = lambda: False
        result = []
        request.reply = lambda code, data: result.append(code)
        request.do_POST()
        self.assertEqual(result, [403])
        self.assertEqual(request.rfile.tell(), len(body), "403 closes before consuming body")

    def test_drop_rejection_and_exact_retry(self):
        self.login()
        body = {"phone": "89990000000", "deliveryType": "delivery", "address": "Ленина, 1", "items": [{"productId": "beini-grill-04", "amount": 1}], "expectedPrices": [1250]}
        self.call("/__mock__", {"drop": True})
        with self.assertRaises(Exception):
            self.call("/api/order", body)
        self.assertEqual(self.call("/api/order", body)[0], 200)
        self.call("/__mock__", {"status": "rejected"})
        self.assertEqual(self.call("/api/order/status")[0], 422)
        self.assertEqual(self.call("/__mock__")[1]["creates"], 1)
        changed = {**body, "phone": "89991111111"}
        self.assertEqual(self.call("/api/order", changed)[0], 422)


if __name__ == "__main__":
    unittest.main()
