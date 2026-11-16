"""Private QA policy/grants. No credential or network I/O in this module."""
import datetime as dt
import json
from pathlib import Path
import uuid

import feat175_q2_meter as v1

PACKAGE = v1.HOST / "internal/codex/testdata/feat175-q2-a0"
POLICY = json.loads((PACKAGE / "policy.json").read_text())
PURPOSE = POLICY["purpose"]


def source_lock():
    return json.loads((PACKAGE / "source-lock.json").read_text())


def verify_sources():
    lock = source_lock()
    for row in lock["sources"]:
        file = (v1.WORKSPACE / row["file"]).resolve()
        file.relative_to(v1.WORKSPACE)
        v1.require(v1.file_digest(file) == row["sha256"], "A0 source changed")
    v1.require(v1.file_digest(v1.WORKSPACE / lock["runtime_relative_path"] / "codex") == lock["runtime_binary_sha256"], "Pinned Runtime changed")
    return lock


def validate_grant(grant, *, at=None):
    v1.require(isinstance(grant, dict) and grant.get("status") == "approved"
               and grant.get("purpose") == PURPOSE and grant.get("schema_version") == 3,
               "New Q2-v2A scoped budget approval required")
    keys = {"schema_version", "feature", "purpose", "status", "authorization_id", "approved_by", "approval_source",
            "approved_at", "expires_at", "source_lock_sha256", "policy_sha256", "package_sha256", "profile_sha256",
            "pricing_sha256", "schedule_sha256", "stages", "money_mode", "allow_inflight_overshoot", "supplier_rest_calls"}
    v1.require(set(grant) == keys and grant["feature"] == "FEAT-175", "Closed grant schema required")
    v1.require(str(uuid.UUID(grant["authorization_id"])) == grant["authorization_id"], "Canonical grant identity required")
    v1.require(grant["approved_by"] == "段成威" and isinstance(grant["approval_source"], str)
               and grant["approval_source"].startswith("current-user-reply:"), "Current explicit user decision required")
    at = at or v1.now()
    issued, expires = (dt.datetime.fromisoformat(grant[k]) for k in ("approved_at", "expires_at"))
    v1.require(issued.tzinfo and expires.tzinfo and issued <= at < expires <= issued + dt.timedelta(hours=24), "Grant expired or unbounded")
    prices = json.loads((v1.PACKAGE / "pricing-snapshot.json").read_text())
    checked = dt.datetime.fromisoformat(prices["checked_at"])
    v1.require(dt.timedelta(0) <= at - checked <= dt.timedelta(days=7), "Pricing refresh required")
    expected = {"source_lock_sha256":v1.digest((PACKAGE / "source-lock.json").read_bytes()),
                "policy_sha256":v1.digest((PACKAGE / "policy.json").read_bytes()),
                "package_sha256":v1.digest((v1.PACKAGE / "cases.json").read_bytes()),
                "profile_sha256":v1.digest((v1.PACKAGE / "effective-profile.json").read_bytes()),
                "pricing_sha256":v1.shared.canonical_digest(prices),
                "schedule_sha256":v1.shared.canonical_digest(POLICY["schedule"])}
    v1.require(all(grant[k] == value for k, value in expected.items()), "Grant sources differ")
    v1.require(grant["stages"] in (["A1"], ["A1", "A2"]), "Only frozen stage scopes allowed")
    v1.require(grant["money_mode"] == "soft_stop_after_observation" and grant["allow_inflight_overshoot"] is True
               and type(grant["supplier_rest_calls"]) is int and grant["supplier_rest_calls"] == 0,
               "Explicit soft-stop/overshoot scope and zero supplier REST required")
    return grant


def consume_grant(grant, state, output):
    """Call under the existing run.lock, after source validation, before keys."""
    validate_grant(grant)
    path = Path(state) / ("used-" + grant["authorization_id"] + ".json")
    # Shares the prior one-shot registry: changing scope or output cannot reuse an ID.
    with path.open("x", encoding="utf-8") as f:
        json.dump({"authorization_sha256":v1.shared.canonical_digest(grant), "purpose":PURPOSE,
                   "evidence":str(output), "policy":"consumed once including aborted startup; no restart or refund"}, f)
    return path
