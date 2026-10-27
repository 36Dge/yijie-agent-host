import hashlib
import json
import unittest

from responses_stream_observer import ConversationStreamSummary, request_input_summary


class ConversationObservationTests(unittest.TestCase):
    def test_request_tool_results_are_correlated_by_hash_without_retaining_content(self):
        value = [{'type': 'message', 'role': 'user', 'content': 'Compare two cities'},
                 {'type': 'function_call', 'call_id': 'ordinary-call', 'arguments': '{"query":"annual report"}'},
                 {'type': 'function_call_output', 'call_id': 'ordinary-call', 'output': 'No matching figures.'}]
        before = json.dumps(value)
        summary = request_input_summary(value)
        self.assertEqual(summary['item_count'], 3)
        self.assertEqual(summary['items'][1]['call_id_sha256'], summary['items'][2]['call_id_sha256'])
        self.assertEqual(summary['items'][2]['body_sha256'], hashlib.sha256(b'No matching figures.').hexdigest())
        for text in ('ordinary-call', 'annual report', 'No matching figures', 'Compare two cities'):
            self.assertNotIn(text, json.dumps(summary))
        self.assertEqual(json.dumps(value), before)

    def test_observes_public_messages_and_sources_without_retaining_text(self):
        progress = {'id': 'progress', 'type': 'message', 'role': 'assistant', 'phase': 'commentary',
                    'content': [{'type': 'output_text', 'text': 'I will check the official figures.'}]}
        search = {'id': 'search', 'type': 'web_search_call', 'action': {'type': 'search',
                  'query': 'ordinary city comparison', 'sources': [{'url': 'https://example.org/report', 'title': 'Report'}]}}
        answer = {'id': 'answer', 'type': 'message', 'role': 'assistant',
                  'content': [{'type': 'output_text', 'text': 'See [report](https://example.org/report).',
                               'annotations': [{'type': 'url_citation', 'url': 'https://example.org/report'}]}]}
        events = [{'type': 'response.output_item.done', 'item': i} for i in [progress, search, answer]]
        events.append({'type': 'response.completed', 'response': {'output': [progress, search, answer]}})
        wire = ''.join('data: ' + json.dumps(e) + '\r\n\r\n' for e in events).encode()
        observer = ConversationStreamSummary()
        for start in range(0, len(wire), 17):
            observer.feed(wire[start:start + 17])
        result = observer.summary()
        rows = result['items']
        get = lambda identity: rows[hashlib.sha256(identity.encode()).hexdigest()]['completed']
        self.assertEqual(get('progress')['phase'], 'commentary')
        self.assertIsNone(get('answer')['phase'])
        self.assertEqual(get('answer')['url_citations'], 1)
        self.assertEqual(get('search')['source_url_sha256'], get('answer')['text_url_sha256'])
        serialized = json.dumps(result)
        for original in ('I will check', 'https://example.org', 'ordinary city', 'Report'):
            self.assertNotIn(original, serialized)
        self.assertFalse(result['truncated'])

    def test_does_not_turn_empty_summary_into_public_progress(self):
        observer = ConversationStreamSummary()
        observer.feed(b'data: {"type":"response.output_item.done","item":{"id":"r","type":"reasoning","summary":[{"type":"summary_text","text":""}]}}\n\n')
        row = next(iter(observer.summary()['items'].values()))['done']
        self.assertEqual(row, {'type': 'reasoning', 'summary_text_bytes': 0})

    def test_stops_at_a_bounded_observation_without_affecting_forwarder(self):
        observer = ConversationStreamSummary()
        observer.feed(b' ' * (1024 * 1024 + 1))
        self.assertTrue(observer.summary()['truncated'])
        self.assertEqual(observer.buffer, b'')

    def test_duplicate_stages_and_terminal_only_message_are_observable(self):
        observer = ConversationStreamSummary(clock=lambda: 1234)
        call = {'type': 'function_call', 'id': 'tool-1', 'call_id': 'call-1', 'arguments': '{"query":"report"}'}
        answer = {'type': 'message', 'id': 'answer', 'role': 'assistant', 'phase': 'final_answer',
                  'content': [{'type': 'output_text', 'text': 'Public answer.'}]}
        events = [{'type': 'response.output_item.done', 'item': call}] * 2
        events += [{'type': 'response.completed', 'response': {'id': 'response-1', 'status': 'completed',
                    'output': [call, answer], 'usage': {'input_tokens': 2, 'output_tokens': 3, 'total_tokens': 5}}}]
        for e in events:
            observer.feed(('data: ' + json.dumps(e) + '\n\n').encode())
        summary = observer.summary()
        rows = list(summary['items'].values())
        self.assertEqual(rows[0]['stage_counts'], {'done': 2, 'completed': 1})
        self.assertEqual(rows[1]['stage_counts'], {'completed': 1})
        self.assertEqual(len(summary['timeline']), 3)
        self.assertEqual(summary['terminal']['usage']['total_tokens'], 5)
        self.assertEqual(rows[0]['done']['call_id_sha256'], hashlib.sha256(b'call-1').hexdigest())
        self.assertNotIn('Public answer', json.dumps(summary))

    def test_array_output_canonical_digest_is_stable_and_private(self):
        a = request_input_summary([{'type': 'function_call_output', 'call_id': 'a',
                                   'output': [{'type': 'text', 'text': 'Ordinary tool output'}]}])
        b = request_input_summary([{'type': 'function_call_output', 'call_id': 'a',
                                   'output': [{'text': 'Ordinary tool output', 'type': 'text'}]}])
        self.assertEqual(a, b)
        self.assertEqual(a['items'][0]['body_encoding'], 'canonical_json')
        self.assertNotIn('Ordinary tool output', json.dumps(a))


if __name__ == '__main__':
    unittest.main()
