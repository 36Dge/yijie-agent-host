"""FEAT-175 test-only meter. No product planner, supplier REST client or old grant.

The preserved request goes through Codex's existing provider endpoint override.
Money is an observed soft stop, never a claimed bound on an in-flight response.
"""
import copy
import datetime as dt
import hashlib
import hmac
import http.client
import http.server
import importlib.util
import json
import math
from pathlib import Path
import threading
import time
import uuid

HOST = Path(__file__).resolve().parents[1]
PACKAGE = HOST / "internal/codex/testdata/feat175-q2"
WORKSPACE = HOST.parent
_spec = importlib.util.spec_from_file_location(
    "feat175_shared_observation", WORKSPACE / "yijie-desktop/scripts/feat164-budget-meter.py")
shared = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(shared)  # main guarded; no credentials, network or old Budget.
MODELS = {"kimi-k3.1-pro": ("api.moonshot.cn", "max"), "MiniMax-M3": ("api.minimaxi.com", "high")}


def require(ok, reason):
    if not ok:
        raise ValueError(reason)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def file_digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            value.update(chunk)
    return value.hexdigest()


def now():
    return dt.datetime.now(dt.timezone.utc)


def save(path, value):
    # Only the newly created owner-only evidence directory is written.
    path = Path(path)
    temp = path.with_suffix(path.suffix + ".new")
    with temp.open("w", encoding="utf-8") as f:
        json.dump(value, f, ensure_ascii=False, indent=2)
        f.write("\n")
    temp.replace(path)


def validate_authorization(auth, prices, *, at=None):
    """No I/O except frozen public source reads; runs BEFORE credential loading."""
    require(auth.get("feature") == "FEAT-175" and auth.get("purpose") == "Q2-route-feasibility"
            and auth.get("status") == "approved", "Current FEAT-175 paid authorization absent")
    require(set(auth) == {"feature", "purpose", "status", "authorization_id", "package_sha256", "source_lock_sha256",
                         "pricing_sha256", "approved_by", "approval_source", "approved_at", "expires_at", "models",
                         "tasks", "model_http_requests", "per_model_http_requests", "per_task_http_requests",
                         "local_mcp_per_task", "hosted_observation_stop_count", "supplier_rest_calls", "money_mode",
                         "allow_inflight_overshoot", "soft_stop_micro_cny"}, "Closed, task-specific authorization required")
    require(str(uuid.UUID(auth["authorization_id"])) == auth["authorization_id"], "Canonical one-shot grant identity required")
    require(auth.get("package_sha256") == digest((PACKAGE / "cases.json").read_bytes())
            and auth.get("source_lock_sha256") == digest((PACKAGE / "source-lock.json").read_bytes())
            and auth.get("pricing_sha256") == shared.canonical_digest(prices), "Authorization sources differ")
    require(auth.get("approved_by") == "段成威" and isinstance(auth.get("approval_source"), str)
            and auth["approval_source"].strip(), "Explicit current user decision required")
    at = at or now()
    issued = dt.datetime.fromisoformat(auth.get("approved_at", ""))
    expires = dt.datetime.fromisoformat(auth.get("expires_at", ""))
    require(issued.tzinfo is not None and expires.tzinfo is not None
            and issued <= at < expires <= issued + dt.timedelta(hours=24), "Authorization expired or unbounded")
    for key, cap in {"tasks": 12, "model_http_requests": 48, "per_model_http_requests": 24,
                     "per_task_http_requests": 4, "local_mcp_per_task": 4,
                     "hosted_observation_stop_count": 24}.items():
        require(type(auth.get(key)) is int and 0 < auth[key] <= cap, "Invalid authorized " + key)
    require(auth.get("supplier_rest_calls") == 0, "Q2 never admits supplier REST")
    require(auth.get("money_mode") == "soft_stop_after_observation" and auth.get("allow_inflight_overshoot") is True,
            "Unchanged requests require explicit observed soft-stop acceptance")
    require(type(auth.get("soft_stop_micro_cny")) is int and 0 < auth["soft_stop_micro_cny"] <= 20_000_000,
            "Explicit positive soft stop, at most proposed 20 CNY, required")
    require(prices.get("status") == "official_public_snapshot" and prices.get("currency") == "CNY",
            "Reviewed official pricing snapshot required")
    checked = dt.datetime.fromisoformat(prices["checked_at"])
    require(checked.tzinfo is not None and dt.timedelta(0) <= at - checked <= dt.timedelta(days=7),
            "Pricing snapshot must be rechecked within seven days")
    require(auth.get("models") == list(MODELS), "Both exact Q2 model profiles required")


def inspect_request(payload, headers):
    """Inspect only. The caller forwards the ORIGINAL bytes, including tools/caps."""
    body = json.loads(payload)
    require(isinstance(body, dict) and body.get("model") in MODELS and body.get("stream") is True,
            "Unqualified model or streaming profile")
    model = body["model"]
    require(body.get("reasoning", {}).get("effort") == MODELS[model][1], "Reasoning profile changed")
    require(not any(body.get(k) for k in ("previous_response_id", "conversation", "background")),
            "Server context not qualified")
    require(body.get("service_tier") in (None, "default", "auto")
            and body.get("prompt_cache_options", {}).get("ttl", "5m") == "5m", "Billing tier not qualified")
    tools = body.get("tools", [])
    require(isinstance(tools, list) and all(isinstance(t, dict) and t.get("type") in
            ("function", "custom", "namespace", "tool_search", "web_search") for t in tools),
            "Unpriced hosted tool declaration")
    meta = body.get("client_metadata", {})
    require(isinstance(meta, dict), "Invalid native metadata")
    header_meta = json.loads(headers.get("x-codex-turn-metadata", "{}"))
    nested = json.loads(meta.get("x-codex-turn-metadata", "{}"))
    require(isinstance(header_meta, dict) and isinstance(nested, dict), "Invalid native identity")
    identity = {}
    for key in ("thread_id", "turn_id"):
        values = [v[key] for v in (meta, nested, header_meta) if key in v]
        if key == "thread_id" and headers.get("thread-id"):
            values.append(headers["thread-id"])
        require(values and all(isinstance(v, str) and 0 < len(v) <= 128 and v == values[0] for v in values),
                "Missing or inconsistent native " + key)
        identity[key] = values[0]
    return body, {"model": model, **identity, "request_sha256": digest(payload), "request_bytes": len(payload),
                  "request_adjustments": [], "native_search_declared": any(t["type"] == "web_search" for t in tools),
                  "tools_sha256": shared.canonical_digest(tools), "instructions_sha256": shared.canonical_digest(body.get("instructions")),
                  "max_output_tokens": body.get("max_output_tokens"), "include": body.get("include", [])}


class Observer(shared.Observer):
    """Reuse the bounded native Responses observer; retain enough billing proof."""
    def __init__(self):
        super().__init__()
        self.extra_buffer = b""
        self.extra_invalid = False

    def feed(self, data):
        super().feed(data)
        if self.extra_invalid:
            return
        self.extra_buffer += data
        if len(self.extra_buffer) > 2 * 1024 * 1024:
            self.extra_invalid = True
            self.extra_buffer = b""
            return
        while b"\n" in self.extra_buffer:
            line, self.extra_buffer = self.extra_buffer.split(b"\n", 1)
            if not line.startswith(b"data:") or line[5:].strip() == b"[DONE]":
                continue
            try:
                event = json.loads(line[5:])
                if event.get("type") not in ("response.completed", "response.incomplete", "response.failed"):
                    continue
                response = event["response"]
                output = response.get("output")
                require(isinstance(output, list) and all(isinstance(i, dict) for i in output), "Output missing")
                searches = [i for i in output if i.get("type") == "web_search_call"]
                self.result["hosted_output_complete"] = True
                self.result["hosted_item_ids"] = [i.get("id") for i in searches]
                self.result["hosted_actions"] = [i.get("action", {}).get("type") for i in searches]
                self.result["response_service_tier"] = response.get("service_tier")
                details = response.get("usage", {}).get("input_tokens_details", {})
                self.result["cached_input_tokens"] = details.get("cached_tokens")
            except (ValueError, TypeError, KeyError, AttributeError):
                self.extra_invalid = True


class Meter:
    def __init__(self, root, plan, prices, *, local, authorization=None, local_scenario="complete"):
        self.root, self.plan, self.prices, self.local = Path(root), plan, prices, local
        require(local_scenario == "complete" or local and local_scenario == "normal-stop", "Local scenario cannot affect live calls")
        self.local_scenario = local_scenario
        self.authorization = authorization
        self.source_bytes = (PACKAGE / "source-lock.json").read_bytes()
        self.sources = json.loads(self.source_bytes)
        if not local:
            validate_authorization(authorization, prices)
        limits = {"tasks": 12, "model_http_requests": 48, "per_model_http_requests": 24,
                  "per_task_http_requests": 4, "local_mcp_per_task": 4, "hosted_observation_stop_count": 24,
                  "soft_stop_micro_cny": 20_000_000} if local else authorization
        self.limits = limits
        self.lock = threading.RLock()
        self.changed = threading.Condition(self.lock)
        self.state = {"schema_version": 1, "feature": "FEAT-175", "kind": "synthetic" if local else "recorded-native",
                      "package_sha256": digest((PACKAGE / "cases.json").read_bytes()),
                      "source_lock_sha256": digest((PACKAGE / "source-lock.json").read_bytes()),
                      "pricing_sha256": shared.canonical_digest(prices), "money_mode": "soft_stop_after_observation",
                      "local_scenario": local_scenario if local else None,
                      "authorization_sha256": shared.canonical_digest(authorization) if authorization else None,
                      "requests": [], "tasks": [], "active_task": None, "halted": False, "halt_reason": None,
                      "observed_cost_upper_micro_cny": 0, "observed_hosted_calls": 0,
                      "real_model_http_requests": 0, "supplier_rest_calls": 0, "meter_state": "created"}
        require(not (self.root / "ledger.json").exists(), "Fresh independent ledger required; no resume or retry")
        (self.root / "source-lock.json").write_bytes(self.source_bytes)
        self.persist()

    def verify_frozen_sources(self):
        require((PACKAGE / "source-lock.json").read_bytes() == self.source_bytes, "Source lock changed during run")
        for row in self.sources["sources"]:
            path = (WORKSPACE / row["file"]).resolve()
            path.relative_to(WORKSPACE)
            require(file_digest(path) == row["sha256"], "Frozen execution source changed")
        require(file_digest(WORKSPACE / self.sources["runtime_relative_path"] / "codex") == self.sources["runtime_binary_sha256"],
                "Pinned Runtime changed")

    def persist(self):
        with self.lock:
            save(self.root / "ledger.json", self.state)

    def halt(self, reason):
        with self.lock:
            self.state["halted"] = True
            self.state["halt_reason"] = self.state["halt_reason"] or reason
            self.persist()
            self.changed.notify_all()

    def idle(self):
        return all(r["status"] != "inflight" for r in self.state["requests"])

    def arm(self, model, case, thread):
        with self.lock:
            self.verify_frozen_sources()
            require(not self.state["halted"] and self.idle() and self.state["active_task"] is None, "Meter not idle")
            require(model in MODELS and case in {c["id"] for c in self.plan["cases"]}
                    and isinstance(thread, str) and thread, "Unknown frozen task")
            require(len(self.state["tasks"]) < self.limits["tasks"] and not any(
                t["model"] == model and t["case_id"] == case for t in self.state["tasks"]), "No duplicate task attempts")
            row = {"model": model, "case_id": case, "thread_id": thread, "mcp_calls": 0, "status": "active"}
            self.state["tasks"].append(row)
            self.state["active_task"] = len(self.state["tasks"]) - 1
            self.persist()

    def admit(self, metadata):
        with self.changed:
            # Native next HTTP may arrive just before the prior handler has seen EOF.
            require(self.changed.wait_for(lambda: self.idle() or self.state["halted"], 30), "Previous response not settled")
            if not self.local:
                validate_authorization(self.authorization, self.prices)
            require(not self.state["halted"] and self.state["active_task"] is not None, "No active authorized task")
            index = self.state["active_task"]
            task = self.state["tasks"][index]
            rows = self.state["requests"]
            require(metadata["model"] == task["model"], "Auxiliary model changed")
            require(len(rows) < self.limits["model_http_requests"]
                    and sum(r["model"] == metadata["model"] for r in rows) < self.limits["per_model_http_requests"]
                    and sum(r["task_index"] == index for r in rows) < self.limits["per_task_http_requests"], "HTTP admission limit reached")
            require(self.state["observed_cost_upper_micro_cny"] < self.limits["soft_stop_micro_cny"]
                    and self.state["observed_hosted_calls"] < self.limits["hosted_observation_stop_count"], "Observation stop reached")
            row = {**metadata, "ordinal": len(rows) + 1, "task_index": index, "case_id": task["case_id"], "status": "inflight"}
            rows.append(row)
            if not self.local:
                self.state["real_model_http_requests"] += 1  # conservative: admitted even if connection fails.
            self.persist()  # BEFORE outbound HTTP, never refund an uncertain send.
            return row

    def settle(self, row, observation):
        with self.changed:
            row.update(observation)
            usage = observation.get("usage", {})
            valid = (row.get("http_status") == 200 and observation.get("stream_complete") is True
                     and observation.get("observer_valid") is True and observation.get("terminal_count") == 1
                     and observation.get("terminal") == "response.completed" and observation.get("response_status") == "completed"
                     and observation.get("response_model") == row["model"] and observation.get("hosted_output_complete") is True
                     and observation.get("response_service_tier") in (None, "default", "auto")
                     and observation.get("cache_ttl") == "5m"
                     and all(type(usage.get(k)) is int and 0 <= usage[k] <= 2_097_152 for k in ("input_tokens", "output_tokens")))
            searches = observation.get("native_search_calls")
            valid = valid and type(searches) is int and 0 <= searches <= 10000
            if not valid:
                row["status"] = "outcome_unknown"
                row["observed_cost_upper_micro_cny"] = None
                self.halt("Incomplete usage, stream, model or hosted visibility; no zero-cost assumption")
            else:
                rates = self.prices["models"][row["model"]]
                # Conservative accounting deliberately does not claim a cache discount.
                # One CNY/MTok equals one micro-CNY/token. Ceil fractional rates.
                fee = math.ceil(usage["input_tokens"] * rates["input_upper_cny_per_mtok"]
                                + usage["output_tokens"] * rates["output_upper_cny_per_mtok"])
                fee += searches * rates["hosted_search_micro_cny"]
                row["status"], row["observed_cost_upper_micro_cny"] = "settled", fee
                self.state["observed_cost_upper_micro_cny"] += fee
                self.state["observed_hosted_calls"] += searches
                if (self.state["observed_cost_upper_micro_cny"] >= self.limits["soft_stop_micro_cny"]
                        or self.state["observed_hosted_calls"] >= self.limits["hosted_observation_stop_count"]):
                    self.halt("Observed soft stop reached; current request may exceed threshold")
            self.persist()
            self.changed.notify_all()

    def finish(self):
        with self.changed:
            require(self.changed.wait_for(self.idle, 30), "Response settlement still pending")
            index = self.state["active_task"]
            require(index is not None, "No active task")
            rows = [r for r in self.state["requests"] if r["task_index"] == index]
            task = self.state["tasks"][index]
            task["status"] = "halted" if self.state["halted"] else "finished"
            self.state["active_task"] = None
            self.persist()
            return {"halted": self.state["halted"], "requests": copy.deepcopy(rows), "task": copy.deepcopy(task),
                    "hosted_visibility": "complete" if rows and all(r["status"] == "settled" for r in rows) else "unknown"}

    def fixture(self, name, args, meta):
        with self.lock:
            index = self.state["active_task"]
            require(not self.state["halted"] and index is not None, "No live test task")
            thread, turn = meta.get("threadId"), meta.get("x-codex-turn-metadata", {}).get("turn_id")
            require(any(r["task_index"] == index and r["thread_id"] == thread and r["turn_id"] == turn for r in self.state["requests"]),
                    "MCP native binding not observed on model request")
            task = self.state["tasks"][index]
            require(task["mcp_calls"] < self.limits["local_mcp_per_task"], "Local MCP task limit")
            task["mcp_calls"] += 1
            self.persist()
        return synthetic_tool_result(name, args)


def synthetic_tool_result(name, args):
    """Ordinary data fixture. Never fetches supplied URLs or queries a supplier."""
    marker = "Q2 SYNTHETIC LOCAL MATERIAL; not a real fetched source."
    if name == "kimi_web_fetch":
        require(isinstance(args.get("url"), str) and args["url"].startswith(("https://", "http://")), "Fixture URL required")
        return {"url": args["url"], "title": marker, "markdown": marker, "truncated": False}
    require(name in ("kimi_web_search", "kimi_web_search_pro") and isinstance(args.get("query"), str), "Fixture tool input")
    count = args.get("limit", 5)
    require(type(count) is int and 1 <= count <= 20, "Fixture result limit")
    results = []
    for i in range(count):
        row = {"title": f"{marker} {i + 1}", "url": f"https://example.com/q2/{i + 1}", "snippet": marker}
        if name == "kimi_web_search_pro":
            site = args.get("sites", ["example.com"])[0]
            row.update(url=f"https://{site}/q2-synthetic/{i + 1}", site_name=site,
                       date=args.get("time_window", {}).get("end", "2026-11-15"), authority="synthetic",
                       chunks=[{"text": marker, "score": 1}], content_status="available")
        elif args.get("include_content") is True:
            row.update(text=marker, content_status="available")
        results.append(row)
    result = {"results": results, "truncated": False}
    if name == "kimi_web_search_pro" or args.get("include_content") is True:
        result["dropped_results"] = 0
    return result


def synthetic_response(body, row):
    """Scripted loopback model response; proves wiring, never model selection."""
    case, model, n = row["case_id"], row["model"], row["case_request_number"]
    message = {"type": "message", "role": "assistant", "phase": "final_answer", "status": "completed",
               "content": [{"type": "output_text", "text": "Q2 synthetic local wiring complete.", "annotations": []}]}
    output = [message]
    if case == "Q2-02":
        output.insert(0, {"type": "web_search_call", "status": "completed", "action": {"type": "search", "query": "Python stable release"}})
    elif case not in ("Q2-01", "Q2-02"):
        tool = {"Q2-03": "kimi_web_search_pro", "Q2-04": "kimi_web_search", "Q2-05": "kimi_web_search", "Q2-06": "kimi_web_fetch"}[case]
        args = {"Q2-03": {"query": "Shopify API rate limits", "sites": ["shopify.dev"], "time_window": {"start": "2026-01-01", "end": "2026-11-15"}},
                "Q2-04": {"query": "Shopify webhook retry", "limit": 5, "include_content": False},
                "Q2-05": {"query": "WebP compression", "limit": 3, "include_content": True},
                "Q2-06": {"url": "https://www.rfc-editor.org/rfc/rfc9110"}}[case]
        if model == "kimi-k3.1-pro" and n == 1:
            name, arguments = "tool_search", {"query": "web research source page read", "limit": 3}
        elif n == (2 if model == "kimi-k3.1-pro" else 1):
            name = "exec" if model == "kimi-k3.1-pro" else "mcp__yijie_web__" + tool
            arguments = {"input": f"text(await tools.mcp__yijie_web__{tool}({json.dumps(args)}));"} if name == "exec" else args
        else:
            name = None
        if name:
            output = [{"type": "function_call", "name": name, "call_id": f"q2_call_{row['ordinal']}", "arguments": json.dumps(arguments)}]
    events = [{"type": "response.created", "response": {"id": f"q2_{row['ordinal']}", "status": "in_progress", "output": []}}]
    for i, item in enumerate(output):
        item["id"] = f"q2_item_{row['ordinal']}_{i}"
        events += [{"type": kind, "output_index": i, "item": item} for kind in ("response.output_item.added", "response.output_item.done")]
    events.append({"type": "response.completed", "response": {"id": f"q2_{row['ordinal']}", "model": model, "status": "completed", "output": output,
                   "usage": {"input_tokens": 100, "output_tokens": 10, "total_tokens": 110}}})
    return b"".join(b"data: " + json.dumps(e).encode() + b"\n\n" for e in events)


def server_for(meter, control, runtime_bearer, mcp_token, keys):
    catalogue = json.loads((HOST / "internal/contracts/builtinweb/tools.json").read_text())

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def reply(self, status, value=None):
            payload = b"" if value is None else json.dumps(value).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def do_POST(self):
            self.connection.settimeout(300)
            row = None
            try:
                size = int(self.headers.get("Content-Length", "0"))
                require(0 < size <= 4 * 1024 * 1024, "Bounded JSON body required")
                payload = self.rfile.read(size)
                require(len(payload) == size, "Incomplete request")
                if self.path.startswith("/control/"):
                    require(hmac.compare_digest(self.headers.get("X-FEAT175-Control", ""), control), "Control capability required")
                    body = json.loads(payload)
                    if self.path == "/control/arm":
                        meter.arm(body["model"], body["case_id"], body["thread_id"])
                        self.reply(200, {"armed": True})
                    elif self.path == "/control/finish":
                        self.reply(200, meter.finish())
                    elif self.path == "/control/stop":
                        meter.halt("Normal owner stop")
                        self.reply(200, {"stopping": True})
                    elif self.path == "/control/status":
                        with meter.lock:
                            self.reply(200, {"halted": meter.state["halted"]})
                    else:
                        raise ValueError("Unknown control route")
                    return
                if self.path == "/mcp":
                    require(hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + mcp_token), "MCP capability required")
                    req = json.loads(payload)
                    if "id" not in req:
                        self.reply(202)
                        return
                    if req["method"] == "initialize":
                        result = {"protocolVersion": "2025-03-26", "capabilities": {"tools": {}}, "serverInfo": {"name": "feat175-q2-synthetic", "version": "1"}}
                    elif req["method"] == "tools/list":
                        result = {"tools": catalogue}
                    elif req["method"] == "ping":
                        result = {}
                    elif req["method"] == "tools/call":
                        p = req["params"]
                        output = meter.fixture(p["name"], p.get("arguments", {}), p.get("_meta", {}))
                        result = {"content": [{"type": "text", "text": json.dumps(output)}], "structuredContent": output, "isError": False}
                    else:
                        raise ValueError("Unknown MCP method")
                    self.reply(200, {"jsonrpc": "2.0", "id": req["id"], "result": result})
                    return
                require(self.path == "/v1/responses" and hmac.compare_digest(
                    self.headers.get("Authorization", ""), "Bearer " + runtime_bearer), "Fixed model route required")
                body, meta = inspect_request(payload, {k.lower(): v for k, v in self.headers.items()})
                row = meter.admit(meta)
                with meter.lock:
                    row["case_request_number"] = sum(r["task_index"] == row["task_index"] for r in meter.state["requests"])
            except (ValueError, KeyError, TypeError, AttributeError, OSError):
                meter.halt("Admission or control rejected")
                self.reply(429, {"error": {"message": "FEAT-175 verification admission stopped"}})
                return
            observer, connection, complete = Observer(), None, False
            response_hash, response_bytes, error_bytes = hashlib.sha256(), 0, bytearray()
            client_connected = True
            try:
                if meter.local:
                    # No connection factory, credentials or provider DNS in local mode.
                    with meter.lock:
                        row["http_status"] = 200
                    generated = synthetic_response(body, row)
                    if meter.local_scenario == "normal-stop":
                        def ordinary_stop():
                            created, remaining = generated.split(b"\n\n", 1)
                            yield created + b"\n\n"
                            meter.halt("Normal local owner-stop qualification")
                            # An ordinary slow response gives the existing native
                            # interrupt path time to finish. No forced process exit.
                            time.sleep(2)
                            yield remaining
                        chunks = ordinary_stop()
                    else:
                        chunks = iter([generated])
                    content_type = "text/event-stream"
                else:
                    connection = http.client.HTTPSConnection(MODELS[row["model"]][0], timeout=240)
                    connection.request("POST", "/v1/responses", body=payload,
                                       headers={"Authorization": "Bearer " + keys[row["model"]], "Content-Type": "application/json"})
                    response = connection.getresponse()
                    with meter.lock:
                        row["http_status"] = response.status
                    content_type = response.getheader("Content-Type", "")
                    chunks = iter(lambda: response.read1(65536), b"")
                try:
                    self.send_response(row["http_status"])
                    self.send_header("Content-Type", content_type)
                    self.send_header("Connection", "close")
                    self.end_headers()
                except OSError:
                    client_connected = False
                self.close_connection = True
                for chunk in chunks:
                    response_hash.update(chunk)
                    response_bytes += len(chunk)
                    observer.feed(chunk)
                    if row["http_status"] >= 400:
                        error_bytes.extend(chunk[:max(0, 16385 - len(error_bytes))])
                    if client_connected:
                        try:
                            self.wfile.write(chunk)
                            self.wfile.flush()
                        except OSError:
                            client_connected = False  # Drain usage; never resend after a disconnect.
                complete = True
            except (OSError, http.client.HTTPException, ValueError, TypeError):
                client_connected = False
            finally:
                if connection:
                    connection.close()
                observer.result.update({"stream_complete": complete, "observer_valid": not observer.invalid and not observer.extra_invalid
                                        and not observer.buffer.strip() and not observer.extra_buffer.strip(),
                                        "response_sha256": response_hash.hexdigest(), "response_bytes": response_bytes,
                                        "client_connected_at_eof": client_connected})
                if row.get("http_status", 0) >= 400:
                    observer.result.update(shared.error_metadata(error_bytes))
                meter.settle(row, observer.result)

    return http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
