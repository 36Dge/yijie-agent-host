#!/usr/bin/env python3
"""FEAT-158 fixed Runtime lifecycle qualification: no remote/model requests.

Uses only a new private directory, the existing pinned binary, and normal stdin
EOF. The local HTTP guard is an observer, not a model. No product entrypoint,
user history, credential, permission fault, or executable replacement is used.
Default mode sends no turns. --local-responses explicitly enables three benign
synthetic Responses exchanges to observe base-instruction routing, not AI quality.
"""

import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import queue
import shlex
import subprocess
import threading
import tempfile
import time
import uuid


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--local-responses", action="store_true",
                        help="opt in to three normal synthetic loopback Responses; no real model")
    parser.add_argument("--read-reference", action="store_true",
                        help="also read one self-created reference with an already exposed native command tool")
    parser.add_argument("--reopen", action="store_true",
                        help="also normally stop and reopen the isolated Runtime while the expert is selected")
    parser.add_argument("--permission-mode", choices=("retained-read-only", "ask", "auto", "full"),
                        default="retained-read-only", help="existing Host permission profile for this isolated qualification only")
    parser.add_argument("--installed-reference", type=Path,
                        help="read-only installed expert reference, already verified against its managed receipt")
    parser.add_argument("--reference-sha256", help="expected digest of that exact installed reference")
    args = parser.parse_args()
    if bool(args.installed_reference) != bool(args.reference_sha256):
        raise SystemExit("installed reference and its verified SHA-256 must be supplied together")
    if args.installed_reference and not args.read_reference:
        raise SystemExit("--installed-reference requires --read-reference")
    policies = {
        "retained-read-only": ("never", "user", "read-only", "readOnly"),
        "ask": ("on-request", "user", "workspace-write", "workspaceWrite"),
        "auto": ("on-request", "auto_review", "workspace-write", "workspaceWrite"),
        "full": ("never", "user", "danger-full-access", "dangerFullAccess"),
    }
    approval, reviewer, sandbox, effective_sandbox = policies[args.permission_mode]
    if args.read_reference and not args.local_responses:
        raise SystemExit("--read-reference requires the explicit --local-responses mode")
    if args.reopen and not args.local_responses:
        raise SystemExit("--reopen requires the explicit --local-responses mode")
    source_lock = Path(__file__).resolve().parents[1] / "api/runtime-chat-models.candidate.json"
    pinned = json.loads(source_lock.read_text())
    binary, manifest_path = args.binary.resolve(), args.manifest.resolve()
    manifest = json.loads(manifest_path.read_text())
    if digest(binary) != pinned["runtime_artifact"]["sha256"]:
        raise SystemExit("binary differs from existing Host runtime pin")
    if binary.stat().st_size != pinned["runtime_artifact"]["sizeBytes"]:
        raise SystemExit("binary size differs from existing Host runtime pin")
    if digest(manifest_path) != pinned["runtime_manifest_sha256"]:
        raise SystemExit("manifest differs from existing Host runtime pin")
    if manifest["runtime"] != pinned["runtime_artifact"]:
        raise SystemExit("manifest runtime identity differs from existing Host runtime pin")

    root = Path(tempfile.mkdtemp(prefix="feat158-native-no-turn-")).resolve()
    workspace, home = root / "workspace", root / "codex-home"
    workspace.mkdir(mode=0o700)
    home.mkdir(mode=0o700)
    reference = root / "managed-reference" / "reference.txt"
    reference_marker = "FEAT158_REFERENCE_ORDINARY_TEXT"
    if args.installed_reference:
        reference = args.installed_reference.resolve(strict=True)
        if not reference.is_file() or digest(reference) != args.reference_sha256:
            raise SystemExit("installed reference differs from its verified pin")
        reference_marker = next(line for line in reference.read_text().splitlines() if line.strip())
    elif args.read_reference:
        reference.parent.mkdir(mode=0o700)
        reference.write_text(reference_marker + "\nA benign local reference for native read qualification.\n")
    observed = []
    read_turn_requested = False
    reference_call_emitted = False
    reference_call_id = "call_feat158_reference"
    expected_requests = (5 if args.read_reference else 3) + int(args.reopen)
    report = {
        "feature": "FEAT-158", "kind": "fixed-runtime-local-responses" if args.local_responses else "fixed-runtime-no-turn-lifecycle",
        "runtime_sha256": manifest["runtime"]["sha256"],
        "runtime_manifest_sha256": digest(manifest_path),
        "root": str(root), "paid_calls": 0, "turn_start_calls": 0,
        "permission_mode": args.permission_mode,
        "permission_policy_source": "internal/codex/runtime_permissions.go:permissionPolicy (ask/auto/full)",
        "reference": str(reference) if args.read_reference else None,
        "reference_sha256": digest(reference) if args.read_reference else None,
        "installed_reference": bool(args.installed_reference),
        "http_requests": observed, "steps": [], "passed": False,
        "role_effectiveness": "NOT RUN: no real Provider or model evaluation",
        "resource_readability": "NOT RUN: no tool execution",
    }

    class Guard(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            observed.append({"method": "GET", "path": self.path})
            self.send_error(503, "No model request is authorized in this qualification")

        def do_POST(self):
            nonlocal reference_call_emitted
            if not args.local_responses:
                observed.append({"method": "POST", "path": self.path})
                self.send_error(503, "No model request is authorized in this qualification")
                return
            length = int(self.headers.get("Content-Length", "0"))
            if self.path != "/v1/responses" or not 0 < length <= 4 * 1024 * 1024 or len(observed) >= expected_requests:
                observed.append({"method": "POST", "path": self.path, "unexpected": True})
                self.send_error(400, "Outside the finite normal qualification exchanges")
                return
            body = json.loads(self.rfile.read(length))
            observed.append({"method": "POST", "path": self.path,
                             "model": body.get("model"), "instructions": body.get("instructions"),
                             "system_input": [item for item in body.get("input", [])
                                              if item.get("role") == "system"]})
            rid = "resp_" + uuid.uuid4().hex
            item = {"id": "msg_" + uuid.uuid4().hex, "type": "message", "role": "assistant",
                    "phase": "final_answer", "status": "completed", "content": [
                        {"type": "output_text", "text": "LOCAL_SYNTHETIC_OK", "annotations": []}]}
            if read_turn_requested and not reference_call_emitted:
                available = {tool.get("name"): tool for tool in body.get("tools", [])}
                name = next((candidate for candidate in ("exec_command", "shell_command") if candidate in available), None)
                if name is None:
                    report["reference_failure"] = "no supported native command tool exposed; no tool override attempted"
                    self.send_error(400, "Native command tool unavailable")
                    return
                properties = available[name].get("parameters", {}).get("properties", {})
                arguments = {"cmd" if name == "exec_command" else "command": "/bin/cat " + shlex.quote(str(reference)),
                             "workdir": str(workspace)}
                if "login" in properties:
                    arguments["login"] = False
                if name == "shell_command":
                    arguments["timeout_ms"] = 1000
                else:
                    arguments["yield_time_ms"] = 1000
                    arguments["max_output_tokens"] = 1000
                item = {"id": "fc_" + uuid.uuid4().hex, "type": "function_call", "status": "completed",
                        "name": name, "call_id": reference_call_id, "arguments": json.dumps(arguments)}
                reference_call_emitted = True
                report["reference_command_tool"] = name
            elif read_turn_requested:
                outputs = [item.get("output") for item in body.get("input", [])
                           if item.get("type") == "function_call_output" and item.get("call_id") == reference_call_id]
                if outputs:
                    report["reference_function_output_contains_marker"] = reference_marker in json.dumps(outputs, ensure_ascii=False)
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            events = [
                {"type": "response.created", "response": {"id": rid, "status": "in_progress", "output": []}},
                {"type": "response.output_item.added", "output_index": 0, "item": item},
                {"type": "response.output_item.done", "output_index": 0, "item": item},
                {"type": "response.completed", "response": {"id": rid, "status": "completed", "output": [item],
                    "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
            ]
            for event in events:
                self.wfile.write(("data: " + json.dumps(event) + "\n\n").encode())
            self.wfile.flush()

    guard = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Guard)
    guard_thread = threading.Thread(target=guard.serve_forever, daemon=True)
    guard_thread.start()
    base = f"http://127.0.0.1:{guard.server_port}/v1"
    # Ordinary existing Responses provider configuration, with no credential or
    # remote URL. The server allows only the explicitly selected finite exchanges.
    config = [
        'model="feat158-no-turn"', 'model_provider="feat158_local_guard"',
        'approval_policy=' + json.dumps(approval), 'sandbox_mode=' + json.dumps(sandbox),
        '[features]', 'hooks=false', 'plugins=false', 'apps=false',
        'shell_snapshot=false', 'tool_suggest=false', 'memories=false',
        'multi_agent=false',
        '[model_providers.feat158_local_guard]', 'name="Local no-turn guard"',
        'base_url=' + json.dumps(base), 'wire_api="responses"',
        'requires_openai_auth=false', 'supports_websockets=false',
        'request_max_retries=0', 'stream_max_retries=0',
    ]
    (home / "config.toml").write_text("\n".join(config) + "\n")
    ordinary = "FEAT158_ORDINARY_BASE: follow the current task and native permission policy."
    expert = ordinary + "\nFEAT158_SELECTED_ROLE: organize benign reference notes."
    log = (root / "runtime.stderr.log").open("w")
    proc = None
    messages = queue.Queue()
    request_id = 0
    notifications = []

    def read_stdout():
        try:
            for line in proc.stdout:
                messages.put(json.loads(line))
        except Exception as error:
            messages.put({"reader_error": type(error).__name__})
        finally:
            messages.put(None)

    def rpc(method, params):
        nonlocal request_id
        if method == "turn/start" and not args.local_responses:
            raise RuntimeError("turn/start is outside this qualification")
        if method == "turn/start":
            report["turn_start_calls"] += 1
        request_id += 1
        ident = request_id
        proc.stdin.write(json.dumps({"id": ident, "method": method, "params": params}) + "\n")
        proc.stdin.flush()
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            value = messages.get(timeout=max(0.01, deadline - time.monotonic()))
            if value is None or "reader_error" in value:
                raise RuntimeError("Runtime stdout closed or became unreadable")
            if "id" in value and "method" in value:
                proc.stdin.write(json.dumps({"id": value["id"], "error": {
                    "code": -32601, "message": "No reverse action authorized"}}) + "\n")
                proc.stdin.flush()
                raise RuntimeError("unexpected Runtime reverse request")
            if value.get("id") == ident:
                if "error" in value:
                    raise RuntimeError(method + ": " + json.dumps(value["error"]))
                return value["result"]
            notifications.append(value)
        raise RuntimeError(method + " timed out")

    def normal_params(instructions):
        return {
            "model": "feat158-no-turn", "modelProvider": "feat158_local_guard",
            "cwd": str(workspace), "approvalPolicy": approval, "approvalsReviewer": reviewer, "sandbox": sandbox,
            "baseInstructions": instructions,
            "developerInstructions": "Preserve native permission policy.",
        }

    def verify_policy(value):
        if (value["approvalPolicy"] != approval or value["approvalsReviewer"] != reviewer
                or value["sandbox"]["type"] != effective_sandbox):
            raise RuntimeError("Runtime effective permission policy differs from requested Host profile")
        if effective_sandbox == "workspaceWrite" and value["sandbox"]["networkAccess"] is not False:
            raise RuntimeError("workspace network unexpectedly enabled")

    def complete_local_turn(thread, expected_base):
        value = rpc("turn/start", {"threadId": thread, "input": [
            {"type": "text", "text": "Read the explicitly provided benign local reference once." if read_turn_requested
             else "Return the benign local fixture text without tools."}]})
        turn = value["turn"]["id"]
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            for notification in list(notifications):
                if notification.get("method") == "turn/completed" and notification["params"]["turn"]["id"] == turn:
                    if notification["params"]["turn"]["status"] != "completed":
                        raise RuntimeError("local synthetic turn did not complete")
                    if observed[-1].get("instructions") != expected_base:
                        raise RuntimeError("observed base instructions differ from current complete baseline")
                    return
            incoming = messages.get(timeout=max(0.01, deadline - time.monotonic()))
            if incoming is None:
                raise RuntimeError("Runtime closed during local exchange")
            if "id" in incoming and "method" in incoming:
                proc.stdin.write(json.dumps({"id": incoming["id"], "error": {
                    "code": -32601, "message": "No reverse action authorized"}}) + "\n")
                proc.stdin.flush()
                raise RuntimeError("unexpected action in benign local exchange")
            notifications.append(incoming)
        raise RuntimeError("local synthetic turn completion timed out")

    try:
        # A whitelist environment cannot read the user's credential variables or
        # startup files. HOME and CODEX_HOME are private to this child only.
        env = {"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "HOME": str(root),
               "CODEX_HOME": str(home), "RUST_LOG": "error"}
        proc = subprocess.Popen(
            [str(binary), "app-server", "--listen", "stdio://", "--strict-config"],
            cwd=workspace, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=log, text=True, bufsize=1,
        )
        report["pid"] = proc.pid
        reader_thread = threading.Thread(target=read_stdout, daemon=True)
        reader_thread.start()
        rpc("initialize", {"clientInfo": {"name": "yijie_feat158_no_turn", "version": "0.1.0"},
                           "capabilities": {"experimentalApi": False}})
        proc.stdin.write(json.dumps({"method": "initialized", "params": {}}) + "\n")
        proc.stdin.flush()
        initial = rpc("thread/start", dict(normal_params(ordinary), ephemeral=False))
        verify_policy(initial)
        thread = initial["thread"]["id"]
        report["steps"].append({"step": "ordinary_start", "thread_id": thread,
                                "approval_policy": initial["approvalPolicy"], "approvals_reviewer": initial["approvalsReviewer"],
                                "sandbox": initial["sandbox"]})
        if args.local_responses:
            complete_local_turn(thread, ordinary)
        for name, instructions in [("expert_apply", expert), ("expert_remove", ordinary)]:
            status = rpc("thread/read", {"threadId": thread, "includeTurns": False})
            report["steps"].append({"step": name + "_before", "status": status["thread"]["status"]})
            rpc("thread/unsubscribe", {"threadId": thread})
            resumed = rpc("thread/resume", dict(normal_params(instructions), threadId=thread))
            if resumed["thread"]["id"] != thread:
                raise RuntimeError("normal resume changed native thread identity")
            verify_policy(resumed)
            report["steps"].append({"step": name, "same_thread": True,
                                    "base_instruction_sent_sha256": hashlib.sha256(instructions.encode()).hexdigest(),
                                    "instruction_effective_receipt": "unavailable in native response"})
            if args.local_responses:
                complete_local_turn(thread, instructions)
                report["steps"][-1]["loopback_base_matches"] = True
                if name == "expert_apply" and args.read_reference:
                    read_turn_requested = True
                    complete_local_turn(thread, instructions)
                    read_turn_requested = False
                    commands = [value["params"]["item"] for value in notifications
                                if value.get("method") == "item/completed"
                                and value.get("params", {}).get("item", {}).get("type") == "commandExecution"]
                    if not report.get("reference_function_output_contains_marker") or not any(
                        item.get("exitCode") == 0 and reference_marker in item.get("aggregatedOutput", "") for item in commands
                    ):
                        raise RuntimeError("native read did not return the verified reference marker")
                    if digest(reference) != report["reference_sha256"]:
                        raise RuntimeError("reference changed during read qualification")
                    report["resource_readability"] = (
                        "PASS: exact installed expert reference outside workspace, existing native tool, verified effective " + args.permission_mode
                        if args.installed_reference else
                        "PASS: own ordinary reference outside workspace, existing native tool; actual installed package NOT RUN")
                if name == "expert_apply" and args.reopen:
                    proc.stdin.close()
                    try:
                        stopped = proc.wait(timeout=25)
                    except subprocess.TimeoutExpired:
                        report["normal_stop_pending"] = True
                        raise RuntimeError("normal EOF stop pending; no second Runtime started")
                    if stopped != 0:
                        raise RuntimeError("normal EOF stop failed; no second Runtime started")
                    reader_thread.join(timeout=2)
                    if reader_thread.is_alive():
                        raise RuntimeError("closed Runtime stdout still active; no second Runtime started")
                    report["first_normal_eof_exit"] = stopped
                    report["first_pid"] = proc.pid
                    messages = queue.Queue()
                    notifications = []
                    proc = subprocess.Popen(
                        [str(binary), "app-server", "--listen", "stdio://", "--strict-config"],
                        cwd=workspace, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=log, text=True, bufsize=1,
                    )
                    report["pid"] = proc.pid
                    reader_thread = threading.Thread(target=read_stdout, daemon=True)
                    reader_thread.start()
                    rpc("initialize", {"clientInfo": {"name": "yijie_feat158_no_turn", "version": "0.1.0"},
                                       "capabilities": {"experimentalApi": False}})
                    proc.stdin.write(json.dumps({"method": "initialized", "params": {}}) + "\n")
                    proc.stdin.flush()
                    cold = rpc("thread/resume", dict(normal_params(instructions), threadId=thread))
                    if cold["thread"]["id"] != thread:
                        raise RuntimeError("cold resume changed thread identity")
                    verify_policy(cold)
                    complete_local_turn(thread, instructions)
                    report["steps"].append({"step": "normal_eof_reopen_expert", "same_thread": True,
                                            "loopback_base_matches": True})
        report["passed"] = True
    except Exception as error:
        report["failure"] = str(error) or type(error).__name__
    finally:
        if proc is not None:
            try:
                if proc.stdin is not None:
                    proc.stdin.close()
            except (BrokenPipeError, OSError):
                pass
            try:
                report["normal_eof_exit"] = proc.wait(timeout=25)
            except subprocess.TimeoutExpired:
                report["normal_stop_pending"] = True
                report["passed"] = False
        guard.shutdown()
        guard.server_close()
        log.close()
        # Read only synthetic files created under this invocation's private home.
        metadata = []
        if report.get("normal_eof_exit") == 0:
            for path in home.rglob("*.jsonl"):
                for line in path.read_text().splitlines():
                    row = json.loads(line)
                    if row.get("type") == "session_meta":
                        payload = row.get("payload", {})
                        value = payload.get("base_instructions")
                        metadata.append({"path": str(path.relative_to(home)),
                                         "base_instructions": value})
            report["synthetic_session_metadata"] = metadata
        if (not args.local_responses and observed) or (args.local_responses and len(observed) != expected_requests) or report.get("normal_eof_exit") != 0:
            report["passed"] = False
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
