#!/usr/bin/env python3
"""Local-only Q2-v2 native qualification. NO live mode or credential inputs."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import threading

import feat175_q2_v2_meter as q2


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["local"])
    parser.add_argument("--sampling", choices=["first_action", "full_turn"], default="first_action")
    parser.add_argument("--scenario", choices=["complete", "repeated-hosted", "mcp-quota"], default="complete")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    q2.v1.require(args.output.is_absolute() and not args.output.exists(), "Fresh absolute output directory required")
    q2.v1.require(args.scenario == "complete" or args.sampling == "full_turn", "Extra scenarios use full_turn")
    verified = subprocess.run(["node", "scripts/feat175-q2-v2.mjs", "preflight"], cwd=q2.v1.HOST, capture_output=True, text=True)
    q2.v1.require(verified.returncode == 0, "Frozen source preflight failed")
    preflight = json.loads(verified.stdout)
    state = q2.v1.HOST / ".local/feat175-q2"
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (state / "run.lock").open("a") as guard:
        fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)
        args.output.mkdir(mode=0o700)
        root = args.output.resolve()
        meter = q2.Meter(root, mode=args.sampling, scenario=args.scenario)
        control, bearer, mcp = (secrets.token_hex(32) for _ in range(3))
        server = q2.server_for(meter, control, bearer, mcp)
        worker = threading.Thread(target=server.serve_forever)
        worker.start()
        meter.state["meter_state"] = "running"
        meter.persist()
        config = {"root": str(root), "kind": "synthetic", "mode": args.sampling,
                  "control_token": control, "runtime_bearer": bearer, "mcp_token": mcp,
                  "endpoint": f"http://127.0.0.1:{server.server_port}", "package_sha256": preflight["package_sha256"],
                  "sampling_sha256": q2.v1.digest((q2.PACKAGE / "sampling.json").read_bytes())}
        q2.v1.save(root / "run-config.json", config)
        runtime = q2.v1.WORKSPACE / meter.sources["runtime_relative_path"]
        env = {key: os.environ[key] for key in ("PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "GOCACHE") if key in os.environ}
        env.update({"GOPROXY": "off", "HTTP_PROXY": "", "HTTPS_PROXY": "", "ALL_PROXY": "", "NO_PROXY": "127.0.0.1,localhost",
                    "YIJIE_CODEX_INTEGRATION_BINARY": str(runtime / "codex"), "YIJIE_CODEX_INTEGRATION_MANIFEST": str(runtime / "runtime-manifest.json"),
                    "YIJIE_FEAT175_Q2_V2_RUN_CONFIG": str(root / "run-config.json")})
        previous_handler = signal.signal(signal.SIGINT, lambda *_: meter.halt("Normal owner stop"))
        try:
            with (root / "native-run.log").open("w") as log:
                child = subprocess.Popen(["go", "test", "-race", "./internal/codex", "-run", "^TestFEAT175Q2V2Native$", "-count=1", "-v", "-timeout", "0"],
                                         cwd=q2.v1.HOST, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                print(json.dumps({"feature": "FEAT-175", "revision": 2, "mode": args.sampling, "scenario": args.scenario,
                                  "new_provider_calls_authorized": 0, "evidence": str(root)}), flush=True)
                exit_code = child.wait()  # Native interrupt/normal shutdown only; never terminate/kill.
        finally:
            meter.halt("Execution driver ended")
            server.shutdown()
            worker.join(5)
            server.server_close()
            meter.state["meter_state"] = "stopped_normally" if meter.idle() and meter.mcp_inflight == 0 and not worker.is_alive() else "STOP_PENDING"
            meter.persist()
            signal.signal(signal.SIGINT, previous_handler)
        raw = root / "evidence.json"
        scoring = subprocess.run(["node", "scripts/feat175-q2-v2.mjs", "score", str(raw)], cwd=q2.v1.HOST, capture_output=True, text=True)
        scored = json.loads(scoring.stdout) if scoring.returncode == 0 else {"error": "Canonical scoring failed", "rows": []}
        q2.v1.save(root / "score-v2.json", scored)
        evidence = json.loads(raw.read_text()) if raw.exists() else {"observations": []}
        shutdown = json.loads((root / "native-shutdown.json").read_text()) if (root / "native-shutdown.json").exists() else {"state": "unknown"}
        normal = shutdown["state"] == meter.state["meter_state"] == "stopped_normally"
        rows = scored["rows"]
        complete = len(evidence["observations"]) == 12 and all(r["first_route_verdict"] in ("MATCH", "REVIEW_REQUIRED") for r in rows)
        expected_only = all(d["expected_sampling_stop"] for d in meter.state["denials"])
        no_content = all(r["kind"] != "explicit_synthetic_content" for r in meter.state["mcp_results"])
        boundary_ok = all(not task.get("sampling_closed") or all(
            row["admission_sequence"] < task["sample_boundary"]["sequence"]
            for row in meter.state["requests"] if row["task_index"] == index)
            for index, task in enumerate(meter.state["tasks"]))
        scenario_observed = args.scenario == "complete" or (args.scenario == "repeated-hosted" and sum(
            row["case_id"] == "Q2-02" and row["hosted_calls"] == 2 and row["additional_hosted_after_first"] == 1 for row in rows) == 2)
        protocol_pass = (scenario_observed and exit_code == 0 and scoring.returncode == 0 and normal and complete and expected_only and boundary_ok
                         and (args.sampling != "first_action" or no_content))
        denial_pass = (args.scenario == "mcp-quota" and exit_code != 0 and normal and scoring.returncode == 0
                       and any(d["code"] == "mcp_task_limit" and d["surface"] == "mcp" and d["counts"]["mcp_task"] == 4 for d in meter.state["denials"]))
        receipt = {"feature": "FEAT-175", "revision": 2, "sample_kind": "synthetic", "mode": args.sampling, "scenario": args.scenario,
                   "local_protocol_qualification": "PASS" if protocol_pass else "NOT GRANTED",
                   "local_quota_diagnostic_qualification": "PASS" if denial_pass else "NOT RUN",
                   "driver_exit_code": exit_code, "observed_tasks": len(evidence["observations"]),
                   "local_model_requests": len(meter.state["requests"]), "local_mcp_admitted": sum(t["mcp_calls"] for t in meter.state["tasks"]),
                   "native_terminal_statuses": {s: sum(r["task_execution"] == s for r in rows) for s in ("completed", "interrupted", "failed", "MISSING")},
                   "first_action_source_bodies_delivered": sum(r["kind"] == "explicit_synthetic_content" for r in meter.state["mcp_results"]) if args.sampling == "first_action" else None,
                   "admission_cutoffs_respected": boundary_ok, "denials": len(meter.state["denials"]),
                   "new_real_model_http": 0, "new_real_hosted_searches": 0, "supplier_rest": 0, "actual_cost_cny": 0,
                   "runtime_shutdown": shutdown["state"], "meter_shutdown": meter.state["meter_state"],
                   "source_lock_sha256": meter.state["source_lock_v2_sha256"], "v1_source_lock_sha256": meter.state["source_lock_sha256"],
                   "real_model_routing_qualification": "NOT GRANTED", "full_task_success_qualification": "NOT GRANTED", "product_code_changed": False}
        q2.v1.save(root / "receipt.json", receipt)
        print(json.dumps(receipt), flush=True)
        return 0 if protocol_pass or denial_pass else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError) as error:
        print("Local Q2-v2 stopped: " + str(error), file=sys.stderr)
        sys.exit(1)
