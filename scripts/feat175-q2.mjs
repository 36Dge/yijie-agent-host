// Offline FEAT-175 preparation and evidence scoring only. This module has no
// network, child-process, credential, Runtime-start or live-execution path.
import assert from 'node:assert/strict';
import {readFileSync, writeFileSync, openSync, readSync, closeSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import {fileURLToPath, pathToFileURL} from 'node:url';
import path from 'node:path';

export const hostRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const workspace = path.dirname(hostRoot);
export const packageRoot = path.join(hostRoot, 'internal/codex/testdata/feat175-q2');
const read = p => readFileSync(p);
const json = p => JSON.parse(read(p));
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
function fileDigest(file) {
  const hash=createHash('sha256'), buffer=Buffer.alloc(1<<20), fd=openSync(file,'r');
  try { let n; while ((n=readSync(fd,buffer,0,buffer.length,null))>0) hash.update(buffer.subarray(0,n)); }
  finally { closeSync(fd); }
  return hash.digest('hex');
}
const q2 = () => json(path.join(packageRoot, 'cases.json'));
const lockedSource = () => json(path.join(packageRoot, 'source-lock.json'));
const require = createRequire(path.join(workspace, 'yijie-contracts/package.json'));
const Ajv = require('ajv').default, Ajv2020 = require('ajv/dist/2020.js').default;
const addFormats = require('ajv-formats').default;

export function packageDigest() { return digest(read(path.join(packageRoot, 'cases.json'))); }

export function freezeSources() {
  const candidate = json(path.join(hostRoot, 'api/builtin-web.candidate.json'));
  const runtime = 'yijie-codex/' + candidate.runtime_relative_path;
  const files = [
    'yijie-agent-host/internal/codex/testdata/feat175-q2/cases.json',
    'yijie-agent-host/internal/codex/testdata/feat175-q2/effective-profile.json',
    'yijie-agent-host/internal/codex/builtin_web_q2_snapshot_test.go',
    'yijie-agent-host/scripts/feat175-q2.mjs',
    'yijie-agent-host/scripts/check-builtin-web-q2.sh',
    'yijie-agent-host/internal/contracts/builtinweb/tools.json',
    'yijie-agent-host/internal/contracts/builtinweb/policy.gen.go',
    'yijie-agent-host/internal/codex/builtin_web.go',
    'yijie-agent-host/internal/codex/session_protocol.go',
    'yijie-agent-host/internal/codex/runtime.go',
    'yijie-agent-host/internal/codex/market_runtime.go',
    'yijie-agent-host/internal/codex/expert_instructions.go',
    'yijie-agent-host/internal/codex/web_retrieval_instructions.go',
    'yijie-agent-host/internal/codex/execution_progress.go',
    'yijie-agent-host/internal/codex/public_progress_policy.go',
    'yijie-agent-host/internal/codex/runtime_permissions.go',
    'yijie-agent-host/internal/codex/chat_models.go',
    'yijie-agent-host/internal/codex/kimi_k31_candidate.go',
    'yijie-agent-host/internal/codex/provider.go',
    'yijie-agent-host/internal/codex/native_tools.go',
    'yijie-agent-host/internal/contracts/chatmodels/types.gen.go',
    'yijie-agent-host/internal/contracts/chatmodelsv2/types.gen.go',
    'yijie-agent-host/internal/codex/tool_search.go',
    'yijie-agent-host/internal/codex/feat164_meter.go',
    'yijie-agent-host/internal/contracts/runtimetoolsearch/profile.json',
    'yijie-agent-host/internal/contracts/runtimenativetools/profile.json',
    'yijie-agent-host/scripts/chat-model-smoke-meter.py',
    'yijie-agent-host/scripts/responses_stream_observer.py',
    'yijie-agent-host/scripts/feat175_q2_meter.py',
    'yijie-agent-host/scripts/feat175_q2_run.py',
    'yijie-agent-host/scripts/test_feat175_q2_meter.py',
    'yijie-agent-host/scripts/test-feat175-q2-fixtures.mjs',
    'yijie-agent-host/internal/codex/builtin_web_q2_runner_test.go',
    'yijie-agent-host/internal/codex/builtin_web_integration_test.go',
    'yijie-agent-host/internal/codex/runtime_integration_test.go',
    'yijie-agent-host/internal/codex/testdata/feat175-q2/pricing-snapshot.json',
    'yijie-desktop/scripts/feat164-budget-meter.py',
    'yijie-contracts/compatibility/builtin-web-retrieval/policy.json',
    'yijie-contracts/compatibility/builtin-web-retrieval/wire.schema.json',
    runtime + '/runtime-manifest.json', runtime + '/config.schema.json',
    runtime + '/model-catalog.schema.json',
    runtime + '/generated-json-schema/v2/ThreadReadResponse.json',
  ];
  const manifest = json(path.join(workspace, runtime, 'runtime-manifest.json'));
  assert.equal(fileDigest(path.join(workspace, runtime, 'codex')), candidate.runtime_binary_sha256);
  assert.equal(digest(read(path.join(workspace, runtime, 'runtime-manifest.json'))), candidate.runtime_manifest_sha256);
  const lock = {schema_version: 1, feature: 'FEAT-175', package_sha256: packageDigest(), provenance: 'local source freeze, not release or paid-call approval',
    runtime_relative_path: runtime, runtime_binary_sha256: manifest.runtime.sha256,
    sources: files.map(file => ({file, sha256: digest(read(path.join(workspace, file)))}))};
  writeFileSync(path.join(packageRoot, 'source-lock.json'), JSON.stringify(lock, null, 2) + '\n');
  return lock;
}

export function preflight() {
  const plan = q2(), lock = lockedSource();
  assert.equal(plan.feature, 'FEAT-175'); assert.equal(plan.tasks, 12);
  assert.equal(plan.models.length, 2); assert.equal(plan.cases.length, 6);
  assert.equal(new Set(plan.cases.map(c => c.id)).size, 6);
  for (const c of plan.cases) {
    assert.equal(typeof c.prompt, 'string'); assert(c.prompt.trim());
    assert(!/kimi_web_|tool_search|mcp__|approval_mode/.test(c.prompt), 'No tool-name hints in natural-language cases');
  }
  for (const [name, value] of Object.entries(plan.limits).filter(([name]) => name.startsWith('authorized_'))) assert.equal(value, 0, name);
  assert.equal(packageDigest(), lock.package_sha256, 'Frozen cases changed');
  for (const s of lock.sources) {
    const file = path.resolve(workspace, s.file);
    assert(file.startsWith(workspace + path.sep), 'Source must remain in the workspace');
    assert.equal(digest(read(file)), s.sha256, 'Source changed: ' + s.file);
  }
  assert.equal(fileDigest(path.join(workspace, lock.runtime_relative_path, 'codex')), lock.runtime_binary_sha256);
  return {feature: plan.feature, package_sha256: lock.package_sha256, source_files: lock.sources.length,
    prepared_tasks: plan.tasks, offline_preparation: 'PASS', real_execution: 'NOT RUN',
    authorized_total_cny: 0, authorized_model_http_requests: 0, supplier_rest_calls: 0,
    live_readiness: 'BLOCKED: current-task positive budget and soft-stop acceptance required; actual provider evidence remains NOT RUN'};
}

function validators() {
  const lock = lockedSource();
  const native = json(path.join(workspace, lock.runtime_relative_path, 'generated-json-schema/v2/ThreadReadResponse.json'));
  const ajv = new Ajv({strict: false, allErrors: true}); addFormats(ajv);
  for (const name of ['uint','uint64','uint32','uint16','int64']) ajv.addFormat(name, {type:'number',validate:Number.isSafeInteger});
  const item = ajv.compile({$schema: native.$schema, definitions: native.definitions, $ref:'#/definitions/ThreadItem'});
  const input = new Ajv2020({strict:true,allErrors:true}); addFormats(input);
  const tools = json(path.join(hostRoot, 'internal/contracts/builtinweb/tools.json'));
  return {item, tools: Object.fromEntries(tools.map(t => [t.name, input.compile(t.inputSchema)]))};
}

// Consume original item/started order, then update each original item from its
// completion. Never infer tool calls by parsing code-mode JavaScript or text.
function retrievalItems(observation, validateItem) {
  assert(Array.isArray(observation.events) && observation.events.length <= 2000);
  // Additional bindings must be captured from native thread/read ancestry and
  // turn/started, never from model arguments. This is evidence grouping only.
  const additional=observation.additional_turns ?? [];
  assert(Array.isArray(additional) && additional.length <= 64);
  const parents=new Map([[observation.thread_id,null]]), bindings=new Set([observation.thread_id+'/'+observation.turn_id]);
  for(const b of additional) {
    assert(typeof b.thread_id==='string' && b.thread_id && typeof b.turn_id==='string' && b.turn_id && typeof b.parent_thread_id==='string' && b.parent_thread_id);
    assert(b.thread_id!==observation.thread_id, 'A child cannot replace the task root');
    if(parents.has(b.thread_id)) assert.equal(parents.get(b.thread_id),b.parent_thread_id,'Native ancestry changed');
    parents.set(b.thread_id,b.parent_thread_id);
    const key=b.thread_id+'/'+b.turn_id;assert(!bindings.has(key),'Duplicate native turn binding');bindings.add(key);
  }
  for(const b of additional) {
    let current=b.thread_id;const seen=new Set();
    while(current!==observation.thread_id) {assert(!seen.has(current)&&seen.size<16&&parents.has(current),'Native child ancestry unavailable');seen.add(current);current=parents.get(current);}
  }
  const items = new Map(); let orderComplete = true, turnComplete = false, turnStatus = 'unknown';
  const ended=new Set();
  for (const event of observation.events) {
    assert(event && typeof event.method === 'string' && event.params && typeof event.params === 'object');
    if (event.method === 'turn/completed') {
      assert(bindings.has(event.params.threadId+'/'+event.params.turn?.id),'Mixed native turn');
      ended.add(event.params.threadId+'/'+event.params.turn.id);
      if(event.params.threadId===observation.thread_id&&event.params.turn.id===observation.turn_id){turnComplete=true;turnStatus=event.params.turn?.status??'unknown';}
      continue;
    }
    if (!['item/started','item/completed'].includes(event.method)) continue;
    assert(bindings.has(event.params.threadId+'/'+event.params.turnId), 'Mixed native thread/turn');
    const item = event.params.item;
    assert(validateItem(item), 'Invalid canonical ThreadItem: ' + JSON.stringify(validateItem.errors));
    if (!['webSearch','mcpToolCall'].includes(item.type)) continue;
    const identity=event.params.threadId+'/'+event.params.turnId+'/'+item.id;
    const known = items.get(identity);
    if (!known) { if (event.method !== 'item/started') orderComplete = false; items.set(identity, {item, completed:event.method==='item/completed'}); }
    else {
      assert.equal(known.item.type, item.type, 'Native item identity changed');
      if(item.type==='mcpToolCall'){assert.equal(known.item.server,item.server,'Native MCP identity changed');assert.equal(known.item.tool,item.tool,'Native MCP identity changed');}
      known.item = item; known.completed ||= event.method==='item/completed';
    }
  }
  return {items:[...items.values()], orderComplete, turnComplete:turnComplete&&ended.size===bindings.size, turnStatus};
}

const action = item => item.type === 'webSearch' ? 'native' : item.server === 'yijie_web' ? item.tool : 'other_mcp';
function parametersMatch(expected, item, tools) {
  if (!item || expected.first === 'native' || expected.first === 'none') return true;
  const valid = tools[item.tool]; if (!valid || !valid(item.arguments)) return false;
  const args = item.arguments;
  if (expected.limit !== undefined && (args.limit ?? 5) !== expected.limit) return false;
  if (expected.include_content !== undefined && (args.include_content ?? false) !== expected.include_content) return false;
  if (expected.url !== undefined && args.url !== expected.url) return false;
  if (expected.sites && JSON.stringify([...(args.sites ?? [])].sort()) !== JSON.stringify([...expected.sites].sort())) return false;
  if (expected.time_window && (args.time_window?.start !== expected.time_window.start || args.time_window?.end !== expected.time_window.end)) return false;
  return true;
}

export function evaluate(evidence) {
  preflight(); const plan = q2(), v = validators();
  assert.equal(evidence.schema_version, 1); assert.equal(evidence.feature, 'FEAT-175');
  assert.equal(evidence.package_sha256, packageDigest(), 'Evidence belongs to different frozen cases');
  assert(['synthetic','recorded-native'].includes(evidence.kind));
  assert(Array.isArray(evidence.observations) && evidence.observations.length <= 12);
  const received = new Map();
  for (const o of evidence.observations) {
    assert(plan.models.some(m => m.model === o.model)); assert(plan.cases.some(c => c.id === o.case_id));
    assert.equal(o.submitted_prompt_sha256,digest(Buffer.from(plan.cases.find(c=>c.id===o.case_id).prompt)),'Submitted prompt differs from frozen natural-language case');
    assert(typeof o.thread_id === 'string' && o.thread_id && typeof o.turn_id === 'string' && o.turn_id);
    assert(['complete','partial','unknown'].includes(o.hosted_visibility));
    assert(['pass','fail','not_reviewed'].includes(o.query_relevance));
    const key = o.model + '/' + o.case_id; assert(!received.has(key), 'Duplicate task observation'); received.set(key, o);
  }
  const rows = [];
  for (const model of plan.models) for (const c of plan.cases) {
    const o = received.get(model.model + '/' + c.id);
    if (!o) { rows.push({model:model.model,case_id:c.id,first_route:'missing',verdict:'MISSING'}); continue; }
    const parsed = retrievalItems(o, v.item), first = parsed.items[0]?.item;
    const route = first ? action(first) : 'none';
    const params = parametersMatch(c.expected, first, v.tools);
    const matched = route === c.expected.first && params;
    let verdict = !parsed.orderComplete || !parsed.turnComplete || !['completed','failed','interrupted'].includes(parsed.turnStatus) || o.hosted_visibility !== 'complete' || (!first && parsed.turnStatus !== 'completed') ? 'INCONCLUSIVE'
      : !matched || o.query_relevance === 'fail' ? 'FAIL'
      : c.expected.manual_query_review && o.query_relevance !== 'pass' ? 'REVIEW_REQUIRED' : 'MATCH';
    const extraHosted = c.expected.first !== 'native' && parsed.items.some(r => action(r.item)==='native');
    const laterRecovery = !matched && parsed.items.slice(1).some(r => action(r.item)===c.expected.first && parametersMatch(c.expected,r.item,v.tools));
    rows.push({model:model.model,case_id:c.id,first_route:route,parameters_match:params,verdict,
      hosted_items:parsed.items.filter(r => action(r.item)==='native').length,extra_hosted:extraHosted,
      mcp_items:parsed.items.filter(r => r.item.type==='mcpToolCall').length,
      unplanned_followups:parsed.items.slice(1).filter(r => !(c.expected.allowed_followups ?? []).includes(action(r.item))).length,
      later_recovery:laterRecovery,
      turn_status:parsed.turnStatus,
      execution_status:parsed.items.filter(r => r.item.type==='mcpToolCall').map(r => ({tool:r.item.tool,status:r.item.status,terminal_observed:r.completed})),
      source_quality:'separate review required; synthetic MCP never proves real retrieval'});
  }
  return {feature:'FEAT-175', package_sha256:packageDigest(), evidence_kind:evidence.kind, rows,
    per_model:plan.models.map(m => {const r=rows.filter(r=>r.model===m.model);return {model:m.model,tasks:6,observed:r.filter(r=>r.verdict!=='MISSING').length,matched:r.filter(r=>r.verdict==='MATCH').length,failed:r.filter(r=>r.verdict==='FAIL').length,pending:r.filter(r=>['MISSING','INCONCLUSIVE','REVIEW_REQUIRED'].includes(r.verdict)).length,independent_retrieval_denominator:4,extra_hosted_tasks:r.filter(r=>['Q2-03','Q2-04','Q2-05','Q2-06'].includes(r.case_id)&&r.extra_hosted).length};}),
    qualification:'NOT GRANTED by this offline scorer; requires actual run provenance, budget/usage evidence, execution and human review',
    live_execution_performed:false};
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    const [command, input, extra] = process.argv.slice(2); assert(!extra);
    let result;
    if (command === 'freeze' && !input) result = {source_files:freezeSources().sources.length, execution:'disabled'};
    else if (command === 'preflight' && !input) result = preflight();
    else if (command === 'score' && input) { assert(read(input).length <= 4<<20, 'Evidence file exceeds bound'); result = evaluate(json(input)); }
    else throw Error('Supported offline commands: freeze | preflight | score EVIDENCE.json. Live execution is not enabled.');
    process.stdout.write(JSON.stringify(result,null,2)+'\n');
  } catch (error) { process.stderr.write(String(error.message)+'\n'); process.exitCode = 1; }
}
