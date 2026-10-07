#!/usr/bin/env python3
import json
import os
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

MSG_INIT = 1
MSG_SAVE = 2
MSG_NEXT = 5
MSG_CLEAR_CACHE = 7
MSG_STATE = 101
MSG_IMAGE = 102
MSG_STATUS = 103
MSG_TODAY = 18
MSG_TODAY_ACT = 19
MSG_TODAY_STATE = 109
MSG_TODAY_RESULT = 110
AS_OF = "2026-01-01T09:00:00-03:00"
LAYOUT = {"screen": "tablet", "rev": 1, "grid": {"cols": 2}, "items": [
    {"widget": "demo.notes", "view": "alert", "x": 0, "y": 0, "w": 2, "h": 1},
    {"widget": "demo.tasks", "view": "list", "x": 0, "y": 1, "w": 1, "h": 2},
    {"widget": "demo.money", "view": "stat", "x": 1, "y": 1, "w": 1, "h": 1},
    {"widget": "demo.bots", "view": "spark", "x": 1, "y": 2, "w": 1, "h": 1},
    {"widget": "demo.down", "view": "list", "x": 0, "y": 3, "w": 1, "h": 1},
    # "table" was dropped: like any view the tablet doesn't know, it is "unavailable".
    {"widget": "demo.money", "view": "table", "x": 1, "y": 3, "w": 1, "h": 1}]}
TASK = {"key": "t:1", "title": "Pay invoice", "subtitle": "due today", "tone": "warn", "actions": ["tick", "purge"]}
GATEWAY_STATE = {"ticked": False}


def entry(wid, title, state, data=None, actions=(), error=None):
    e = {"widget": wid, "app": "demo", "title": title, "views": [], "state": state, "actions": list(actions)}
    if data is not None:
        e["data"] = dict(data, id=wid.split(".")[1], title=title, as_of=AS_OF)
    if error:
        e["error"] = error
    return e


def screen():
    items = [] if GATEWAY_STATE["ticked"] else [TASK]
    return {"screen": "tablet", "layout": LAYOUT, "widgets": {
        "demo.notes": entry("demo.notes", "Notes", "ok", {"alert": {"text": "Invoice closes soon", "tone": "warn"}}),
        "demo.tasks": entry("demo.tasks", "Tasks", "ok", {"list": {"groups": [{"title": "Today", "items": items}]}},
                            [{"id": "tick", "label": "Done", "scope": "item", "risk": "low"},
                             {"id": "purge", "label": "Purge", "scope": "item", "risk": "high"}]),
        "demo.money": entry("demo.money", "Money", "stale",
                            {"stat": {"value": "1.234", "label": "Balance", "delta": "+56", "tone": "good"}}),
        "demo.bots": entry("demo.bots", "Bots", "ok",
                           {"spark": {"label": "Equity", "points": [10, 20, 15], "unit": "USD"}}),
        "demo.down": entry("demo.down", "Down", "error", error="unavailable: Demo is down")}}


SYSTEM_TERMINATE = 0xFFFFFFFF


def send(conn, kind, payload=""):
    data = payload.encode()
    conn.send(struct.pack("<II", kind, len(data)))
    # AppLoad's C++ sender emits a second sequence packet even for an empty
    # QString. The production backend must consume it.
    conn.send(data)


def receive(conn, timeout=15):
    conn.settimeout(timeout)
    header = conn.recv(8)
    if len(header) != 8:
        raise RuntimeError(f"short header: {len(header)}")
    kind, length = struct.unpack("<II", header)
    payload = conn.recv(length).decode() if length else ""
    return kind, json.loads(payload or "{}")


def wait_for(conn, expected, timeout=30):
    deadline = time.monotonic() + timeout
    seen = []
    while time.monotonic() < deadline:
        kind, payload = receive(conn, max(1, deadline - time.monotonic()))
        seen.append((kind, payload))
        if kind == expected:
            return payload, seen
    raise TimeoutError(f"message {expected} not received; saw {seen}")


class FakeGateway(BaseHTTPRequestHandler):
    """Answers as tailscaled's proxy would, on behalf of the Mac gateway."""

    def _send(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _route(self):
        url = urlsplit(self.path)  # a proxy request carries the absolute URL
        if url.netloc != "mac.test:8090" or self.headers.get("Authorization") != "Bearer harness-token":
            self._send(401, {"error": "bad token"})
            return None
        return url.path

    def do_GET(self):
        path = self._route()
        if path == "/screens/tablet":
            self._send(200, screen())
        elif path is not None:
            self._send(404, {"error": "no route"})

    def do_POST(self):
        path = self._route()
        if path is None:
            return
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if path != "/widgets/demo.tasks/actions/tick" or body != {"key": "t:1", "screen": "tablet"}:
            self._send(400, {"error": f"unexpected {path} {body}"})
            return
        GATEWAY_STATE["ticked"] = True
        self._send(200, {"ok": True, "message": "Done", "refresh": True, "outcome": "done"})

    def log_message(self, *args):
        pass


def wait_until(conn, expected, pred, log, timeout=30):
    """Receive until a message of kind `expected` satisfies `pred`. Every
    message received, matched or not, is appended to `log`."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        kind, payload = receive(conn, max(1, deadline - time.monotonic()))
        log.append((kind, payload))
        if kind == expected and pred(payload):
            return payload
    raise TimeoutError(f"message {expected} not matched; saw {log}")


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: appload_harness.py BACKEND MOCK_SERVER")
    backend, mock_bin = map(Path, sys.argv[1:])
    with tempfile.TemporaryDirectory(prefix="trmnl-integration-") as td:
        root = Path(td)
        home = root / "home"
        home.mkdir(mode=0o700)
        cfgdir = home / ".config/trmnl-remarkable"
        cfgdir.mkdir(parents=True, mode=0o700)
        token = cfgdir / "today.token"
        token.write_text("harness-token\n")
        token.chmod(0o600)
        today_json = cfgdir / "today.json"
        today_json.write_text(json.dumps({
            "gateway_url": "http://mac.test:8090", "proxy": "http://127.0.0.1:19989",
            "token_file": str(token), "timezone": "America/Sao_Paulo",
            "sections": ["widgets"]}))
        today_json.chmod(0o600)
        gateway = ThreadingHTTPServer(("127.0.0.1", 19989), FakeGateway)
        threading.Thread(target=gateway.serve_forever, daemon=True).start()
        sock_path = str(root / "appload.sock")
        listener = socket.socket(socket.AF_UNIX, socket.SOCK_SEQPACKET)
        listener.bind(sock_path)
        listener.listen(1)
        mock = subprocess.Popen([str(mock_bin), "-listen", "127.0.0.1:19988"], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        try:
            for _ in range(50):
                try:
                    with socket.create_connection(("127.0.0.1", 19988), timeout=.1):
                        break
                except OSError:
                    time.sleep(.1)
            else:
                raise RuntimeError("mock server did not start")
            env = dict(os.environ, HOME=str(home))
            proc = subprocess.Popen([str(backend), sock_path], env=env)
            conn, _ = listener.accept()
            try:
                send(conn, MSG_INIT)
                state, _ = wait_for(conn, MSG_STATE)
                assert state["api_key_configured"] is False
                config = state["config"]
                config.update({"api_key": "local-test", "base_url": "http://127.0.0.1:19988", "device_id": "", "minimum_refresh_seconds": 60})
                send(conn, MSG_SAVE, json.dumps(config))
                wait_for(conn, MSG_STATE)
                send(conn, MSG_NEXT)
                image, seen = wait_for(conn, MSG_IMAGE)
                image_path = Path(image["path"].removeprefix("file://"))
                assert image_path.is_file()
                image_bytes = image_path.stat().st_size  # Clear cache deletes the image later
                assert image_bytes > 1000
                assert image_path.suffix == ".png"
                cfg_path = home / ".config/trmnl-remarkable/config.json"
                assert cfg_path.stat().st_mode & 0o777 == 0o600
                saved = json.loads(cfg_path.read_text())
                assert saved["api_key"] == "local-test"
                log = []  # every message from here on, for the leak check
                send(conn, MSG_TODAY, "{}")
                today = wait_until(conn, MSG_TODAY_STATE, lambda p: p.get("refreshing") is False, log)
                assert today["configured"] is True, today
                assert [s["id"] for s in today["sections"]] == ["widgets"], today
                sec = today["sections"][0]
                assert sec["status"] == "ok", sec
                scr = sec["screen"]
                assert [b["alert"]["text"] for b in scr["banners"]] == ["Invoice closes soon"], scr["banners"]
                cells = scr["cells"]
                assert [c["view"] for c in cells] == ["list", "stat", "spark", "list", "table"], cells
                assert cells[1]["note"].endswith("· offline") and cells[1]["stat"]["trend"] == "up", cells[1]
                assert cells[2]["spark"]["points"] == [0, 1, 0.5], cells[2]
                assert cells[3]["problem"] is True and cells[3]["note"] == "unavailable: Demo is down", cells[3]
                assert cells[4]["problem"] is True and cells[4]["note"].startswith("unavailable"), cells[4]
                assert "stat" not in cells[4], cells[4]   # demo.money's stat data stays out of it
                item = cells[0]["groups"][0]["items"][0]
                # The high-risk action never reaches the view.
                assert item["title"] == "Pay invoice" and [a["id"] for a in item["actions"]] == ["tick"], item
                send(conn, MSG_TODAY_ACT, json.dumps({"section": "widgets", "rev": sec["rev"],
                                                      "action": "tick", "key": item["key"]}))
                result = wait_until(conn, MSG_TODAY_RESULT, lambda p: True, log)
                assert result["ok"] is True and result["message"] == "Done", result
                # The answer carries no data: the refetch shows the item gone.
                wait_until(conn, MSG_TODAY_STATE, lambda p: p.get("refreshing") is False
                           and not p["sections"][0]["screen"]["cells"][0]["groups"][0]["items"], log)
                cache = home / ".cache/trmnl-remarkable/today.json"
                assert cache.stat().st_mode & 0o777 == 0o600
                # Clear cache forgets Today's data too, in memory and on disk.
                send(conn, MSG_CLEAR_CACHE)
                cleared = wait_until(conn, MSG_TODAY_STATE, lambda p: all(
                    s["status"] == "none" for s in p["sections"]) and p.get("refreshing") is False, log)
                assert [s["id"] for s in cleared["sections"]] == ["widgets"], cleared
                assert "screen" not in cleared["sections"][0], cleared
                assert not cache.exists()
                wait_until(conn, MSG_STATUS, lambda p: p.get("message") == "Cache cleared", log)
                # The token reaches neither the QML nor the log.
                leaked = [(k, p) for k, p in log if "harness-token" in json.dumps(p)]
                assert not leaked, leaked
                assert "harness-token" not in (home / ".local/share/trmnl-remarkable/trmnl.log").read_text()
                send(conn, SYSTEM_TERMINATE)
                proc.wait(timeout=10)
                assert proc.returncode == 0
                print(json.dumps({"ok": True, "image": str(image_path), "bytes": image_bytes, "messages_seen": len(seen)}))
            finally:
                conn.close()
                if proc.poll() is None:
                    proc.send_signal(signal.SIGTERM)
                    proc.wait(timeout=10)
        finally:
            mock.terminate()
            mock.wait(timeout=10)
            gateway.shutdown()
            listener.close()


if __name__ == "__main__":
    main()
