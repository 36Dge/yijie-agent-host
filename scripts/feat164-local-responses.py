#!/usr/bin/env python3
"""Ordinary FEAT-164 loopback fixture. Never connects to an external provider.

For local assembly checks only; these results cannot satisfy real-model G2V.
The executable under test is always the canonical compiled Runtime/App.
"""
import argparse
import http.server
import json
import threading
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--ledger", type=Path, required=True)
parser.add_argument("--port", type=int, default=18083)
args = parser.parse_args()
lock = threading.Lock()
ledger = {"scope": "ordinary_loopback_fixture_not_g2v", "external_requests": 0, "requests": [], "state": "starting"}
profiles = {"kimi-k3.1-pro": ("feat164-local-kimi", "max"), "MiniMax-M3": ("feat164-local-minimax", "high")}


def save():
    args.ledger.parent.mkdir(parents=True, exist_ok=True)
    args.ledger.write_text(json.dumps(ledger, indent=2) + "\n")


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path != "/health":
            self.send_error(404)
            return
        data = json.dumps({"fixture": "FEAT-164-loopback-only", "requests": len(ledger["requests"])}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        if self.path == "/shutdown":
            self.send_response(204)
            self.end_headers()
            threading.Thread(target=self.server.shutdown).start()
            return
        if self.path != "/v1/responses":
            self.send_error(404)
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 1024 * 1024:
                raise ValueError()
            body = json.loads(self.rfile.read(length))
            profile = profiles.get(body.get("model"))
            if profile is None or self.headers.get("Authorization") != "Bearer " + profile[0] or (body.get("reasoning") or {}).get("effort") != profile[1]:
                raise ValueError()
        except (ValueError, AttributeError, TypeError):
            self.send_error(400, "Ordinary local fixture profile mismatch")
            return
        with lock:
            if len(ledger["requests"]) >= 6:
                self.send_error(429, "Local fixture limit reached")
                return
            number = len(ledger["requests"]) + 1
            ledger["requests"].append({"ordinal": number, "model": body["model"], "effort": profile[1], "tool_choice": body.get("tool_choice"), "builtin_search_offered": any(t.get("type") == "web_search" for t in body.get("tools", []) if isinstance(t, dict))})
            save()
        identity = f"feat164_local_{number}"
        item = {"id": "message_" + identity, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": [{"type": "output_text", "text": "FEAT164 本地联调完成。这是合成响应，未访问真实模型。", "annotations": []}]}
        events = [
            {"type": "response.created", "response": {"id": identity, "status": "in_progress", "output": []}},
            {"type": "response.output_item.added", "output_index": 0, "item": item},
            {"type": "response.output_item.done", "output_index": 0, "item": item},
            {"type": "response.completed", "response": {"id": identity, "status": "completed", "output": [item], "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
        ]
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for event in events:
            self.wfile.write(("data: " + json.dumps(event) + "\n\n").encode())
            self.wfile.flush()


server = http.server.ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
ledger["state"] = "running"
save()
print("FEAT-164 loopback fixture ready; no external provider connection", flush=True)
try:
    server.serve_forever()
finally:
    server.server_close()
    ledger["state"] = "stopped_normally"
    save()
