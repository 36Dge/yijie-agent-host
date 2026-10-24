#!/usr/bin/env python3
"""Sync FEAT-164 inactive projections from one immutable Contracts commit.

Reads Git objects only. Does not generate old families, activate a service,
modify existing v1 projections, or include unrelated sibling working changes.
"""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path

HOST = Path(__file__).resolve().parents[1]
CONTRACTS = HOST.parent / "yijie-contracts"
DESKTOP = HOST.parent / "yijie-desktop"


def digest(value):
    return hashlib.sha256(value).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--commit", required=True)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    if len(args.commit) != 40 or any(c not in "0123456789abcdef" for c in args.commit):
        raise SystemExit("A full immutable Contracts SHA is required")

    def git(*parts):
        return subprocess.check_output(["git", "-C", str(CONTRACTS), *parts])

    if git("rev-parse", "--verify", args.commit + "^{commit}").decode().strip() != args.commit:
        raise SystemExit("Contracts commit did not resolve exactly")

    def read(name):
        if name.startswith("/") or any(p in ("", ".", "..") for p in name.split("/")):
            raise SystemExit("Source must be a repository-relative path")
        return git("show", args.commit + ":" + name)

    locks = {}
    for family in ("chat-models-v2", "runtime-kimi-k31-pro"):
        name = f"compatibility/{family}/source.lock.json"
        raw = read(name)
        lock = json.loads(raw)
        for row in lock.get("sources", []) + lock.get("generators", []) + lock["generated"]:
            if digest(read(row["path"])) != row["sha256"]:
                raise SystemExit("Committed source digest mismatch: " + row["path"])
        locks[family] = {
            "schema_version": 1, "feature": "FEAT-164", "release": False,
            "mode": "immutable_local_candidate", "source_commit": args.commit,
            "source_lock_path": name, "source_lock_sha256": digest(raw),
            "authority": lock,
        }

    copies = [
        (HOST, "sdks/go/openapi/chat-models-v2/types.gen.go", "internal/contracts/chatmodelsv2/types.gen.go"),
        (HOST, "sdks/jsonschema/chat-models-v2.schema.json", "internal/contracts/chatmodelsv2/schema.json"),
        (HOST, "jsonschema/chat/model-selection-v1.schema.json", "internal/contracts/chatmodelsv2/legacy.schema.json"),
        (HOST, "sdks/go/openapi/runtime-kimi-k31-pro/artifact.gen.go", "internal/contracts/runtimekimik31pro/artifact.gen.go"),
        (DESKTOP, "sdks/rust/chat-models-v2/types.gen.rs", "src-tauri/src/chat/models_v2_generated.rs"),
        (DESKTOP, "sdks/typescript/src/jsonschema/chat-models-v2.gen.ts", "src/domain/chat-models-v2.generated.ts"),
        (DESKTOP, "sdks/jsonschema/chat-models-v2.schema.json", "contracts/chat-models-v2.schema.json"),
        (DESKTOP, "jsonschema/chat/model-selection-v1.schema.json", "contracts/chat-models-v1-history.schema.json"),
    ]
    outputs = [(repo / dest, read(source)) for repo, source, dest in copies]
    for family, lock in locks.items():
        lock["copies"] = [
            {"repository": repo.name, "source": source, "destination": dest, "sha256": digest(data)}
            for (repo, source, dest), (_, data) in zip(copies, outputs)
            if (family == "runtime-kimi-k31-pro") == ("runtime-kimi-k31-pro" in source)
        ]
        value = (json.dumps(lock, indent=2, ensure_ascii=False) + "\n").encode()
        outputs.extend([(HOST / "api" / (family + ".candidate.json"), value),
                        (DESKTOP / "contracts" / (family + ".candidate.json"), value)])
    for file, data in outputs:
        if args.check:
            if not file.is_file() or file.read_bytes() != data:
                raise SystemExit("Consumer projection drift: " + str(file))
        else:
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_bytes(data)
    print(f"FEAT-164 projections {'verified' if args.check else 'synced'} from {args.commit}; inactive.")


if __name__ == "__main__":
    main()
