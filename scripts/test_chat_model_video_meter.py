"""Offline admission checks; real repository Python process, no model request."""
import json
from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest

class VideoMeterTest(unittest.TestCase):
    def test_exact_video_scope_and_normal_eof(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            key = root / 'ordinary-key'
            fd = os.open(key, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'w') as stream:
                stream.write('ordinary-offline-fixture')
            authorization = {'purpose': 'FEAT-173-video-mcp', 'models': ['kimi-k3.1-pro', 'MiniMax-M3'],
                             'preserve_payloads': True, 'max_http_requests': 32,
                             'max_image_understanding_requests': 32, 'max_seedance_creates': 4,
                             'max_seedance_queries': 120, 'max_video_downloads': 8,
                             'max_seedream_generations': 0, 'max_linkfox_calls': 0}
            for change, expected in [({}, 0), ({'purpose': 'FEAT-172-media-mcp'}, 1),
                                     ({'max_http_requests': 33}, 1), ({'max_seedance_creates': 5}, 1)]:
                with self.subTest(change=change):
                    ledger = root / 'ledger.json'
                    ledger.write_text(json.dumps({'authorization': {**authorization, **change},
                                                  'real_http_requests': 0, 'image_understanding_requests': 0,
                                                  'requests': []}))
                    child = subprocess.Popen([sys.executable, '-u', str(Path(__file__).with_name('chat-model-smoke-meter.py')),
                                              '--port', '0', '--ledger', str(ledger), '--key-file', str(key),
                                              '--kimi-key-file', str(key), '--qualify-video-mcp', '--shutdown-on-stdin-eof'],
                                             stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                    # Empty stdin closes normally; no signal or process kill.
                    stdout, _ = child.communicate('', timeout=5)
                    self.assertEqual(child.returncode, expected)
                    result = json.loads(ledger.read_text())
                    self.assertEqual(result['real_http_requests'], 0)
                    if expected == 0:
                        self.assertTrue(json.loads(stdout.splitlines()[0])['ready'])
                        self.assertEqual(result['meter_state'], 'stopped_normally')
                    else:
                        self.assertNotIn('meter_state', result)

if __name__ == '__main__':
    unittest.main()
