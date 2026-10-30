"""Ordinary JSON/path/file tests; no compiler invocation or executable substitution."""
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("artifact_output", Path(__file__).with_name("artifact_build_output.py"))
output = importlib.util.module_from_spec(spec)
spec.loader.exec_module(output)


class ArtifactOutput(unittest.TestCase):
    def setUp(self):
        self.manifest = Path("/ordinary/source/artifact-mcp/Cargo.toml")
        # Read-only validation of the genuine interpreter; never copied or run.
        self.real_file = Path(sys.executable).resolve(strict=True)
        self.artifact = {"reason": "compiler-artifact", "manifest_path": str(self.manifest),
                         "target": {"name": "yijie-artifact-mcp", "kind": ["bin"]},
                         "profile": {"test": False}, "executable": str(self.real_file)}

    def stream(self, *items, success=True):
        return "\n".join(json.dumps(v) for v in (*items, {"reason": "build-finished", "success": success}))

    def test_actual_cargo_path_and_fresh_cache_hit(self):
        for fresh in (False, True):
            self.assertEqual(output.executable(self.stream({**self.artifact, "fresh": fresh}), self.manifest), self.real_file)

    def test_failure_missing_or_wrong_target_never_falls_back(self):
        variants = [self.stream(self.artifact, success=False), self.stream(),
                    self.stream({**self.artifact, "executable": None}),
                    self.stream({**self.artifact, "manifest_path": "/ordinary/other/Cargo.toml"}),
                    self.stream({**self.artifact, "target": {"name": "ordinary", "kind": ["lib"]}})]
        for value in variants:
            with self.assertRaises(ValueError):
                output.executable(value, self.manifest)

    def test_ambiguous_or_absent_result_rejected(self):
        with self.assertRaises(ValueError):
            output.executable(self.stream(self.artifact, {**self.artifact, "executable": "/ordinary/second"}), self.manifest)
        with self.assertRaises(ValueError):
            output.executable(json.dumps(self.artifact), self.manifest)

    def test_normal_data_collection_digest_and_receipt(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            source = root / "ordinary.txt"
            source.write_text("ordinary collection data; never executable\n")
            output.publish(source, root / "collected")
            record = json.loads((root / "collected/yijie-artifact-mcp.build.json").read_text())
            self.assertEqual(record["source"], str(source))
            self.assertEqual(record["sha256"], output.hashlib.sha256(source.read_bytes()).hexdigest())
            self.assertEqual((root / "collected/yijie-artifact-mcp").read_bytes(), source.read_bytes())
            self.assertEqual((root / "collected/yijie-artifact-mcp.sha256").read_text().strip(), record["sha256"])


if __name__ == "__main__":
    unittest.main()
