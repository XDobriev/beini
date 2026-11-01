#!/usr/bin/env python3
"""Loopback-only frontend fixture. Never reads iiko config or contacts iiko.

Run python scripts/mock-ui.py; change scenarios through POST /__mock__ JSON:
{"status":"confirmed"}, {"status":"rejected"}, {"price":650},
{"drop":true}, {"menuAvailable":false}, {"menuError":true}.
GET /__mock__ returns counters and synthetic attempts. State is memory-only;
actual durable restart behavior is tested in cmd/server/application_test.go.
"""
import argparse
import copy
import json
import socket
import threading
from datetime import datetime, timezone
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SEED = json.loads((ROOT / "data/menu_catalog.json").read_text(encoding="utf-8"))
LOCK = threading.RLock()
STATE = {"status": "pending", "price": None, "drop": False,
         "menuAvailable": True, "menuError": False, "creates": 0, "attempts": {}}


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(ROOT), **kwargs)

    def reply(self, code, data):
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Retry-After", "5")
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False).encode())

    def result(self, attempt):
        status = STATE["status"]
        self.reply(422 if status == "rejected" else 200, {
            "success": status != "rejected", "status": status,
            "pending": status == "pending", "outcomeUnknown": False,
            "creationStatus": {"pending": "InProgress", "confirmed": "Success", "rejected": "Error"}[status],
            "totalSum": attempt["total"], "orderId": "offline-only",
        })

    def do_GET(self):
        with LOCK:
            if self.path == "/__mock__":
                return self.reply(200, STATE)
            if self.path == "/api/menu":
                if STATE["menuError"]:
                    return self.reply(503, {"error": "menu_unavailable"})
                data = copy.deepcopy(SEED)
                now = datetime.now(timezone.utc).isoformat()
                data.update(version=1, orderingAvailable=STATE["menuAvailable"],
                            menuUpdatedAt=now, stopListUpdatedAt=now)
                for item in data["items"]:
                    item["available"] = item["available"] and STATE["menuAvailable"]
                    if STATE["price"] is not None:
                        item["price"] = STATE["price"]
                return self.reply(200, data)
            if self.path == "/api/order/status":
                attempt = STATE["attempts"].get(self.headers.get("Idempotency-Key"))
                if not attempt:
                    return self.reply(404, {"error": "not_found"})
                return self.result(attempt)
            if self.path.startswith("/api/"):
                return self.reply(404, {"error": "not_found"})
        super().do_GET()

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        data = json.loads(raw)
        with LOCK:
            if self.path == "/__mock__":
                for key in ("status", "price", "drop", "menuAvailable", "menuError"):
                    if key in data:
                        STATE[key] = data[key]
                return self.reply(200, STATE)
            if self.path != "/api/order":
                return self.reply(404, {"error": "not_found"})
            key = self.headers.get("Idempotency-Key")
            old = STATE["attempts"].get(key)
            if old:
                if old["body"] != raw.decode():
                    return self.reply(422, {"error": "payload_mismatch"})
                return self.result(old)
            items = []
            for line in data["items"]:
                item = next(i for i in SEED["items"] if i["id"] == line["productId"])
                items.append({"productId": item["iikoId"], "amount": line["amount"],
                              "price": STATE["price"] if STATE["price"] is not None else item["price"]})
            total = sum(i["price"] * i["amount"] for i in items)
            if data.get("expectedPrices") != [i["price"] for i in items]:
                return self.reply(409, {"success": False, "code": "price_changed", "items": items, "totalSum": total})
            STATE["creates"] += 1
            attempt = {"body": raw.decode(), "total": total}
            STATE["attempts"][key] = attempt
            if STATE["drop"]:
                STATE["drop"] = False
                self.connection.shutdown(socket.SHUT_RDWR)
                self.connection.close()
                return
            self.result(attempt)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=8765)
    args = parser.parse_args()
    print(f"Offline UI fixture: http://127.0.0.1:{args.port}", flush=True)
    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()
