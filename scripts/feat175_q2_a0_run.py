#!/usr/bin/env python3
"""A0 controlled QA entry. Live execution requires a NEW explicit scoped grant."""
import argparse
import fcntl
import hmac
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import threading
import uuid

from feat175_q2_a0_meter import Meter, v1, v2
from feat175_q2_a0_policy import PACKAGE, POLICY, consume_grant, validate_grant, verify_sources
from feat175_q2_a0_transport import local_upstream, relay


def score_file(file):
    result=subprocess.run(["node","scripts/feat175-q2-a0.mjs","score",str(file)],cwd=v1.HOST,capture_output=True,text=True)
    v1.require(result.returncode==0,"Canonical evidence scoring failed")
    return json.loads(result.stdout)


def review_case(meter,index):
    case_file=meter.root/f"case-{index:02d}.json"
    raw=case_file.read_bytes();evidence=json.loads(raw)
    o=evidence["observations"][0];sha=v1.digest(raw)
    result=score_file(case_file)
    row=next(r for r in result["rows"] if r["model"]==o["model"] and r["case_id"]==o["case_id"])
    v1.save(meter.root/f"case-{index:02d}-score.json",result)
    structural=row["first_route_verdict"] in ("MATCH","REVIEW_REQUIRED") and row["owner_barrier_qualified"] and row["visibility"]["provider_complete"]
    if not structural:meter.deny("control","route_gate_failed",index=index)
    kind="local_scripted_protocol"
    if meter.execution_mode=="live":
        review_file=meter.root/f"case-{index:02d}-review.json"
        if row["first_route_verdict"]=="REVIEW_REQUIRED" and not review_file.exists():
            return {"state":"review_required","case_id":o["case_id"],"model":o["model"],"evidence_sha256":sha}
        if review_file.exists():
            review=json.loads(review_file.read_text())
            v1.require(set(review)=={"evidence_sha256","model","case_id","thread_id","turn_id","query_relevance","reviewer"},"Closed review receipt required")
            v1.require(review["evidence_sha256"]==sha and all(review[k]==o[k] for k in ("model","case_id","thread_id","turn_id"))
                       and review["query_relevance"]=="pass" and isinstance(review["reviewer"],str) and review["reviewer"].strip(),"Bound query review required")
            kind="bound_query_review"
        else:kind="no_query_review_required"
        validate_grant(meter.grant)
    record={"model":o["model"],"case_id":o["case_id"],"qualified":True,"evidence_sha256":sha,"review_kind":kind}
    meter.review(index,record)
    return {"state":"accepted","phase":meter.state["phase"]}


def server_for(meter,control,bearer,mcp,keys,upstream_url):
    # Existing MCP protocol and its same-source tool catalogue stay unchanged.
    server=v1.server_for(meter,control,bearer,mcp,{})
    base=server.RequestHandlerClass
    class Handler(base):
        def reply(self,status,value=None):
            try:super().reply(status,value)
            except (BrokenPipeError,ConnectionResetError):self.close_connection=True
        def do_POST(self):
            meter.request_context.diagnosed=False
            meter.request_context.surface="model" if self.path=="/v1/responses" else "mcp" if self.path=="/mcp" else "control"
            if self.path not in ("/v1/responses","/control/bind","/control/sample","/control/interrupt-returned","/control/close-root","/control/review","/control/settlement-status"):
                return super().do_POST()
            row=None
            try:
                size=int(self.headers.get("Content-Length","0"));v1.require(0<size<=4*1024*1024,"Bounded body required")
                payload=self.rfile.read(size);v1.require(len(payload)==size,"Incomplete body")
                if self.path.startswith("/control/"):
                    v1.require(hmac.compare_digest(self.headers.get("X-FEAT175-Control",""),control),"Control capability required")
                    value=json.loads(payload)
                    if self.path=="/control/bind":out=meter.bind_native(value)
                    elif self.path=="/control/sample":out=meter.sample(value)
                    elif self.path=="/control/interrupt-returned":out=meter.acknowledge_interrupt(value)
                    elif self.path=="/control/close-root":out=meter.close_root(value["root_thread_id"],value["reason"])
                    elif self.path=="/control/settlement-status":
                        with meter.lock:out={"settled":meter.idle() and meter.mcp_inflight==0}
                    else:out=review_case(meter,value["index"])
                    self.reply(200,out);return
                v1.require(hmac.compare_digest(self.headers.get("Authorization",""),"Bearer "+bearer),"Local model capability required")
                _,metadata=v1.inspect_request(payload,{k.lower():v for k,v in self.headers.items()})
                row=meter.admit(metadata)
                relay(self,meter,row,payload,keys,local_url=upstream_url)
            except (ValueError,KeyError,TypeError,AttributeError,OSError):
                meter.halt("Admission or control rejected")
                self.reply(429,{"error":{"message":"FEAT-175 controlled admission stopped"}})
    server.RequestHandlerClass=Handler
    return server


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode",choices=["local","live"])
    parser.add_argument("--output",required=True,type=Path)
    parser.add_argument("--scenario",choices=["ordinary","children","late-binding","cancel-tail"],default="ordinary")
    parser.add_argument("--authorization",type=Path)
    parser.add_argument("--kimi-key-file",type=Path)
    parser.add_argument("--minimax-key-file",type=Path)
    args=parser.parse_args();os.umask(0o077)
    v1.require(args.output.is_absolute() and not args.output.exists(),"Fresh absolute output required")
    if args.mode=="local":
        v1.require(not args.authorization and not args.kimi_key_file and not args.minimax_key_file,"Local run accepts no live authority or keys")
        grant=None
    else:
        v1.require(args.scenario=="ordinary","Local fixture scenarios cannot run live")
        v1.require(args.authorization is not None,"New budget is not approved")
        grant=json.loads(args.authorization.read_text());validate_grant(grant)
        v1.require(args.kimi_key_file and args.minimax_key_file,"Both approved credential sources required")
    lock=verify_sources()
    state=v1.HOST/".local/feat175-q2";state.mkdir(parents=True,exist_ok=True,mode=0o700)
    with (state/"run.lock").open("a") as guard:
        fcntl.flock(guard,fcntl.LOCK_EX|fcntl.LOCK_NB)
        if grant:consume_grant(grant,state,args.output)
        keys={} if args.mode=="local" else {"kimi-k3.1-pro":v1.shared.credential(args.kimi_key_file),"MiniMax-M3":v1.shared.credential(args.minimax_key_file)}
        args.output.mkdir(mode=0o700);root=args.output.resolve()
        meter=Meter(root,mode=args.mode,grant=grant);meter.state["generation"]=str(uuid.uuid4())
        upstream=None;upstream_worker=None;upstream_url=None
        if args.mode=="local":
            upstream=local_upstream(meter,args.scenario)
            upstream_url=f"http://127.0.0.1:{upstream.server_port}"
            upstream_worker=threading.Thread(target=upstream.serve_forever);upstream_worker.start()
        control,bearer,mcp=(secrets.token_hex(32) for _ in range(3))
        server=server_for(meter,control,bearer,mcp,keys,upstream_url)
        worker=threading.Thread(target=server.serve_forever);worker.start()
        meter.state["meter_state"]="running";meter.persist()
        config={"root":str(root),"endpoint":f"http://127.0.0.1:{server.server_port}","control_token":control,
                "runtime_bearer":bearer,"mcp_token":mcp,"generation":meter.state["generation"],"kind":meter.state["kind"],
                "package_sha256":POLICY["base_cases_sha256"],"policy_sha256":v1.digest((PACKAGE/"policy.json").read_bytes()),
                "tasks":meter.max_tasks,"scenario":args.scenario}
        v1.save(root/"run-config.json",config)
        env={k:os.environ[k] for k in ("PATH","HOME","TMPDIR","LANG","LC_ALL","GOCACHE") if k in os.environ}
        runtime=v1.WORKSPACE/lock["runtime_relative_path"]
        env.update({"GOPROXY":"off","HTTP_PROXY":"","HTTPS_PROXY":"","ALL_PROXY":"","NO_PROXY":"127.0.0.1,localhost",
                    "YIJIE_CODEX_INTEGRATION_BINARY":str(runtime/"codex"),"YIJIE_CODEX_INTEGRATION_MANIFEST":str(runtime/"runtime-manifest.json"),
                    "YIJIE_FEAT175_Q2_A0_CONFIG":str(root/"run-config.json")})
        old=signal.signal(signal.SIGINT,lambda *_:meter.halt("owner_stop"))
        try:
            with (root/"native-run.log").open("w") as log:
                child=subprocess.Popen(["go","test","-race","./internal/codex","-run","^TestFEAT175Q2A0Native$","-count=1","-v","-timeout","0"],cwd=v1.HOST,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
                print(json.dumps({"feature":"FEAT-175","mode":args.mode,"scenario":args.scenario,"evidence":str(root)}),flush=True)
                code=child.wait() # native controls/EOF only, no process termination.
        finally:
            meter.halt("driver_finished")
            server.shutdown();worker.join(5);server.server_close()
            if upstream:
                upstream.shutdown();upstream_worker.join(5);upstream.server_close()
            stopped=meter.idle() and meter.mcp_inflight==0 and all(r.get("upstream_reader_stopped",True) for r in meter.state["requests"])
            meter.state["meter_state"]="stopped_normally" if stopped and not worker.is_alive() else "STOP_PENDING"
            meter.persist();signal.signal(signal.SIGINT,old)
        native=json.loads((root/"native-shutdown.json").read_text()) if (root/"native-shutdown.json").exists() else {"state":"unknown"}
        scored=score_file(root/"evidence.json") if (root/"evidence.json").exists() else {"rows":[]}
        v1.save(root/"score-a0.json",scored)
        normal=native["state"]==meter.state["meter_state"]=="stopped_normally"
        raw_kind=meter.state["kind"]
        passed=code==0 and normal and len(meter.state["reviews"])==meter.max_tasks
        tail=(args.mode=="local" and args.scenario=="cancel-tail" and code!=0 and normal
              and len(meter.state["requests"])==1 and all(r["status"]=="settled" for r in meter.state["requests"])
              and any(e["after_cutoff"] for r in meter.state["requests"] for e in r["provider_events"]))
        peers={p["ordinal"]:p for p in meter.state.get("local_upstream_receipts",[])}
        byte_exact=all(r["request_sha256"]==peers[r["ordinal"]]["request_sha256"] for r in meter.state["requests"] if r["dispatch_attempted"]) if args.mode=="local" else None
        receipt={"feature":"FEAT-175","revision":3,"sample_kind":raw_kind,"scenario":args.scenario,
                 "local_qualification":"PASS" if args.mode=="local" and passed and byte_exact else "NOT GRANTED",
                 "cancel_tail_qualification":"PASS" if tail and byte_exact else "NOT RUN",
                 "real_execution_status":"NOT RUN" if args.mode=="local" else "REVIEW REQUIRED",
                 "native_driver_exit":code,"reviewed_tasks":len(meter.state["reviews"]),"tasks_observed":len(meter.state["tasks"]),
                 "model_admissions":len(meter.state["requests"]),"real_model_http":meter.state["real_model_http_requests"],
                 "supplier_rest":0,"actual_new_cost_cny":0 if args.mode=="local" else "bill reconciliation required",
                 "phase":meter.state["phase"],"body_bytes_preserved":byte_exact,"normal_shutdown":normal,
                 "source_lock_sha256":meter.state["source_lock_a0_sha256"],"product_code_changed":False}
        v1.save(root/"receipt.json",receipt);print(json.dumps(receipt),flush=True)
        return 0 if passed or tail else 1


if __name__=="__main__":
    try:sys.exit(main())
    except (ValueError,OSError,KeyError) as error:
        print("A0 admission stopped: "+str(error),file=sys.stderr);sys.exit(1)
