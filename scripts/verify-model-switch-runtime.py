#!/usr/bin/env python3
"""FEAT-156: fixed Runtime + normal loopback Responses, no credentials or remote calls.
Only normal stdin EOF shutdown is used. On timeout the child is left intact and reported.
This verifies native routing, never real-provider quality or D4.
"""
import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import queue
import subprocess
import tempfile
import threading
import time
import uuid

parser = argparse.ArgumentParser()
parser.add_argument('--binary', required=True)
parser.add_argument('--manifest', required=True)
parser.add_argument('--output', required=True)
args = parser.parse_args()
binary = Path(args.binary).resolve()
manifest = json.loads(Path(args.manifest).read_text())
assert hashlib.sha256(binary.read_bytes()).hexdigest() == manifest['runtime']['sha256']
root = Path(tempfile.mkdtemp(prefix='feat156-native-model-'))
workspace = root / 'workspace'
home = root / 'codex-home'
workspace.mkdir()
home.mkdir()
observed = []
report = {'kind': 'fixed-runtime-normal-loopback-provider', 'paid_calls': 0,
          'runtime_sha256': manifest['runtime']['sha256'], 'root': str(root),
          'requests': observed, 'steps': [], 'passed': False}

class Provider(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        n = int(self.headers.get('Content-Length', '0'))
        if not 0 < n <= 4 * 1024 * 1024 or len(observed) >= 6:
            self.send_error(400)
            return
        body = json.loads(self.rfile.read(n))
        observed.append({'route': self.path, 'model': body.get('model'), 'reasoning': body.get('reasoning')})
        rid = 'resp_' + uuid.uuid4().hex
        message = {'type': 'message', 'id': 'msg_' + uuid.uuid4().hex,
                   'role': 'assistant', 'status': 'completed', 'phase': 'final_answer',
                   'content': [{'type': 'output_text', 'text': 'LOCAL_MODEL_ROUTE_OK', 'annotations': []}]}
        response = {'id': rid, 'object': 'response', 'created_at': int(time.time()),
                    'status': 'completed', 'model': body['model'], 'output': [message],
                    'usage': {'input_tokens': 1, 'output_tokens': 1, 'total_tokens': 2}}
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.end_headers()
        events = [
            {'type': 'response.created', 'response': dict(response, status='in_progress', output=[])},
            {'type': 'response.output_item.added', 'output_index': 0,
             'item': dict(message, status='in_progress', content=[])},
            {'type': 'response.output_text.delta', 'item_id': message['id'], 'output_index': 0,
             'content_index': 0, 'delta': 'LOCAL_MODEL_ROUTE_OK'},
            {'type': 'response.output_item.done', 'output_index': 0, 'item': message},
            {'type': 'response.completed', 'response': response},
        ]
        for event in events:
            self.wfile.write(('data: ' + json.dumps(event) + '\n\n').encode())
        self.wfile.flush()

server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Provider)
threading.Thread(target=server.serve_forever, daemon=True).start()
base = f'http://127.0.0.1:{server.server_port}'
models = []
for model, effort, window in [('kimi-k3', 'max', 1048576), ('MiniMax-M3', 'high', 1000000)]:
    models.append(dict(slug=model, display_name=model, description='Normal local route verification',
        default_reasoning_level=effort, supported_reasoning_levels=[{'effort': effort, 'description': effort}],
        shell_type='shell_command', visibility='list', supported_in_api=True, priority=0,
        availability_nux=None, upgrade=None, base_instructions='Reply without calling tools.',
        model_messages=None, supports_reasoning_summaries=True, default_reasoning_summary='none',
        support_verbosity=False, default_verbosity=None, apply_patch_tool_type=None,
        truncation_policy={'mode': 'bytes', 'limit': 10000}, supports_parallel_tool_calls=True,
        supports_image_detail_original=False, context_window=window, max_context_window=window,
        auto_compact_token_limit=900000, effective_context_window_percent=95,
        experimental_supported_tools=[], input_modalities=['text', 'image'],
        supports_search_tool=False, use_responses_lite=False))
(home/'models.json').write_text(json.dumps({'models': models}))
config = ['model="kimi-k3"', 'model_provider="kimi"', 'model_reasoning_effort="max"',
          'model_reasoning_summary="none"', 'model_catalog_json='+json.dumps(str(home/'models.json')),
          'approval_policy="never"', 'sandbox_mode="read-only"',
          '[features]', 'hooks=false', 'plugins=false', 'apps=false', 'shell_snapshot=false', 'web_search_request=false']
for provider in ['kimi', 'minimax']:
    config += [f'[model_providers.{provider}]', f'name="{provider}"',
               'base_url='+json.dumps(base+'/'+provider+'/v1'), 'wire_api="responses"',
               'requires_openai_auth=false', 'supports_websockets=false',
               'request_max_retries=0', 'stream_max_retries=0']
(home/'config.toml').write_text('\n'.join(config)+'\n')
log = (root/'runtime.stderr.log').open('w')
env = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'CODEX_HOME': str(home), 'RUST_LOG': 'error'}
proc = subprocess.Popen([str(binary), 'app-server', '--listen', 'stdio://', '--strict-config'],
                        cwd=workspace, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=log, text=True, bufsize=1)
report['pid'] = proc.pid
inbox = queue.Queue()
def read_stdout():
    for line in proc.stdout:
        inbox.put(json.loads(line))
    inbox.put(None)
threading.Thread(target=read_stdout, daemon=True).start()
next_id = 0
notifications = []
def receive(deadline):
    msg = inbox.get(timeout=max(.01, deadline-time.monotonic()))
    if msg is None:
        raise RuntimeError('Runtime closed stdout')
    if 'method' in msg and 'id' in msg:
        # This test requests no tool or user action; never approve a reverse request.
        proc.stdin.write(json.dumps({'id': msg['id'], 'error': {'code': -32601, 'message': 'No test action authorized'}})+'\n')
        proc.stdin.flush()
    return msg

def rpc(method, params):
    global next_id
    next_id += 1
    ident = next_id
    proc.stdin.write(json.dumps({'id': ident, 'method': method, 'params': params})+'\n')
    proc.stdin.flush()
    deadline = time.monotonic()+20
    while time.monotonic() < deadline:
        msg = receive(deadline)
        if msg.get('id') == ident:
            if 'error' in msg:
                raise RuntimeError(method+': '+json.dumps(msg['error']))
            return msg['result']
        notifications.append(msg)
    raise RuntimeError(method+' timeout')

def complete_turn(tid, model, effort):
    result = rpc('turn/start', {'threadId': tid, 'model': model, 'effort': effort,
        'input': [{'type': 'text', 'text': 'Reply LOCAL_MODEL_ROUTE_OK without tools.'}]})
    turn = result['turn']['id']
    deadline = time.monotonic()+25
    while time.monotonic() < deadline:
        candidates = notifications[:]
        notifications.clear()
        for msg in candidates:
            if msg.get('method') == 'turn/completed' and msg['params']['turn']['id'] == turn:
                assert msg['params']['turn']['status'] == 'completed', msg['params']['turn']['status']
                return
        notifications.append(receive(deadline))
    raise RuntimeError('turn completion timeout')

try:
    rpc('initialize', {'clientInfo': {'name': 'yijie_feat156_probe', 'version': '0.1.0'},
                       'capabilities': {'experimentalApi': False}})
    proc.stdin.write(json.dumps({'method': 'initialized', 'params': {}})+'\n')
    proc.stdin.flush()
    initial = rpc('thread/start', {'model': 'kimi-k3', 'modelProvider': 'kimi', 'cwd': str(workspace),
        'approvalPolicy': 'never', 'sandbox': 'read-only', 'config': {'model_reasoning_effort': 'max'},
        'ephemeral': False})
    tid = initial['thread']['id']
    assert initial['model'] == 'kimi-k3' and initial['modelProvider'] == 'kimi'
    report['steps'].append({'step': 'start', 'model': initial['model'], 'provider': initial['modelProvider']})
    complete_turn(tid, 'kimi-k3', 'max')
    for model, provider, effort in [('MiniMax-M3', 'minimax', 'high'), ('kimi-k3', 'kimi', 'max')]:
        rpc('thread/unsubscribe', {'threadId': tid})
        switched = rpc('thread/resume', {'threadId': tid, 'model': model, 'modelProvider': provider,
            'cwd': str(workspace), 'approvalPolicy': 'never', 'sandbox': 'read-only',
            'config': {'model_reasoning_effort': effort}})
        report['steps'].append({'step': 'switch', 'model': switched['model'], 'provider': switched['modelProvider'],
                                'same_thread': switched['thread']['id'] == tid})
        assert switched['thread']['id'] == tid and switched['model'] == model and switched['modelProvider'] == provider
        complete_turn(tid, model, effort)
    assert [(r['route'], r['model'], r['reasoning']['effort']) for r in observed] == [
        ('/kimi/v1/responses', 'kimi-k3', 'max'), ('/minimax/v1/responses', 'MiniMax-M3', 'high'),
        ('/kimi/v1/responses', 'kimi-k3', 'max')]
    report['passed'] = True
except Exception as exc:
    report['failure'] = str(exc) or type(exc).__name__
finally:
    proc.stdin.close()
    try:
        report['normal_eof_exit'] = proc.wait(timeout=25)
    except subprocess.TimeoutExpired:
        report['normal_stop_pending'] = True
        report['passed'] = False
    server.shutdown()
    server.server_close()
    log.close()
    Path(args.output).write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
raise SystemExit(0 if report['passed'] and report.get('normal_eof_exit') == 0 else 1)
