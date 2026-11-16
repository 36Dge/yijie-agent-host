"""Ordinary local admission, sampling and bounded concurrency; no fault injection."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest

import feat175_q2_v2_meter as q2
from test_feat175_q2_meter import metadata, observation


class DiagnosticsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="feat175-q2-v2-unit-")
        self.root = Path(self.temp.name)
        self.meter = q2.Meter(self.root, mode="full_turn")

    def tearDown(self):
        self.temp.cleanup()

    def arm(self, model="kimi-k3.1-pro", case="Q2-01"):
        self.meter.arm(model, case, "ordinary-thread")

    def settled(self, model="kimi-k3.1-pro"):
        row = self.meter.admit(metadata(model))
        self.meter.settle(row, observation(model))
        return row

    def test_model_task_limit_has_exact_surface_binding_and_counts(self):
        self.arm()
        for _ in range(4):
            self.settled()
        with self.assertRaisesRegex(q2.Denied, "model_task_limit"):
            self.meter.admit(metadata())
        denial = self.meter.state["denials"][0]
        self.assertEqual(denial["surface"], "model")
        self.assertEqual(denial["counts"]["model_task"], 4)
        self.assertEqual(denial["binding"], {"thread_id": "ordinary-thread", "turn_id": "ordinary-turn"})
        self.assertEqual(self.meter.state["primary_denial"], denial["sequence"])
        self.assertNotIn("arguments", denial)

    def test_total_and_per_model_limits_are_distinct(self):
        self.meter.limits = {**self.meter.limits, "model_http_requests": 2}
        self.arm()
        self.settled(); self.settled()
        with self.assertRaisesRegex(q2.Denied, "model_total_limit"):
            self.meter.admit(metadata())
        other = self.root / "other"
        other.mkdir()
        self.meter = q2.Meter(other, mode="full_turn")
        self.meter.limits = {**self.meter.limits, "per_model_http_requests": 1}
        self.arm(); self.settled(); self.meter.finish(); self.arm(case="Q2-02")
        with self.assertRaisesRegex(q2.Denied, "model_model_limit"):
            self.meter.admit(metadata())

    def test_concurrent_normal_quota_rejections_keep_primary_cause(self):
        self.arm(); self.settled()
        binding = {"threadId": "ordinary-thread", "x-codex-turn-metadata": {"turn_id": "ordinary-turn"}}
        for _ in range(4):
            self.meter.fixture("kimi_web_fetch", {"url": "https://example.com/ordinary"}, binding)
        outcomes = []
        def call():
            try:
                self.meter.fixture("kimi_web_fetch", {"url": "https://example.com/ordinary"}, binding)
            except q2.Denied as error:
                outcomes.append(str(error))
        threads = [threading.Thread(target=call) for _ in range(2)]
        for t in threads:t.start()
        for t in threads:t.join(3)
        self.assertTrue(all(not t.is_alive() for t in threads))
        self.assertCountEqual(outcomes, ["mcp_task_limit", "run_stopped"])
        a, b = self.meter.state["denials"]
        self.assertLess(a["sequence"], b["sequence"])
        self.assertEqual(a["counts"]["mcp_task"], 4)
        self.assertEqual(b["caused_by"], a["sequence"])
        self.assertEqual(self.meter.state["halt_reason"], "mcp_task_limit")

    def test_stale_binding_rejection_is_not_provider_throttling(self):
        self.arm(); self.settled()
        with self.assertRaisesRegex(q2.Denied, "binding_unobserved"):
            self.meter.fixture("kimi_web_fetch", {"url": "https://example.com/ordinary"},
                               {"threadId": "ordinary-thread", "x-codex-turn-metadata": {"turn_id": "previous-turn"}})
        row = self.meter.state["denials"][0]
        self.assertEqual(row["surface"], "mcp")
        self.assertEqual(row["counts"]["real_model_http"], 0)

    def test_sampling_waits_for_native_ack_without_delivering_source_text(self):
        other = self.root / "sampling"
        other.mkdir()
        self.meter = q2.Meter(other, mode="first_action")
        self.arm(); self.settled()
        meta = {"threadId": "ordinary-thread", "x-codex-turn-metadata": {"turn_id": "ordinary-turn"}}
        results = []
        worker = threading.Thread(target=lambda: results.append(self.meter.fixture("kimi_web_fetch", {"url": "https://example.com/ordinary"}, meta)))
        worker.start()
        with self.meter.changed:
            self.assertTrue(self.meter.changed.wait_for(lambda: self.meter.mcp_inflight == 1, 3))
        trigger = {"root_thread_id": "ordinary-thread", "thread_id": "ordinary-thread", "turn_id": "ordinary-turn", "item_id": "ordinary-item"}
        boundary = self.meter.sample(trigger)
        self.assertTrue(boundary["closed"])
        self.assertEqual(results, [])
        self.meter.acknowledge_interrupt(trigger)
        worker.join(3)
        self.assertFalse(worker.is_alive())
        self.assertEqual(results[0]["error"]["code"], "cancelled")
        self.assertNotIn("markdown", results[0])
        with self.assertRaisesRegex(q2.Denied, "sampling_boundary_closed"):
            self.meter.admit(metadata())
        self.assertFalse(self.meter.state["halted"])
        self.assertTrue(self.meter.state["denials"][0]["expected_sampling_stop"])
        self.meter.finish()
        self.meter.arm("kimi-k3.1-pro", "Q2-02", "new-thread")
        with self.assertRaisesRegex(q2.Denied, "sampling_boundary_closed"):
            self.meter.admit(metadata())
        self.assertEqual(self.meter.state["denials"][-1]["case_id"], "Q2-01")
        self.assertFalse(self.meter.state["halted"])

    def test_live_and_credentials_are_not_cli_options(self):
        out = self.root / "not-created"
        command = [sys.executable, str(q2.v1.HOST / "scripts/feat175_q2_v2_run.py")]
        for args in (["live", "--output", str(out)], ["local", "--output", str(out), "--kimi-key-file", "/nonexistent/unused"]):
            result = subprocess.run(command + args, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(out.exists())

    def test_child_and_root_requests_share_the_same_sampling_cutoff(self):
        other = self.root / "child-sampling"
        other.mkdir()
        self.meter = q2.Meter(other, mode="first_action")
        self.arm(); self.settled()
        child = {**metadata(), "thread_id": "ordinary-child", "turn_id": "ordinary-child-turn"}
        row = self.meter.admit(child); self.meter.settle(row, observation())
        binding = {"root_thread_id": "ordinary-thread", **{k:child[k] for k in ["thread_id", "turn_id"]}, "item_id": "ordinary-child-item"}
        self.meter.sample(binding); self.meter.acknowledge_interrupt(binding)
        for request in [metadata(), child]:
            with self.assertRaisesRegex(q2.Denied, "sampling_boundary_closed"):
                self.meter.admit(request)
        self.assertTrue(all(d["task_index"] == 0 and d["counts"]["model_task"] == 2 for d in self.meter.state["denials"]))
        self.assertFalse(self.meter.state["halted"])

    def test_repeated_hosted_fixture_keeps_two_distinct_native_items(self):
        raw = q2.response_fixture({}, {"case_id":"Q2-02","model":"kimi-k3.1-pro","ordinal":1,"case_request_number":1}, "repeated-hosted")
        observer = q2.v1.Observer(); observer.feed(raw)
        self.assertEqual(observer.result["native_search_calls"], 2)
        self.assertEqual(len(set(observer.result["hosted_item_ids"])), 2)


if __name__ == "__main__":
    unittest.main()
