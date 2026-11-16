#!/usr/bin/env python3
"""Qualified entry points: local (zero provider calls), live (new explicit grant).

The same canonical Runtime and test driver are used in both modes. Nothing here
installs, replaces or patches a binary or activates the user's daily App.
"""
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

from feat175_q2_meter import HOST, PACKAGE, WORKSPACE, Meter, digest, require, save, server_for, shared, validate_authorization


def verify_sources():
    result = subprocess.run(["node", "scripts/feat175-q2.mjs", "preflight"], cwd=HOST, capture_output=True, text=True)
    require(result.returncode == 0, "Frozen source preflight failed; review source changes before running")
    return json.loads(result.stdout)


def score(root):
    path = root / "evidence.json"
    if not path.exists():
        return
    evidence = json.loads(path.read_text())
    for observation in evidence["observations"]:
        # Provider terminal inventory and native events must agree. A missing
        # app-server item is NOT evidence that a provider did no search.
        provider_ids = [item for row in observation["meter_settlement"]["requests"] for item in row.get("hosted_item_ids", [])]
        native_ids = [e["params"]["item"]["id"] for e in observation["events"]
                      if e["method"] == "item/completed" and e["params"]["item"]["type"] == "webSearch"]
        reconciled = (all(isinstance(i, str) and i for i in provider_ids) and len(provider_ids) == len(set(provider_ids))
                      and set(provider_ids) == set(native_ids) and len(native_ids) == len(set(native_ids)))
        observation["hosted_inventory_reconciled"] = reconciled
        if not reconciled:
            observation["hosted_visibility"] = "partial"
    save(path, evidence)
    result = subprocess.run(["node", "scripts/feat175-q2.mjs", "score", str(path)], cwd=HOST, capture_output=True, text=True)
    if result.returncode == 0:
        save(root / "score.json", json.loads(result.stdout))
    else:
        save(root / "score.json", {"qualification": "NOT GRANTED", "state": "unscorable_native_evidence", "error": result.stderr.strip()[:500]})
    return evidence


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["local", "live"])
    parser.add_argument("--output", required=True, type=Path, help="new owner-only evidence directory")
    parser.add_argument("--authorization", type=Path)
    parser.add_argument("--kimi-key-file", type=Path)
    parser.add_argument("--minimax-key-file", type=Path)
    parser.add_argument("--local-scenario", choices=["complete", "normal-stop"], default="complete")
    args = parser.parse_args()
    os.umask(0o077)
    local = args.mode == "local"
    plan = json.loads((PACKAGE / "cases.json").read_text())
    prices = json.loads((PACKAGE / "pricing-snapshot.json").read_text())
    require(args.output.is_absolute() and not args.output.exists(), "Fresh absolute evidence directory required")
    if local:
        require(not args.authorization and not args.kimi_key_file and not args.minimax_key_file,
                "Local mode never accepts authorization or credential inputs")
        auth = None
    else:
        require(args.local_scenario == "complete", "Local scenario is not a live execution option")
        require(args.authorization is not None, "Current task has zero paid authorization")
        auth = json.loads(args.authorization.read_text())
        validate_authorization(auth, prices)  # BEFORE creating state, listening, Runtime or credentials.
        require(args.kimi_key_file and args.minimax_key_file, "Both approved provider credential sources required")
    preflight = verify_sources()
    lock = json.loads((PACKAGE / "source-lock.json").read_text())
    # One run at a time, one grant ever. A new output directory cannot reset fees.
    state = HOST / ".local/feat175-q2"
    state.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (state / "run.lock").open("a") as guard:
        fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if not local:
            used = state / ("used-" + auth["authorization_id"] + ".json")
            with used.open("x") as record:
                json.dump({"authorization_sha256": shared.canonical_digest(auth), "evidence": str(args.output),
                           "policy": "consumed once, including aborted startup; no retry or fee reset"}, record)
        # Only the meter sees provider keys. Runtime receives a random local capability.
        keys = {} if local else {"kimi-k3.1-pro": shared.credential(args.kimi_key_file), "MiniMax-M3": shared.credential(args.minimax_key_file)}
        args.output.mkdir(mode=0o700, parents=False)
        root = args.output.resolve()
        meter = Meter(root, plan, prices, local=local, authorization=auth, local_scenario=args.local_scenario)
        control, runtime_bearer, mcp_token = (secrets.token_hex(32) for _ in range(3))
        server = server_for(meter, control, runtime_bearer, mcp_token, keys)
        server_thread = threading.Thread(target=server.serve_forever)
        server_thread.start()
        meter.state["meter_state"] = "running"
        meter.persist()
        config = {"root": str(root), "kind": "synthetic" if local else "recorded-native", "control_token": control,
                  "runtime_bearer": runtime_bearer, "mcp_token": mcp_token,
                  "endpoint": "http://127.0.0.1:" + str(server.server_port), "package_sha256": preflight["package_sha256"]}
        save(root / "run-config.json", config)
        # No ambient model keys, alternate providers, updater flags or user Codex home.
        env = {k: os.environ[k] for k in ("PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "GOCACHE") if k in os.environ}
        runtime = WORKSPACE / lock["runtime_relative_path"]
        env.update({"GOPROXY": "off", "HTTP_PROXY": "", "HTTPS_PROXY": "", "ALL_PROXY": "", "NO_PROXY": "127.0.0.1,localhost",
                    "YIJIE_CODEX_INTEGRATION_BINARY": str(runtime / "codex"),
                    "YIJIE_CODEX_INTEGRATION_MANIFEST": str(runtime / "runtime-manifest.json"),
                    "YIJIE_FEAT175_Q2_RUN_CONFIG": str(root / "run-config.json")})
        old_handler = signal.signal(signal.SIGINT, lambda *_: meter.halt("Owner requested normal stop"))
        process = None
        try:
            # A new session prevents terminal Ctrl-C from force-stopping the Go
            # driver/Runtime. The meter stop is polled and uses turn/interrupt.
            with (root / "native-run.log").open("w") as log:
                process = subprocess.Popen(["go", "test", "./internal/codex", "-run", "^TestFEAT175Q2Execution$", "-count=1", "-v", "-timeout", "0"],
                                           cwd=HOST, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                print(json.dumps({"feature": "FEAT-175", "mode": args.mode, "evidence": str(root), "real_api_authorized": not local}), flush=True)
                result = process.wait()  # No timeout kill, terminate(), signal or executable substitution.
        finally:
            meter.halt("Execution driver ended")
            server.shutdown()
            server_thread.join(5)
            server.server_close()  # Current provider handlers drain through normal response/timeout.
            meter.state["meter_state"] = "stopped_normally" if meter.idle() and not server_thread.is_alive() else "STOP_PENDING"
            meter.persist()
            signal.signal(signal.SIGINT, old_handler)
        evidence = score(root)
        scored = json.loads((root / "score.json").read_text()) if (root / "score.json").exists() else {}
        expected = {c["id"]: c["expected"]["first"] for c in plan["cases"]}
        scripted_routes_valid = (len(scored.get("rows", [])) == 12 and all(
            row["first_route"] == expected[row["case_id"]] and row["parameters_match"]
            and row["verdict"] in ("MATCH", "REVIEW_REQUIRED")
            and all(item["status"] == "completed" and item["terminal_observed"] for item in row["execution_status"])
            for row in scored.get("rows", [])))
        stopped = json.loads((root / "native-shutdown.json").read_text()) if (root / "native-shutdown.json").exists() else {"state": "unknown"}
        passed = (result == 0 and stopped["state"] == "stopped_normally" and meter.state["meter_state"] == "stopped_normally"
                  and evidence is not None and len(evidence["observations"]) == 12
                  and all(o.get("hosted_inventory_reconciled") for o in evidence["observations"])
                  and (not local or scripted_routes_valid))
        stop_passed = (local and args.local_scenario == "normal-stop" and result != 0 and stopped["state"] == "stopped_normally"
                       and meter.state["meter_state"] == "stopped_normally" and len(meter.state["requests"]) == 1
                       and evidence is not None and len(evidence["observations"]) == 1 and any(
                           e["method"] == "turn/completed" and e["params"]["turn"]["status"] == "interrupted"
                           for e in evidence["observations"][0]["events"]))
        receipt = {"feature": "FEAT-175", "kind": config["kind"], "local_wiring": "PASS" if local and passed else "NOT GRANTED",
                   "real_q2": "NOT RUN" if local else "REVIEW REQUIRED", "paid_authorization_used": not local,
                   "model_http_requests": meter.state["real_model_http_requests"], "supplier_rest_calls": 0,
                   "actual_cost_cny": 0 if local else "provider bill reconciliation required",
                   "simulated_http_requests": len(meter.state["requests"]) if local else 0,
                   "runtime_core_changes": 0, "daily_app_activated": False, "force_kill": False,
                   "normal_shutdown": stopped["state"], "process_exit": result,
                   "normal_stop_qualification": "PASS" if stop_passed else "NOT RUN",
                   "scripted_routes_valid": scripted_routes_valid if local and args.local_scenario == "complete" else None,
                   "source_lock_sha256": meter.state["source_lock_sha256"]}
        save(root / "receipt.json", receipt)
        print(json.dumps(receipt, ensure_ascii=False), flush=True)
        return 0 if passed or stop_passed else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError) as error:
        print("FEAT-175 stopped: " + str(error), file=sys.stderr)
        sys.exit(1)
