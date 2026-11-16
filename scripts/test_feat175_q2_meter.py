"""Normal, non-destructive FEAT-175 meter tests; no providers or key loading."""
import copy
import datetime as dt
import http.client
import json
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest

import feat175_q2_meter as q2


def observation(model="kimi-k3.1-pro", searches=0, tokens=100):
    return {"http_status": 200, "stream_complete": True, "observer_valid": True, "terminal_count": 1,
            "terminal": "response.completed", "response_status": "completed", "response_model": model,
            "hosted_output_complete": True, "native_search_calls": searches, "cache_ttl": "5m",
            "usage": {"input_tokens": tokens, "output_tokens": 10}}


def metadata(model="kimi-k3.1-pro"):
    return {"model": model, "thread_id": "ordinary-thread", "turn_id": "ordinary-turn", "request_adjustments": []}


class MeterTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="feat175-q2-unit-")
        self.root = Path(self.temp.name)
        self.plan = json.loads((q2.PACKAGE / "cases.json").read_text())
        self.prices = json.loads((q2.PACKAGE / "pricing-snapshot.json").read_text())
        self.meter = q2.Meter(self.root, self.plan, self.prices, local=True)

    def tearDown(self):
        self.temp.cleanup()

    def arm(self, model="kimi-k3.1-pro", case="Q2-01"):
        self.meter.arm(model, case, "ordinary-thread")

    def grant(self):
        at = dt.datetime.fromisoformat(self.prices["checked_at"]) + dt.timedelta(seconds=1)
        return {"feature": "FEAT-175", "purpose": "Q2-route-feasibility", "status": "approved",
                "authorization_id": "00000000-0000-4000-8000-000000000175",
                "package_sha256": q2.digest((q2.PACKAGE / "cases.json").read_bytes()),
                "source_lock_sha256": q2.digest((q2.PACKAGE / "source-lock.json").read_bytes()),
                "pricing_sha256": q2.shared.canonical_digest(self.prices), "approved_by": "段成威",
                "approval_source": "UNIT TEST DATA ONLY; never used for execution",
                "approved_at": at.isoformat(), "expires_at": (at + dt.timedelta(hours=1)).isoformat(),
                "models": list(q2.MODELS), "tasks": 12, "model_http_requests": 48, "per_model_http_requests": 24,
                "per_task_http_requests": 4, "local_mcp_per_task": 4, "hosted_observation_stop_count": 24,
                "supplier_rest_calls": 0, "money_mode": "soft_stop_after_observation", "allow_inflight_overshoot": True,
                "soft_stop_micro_cny": 20_000_000}, at

    def test_authorization_exact_scope_expiry_sources_and_soft_stop(self):
        auth, at = self.grant()
        q2.validate_authorization(auth, self.prices, at=at)
        for field, changed in {"feature": "FEAT-171", "status": "not_authorized", "source_lock_sha256": "",
                               "allow_inflight_overshoot": False, "supplier_rest_calls": 1, "soft_stop_micro_cny": 0,
                               "model_http_requests": 49, "per_task_http_requests": 5, "models": ["kimi-k3.1-pro"]}.items():
            with self.subTest(field=field), self.assertRaises(ValueError):
                q2.validate_authorization({**auth, field: changed}, self.prices, at=at)
        with self.assertRaises(ValueError):
            q2.validate_authorization(auth, self.prices, at=at + dt.timedelta(hours=2))
        with self.assertRaises(ValueError):
            q2.validate_authorization({**auth, "unreviewed_note": "extra"}, self.prices, at=at)

    def test_current_zero_authorization_refuses_before_credentials_or_output(self):
        out = self.root / "never-created"
        result = subprocess.run(["python3", str(q2.HOST / "scripts/feat175_q2_run.py"), "live", "--output", str(out),
                                 "--authorization", str(q2.PACKAGE / "authorization-not-granted.json"),
                                 "--kimi-key-file", str(self.root / "not-a-key"), "--minimax-key-file", str(self.root / "not-a-key")],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("paid authorization absent", result.stderr)
        self.assertFalse(out.exists())

    def test_payload_and_all_native_declarations_remain_unchanged(self):
        body = {"model": "kimi-k3.1-pro", "reasoning": {"effort": "max"}, "stream": True,
                "client_metadata": {"thread_id": "ordinary-thread", "turn_id": "ordinary-turn"},
                "tools": [{"type": "web_search"}, {"type": "tool_search", "name": "tool_search"}],
                "max_output_tokens": 110000, "include": ["web_search_call.action.sources"], "input": []}
        payload = json.dumps(body, indent=3).encode()
        parsed, meta = q2.inspect_request(payload, {})
        self.assertEqual(parsed, body)
        self.assertEqual(meta["request_sha256"], q2.digest(payload))
        self.assertEqual(meta["request_adjustments"], [])
        self.assertEqual(meta["max_output_tokens"], 110000)
        self.assertTrue(meta["native_search_declared"])

    def test_request_billing_tier_and_native_binding_are_closed(self):
        body = {"model": "MiniMax-M3", "reasoning": {"effort": "high"}, "stream": True, "client_metadata": {"thread_id": "t", "turn_id": "u"}}
        q2.inspect_request(json.dumps(body).encode(), {})
        for update in ({"service_tier": "priority"}, {"prompt_cache_options": {"ttl": "1h"}}, {"client_metadata": {}}, {"model": "MiniMax-M3.1-Flash"}):
            with self.subTest(update=update), self.assertRaises(ValueError):
                q2.inspect_request(json.dumps({**body, **update}).encode(), {})

    def test_four_http_per_task_no_duplicate_task_or_fee_refund(self):
        self.arm()
        for _ in range(4):
            row = self.meter.admit(metadata())
            self.meter.settle(row, observation())
        with self.assertRaises(ValueError):
            self.meter.admit(metadata())
        self.assertEqual(len(self.meter.finish()["requests"]), 4)
        with self.assertRaises(ValueError):
            self.arm()
        self.assertEqual(self.meter.state["real_model_http_requests"], 0)

    def test_total_and_per_model_caps_share_child_and_auxiliary_requests(self):
        self.meter.limits = {**self.meter.limits, "model_http_requests": 3, "per_model_http_requests": 2}
        self.arm()
        for thread in ("ordinary-thread", "ordinary-child"):
            row = self.meter.admit({**metadata(), "thread_id": thread})
            self.meter.settle(row, observation())
        self.meter.finish()
        self.arm(case="Q2-02")
        with self.assertRaises(ValueError):
            self.meter.admit(metadata())
        self.meter.finish()
        self.arm(model="MiniMax-M3")
        row = self.meter.admit(metadata("MiniMax-M3"))
        self.meter.settle(row, observation("MiniMax-M3"))
        with self.assertRaises(ValueError):
            self.meter.admit(metadata("MiniMax-M3"))

    def test_money_soft_stop_can_overshoot_but_never_admits_next(self):
        self.meter.limits = {**self.meter.limits, "soft_stop_micro_cny": 1000}
        self.arm()
        row = self.meter.admit(metadata())
        self.meter.settle(row, observation(searches=1))
        self.assertGreater(self.meter.state["observed_cost_upper_micro_cny"], 1000)
        self.assertTrue(self.meter.state["halted"])
        with self.assertRaises(ValueError):
            self.meter.admit(metadata())

    def test_unknown_stream_keeps_send_and_stops_instead_of_zero_charge(self):
        self.arm()
        row = self.meter.admit(metadata())
        self.meter.settle(row, {**observation(), "stream_complete": False})
        self.assertEqual(row["status"], "outcome_unknown")
        self.assertIsNone(row["observed_cost_upper_micro_cny"])
        self.assertTrue(self.meter.state["halted"])
        self.assertEqual(len(self.meter.state["requests"]), 1)

    def test_hosted_observation_threshold_is_not_a_hard_bound(self):
        self.meter.limits = {**self.meter.limits, "hosted_observation_stop_count": 1}
        self.arm()
        row = self.meter.admit(metadata())
        self.meter.settle(row, observation(searches=2))
        self.assertEqual(self.meter.state["observed_hosted_calls"], 2)
        self.assertTrue(self.meter.state["halted"])

    def test_observer_counts_terminal_inventory_without_double_counting_events(self):
        row = {"model": "MiniMax-M3", "case_id": "Q2-02", "ordinal": 1, "case_request_number": 1}
        payload = q2.synthetic_response({}, row)
        observer = q2.Observer()
        for offset in range(0, len(payload), 19):
            observer.feed(payload[offset:offset + 19])
        self.assertEqual(observer.result["terminal_count"], 1)
        self.assertEqual(observer.result["native_search_calls"], 1)
        self.assertEqual(len(observer.result["hosted_item_ids"]), 1)
        self.assertFalse(observer.invalid or observer.extra_invalid)

    def test_normal_mcp_fixture_has_nonempty_pro_and_requested_basic_body(self):
        for name, args in [("kimi_web_search", {"query": "ordinary documents", "limit": 5}),
                           ("kimi_web_search", {"query": "ordinary documents", "limit": 3, "include_content": True}),
                           ("kimi_web_search_pro", {"query": "ordinary research"}),
                           ("kimi_web_fetch", {"url": "https://example.com/ordinary"})]:
            result = q2.synthetic_tool_result(name, args)
            self.assertIn("SYNTHETIC", json.dumps(result))
            if "results" in result:
                self.assertEqual(len(result["results"]), args.get("limit", 5))
            if args.get("include_content"):
                self.assertTrue(all(r["text"] for r in result["results"]))
            if name.endswith("_pro"):
                self.assertTrue(result["results"][0]["chunks"])

    def test_fixture_limit_and_binding_are_shared_with_native_requests(self):
        self.arm()
        row = self.meter.admit(metadata())
        self.meter.settle(row, observation())
        binding = {"threadId": "ordinary-thread", "x-codex-turn-metadata": {"turn_id": "ordinary-turn"}}
        for _ in range(4):
            self.meter.fixture("kimi_web_fetch", {"url": "https://example.com/"}, binding)
        with self.assertRaises(ValueError):
            self.meter.fixture("kimi_web_fetch", {"url": "https://example.com/"}, binding)

    def test_loopback_meter_serves_normal_response_and_stops_without_signals(self):
        self.arm()
        server = q2.server_for(self.meter, "ordinary-control", "ordinary-runtime", "ordinary-mcp", {})
        worker = threading.Thread(target=server.serve_forever)
        worker.start()
        try:
            body = {"model": "kimi-k3.1-pro", "reasoning": {"effort": "max"}, "stream": True,
                    "client_metadata": {"thread_id": "ordinary-thread", "turn_id": "ordinary-turn"}, "tools": [{"type": "web_search"}]}
            connection = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=5)
            connection.request("POST", "/v1/responses", json.dumps(body).encode(), {"Authorization": "Bearer ordinary-runtime"})
            response = connection.getresponse()
            self.assertEqual(response.status, 200)
            self.assertIn(b"response.completed", response.read())
            connection.close()
            settled = self.meter.finish()
            self.assertEqual(settled["hosted_visibility"], "complete")
            self.assertEqual(settled["requests"][0]["request_adjustments"], [])
        finally:
            server.shutdown()
            worker.join(5)
            server.server_close()
        self.assertFalse(worker.is_alive())


if __name__ == "__main__":
    unittest.main()
