"""Local-only Q2 sampling and diagnostics; composes the frozen v1 primitives.

No provider client, credential argument, paid grant or request rewriting is
introduced. Product policy/Runtime remain the authority for actual tools.
"""
import copy
import hashlib
import hmac
import json
from pathlib import Path
import threading
import time

import feat175_q2_meter as v1

PACKAGE = v1.HOST / "internal/codex/testdata/feat175-q2-v2"
CODES = {"model_total_limit", "model_model_limit", "model_task_limit", "mcp_task_limit",
         "run_stopped", "no_active_task", "binding_unobserved", "model_mismatch",
         "sampling_boundary_closed", "sample_signal_timeout", "settlement_pending",
         "source_changed", "invalid_protocol", "invalid_control", "unknown_outcome",
         "observation_limit", "owner_stop", "driver_finished", "sampling_identity_conflict"}


class Denied(ValueError):
    pass


class Meter(v1.Meter):
    def __init__(self, root, *, mode="first_action", scenario="complete"):
        v1.require(mode in ("first_action", "full_turn"), "Unknown sampling mode")
        v1.require(scenario in ("complete", "repeated-hosted", "mcp-quota"), "Unknown local scenario")
        v1.require(scenario == "complete" or mode == "full_turn", "Extra scenarios qualify full-turn diagnostics only")
        plan = json.loads((v1.PACKAGE / "cases.json").read_text())
        prices = json.loads((v1.PACKAGE / "pricing-snapshot.json").read_text())
        super().__init__(root, plan, prices, local=True)
        self.mode, self.scenario = mode, scenario
        self.v2_source_bytes = (PACKAGE / "source-lock.json").read_bytes()
        self.v2_sources = json.loads(self.v2_source_bytes)
        (self.root / "source-lock-v2.json").write_bytes(self.v2_source_bytes)
        self.request_context = threading.local()
        self.mcp_inflight = 0
        self.state.update(revision=2, sampling_mode=mode, local_scenario=scenario, journal=[], denials=[],
                          primary_denial=None, mcp_results=[], new_paid_calls_authorized=0,
                          source_lock_v2_sha256=v1.digest(self.v2_source_bytes))
        self.persist()

    def verify_frozen_sources(self):
        try:
            super().verify_frozen_sources()
            v1.require((PACKAGE / "source-lock.json").read_bytes() == self.v2_source_bytes, "V2 lock changed")
            for row in self.v2_sources["sources"]:
                file = (v1.WORKSPACE / row["file"]).resolve()
                file.relative_to(v1.WORKSPACE)
                v1.require(v1.file_digest(file) == row["sha256"], "V2 source changed")
        except (ValueError, OSError):
            self.deny("control", "source_changed")

    def journal(self, event, **fields):
        # Caller holds self.lock: sequence defines OBSERVED serialization order.
        row = {"sequence": len(self.state["journal"]) + 1, "monotonic_ns": time.monotonic_ns(), "event": event, **fields}
        self.state["journal"].append(row)
        return row

    def counts(self, index):
        rows = self.state["requests"]
        task = self.state["tasks"][index] if index is not None else None
        return {"model_total": len(rows), "model_same_model": sum(r["model"] == task["model"] for r in rows) if task else 0,
                "model_task": sum(r["task_index"] == index for r in rows) if task else 0,
                "mcp_task": task["mcp_calls"] if task else 0, "real_model_http": 0, "supplier_rest": 0}

    def deny(self, surface, code, *, index=None, identity=None, expected=False):
        assert surface in ("model", "mcp", "control") and code in CODES
        with self.changed:
            if index is None:
                index = self.state["active_task"]
            task = self.state["tasks"][index] if index is not None else {}
            identity = identity or {}
            # Only bounded protocol IDs, never args, prompts, exception text or secrets.
            binding = {k: identity[k] for k in ("thread_id", "turn_id", "item_id")
                       if isinstance(identity.get(k), str) and 0 < len(identity[k]) <= 128}
            row = self.journal("admission_denied", surface=surface, code=code, expected_sampling_stop=expected,
                               task_index=index, case_id=task.get("case_id"), model=task.get("model"),
                               binding=binding, counts=self.counts(index), caused_by=self.state["primary_denial"])
            self.state["denials"].append(copy.deepcopy(row))
            if not expected:
                if self.state["primary_denial"] is None:
                    self.state["primary_denial"] = row["sequence"]
                self.state["halted"] = True
                self.state["halt_reason"] = self.state["halt_reason"] or code
            self.request_context.diagnosed = True
            self.persist()
            self.changed.notify_all()
        raise Denied(code)

    def halt(self, reason):
        # v1's HTTP handler catches ValueError; do not turn a planned sample
        # boundary into a second generic run-wide failure or erase its cause.
        if reason == "Admission or control rejected":
            if getattr(self.request_context, "diagnosed", False):
                self.request_context.diagnosed = False
                return
            try:
                self.deny(getattr(self.request_context, "surface", "control"), "invalid_protocol")
            except Denied:
                return
        code = {"Normal owner stop": "owner_stop", "Execution driver ended": "driver_finished",
                "Incomplete usage, stream, model or hosted visibility; no zero-cost assumption": "unknown_outcome",
                "Observed soft stop reached; current request may exceed threshold": "observation_limit"}.get(reason, "owner_stop")
        with self.changed:
            self.journal("run_stop", code=code)
            super().halt(code)

    def arm(self, model, case, thread):
        with self.changed:
            if self.mcp_inflight:
                self.deny("control", "settlement_pending")
            super().arm(model, case, thread)
            task = self.state["tasks"][self.state["active_task"]]
            task.update(sampling_closed=False, interrupt_acknowledged=False)
            self.journal("task_armed", task_index=self.state["active_task"], model=model, case_id=case, thread_id=thread)
            self.persist()

    def bound_index(self, identity):
        matches = {r["task_index"] for r in self.state["requests"]
                   if r["thread_id"] == identity.get("thread_id") and r["turn_id"] == identity.get("turn_id")}
        return next(iter(matches)) if len(matches) == 1 else None

    def check_task(self, surface, identity):
        index = self.bound_index(identity)
        if index is not None and self.state["tasks"][index].get("sampling_closed"):
            task = self.state["tasks"][index]
            # Let the native interrupt return before delivering an expected
            # denial, so a transport error cannot race ahead of the normal stop.
            if not self.changed.wait_for(lambda: task["interrupt_acknowledged"] or self.state["halted"], 10):
                self.deny(surface, "sample_signal_timeout", index=index, identity=identity)
            self.deny(surface, "sampling_boundary_closed", index=index, identity=identity, expected=True)
        index = self.state["active_task"]
        if self.state["halted"]:
            self.deny(surface, "run_stopped", identity=identity)
        if index is None:
            self.deny(surface, "no_active_task", identity=identity)
        return index

    def admit(self, metadata):
        with self.changed:
            index = self.check_task("model", metadata)
            if not self.changed.wait_for(lambda: self.idle() or self.state["halted"] or self.state["tasks"][index].get("sampling_closed"), 30):
                self.deny("model", "settlement_pending", identity=metadata)
            index = self.check_task("model", metadata)
            counts = self.counts(index)
            if metadata["model"] != self.state["tasks"][index]["model"]:
                self.deny("model", "model_mismatch", identity=metadata)
            for code, value, maximum in [("model_total_limit", counts["model_total"], self.limits["model_http_requests"]),
                                         ("model_model_limit", counts["model_same_model"], self.limits["per_model_http_requests"]),
                                         ("model_task_limit", counts["model_task"], self.limits["per_task_http_requests"])]:
                if value >= maximum:
                    self.deny("model", code, identity=metadata)
            row = super().admit(metadata)
            event = self.journal("model_admitted", task_index=index, ordinal=row["ordinal"],
                                 thread_id=row["thread_id"], turn_id=row["turn_id"], counts=self.counts(index))
            row["admission_sequence"] = event["sequence"]
            self.persist()
            return row

    def sample(self, binding):
        with self.changed:
            index = self.bound_index(binding)
            if self.mode != "first_action" or index is None or index != self.state["active_task"]:
                self.deny("control", "binding_unobserved", identity=binding)
            task = self.state["tasks"][index]
            if binding.get("root_thread_id") != task["thread_id"] or not isinstance(binding.get("item_id"), str):
                self.deny("control", "sampling_identity_conflict", identity=binding)
            if task.get("sampling_closed"):
                if task["sample_trigger"] != binding:
                    self.deny("control", "sampling_identity_conflict", identity=binding)
                return task["sample_boundary"]
            event = self.journal("sampling_closed", task_index=index, binding=copy.deepcopy(binding), counts=self.counts(index))
            task.update(sampling_closed=True, sample_trigger=copy.deepcopy(binding),
                        sample_boundary={"closed": True, "sequence": event["sequence"]})
            self.persist()
            self.changed.notify_all()
            return task["sample_boundary"]

    def acknowledge_interrupt(self, binding):
        with self.changed:
            index = self.bound_index(binding)
            if index is None or index != self.state["active_task"] or not self.state["tasks"][index].get("sampling_closed"):
                self.deny("control", "binding_unobserved", identity=binding)
            self.state["tasks"][index]["interrupt_acknowledged"] = True
            self.journal("native_interrupt_returned", task_index=index)
            self.persist()
            self.changed.notify_all()
            return {"acknowledged": True}

    def fixture(self, name, args, meta):
        identity = {"thread_id": meta.get("threadId"), "turn_id": meta.get("x-codex-turn-metadata", {}).get("turn_id")}
        with self.changed:
            index = self.check_task("mcp", identity)
            if self.bound_index(identity) != index:
                self.deny("mcp", "binding_unobserved", identity=identity)
            task = self.state["tasks"][index]
            if task["mcp_calls"] >= self.limits["local_mcp_per_task"]:
                self.deny("mcp", "mcp_task_limit", identity=identity)
            if name not in {"kimi_web_search", "kimi_web_search_pro", "kimi_web_fetch"}:
                self.deny("mcp", "invalid_protocol", identity=identity)
            if self.mode == "full_turn":
                output = super().fixture(name, args, meta)
                self.journal("mcp_admitted", task_index=index, tool=name, binding=identity, counts=self.counts(index))
                self.state["mcp_results"].append({"task_index": index, "kind": "explicit_synthetic_content"})
                self.persist()
                return output
            task["mcp_calls"] += 1
            self.mcp_inflight += 1
            self.journal("mcp_admitted", task_index=index, tool=name, binding=identity, counts=self.counts(index))
            self.persist()
            try:
                # Do not feed fake source material back into a first-choice
                # experiment. Release only after native interrupt has returned.
                if not self.changed.wait_for(lambda: task["interrupt_acknowledged"] or self.state["halted"], 10):
                    self.deny("mcp", "sample_signal_timeout", identity=identity)
                self.state["mcp_results"].append({"task_index": index, "kind": "sampling_cancelled_no_source_body"})
                return {"error": {"code": "cancelled", "message": "网页检索已取消。"}}
            finally:
                self.mcp_inflight -= 1
                self.persist()
                self.changed.notify_all()

    def finish(self):
        with self.changed:
            if not self.changed.wait_for(lambda: self.mcp_inflight == 0, 12):
                self.deny("control", "settlement_pending")
            index = self.state["active_task"]
            result = super().finish()
            result["diagnostics"] = [copy.deepcopy(r) for r in self.state["denials"] if r["task_index"] == index]
            result["journal"] = [copy.deepcopy(r) for r in self.state["journal"] if r.get("task_index") == index]
            return result


def response_fixture(body, row, scenario):
    raw = v1.synthetic_response(body, row)
    events = [json.loads(line[5:]) for line in raw.splitlines() if line.startswith(b"data:")]
    output = events[-1]["response"]["output"]
    if scenario == "repeated-hosted" and row["case_id"] == "Q2-02":
        extra = copy.deepcopy(output[0])
        extra["id"] += "_second"
        extra["action"]["query"] = "Python official stable release date"
        output.insert(1, extra)
    if scenario == "mcp-quota" and row["case_id"] == "Q2-06" and row["case_request_number"] == (2 if row["model"] == "kimi-k3.1-pro" else 1):
        # Distinct ordinary documents exercise the quota without disabling the
        # Runtime's normal protection against duplicate pending operations.
        arguments = [{"url": f"https://www.rfc-editor.org/rfc/rfc{9110 + i}"} for i in range(5)]
        if row["model"] == "kimi-k3.1-pro":
            calls = [f"tools.mcp__yijie_web__kimi_web_fetch({json.dumps(args)})" for args in arguments]
            output[0]["arguments"] = json.dumps({"input": "text(await Promise.allSettled([" + ",".join(calls) + "]));"})
        else:
            original = copy.deepcopy(output[0])
            output[:] = [{**original, "id": original["id"] + str(i), "call_id": original["call_id"] + str(i),
                          "arguments": json.dumps(args)} for i, args in enumerate(arguments)]
    rebuilt = [events[0]]
    for i, item in enumerate(output):
        rebuilt.extend({"type": kind, "output_index": i, "item": item} for kind in ("response.output_item.added", "response.output_item.done"))
    rebuilt.append(events[-1])
    return b"".join(b"data: " + json.dumps(e).encode() + b"\n\n" for e in rebuilt)


def server_for(meter, control, runtime_bearer, mcp_token):
    # Reuse MCP catalogue/auth/JSON-RPC and the existing control endpoints.
    # Replace only the model endpoint with an explicitly LOCAL response factory;
    # this handler has no path that calls a provider or reads a provider key.
    server = v1.server_for(meter, control, runtime_bearer, mcp_token, {})
    base = server.RequestHandlerClass

    class Handler(base):
        def reply(self, status, value=None):
            try:
                super().reply(status, value)
            except (BrokenPipeError, ConnectionResetError):
                self.close_connection = True  # Ordinary native cancellation.

        def do_POST(self):
            meter.request_context.diagnosed = False
            meter.request_context.surface = "model" if self.path == "/v1/responses" else "mcp" if self.path == "/mcp" else "control"
            if self.path not in ("/v1/responses", "/control/sample", "/control/interrupt-returned"):
                return super().do_POST()
            row = None
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 < size <= 4 * 1024 * 1024:
                    meter.deny(meter.request_context.surface, "invalid_protocol")
                payload = self.rfile.read(size)
                if self.path.startswith("/control/"):
                    if not hmac.compare_digest(self.headers.get("X-FEAT175-Control", ""), control):
                        meter.deny("control", "invalid_control")
                    body = json.loads(payload)
                    result = meter.sample(body) if self.path == "/control/sample" else meter.acknowledge_interrupt(body)
                    self.reply(200, result)
                    return
                if not hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + runtime_bearer):
                    meter.deny("model", "invalid_protocol")
                body, meta = v1.inspect_request(payload, {k.lower(): v for k, v in self.headers.items()})
                row = meter.admit(meta)
                with meter.lock:
                    row["case_request_number"] = meter.counts(row["task_index"])["model_task"]
                    row["http_status"] = 200
            except (ValueError, KeyError, TypeError, AttributeError, OSError):
                meter.halt("Admission or control rejected")
                self.reply(429, {"error": {"message": "FEAT-175 local verification admission stopped"}})
                return
            raw = response_fixture(body, row, meter.scenario)
            observer = v1.Observer()
            observer.feed(raw)  # Full local response inventory, even if native interrupts.
            connected = True
            try:
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Content-Length", str(len(raw)))
                self.send_header("Connection", "close")
                self.end_headers()
                self.wfile.write(raw)
                self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                connected = False
            finally:
                self.close_connection = True
                observer.result.update(stream_complete=True, observer_valid=not observer.invalid and not observer.extra_invalid,
                                       response_sha256=hashlib.sha256(raw).hexdigest(), response_bytes=len(raw), client_connected_at_eof=connected)
                meter.settle(row, observer.result)

    server.RequestHandlerClass = Handler
    return server
