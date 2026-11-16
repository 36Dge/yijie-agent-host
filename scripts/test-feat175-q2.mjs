import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {evaluate,preflight,packageDigest,packageRoot,hostRoot} from './feat175-q2.mjs';

const plan=JSON.parse(readFileSync(path.join(packageRoot,'cases.json')));
function observation(model, c) {
  const thread='ordinary-q2-'+model+'-'+c.id,turn='ordinary-turn-'+c.id;
  const expected=c.expected;let item;
  if(expected.first==='native')item={type:'webSearch',id:'search-1',query:'ordinary official reference',action:{type:'search',query:'ordinary official reference'}};
  else if(expected.first!=='none') {
    const args=expected.url?{url:expected.url}:{query:'ordinary synthetic query'};
    for(const k of ['limit','include_content','sites','time_window'])if(expected[k]!==undefined)args[k]=structuredClone(expected[k]);
    item={type:'mcpToolCall',id:'mcp-1',server:'yijie_web',tool:expected.first,arguments:args,status:'completed',result:{content:[{type:'text',text:'Q2 explicitly synthetic material'}]}};
  }
  const events=[];
  if(item)for(const method of ['item/started','item/completed'])events.push({method,params:{threadId:thread,turnId:turn,item:structuredClone(item)}});
  events.push({method:'turn/completed',params:{threadId:thread,turn:{id:turn,status:'completed'}}});
  return {model,case_id:c.id,submitted_prompt_sha256:createHash('sha256').update(c.prompt).digest('hex'),thread_id:thread,turn_id:turn,hosted_visibility:'complete',query_relevance:'pass',events};
}
function evidence() {return {schema_version:1,feature:'FEAT-175',package_sha256:packageDigest(),kind:'synthetic',observations:plan.models.flatMap(m=>plan.cases.map(c=>observation(m.model,c)))};}
const row=(r,id='Q2-03')=>r.rows.find(r=>r.model==='kimi-k3.1-pro'&&r.case_id===id);

test('Q2 preflight freezes 12 natural tasks and keeps actual budgets at zero',()=>{
  const p=preflight();assert.equal(p.prepared_tasks,12);assert.equal(p.authorized_model_http_requests,0);assert.equal(p.authorized_total_cny,0);assert.equal(p.real_execution,'NOT RUN');
});
test('canonical synthetic sequence can exercise scoring but cannot grant Q2 qualification',()=>{
  const r=evaluate(evidence());assert.equal(r.rows.length,12);assert(r.rows.every(r=>r.verdict==='MATCH'));assert(r.qualification.startsWith('NOT GRANTED'));assert.equal(r.evidence_kind,'synthetic');
});
test('wrong first tool remains wrong after later correction',()=>{
  const e=evidence(),o=e.observations.find(o=>o.model==='kimi-k3.1-pro'&&o.case_id==='Q2-03');
  const first=structuredClone(o.events[0].params.item);first.id='wrong-first';first.tool='kimi_web_search';first.arguments={query:'ordinary query'};
  o.events.unshift(...['item/started','item/completed'].map(method=>({method,params:{threadId:o.thread_id,turnId:o.turn_id,item:first}})));
  const r=row(evaluate(e));assert.equal(r.verdict,'FAIL');assert.equal(r.later_recovery,true);assert.equal(r.mcp_items,2);
});
test('hosted presearch is counted even if the expected MCP tool later succeeds',()=>{
  const e=evidence(),o=e.observations[2];
  const item={type:'webSearch',id:'presearch',query:'ordinary query',action:{type:'search'}};
  o.events.unshift(...['item/started','item/completed'].map(method=>({method,params:{threadId:o.thread_id,turnId:o.turn_id,item}})));
  const r=evaluate(e);assert.equal(row(r).first_route,'native');assert.equal(row(r).verdict,'FAIL');assert.equal(r.per_model[0].extra_hosted_tasks,1);assert.equal(r.per_model[0].independent_retrieval_denominator,4);
});
test('argument mismatches, missing query review and incomplete hosted visibility do not pass',()=>{
  const wrong=evidence();for(const e of wrong.observations[4].events)if(e.params.item)e.params.item.arguments.include_content=false;
  assert.equal(row(evaluate(wrong),'Q2-05').verdict,'FAIL');
  const pending=evidence();pending.observations[2].query_relevance='not_reviewed';assert.equal(row(evaluate(pending)).verdict,'REVIEW_REQUIRED');
  pending.observations[2].hosted_visibility='unknown';assert.equal(row(evaluate(pending)).verdict,'INCONCLUSIVE');
});
test('missing tasks remain in the denominator and a cancelled no-call task is inconclusive',()=>{
  const e=evidence();e.observations.pop();e.observations[0].events[0].params.turn.status='interrupted';
  const r=evaluate(e);assert.equal(r.rows.length,12);assert.equal(r.per_model[1].tasks,6);assert.equal(r.per_model[1].observed,5);assert.equal(row(r,'Q2-01').verdict,'INCONCLUSIVE');
});
test('completion order cannot replace missing start order or cross-thread evidence',()=>{
  const missing=evidence();missing.observations[2].events.shift();assert.equal(row(evaluate(missing)).verdict,'INCONCLUSIVE');
  const mixed=evidence();mixed.observations[2].events[0].params.threadId='different-normal-thread';assert.throws(()=>evaluate(mixed),/Mixed native thread/);
});
test('the preparation CLI has no executable live mode',()=>{
  assert.throws(()=>execFileSync(process.execPath,[path.join(hostRoot,'scripts/feat175-q2.mjs'),'execute'],{encoding:'utf8',stdio:'pipe'}),e=>e.status===1&&e.stderr.includes('Live execution is not enabled'));
});
test('native-owned child retrieval is included in first-route and extra-hosted accounting',()=>{
  const e=evidence(),o=e.observations[2],child='ordinary-child',turn='ordinary-child-turn';
  o.additional_turns=[{thread_id:child,turn_id:turn,parent_thread_id:o.thread_id}];
  const item={type:'webSearch',id:'child-search',query:'ordinary reference',action:{type:'search'}};
  o.events.unshift(...['item/started','item/completed'].map(method=>({method,params:{threadId:child,turnId:turn,item}})),{method:'turn/completed',params:{threadId:child,turn:{id:turn,status:'completed'}}});
  const r=row(evaluate(e));assert.equal(r.first_route,'native');assert.equal(r.verdict,'FAIL');assert.equal(r.extra_hosted,true);
});
test('different prompts or a changed MCP identity cannot masquerade as the frozen first route',()=>{
  const prompt=evidence();prompt.observations[2].submitted_prompt_sha256='0'.repeat(64);assert.throws(()=>evaluate(prompt),/Submitted prompt differs/);
  const identity=evidence();identity.observations[2].events[1].params.item.tool='kimi_web_search';assert.throws(()=>evaluate(identity),/Native MCP identity changed/);
});
