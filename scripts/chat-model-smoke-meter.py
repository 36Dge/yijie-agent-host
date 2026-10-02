#!/usr/bin/env python3
"""FEAT-156 owner-authorized two-provider verification meter.
No prompts, response bodies or credentials are logged. This is verification
support, not an approval engine; Codex still produces and handles approvals.
"""
import argparse
import hashlib
import hmac
import http.client
import http.server
import json
import os
from pathlib import Path
import threading
import time

parser = argparse.ArgumentParser()
parser.add_argument('--port', type=int, default=18083)
parser.add_argument('--ledger', type=Path, required=True)
parser.add_argument('--key-file', type=Path, required=True)
parser.add_argument('--kimi-key-file', type=Path, required=True)
parser.add_argument('--limit', type=int, help='Optional batch ceiling within the approved ledger total.')
parser.add_argument('--observe-tool-stream', action='store_true',
                    help='Record only tool argument byte counts/digests, never content.')
args = parser.parse_args()
def read_key(path):
    import stat
    info=path.lstat()
    if not path.is_absolute() or not stat.S_ISREG(info.st_mode) or info.st_uid!=os.getuid() or info.st_nlink!=1 or info.st_mode & 0o077 or not 0<info.st_size<=16384:
        raise SystemExit('Provider credential file is not an owner-only source.')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
    with os.fdopen(fd) as file:
        current=os.fstat(file.fileno())
        if (info.st_dev,info.st_ino)!=(current.st_dev,current.st_ino): raise SystemExit('Provider source changed.')
        key=file.read(16385).strip()
    if not key or len(key)>16384 or any(c in key for c in ('\r','\n','\0')): raise SystemExit('Provider credential is unavailable.')
    return key
providers={'MiniMax-M3':('api.minimaxi.com',read_key(args.key_file),'high'),'kimi-k3':('api.moonshot.cn',read_key(args.kimi_key_file),'max')}
lock=threading.Lock()
ledger=json.loads(args.ledger.read_text())
authorization=ledger.get('authorization',{})
approved_limit = authorization.get('max_http_requests')
approved_images = authorization.get('max_image_understanding_requests')
if (approved_limit, approved_images) not in ((24, 2), (40, 4), (50, 4)) or authorization.get('max_output_tokens_per_request')!=8192:
    raise SystemExit('Explicit FEAT-156 authorization ledger is required.')
if args.limit is None:
    args.limit = approved_limit
if not 0 < args.limit <= approved_limit:
    raise SystemExit('Batch ceiling exceeds the approved FEAT-156 total.')
if ledger['real_http_requests']!=len(ledger['requests']): raise SystemExit('Ledger count mismatch.')

def save():
    temporary = args.ledger.with_suffix('.pending.json')
    with open(temporary, 'w', encoding='utf8') as output:
        json.dump(ledger, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, args.ledger)

class ToolStreamSummary:
    """Passive, bounded SSE diagnostics; forwarded bytes are never rewritten."""
    def __init__(self):
        self.buffer = b''
        self.items = {}
        self.truncated = False

    def feed(self, chunk):
        if self.truncated:
            return
        self.buffer += chunk
        if len(self.buffer) > 1024 * 1024:
            self.truncated = True
            self.buffer = b''
            return
        while b'\n' in self.buffer:
            line, self.buffer = self.buffer.split(b'\n', 1)
            if not line.startswith(b'data:'):
                continue
            try:
                event = json.loads(line[5:].strip())
                if not isinstance(event, dict):
                    continue
                kind = event.get('type')
                if kind in ('response.function_call_arguments.delta', 'response.function_call_arguments.done'):
                    identity = event.get('item_id')
                    if not isinstance(identity, str):
                        continue
                    row = self.row(identity)
                    value = event.get('delta' if kind.endswith('.delta') else 'arguments')
                    if not isinstance(value, str):
                        continue
                    data = value.encode()
                    if kind.endswith('.delta'):
                        row['delta_count'] += 1
                        row['delta_bytes'] += len(data)
                        row['_delta_hash'].update(data)
                    else:
                        row['arguments_done'] = self.argument_summary(value)
                elif kind in ('response.output_item.added', 'response.output_item.done'):
                    self.item(event.get('item'), 'item_added' if kind.endswith('.added') else 'item_done')
                elif kind == 'response.completed':
                    response = event.get('response') or {}
                    if isinstance(response, dict):
                        for item in response.get('output') or []:
                            self.item(item, 'response_completed')
            except (ValueError, TypeError):
                continue

    def row(self, identity):
        key = hashlib.sha256(identity.encode()).hexdigest()
        if key not in self.items:
            if len(self.items) >= 64:
                self.truncated = True
                raise ValueError('diagnostic item bound')
            self.items[key] = {'delta_count': 0, 'delta_bytes': 0, '_delta_hash': hashlib.sha256()}
        return self.items[key]

    @staticmethod
    def argument_summary(value):
        data = value.encode()
        try:
            valid_object = isinstance(json.loads(value), dict)
        except ValueError:
            valid_object = False
        return {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest(), 'json_object': valid_object}

    def item(self, item, stage):
        if not isinstance(item, dict) or item.get('type') != 'function_call':
            return
        identity = item.get('id')
        if isinstance(identity, str) and isinstance(item.get('arguments'), str):
            self.row(identity)[stage] = self.argument_summary(item['arguments'])

    def summary(self):
        rows = []
        for identity, row in self.items.items():
            rows.append({'item_sha256': identity,
                         **{k: v for k, v in row.items() if not k.startswith('_')},
                         'delta_sha256': row['_delta_hash'].hexdigest()})
        return {'items': rows, 'truncated': self.truncated}

class Meter(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        if self.path != '/v1/responses':
            self.send_error(403, 'Request is not authorized')
            return
        length = int(self.headers.get('Content-Length', '0'))
        if not 0 < length <= 32 * 1024 * 1024:
            self.send_error(400, 'Request size is invalid')
            return
        payload = self.rfile.read(length)
        try:
            body = json.loads(payload)
            provider=providers.get(body.get('model'))
            if provider is None or not hmac.compare_digest(self.headers.get('Authorization',''),'Bearer '+provider[1]):
                self.send_error(403,'Provider profile differs'); return
            effort=(body.get('reasoning') or {}).get('effort')
            if effort != provider[2]:
                self.send_error(400,'Reasoning profile differs'); return
            def image_input(value):
                if isinstance(value,dict): return value.get('type')=='input_image' or any(image_input(v) for v in value.values())
                return isinstance(value,list) and any(image_input(v) for v in value)
            images=image_input(body.get('input'))
            body['max_output_tokens']=min(body.get('max_output_tokens') or 8192,8192)
            payload=json.dumps(body).encode()
            output_format = (body.get('text') or {}).get('format') or {}
            structured_output = output_format.get('type') == 'json_schema'
        except (ValueError, AttributeError):
            self.send_error(400, 'Verification request metadata is invalid')
            return
        with lock:
            if len(ledger['requests']) >= args.limit or (images and ledger['image_understanding_requests']>=approved_images):
                self.send_error(429, 'Approved verification request limit reached')
                return
            item={'ordinal':len(ledger['requests'])+1,'admitted_at':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'status':'forwarding','model':body['model'],'effort':effort,'structured_output':structured_output,'image_understanding':images,'max_output_tokens':body['max_output_tokens'],'tool_choice':body.get('tool_choice'),'tool_count':len(body.get('tools') or []),'upstream':'https://'+provider[0]+'/v1/responses'}
            ledger['real_http_requests']+=1
            if images: ledger['image_understanding_requests']+=1
            ledger['requests'].append(item)
            save()
        connection = http.client.HTTPSConnection(provider[0], timeout=240)
        observer = ToolStreamSummary() if args.observe_tool_stream else None
        try:
            headers = {k: v for k, v in self.headers.items() if k.lower() not in ('host', 'connection', 'content-length', 'transfer-encoding')}
            headers['Content-Length'] = str(len(payload))
            connection.request('POST', '/v1/responses', body=payload, headers=headers)
            result = connection.getresponse()
            with lock:
                item['http_status'] = result.status
                save()
            if result.status >= 400:
                failure=result.read(16384)
                try:
                    detail=json.loads(failure).get('error',{})
                    message=str(detail.get('message',''))[:1000]
                    for _,secret,_ in providers.values(): message=message.replace(secret,'[redacted]')
                    import re
                    message=re.sub(r'sk-[A-Za-z0-9_-]+','[redacted]',message)
                    with lock:
                        item['error_code']=str(detail.get('code',''))[:80]
                        item['error_message']=message
                        save()
                except (ValueError,AttributeError): pass
                self.send_response(result.status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(failure)));self.end_headers();self.wfile.write(failure)
                with lock: item['status']='completed';save()
                return
            self.send_response(result.status)
            for name, value in result.getheaders():
                if name.lower() not in ('connection', 'transfer-encoding', 'content-length'):
                    self.send_header(name, value)
            self.end_headers()
            while True:
                chunk = result.read1(65536)
                if not chunk:
                    break
                if observer is not None:
                    observer.feed(chunk)
                self.wfile.write(chunk)
                self.wfile.flush()
            with lock:
                item['status'] = 'completed'
                if observer is not None:
                    item['tool_stream'] = observer.summary()
                save()
        except (OSError, http.client.HTTPException) as error:
            with lock:
                item['status'] = 'transport_ended'
                # Classification only; exception text may contain request data.
                item['transport_error_class'] = type(error).__name__
                if observer is not None:
                    item['tool_stream'] = observer.summary()
                save()
            self.close_connection = True
        finally:
            connection.close()

server = http.server.ThreadingHTTPServer(('127.0.0.1', args.port), Meter)
with lock:
    ledger['meter_state']='running'
    save()
print(json.dumps({'ready': True, 'port': args.port, 'used': len(ledger['requests']), 'limit': args.limit}), flush=True)
try:
    server.serve_forever()
except KeyboardInterrupt:
    pass
finally:
    server.server_close()
    with lock:
        ledger['meter_state']='stopped_normally'
        save()
