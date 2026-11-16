// Ordinary fixture conformance to the SAME canonical MCP output schemas.
import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {spawnSync} from 'node:child_process';
import {hostRoot} from './feat175-q2.mjs';
import path from 'node:path';
const require = createRequire(path.join(path.dirname(hostRoot), 'yijie-contracts/package.json'));
const Ajv = require('ajv/dist/2020.js').default, addFormats = require('ajv-formats').default;
const ajv = new Ajv({strict: false, allErrors: true}); addFormats(ajv);
const tools = JSON.parse(readFileSync(path.join(hostRoot, 'internal/contracts/builtinweb/tools.json')));

test('Q2 local Basic/list, Basic/body, Pro/chunks and Fetch match canonical schemas', () => {
  const result = spawnSync('python3', ['-c', `import json,sys
sys.path.insert(0,'scripts')
from feat175_q2_meter import synthetic_tool_result
cases=[('kimi_web_search',{'query':'ordinary sources','limit':5}),('kimi_web_search',{'query':'ordinary documents','limit':3,'include_content':True}),('kimi_web_search_pro',{'query':'ordinary research','sites':['shopify.dev'],'time_window':{'start':'2026-01-01','end':'2026-11-15'}}),('kimi_web_fetch',{'url':'https://www.rfc-editor.org/rfc/rfc9110'})]
print(json.dumps([{'name':n,'args':a,'output':synthetic_tool_result(n,a)} for n,a in cases]))`], {cwd:hostRoot, encoding:'utf8'});
  assert.equal(result.status, 0, result.stderr);
  for (const row of JSON.parse(result.stdout)) {
    const tool = tools.find(t => t.name === row.name);
    for (const [schema, value] of [[tool.inputSchema, row.args], [tool.outputSchema, row.output]]) {
      const validate = ajv.compile(schema);
      assert(validate(value), JSON.stringify(validate.errors));
    }
  }
});
