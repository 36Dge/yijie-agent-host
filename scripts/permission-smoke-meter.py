#!/usr/bin/env python3
"""Local permission verification paid-request cap, shared by S2 through S4.
No prompts, response bodies or credentials are logged. This is verification
support, not an approval engine; Codex still produces and handles approvals.
"""
import argparse
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
parser.add_argument('--limit', type=int, required=True)
args = parser.parse_args()
if not 0 < args.limit <= 45:
    raise SystemExit('The approved request limit must be between 1 and 45.')
key = args.key_file.read_text().strip()
if not key:
    raise SystemExit('Provider credential is unavailable.')
lock = threading.Lock()
args.ledger.parent.mkdir(parents=True, exist_ok=True)
ledger = json.loads(args.ledger.read_text()) if args.ledger.exists() else {'limit': args.limit, 'requests': []}
if ledger['limit'] != args.limit:
    raise SystemExit('Existing request ledger limit differs.')

def save():
    temporary = args.ledger.with_suffix('.pending.json')
    with open(temporary, 'w', encoding='utf8') as output:
        json.dump(ledger, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, args.ledger)

with lock:
    save()

class Meter(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        if self.path != '/v1/responses' or not hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + key):
            self.send_error(403, 'Request is not authorized')
            return
        length = int(self.headers.get('Content-Length', '0'))
        if not 0 < length <= 32 * 1024 * 1024:
            self.send_error(400, 'Request size is invalid')
            return
        payload = self.rfile.read(length)
        try:
            body = json.loads(payload)
            if body.get('model') != 'MiniMax-M3':
                self.send_error(400, 'Verification model differs from the approved provider model')
                return
            output_format = (body.get('text') or {}).get('format') or {}
            structured_output = output_format.get('type') == 'json_schema'
        except (ValueError, AttributeError):
            self.send_error(400, 'Verification request metadata is invalid')
            return
        with lock:
            if len(ledger['requests']) >= args.limit:
                self.send_error(429, 'Approved verification request limit reached')
                return
            item = {'ordinal': len(ledger['requests']) + 1, 'admitted_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'status': 'forwarding', 'model': 'MiniMax-M3', 'structured_output': structured_output, 'upstream': 'https://api.minimaxi.com/v1/responses'}
            ledger['requests'].append(item)
            save()
        connection = http.client.HTTPSConnection('api.minimaxi.com', timeout=240)
        try:
            headers = {k: v for k, v in self.headers.items() if k.lower() not in ('host', 'connection', 'content-length', 'transfer-encoding')}
            headers['Content-Length'] = str(len(payload))
            connection.request('POST', '/v1/responses', body=payload, headers=headers)
            result = connection.getresponse()
            with lock:
                item['http_status'] = result.status
                save()
            self.send_response(result.status)
            for name, value in result.getheaders():
                if name.lower() not in ('connection', 'transfer-encoding', 'content-length'):
                    self.send_header(name, value)
            self.end_headers()
            while True:
                chunk = result.read1(65536)
                if not chunk:
                    break
                self.wfile.write(chunk)
                self.wfile.flush()
            with lock:
                item['status'] = 'completed'
                save()
        except (OSError, http.client.HTTPException):
            with lock:
                item['status'] = 'transport_ended'
                save()
            self.close_connection = True
        finally:
            connection.close()

server = http.server.ThreadingHTTPServer(('127.0.0.1', args.port), Meter)
print(json.dumps({'ready': True, 'port': args.port, 'used': len(ledger['requests']), 'limit': args.limit}), flush=True)
try:
    server.serve_forever()
except KeyboardInterrupt:
    pass
finally:
    server.server_close()
