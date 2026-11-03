#!/usr/bin/env python3
"""Change only the loopback mock UI fixture, never a real BEINI server.

python scripts/mock-control.py '{"status":"confirmed"}'
Use --port when the fixture listens on a different loopback port.
"""
import argparse
import http.cookiejar
import json
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenario", help="JSON fixture controls, no passwords")
    parser.add_argument("--port", type=int, default=8765)
    args = parser.parse_args()
    data = json.loads(args.scenario)
    if not isinstance(data, dict) or not 1 <= args.port <= 65535:
        parser.error("Expected JSON object and valid port")
    origin = f"http://127.0.0.1:{args.port}"
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    with client.open(origin + "/api/access", timeout=5) as response:
        access = json.load(response)
    if access.get("mock") is not True:
        parser.error("Target is not the mock fixture")
    request = urllib.request.Request(origin + "/__mock__", data=json.dumps(data).encode(),
                                     headers={"Origin": origin, "Content-Type": "application/json", "X-CSRF-Token": access["csrfToken"]})
    with client.open(request, timeout=5) as response:
        if response.status != 200:
            parser.error("Scenario rejected")
    with client.open(origin + "/__mock__", timeout=5) as response:
        state = json.load(response)
    print(json.dumps({"mock": True, "mode": state["mode"], "status": state["status"], "creates": state["creates"]}))


if __name__ == "__main__":
    main()
