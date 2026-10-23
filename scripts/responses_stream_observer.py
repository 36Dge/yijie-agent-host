"""Bounded, passive diagnostics for the existing local Responses meter.

Only closed event kinds, counts and digests leave this observer. It neither
retains response text nor changes the bytes forwarded to the fixed provider.
"""
import hashlib
import json
import re


def request_input_summary(value):
    """Passive tool-history correlation, without retaining inputs or outputs."""
    items = value if isinstance(value, list) else []
    rows = []
    digest = lambda text: hashlib.sha256(text.encode()).hexdigest()
    known = {'message', 'reasoning', 'web_search_call', 'function_call',
             'function_call_output', 'custom_tool_call', 'custom_tool_call_output'}
    for item in items[:128]:
        if not isinstance(item, dict):
            rows.append({'type': 'other'})
            continue
        kind = item.get('type')
        row = {'type': kind if isinstance(kind, str) and kind in known else 'other'}
        if kind in ('function_call', 'function_call_output', 'custom_tool_call', 'custom_tool_call_output'):
            if isinstance(item.get('call_id'), str):
                row['call_id_sha256'] = digest(item['call_id'])
            field = 'output' if kind.endswith('_output') else 'arguments' if kind == 'function_call' else 'input'
            content = item.get(field)
            if isinstance(content, str):
                row['body_bytes'] = len(content.encode())
                row['body_sha256'] = digest(content)
            elif isinstance(content, list):
                row['content_item_count'] = len(content)
        rows.append(row)
    return {'item_count': len(items), 'items': rows, 'truncated': len(items) > 128}


class ConversationStreamSummary:
    def __init__(self):
        self.buffer = b''
        self.items = {}
        self.events = {}
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
            except (ValueError, UnicodeDecodeError):
                continue
            if not isinstance(event, dict):
                continue
            kind = event.get('type')
            known = {'response.created', 'response.completed', 'response.failed',
                     'response.output_item.added', 'response.output_item.done',
                     'response.output_text.delta', 'response.output_text.annotation.added',
                     'response.reasoning_summary_text.delta', 'response.reasoning_summary_text.done'}
            key = kind if isinstance(kind, str) and kind in known else 'other'
            self.events[key] = self.events.get(key, 0) + 1
            if kind in ('response.output_item.added', 'response.output_item.done'):
                self.item(event.get('item'), 'added' if kind.endswith('.added') else 'done')
            elif kind == 'response.completed':
                response = event.get('response')
                if isinstance(response, dict) and isinstance(response.get('output'), list):
                    for item in response['output']:
                        self.item(item, 'completed')

    def item(self, item, stage):
        if not isinstance(item, dict) or not isinstance(item.get('id'), str):
            return
        identity = hashlib.sha256(item['id'].encode()).hexdigest()
        if identity not in self.items and len(self.items) >= 128:
            self.truncated = True
            return
        kind = item.get('type')
        if kind not in ('message', 'reasoning', 'web_search_call', 'function_call', 'custom_tool_call'):
            kind = 'other'
        row = {'type': kind}
        if kind == 'message':
            row['role'] = item.get('role') if item.get('role') in ('assistant', 'user', 'developer', 'system') else 'other'
            row['phase'] = item.get('phase') if item.get('phase') in ('commentary', 'final_answer', None) else 'other'
            content = item.get('content')
            blocks = [b for b in content if isinstance(b, dict)] if isinstance(content, list) else []
            text = ''.join(b.get('text', '') for b in blocks if b.get('type') == 'output_text' and isinstance(b.get('text'), str))
            row['text_bytes'] = len(text.encode())
            row['text_sha256'] = hashlib.sha256(text.encode()).hexdigest()
            row['text_url_sha256'] = sorted({hashlib.sha256(url.encode()).hexdigest() for url in re.findall(r'https?://[^\s<>）)\]]+', text)})[:128]
            annotations = [a for b in blocks if isinstance(b.get('annotations'), list) for a in b['annotations']]
            row['annotations'] = len(annotations)
            row['url_citations'] = sum(isinstance(a, dict) and a.get('type') == 'url_citation' for a in annotations)
        elif kind == 'reasoning':
            summary = item.get('summary')
            row['summary_text_bytes'] = sum(len(b['text'].encode()) for b in summary if isinstance(b, dict) and isinstance(b.get('text'), str)) if isinstance(summary, list) else 0
        elif kind == 'web_search_call':
            action = item.get('action')
            if isinstance(action, dict):
                row['action'] = action.get('type') if action.get('type') in ('search', 'open_page', 'find_in_page') else 'other'
                sources = action.get('sources')
                row['source_count'] = len(sources) if isinstance(sources, list) else 0
                row['source_url_sha256'] = sorted({hashlib.sha256(s['url'].encode()).hexdigest() for s in sources if isinstance(s, dict) and isinstance(s.get('url'), str)})[:128] if isinstance(sources, list) else []
        self.items.setdefault(identity, {})[stage] = row

    def summary(self):
        return {'events': self.events, 'items': self.items, 'truncated': self.truncated}
