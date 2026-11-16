"""A0 QA admission: native bindings, permanent closed roots, staged accounting.

Nothing here plans model work. Bindings arrive only on the private control path
after the driver resolves the real native active turn/ancestry.
"""
import copy
import json
import threading
import time

import feat175_q2_v2_meter as v2
from feat175_q2_a0_policy import PACKAGE, POLICY, validate_grant, verify_sources

v1 = v2.v1
CODES = v2.CODES | {"binding_unverified", "binding_conflict", "turn_inactive", "root_closed", "unowned_root",
                     "case_order_mismatch", "review_required", "stage_not_authorized", "route_gate_failed",
                     "native_barrier_pending", "provider_http_error", "provider_tool_error", "stream_deadline",
                     "stream_idle", "late_binding_after_finish", "driver_error", "grant_invalid"}


class Meter(v2.Meter):
    def __init__(self, root, *, mode="local", grant=None, binding_wait=None):
        v1.require(mode in ("local", "live"), "Unknown execution mode")
        if mode == "live":
            validate_grant(grant)
        else:
            v1.require(grant is None, "Local mode does not accept or consume a live grant")
        super().__init__(root, mode="first_action", scenario="complete")
        self.execution_mode, self.grant = mode, grant
        self.a0_source_bytes = (PACKAGE / "source-lock.json").read_bytes()
        (self.root / "source-lock-a0.json").write_bytes(self.a0_source_bytes)
        self.roots, self.bindings = {}, {}
        self.binding_wait = POLICY["deadlines_seconds"]["binding"] if binding_wait is None else binding_wait
        v1.require(mode == "local" or binding_wait is None, "Live timing cannot be overridden")
        self.stage_index = 0
        self.max_tasks = 12 if mode == "local" or "A2" in grant["stages"] else 4
        self.state.update(revision=3, kind="synthetic" if mode == "local" else "recorded-native",
                          execution_mode=mode, generation=None, bindings=[], reviews=[], phase="A1",
                          authorization_sha256=v1.shared.canonical_digest(grant) if grant else None,
                          new_paid_calls_authorized=48 if mode == "live" and self.max_tasks == 12 else 16 if mode == "live" else 0,
                          source_lock_a0_sha256=v1.digest(self.a0_source_bytes))
        self.update_limits()
        self.persist()

    def update_limits(self):
        stage = POLICY["stages"][self.stage_index]
        self.limits = {"tasks":stage["tasks"], "model_http_requests":stage["model_http"],
                       "per_model_http_requests":stage["per_model_http"], "per_task_http_requests":POLICY["per_task_http"],
                       "local_mcp_per_task":POLICY["per_task_mcp"], "hosted_observation_stop_count":stage["hosted_stop"],
                       "soft_stop_micro_cny":stage["soft_stop_micro_cny"]}

    def verify_frozen_sources(self):
        try:
            super().verify_frozen_sources()
            v1.require((PACKAGE / "source-lock.json").read_bytes() == self.a0_source_bytes, "A0 lock changed")
            verify_sources()
        except (ValueError, OSError):
            self.deny("control", "source_changed")

    def counts(self, index):
        counts = super().counts(index)
        counts["real_model_http"] = self.state["real_model_http_requests"]
        return counts

    def deny(self, surface, code, *, index=None, identity=None, expected=False):
        assert surface in ("model", "mcp", "control") and code in CODES
        with self.changed:
            # Deliberately NO active_task fallback for an unknown identity.
            task = self.state["tasks"][index] if index is not None else {}
            identity = identity or {}
            binding = {k:identity[k] for k in ("thread_id", "turn_id", "item_id")
                       if isinstance(identity.get(k),str) and 0 < len(identity[k]) <= 128}
            row = self.journal("admission_denied", surface=surface, code=code, expected_sampling_stop=expected,
                               task_index=index, case_id=task.get("case_id"), model=task.get("model"), binding=binding,
                               counts=self.counts(index), caused_by=self.state["primary_denial"])
            self.state["denials"].append(copy.deepcopy(row))
            if not expected:
                self.state["primary_denial"] = self.state["primary_denial"] or row["sequence"]
                self.state["halted"] = True
                self.state["halt_reason"] = self.state["halt_reason"] or code
            self.request_context.diagnosed = True
            self.persist(); self.changed.notify_all()
        raise v2.Denied(code)

    def halt(self, reason):
        if reason == "Admission or control rejected":
            if getattr(self.request_context,"diagnosed",False):
                self.request_context.diagnosed = False
                return
            try:
                self.deny(getattr(self.request_context,"surface","control"),"invalid_protocol")
            except v2.Denied:
                return
        code = {"Normal owner stop":"owner_stop", "Execution driver ended":"driver_finished",
                "Incomplete usage, stream, model or hosted visibility; no zero-cost assumption":"unknown_outcome",
                "Observed soft stop reached; current request may exceed threshold":"observation_limit"}.get(reason,reason)
        if code not in CODES: code = "driver_error"
        with self.changed:
            self.journal("run_stop", code=code)
            for task in self.state["tasks"]:
                if not task.get("root_closed"):
                    self.close_root(task["thread_id"],code)
            v1.Meter.halt(self,code)

    def arm(self, model, case, thread):
        with self.changed:
            self.verify_frozen_sources()
            n = len(self.state["tasks"])
            if self.state["halted"]: self.deny("control","run_stopped")
            if self.state["active_task"] is not None or not self.idle() or self.mcp_inflight:
                self.deny("control","native_barrier_pending")
            if n and not self.state["tasks"][-1].get("reviewed"):
                self.deny("control","review_required",index=n-1)
            if n >= self.max_tasks or n >= self.limits["tasks"]:
                self.deny("control","stage_not_authorized")
            if {"model":model,"case_id":case} != POLICY["schedule"][n] or thread in self.roots:
                self.deny("control","case_order_mismatch")
            task = {"model":model,"case_id":case,"thread_id":thread,"mcp_calls":0,"status":"active",
                    "root_closed":False,"sampling_closed":False,"interrupt_acknowledged":False,
                    "reviewed":False,"late_bindings":0}
            self.state["tasks"].append(task); self.state["active_task"] = n; self.roots[thread] = n
            self.journal("task_armed",task_index=n,model=model,case_id=case,thread_id=thread)
            self.persist(); self.changed.notify_all()

    @staticmethod
    def binding_key(identity):
        return (identity.get("thread_id"),identity.get("turn_id"))

    def bind_native(self, value):
        """Private control receipt, never a model-supplied owner assertion."""
        with self.changed:
            root = value.get("root_thread_id")
            index = self.roots.get(root)
            if index is None: self.deny("control","unowned_root",identity=value)
            if (value.get("generation") != self.state["generation"] or value.get("state") not in ("started","ended")
                    or type(value.get("native_sequence")) is not int or value["native_sequence"] <= 0
                    or any(not isinstance(value.get(k),str) or not 0 < len(value[k]) <= 128 for k in ("thread_id","turn_id"))):
                self.deny("control","binding_conflict",index=index,identity=value)
            key = self.binding_key(value); existing = self.bindings.get(key); task = self.state["tasks"][index]
            if existing and existing["task_index"] != index:
                self.deny("control","binding_conflict",index=index,identity=value)
            if existing and value["native_sequence"] <= existing["native_sequence"]:
                return {"accepted":existing["active"] and not task["root_closed"],"duplicate":True}
            if existing and existing["ended"] and value["state"] == "started":
                self.deny("control","binding_conflict",index=index,identity=value)
            late = existing is None and task["root_closed"]
            binding = {"task_index":index,"root_thread_id":root,"thread_id":key[0],"turn_id":key[1],
                       "native_sequence":value["native_sequence"],"active":value["state"]=="started" and not task["root_closed"],
                       "ended":value["state"]=="ended","native_status":value.get("native_status"),"tombstone":task["root_closed"]}
            self.bindings[key] = binding
            if late: task["late_bindings"] += 1
            self.journal("native_binding",task_index=index,binding=copy.deepcopy(binding),late=late)
            self.state["bindings"] = [copy.deepcopy(b) for b in self.bindings.values()]
            self.persist(); self.changed.notify_all()
            if late and task["status"] != "active":
                self.halt("late_binding_after_finish")
            return {"accepted":binding["active"],"closed":task["root_closed"],"task_index":index}

    def bound_index(self, identity):
        binding = self.bindings.get(self.binding_key(identity))
        return binding["task_index"] if binding else None

    def check_task(self, surface, identity):
        key = self.binding_key(identity)
        if not self.changed.wait_for(lambda:key in self.bindings or self.state["halted"],self.binding_wait):
            self.deny(surface,"binding_unverified",identity=identity)
        binding = self.bindings.get(key); index = binding["task_index"] if binding else None
        if binding:
            task = self.state["tasks"][index]
            if task["root_closed"]:
                # Let the native cancellation finish before an expected local denial.
                self.changed.wait_for(lambda:task["interrupt_acknowledged"] or binding["ended"] or self.state["halted"],10)
                self.deny(surface,"root_closed",index=index,identity=identity,expected=True)
        if self.state["halted"]: self.deny(surface,"run_stopped",index=index,identity=identity)
        if not binding: self.deny(surface,"binding_unverified",identity=identity)
        if not binding["active"]: self.deny(surface,"turn_inactive",index=index,identity=identity)
        if index != self.state["active_task"]: self.deny(surface,"binding_conflict",index=index,identity=identity)
        return index

    def admit(self, metadata):
        with self.changed:
            if self.execution_mode == "live":
                try: validate_grant(self.grant)
                except (ValueError,TypeError,KeyError): self.deny("model","grant_invalid",identity=metadata)
            index = self.check_task("model",metadata)
            if not self.changed.wait_for(lambda:self.idle() or self.state["halted"] or self.state["tasks"][index]["root_closed"],30):
                self.deny("model","settlement_pending",index=index,identity=metadata)
            index = self.check_task("model",metadata); task = self.state["tasks"][index]; counts = self.counts(index)
            if metadata["model"] != task["model"]: self.deny("model","model_mismatch",index=index,identity=metadata)
            for code,count,limit in [("model_total_limit",counts["model_total"],self.limits["model_http_requests"]),
                                     ("model_model_limit",counts["model_same_model"],self.limits["per_model_http_requests"]),
                                     ("model_task_limit",counts["model_task"],self.limits["per_task_http_requests"])]:
                if count >= limit: self.deny("model",code,index=index,identity=metadata)
            if (self.state["observed_cost_upper_micro_cny"] >= self.limits["soft_stop_micro_cny"]
                    or self.state["observed_hosted_calls"] >= self.limits["hosted_observation_stop_count"]):
                self.deny("model","observation_limit",index=index,identity=metadata)
            row = {**metadata,"ordinal":len(self.state["requests"])+1,"task_index":index,"case_id":task["case_id"],
                   "root_thread_id":task["thread_id"],"status":"inflight","dispatch_attempted":False,
                   "started_at":time.monotonic(),"provider_events":[]}
            self.state["requests"].append(row)
            event=self.journal("model_admitted",task_index=index,ordinal=row["ordinal"],thread_id=row["thread_id"],turn_id=row["turn_id"],counts=self.counts(index))
            row["admission_sequence"]=event["sequence"]
            self.persist();return row

    def dispatch(self,row):
        with self.changed:
            task=self.state["tasks"][row["task_index"]]
            if task["root_closed"] or self.state["halted"]:
                row["status"]="cancelled_before_dispatch";row["observed_cost_upper_micro_cny"]=0
                self.persist();self.changed.notify_all()
                self.deny("model","root_closed",index=row["task_index"],identity=row,expected=True)
            row["dispatch_attempted"]=True
            if self.execution_mode=="live":self.state["real_model_http_requests"]+=1
            self.journal("model_dispatch_committed",task_index=row["task_index"],ordinal=row["ordinal"])
            self.persist() # before the irreversible request; never refund unknown sends.

    def close_root(self,root,reason):
        with self.changed:
            index=self.roots.get(root)
            if index is None:self.deny("control","unowned_root")
            task=self.state["tasks"][index]
            if not task["root_closed"]:
                task["root_closed"]=True;task["closed_at"]=time.monotonic()
                event=self.journal("root_closed",task_index=index,reason=reason,counts=self.counts(index))
                task["root_boundary"]={"closed":True,"sequence":event["sequence"]}
            if reason=="natural_terminal":task["interrupt_acknowledged"]=True
            self.persist();self.changed.notify_all();return task["root_boundary"]

    def sample(self,binding):
        with self.changed:
            # v2 verifies the observed item and original native root; closure is task-wide.
            boundary=super().sample(binding)
            self.close_root(binding["root_thread_id"],"first_native_action")
            return {**boundary,"root_boundary":self.state["tasks"][self.bound_index(binding)]["root_boundary"]}

    def finish(self):
        with self.changed:
            index=self.state["active_task"]
            if index is None:self.deny("control","no_active_task")
            task=self.state["tasks"][index]
            if not task["root_closed"] or any(b["task_index"]==index and not b["ended"] for b in self.bindings.values()):
                self.deny("control","native_barrier_pending",index=index)
            result=super().finish()
            result["bindings"]=[copy.deepcopy(b) for b in self.bindings.values() if b["task_index"]==index]
            result["task"]=copy.deepcopy(task)
            return result

    def review(self,index,record):
        with self.changed:
            if self.state["halted"]:self.deny("control","run_stopped",index=index)
            if index!=len(self.state["tasks"])-1 or self.state["active_task"] is not None:
                self.deny("control","review_required",index=index)
            task=self.state["tasks"][index]
            if record.get("case_id")!=task["case_id"] or record.get("model")!=task["model"] or record.get("qualified") is not True:
                self.deny("control","route_gate_failed",index=index)
            if task["reviewed"]:return {"reviewed":True,"phase":self.state["phase"]}
            task["reviewed"]=True
            self.state["reviews"].append(copy.deepcopy(record))
            self.journal("task_reviewed",task_index=index,evidence_sha256=record["evidence_sha256"],review_kind=record["review_kind"])
            if index==3 and self.max_tasks==12:
                if not all(t["reviewed"] for t in self.state["tasks"]):self.deny("control","review_required")
                self.stage_index=1;self.state["phase"]="A2";self.update_limits()
                self.journal("stage_promoted",phase="A2",charged_upper_micro_cny=self.state["observed_cost_upper_micro_cny"],model_http=len(self.state["requests"]))
            self.persist();return {"reviewed":True,"phase":self.state["phase"]}
