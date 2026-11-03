#!/usr/bin/env python3
"""Loopback-only UI fixture; never loads iiko config or contacts a restaurant.

python scripts/mock-ui.py --password-hash-file <private dev hash file>
Without a hash the storefront works and tester login stays disabled.
POST /__mock__ requires CSRF from GET /api/access:
{"status":"confirmed|pending|rejected", "mode":"closed|tester|public"},
{"drop":true}, {"dropBefore":true}, {"expireSessions":true},
{"price":650}, {"menuAvailable":false}, {"menuError":true}, {"stopIds":["ribeye"]}.
Memory-only attempts. Durable recovery and real auth are tested in Go.
"""
import argparse
import base64
import copy
import hashlib
import hmac
import json
import secrets
import socket
import threading
import time
from datetime import datetime, timezone
from http.cookies import SimpleCookie
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
SEED = json.loads((ROOT / "data/menu_catalog.json").read_text(encoding="utf-8"))


def valid_hash(encoded):
    try:
        algorithm, iterations, salt, key = encoded.split("$")
        return (algorithm == "pbkdf2-sha256" and iterations == "600000"
                and len(base64.b64decode(salt + "==", validate=True)) == 16
                and len(base64.b64decode(key + "=", validate=True)) == 32)
    except (ValueError, TypeError):
        return False


class Fixture:
    def __init__(self, password_hash="", mode="tester"):
        if password_hash and not valid_hash(password_hash):
            raise ValueError("Invalid mock password hash")
        if mode not in ("closed", "tester", "public"):
            raise ValueError("Invalid mock mode")
        self.password_hash = password_hash
        self.secret = secrets.token_bytes(32)
        self.sessions = {}
        self.logins = []
        self.lock = threading.RLock()
        self.state = {"status": "pending", "mode": mode, "price": None,
                      "drop": False, "dropBefore": False, "stopIds": [],
                      "menuAvailable": True, "menuError": False,
                      "creates": 0, "attempts": {}}

    def verify(self, password):
        if not self.password_hash or not isinstance(password, str) or len(password.encode()) > 1024:
            return False
        _, _, salt, key = self.password_hash.split("$")
        derived = hashlib.pbkdf2_hmac("sha256", password.encode(), base64.b64decode(salt + "=="), 600000)
        return hmac.compare_digest(derived, base64.b64decode(key + "="))


class MockServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, port=8765, password_hash="", mode="tester"):
        self.fixture = Fixture(password_hash, mode)
        super().__init__(("127.0.0.1", port), Handler)


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(ROOT), **kwargs)

    @property
    def fixture(self):
        return self.server.fixture

    def reply(self, code, data, cookies=()):
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Vary", "Cookie")
        self.send_header("Retry-After", "60" if code == 429 else "5")
        for cookie in cookies:
            self.send_header("Set-Cookie", cookie)
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False).encode())

    def cookie(self, name):
        cookies = SimpleCookie()
        try:
            cookies.load(self.headers.get("Cookie", ""))
            return cookies[name].value if name in cookies else ""
        except (ValueError, KeyError):
            return ""

    @staticmethod
    def set_cookie(name, value, max_age=7200):
        return f"{name}={value}; Path=/; HttpOnly; SameSite=Strict; Max-Age={max_age}"

    def signature(self, body, session):
        return hmac.new(self.fixture.secret, (body + "|" + session).encode(), hashlib.sha256).hexdigest()

    def csrf_token(self, session):
        body = secrets.token_urlsafe(32) + "." + str(int(time.time()) + 7200)
        return body + "." + self.signature(body, session)

    def valid_csrf(self, token, session):
        parts = token.split(".")
        if not token or len(parts) != 3:
            return False
        try:
            if time.time() >= int(parts[1]):
                return False
        except ValueError:
            return False
        return hmac.compare_digest(parts[2], self.signature(".".join(parts[:2]), session))

    def csrf(self):
        token = self.headers.get("X-CSRF-Token", "")
        session = self.cookie("beini_tester_dev") if self.authenticated() else ""
        return hmac.compare_digest(self.cookie("beini_csrf_dev"), token) and self.valid_csrf(token, session)

    def authenticated(self):
        token = self.cookie("beini_tester_dev")
        return self.fixture.sessions.get(hashlib.sha256(token.encode()).digest(), 0) > time.time()

    def result(self, attempt):
        status = self.fixture.state["status"]
        self.reply(422 if status == "rejected" else 200, {
            "success": status != "rejected", "status": status, "mock": True,
            "pending": status == "pending", "outcomeUnknown": False,
            "creationStatus": {"pending": "InProgress", "confirmed": "Success", "rejected": "Error"}[status],
            "totalSum": attempt["total"], "orderId": "offline-only",
        })

    def do_GET(self):
        with self.fixture.lock:
            state = self.fixture.state
            if self.path == "/__mock__":
                return self.reply(200, {**{k: v for k, v in state.items() if k != "attempts"},
                                        "attemptKeys": list(state["attempts"]), "mock": True})
            if self.path == "/api/access":
                authenticated = self.authenticated()
                session = self.cookie("beini_tester_dev") if authenticated else ""
                token = self.cookie("beini_csrf_dev")
                if not self.valid_csrf(token, session):
                    token = self.csrf_token(session)
                cookies = [self.set_cookie("beini_csrf_dev", token)]
                expires = self.fixture.sessions.get(hashlib.sha256(session.encode()).digest())
                return self.reply(200, {"mode": state["mode"], "authenticated": authenticated,
                                       "canOrder": state["mode"] == "public" or state["mode"] == "tester" and authenticated,
                                       "loginAvailable": bool(self.fixture.password_hash), "csrfToken": token,
                                       "sessionExpiresAt": datetime.fromtimestamp(expires, timezone.utc).isoformat() if expires else None,
                                       "mock": True}, cookies)
            if self.path == "/api/menu":
                if state["menuError"]:
                    return self.reply(503, {"error": "menu_unavailable"})
                data = copy.deepcopy(SEED)
                now = datetime.now(timezone.utc).isoformat()
                data.update(version=1, orderingAvailable=state["menuAvailable"], menuUpdatedAt=now, stopListUpdatedAt=now)
                data["items"] = [i for i in data["items"] if not i.get("hidden")]
                for item in data["items"]:
                    item["available"] = item["available"] and state["menuAvailable"] and item["id"] not in state["stopIds"]
                    if state["price"] is not None:
                        item["price"] = state["price"]
                return self.reply(200, data)
            if self.path == "/api/order/status":
                attempt = state["attempts"].get(self.headers.get("Idempotency-Key"))
                return self.result(attempt) if attempt else self.reply(404, {"error": "not_found", "mock": True})
            if self.path.startswith("/api/"):
                return self.reply(404, {"error": "not_found"})
        path = urlsplit(self.path).path
        if path == "/tester":
            self.path = "/tester.html"
        elif path not in ("/", "/index.html", "/demo", "/demo.html") and not path.startswith(("/assets/", "/css/", "/js/")):
            return self.send_error(404)
        if path == "/demo":
            self.path = "/demo.html"
        translated = Path(self.translate_path(self.path)).resolve()
        if not translated.is_relative_to(ROOT) or (translated.suffix.lower() not in (".html", ".css", ".js", ".jpg", ".jpeg", ".png", ".svg", ".webp", ".ico", ".woff2") and translated != ROOT):
            return self.send_error(404)
        relative = translated.relative_to(ROOT)
        if relative.parts and relative.parts[0] not in ("assets", "css", "js", "index.html", "demo.html", "tester.html"):
            return self.send_error(404)
        super().do_GET()

    def end_headers(self):
        if not self.path.startswith(("/api/", "/__mock__")):
            self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def drop_response(self):
        self.connection.shutdown(socket.SHUT_RDWR)
        self.connection.close()

    def do_POST(self):
        # Consume the bounded body before an early rejection. Closing with
        # unread request bytes can reset the Windows socket before 403 arrives.
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length < 0 or length > 32768:
                return self.reply(413, {"error": "body too large"})
            raw = self.rfile.read(length)
        except ValueError:
            return self.reply(400, {"error": "invalid length"})
        host = self.headers.get("Host", "")
        origin = self.headers.get("Origin", "")
        if origin != "http://" + host or urlsplit(origin).hostname not in ("127.0.0.1", "localhost") or not self.csrf():
            return self.reply(403, {"code": "csrf", "error": "Проверка безопасности не пройдена"})
        if self.headers.get_content_type() != "application/json":
            return self.reply(415, {"error": "application/json required"})
        try:
            data = json.loads(raw)
            if not isinstance(data, dict):
                raise ValueError()
        except (ValueError, UnicodeDecodeError):
            return self.reply(400, {"error": "invalid JSON"})
        with self.fixture.lock:
            state = self.fixture.state
            if self.path == "/api/tester/login":
                now = time.time()
                self.fixture.logins = [at for at in self.fixture.logins if now - at < 60]
                if len(self.fixture.logins) >= 5:
                    return self.reply(429, {"error": "Слишком много попыток. Подождите минуту."})
                self.fixture.logins.append(now)
                if not self.fixture.verify(data.get("password")):
                    return self.reply(401, {"error": "Неверный пароль"})
                session = secrets.token_urlsafe(32)
                self.fixture.sessions = {k: v for k, v in self.fixture.sessions.items() if v > now}
                self.fixture.sessions.pop(hashlib.sha256(self.cookie("beini_tester_dev").encode()).digest(), None)
                self.fixture.sessions[hashlib.sha256(session.encode()).digest()] = now + 28800
                token = self.csrf_token(session)
                return self.reply(200, {"success": True, "csrfToken": token}, [self.set_cookie("beini_tester_dev", session, 28800), self.set_cookie("beini_csrf_dev", token)])
            if self.path == "/api/tester/logout":
                self.fixture.sessions.pop(hashlib.sha256(self.cookie("beini_tester_dev").encode()).digest(), None)
                return self.reply(200, {"success": True}, [self.set_cookie("beini_tester_dev", "", -1), self.set_cookie("beini_csrf_dev", "", -1)])
            if self.path == "/__mock__":
                if data.get("status", state["status"]) not in ("pending", "confirmed", "rejected") or data.get("mode", state["mode"]) not in ("closed", "tester", "public"):
                    return self.reply(400, {"error": "invalid scenario"})
                price = data.get("price")
                if price is not None and (type(price) not in (int, float) or not 0 < price < 1000000):
                    return self.reply(400, {"error": "invalid price"})
                for key in ("status", "mode", "price", "drop", "dropBefore", "menuAvailable", "menuError", "stopIds"):
                    if key in data:
                        state[key] = data[key]
                if data.get("expireSessions"):
                    self.fixture.sessions.clear()
                return self.reply(200, {"success": True, "mock": True})
            if self.path != "/api/order":
                return self.reply(404, {"error": "not_found"})
            if state["mode"] == "closed" or state["mode"] == "tester" and not self.authenticated():
                return self.reply(403, {"code": "ordering_closed", "error": "Онлайн-отправка временно закрыта"})
            key = self.headers.get("Idempotency-Key", "")
            if not key or len(key) > 128:
                return self.reply(400, {"error": "invalid key"})
            old = state["attempts"].get(key)
            if old:
                if old["body"] != raw:
                    return self.reply(422, {"error": "payload_mismatch"})
                return self.result(old)
            if state["dropBefore"]:
                state["dropBefore"] = False
                return self.drop_response()
            if not state["menuAvailable"] or state["menuError"]:
                return self.reply(503, {"error": "menu_unavailable"})
            items = []
            try:
                if not 0 < len(data["items"]) <= 50 or len(str(data.get("phone", ""))) < 10 or data.get("deliveryType") not in ("delivery", "pickup"):
                    raise ValueError()
                if data["deliveryType"] == "delivery" and not data.get("address"):
                    raise ValueError()
                for line in data["items"]:
                    item = next(i for i in SEED["items"] if i["id"] == line["productId"] and not i.get("hidden"))
                    if not item["available"] or item["id"] in state["stopIds"] or type(line["amount"]) is not int or not 0 < line["amount"] <= 50:
                        raise ValueError()
                    items.append({"productId": item["iikoId"], "amount": line["amount"], "price": state["price"] if state["price"] is not None else item["price"]})
            except (KeyError, TypeError, StopIteration, ValueError):
                return self.reply(400, {"error": "Проверьте состав и данные заказа"})
            total = sum(i["price"] * i["amount"] for i in items)
            if data.get("expectedPrices") != [i["price"] for i in items]:
                return self.reply(409, {"success": False, "code": "price_changed", "items": items, "totalSum": total})
            state["creates"] += 1
            attempt = {"body": raw, "total": total}
            state["attempts"][key] = attempt
            if state["drop"]:
                state["drop"] = False
                return self.drop_response()
            self.result(attempt)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=8765)
    parser.add_argument("--mode", choices=("closed", "tester", "public"), default="tester")
    parser.add_argument("--password-hash-file", type=Path)
    args = parser.parse_args()
    encoded = args.password_hash_file.read_text(encoding="utf-8").strip() if args.password_hash_file else ""
    server = MockServer(args.port, encoded, args.mode)
    print(f"MOCK ONLY, no restaurant orders: http://127.0.0.1:{server.server_port}", flush=True)
    server.serve_forever()
