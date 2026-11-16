import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import path from 'node:path';
import {evaluate,preflight,policy,digest,root} from './feat175-q2-v2.mjs';
import {packageRoot,hostRoot,packageDigest} from './feat175-q2.mjs';
const plan=JSON.parse(readFileSync(path.join(packageRoot,'cases.json')));
function sample(id='Q2-03',mode='first_action',status='interrupted') {
  const c=plan.cases.find(c=>c.id===id),e=c.expected,thread='ordinary-thread',turn='ordinary-turn';
  const arguments_=e.url?{url:e.url}:{query:'ordinary public documentation'};
  for(const key of ['limit','include_content','sites','time_window'])if(e[key]!==undefined)arguments_[key]=structuredClone(e[key]);
  const item=e.first==='native'?{type:'webSearch',id:'native-search',query:'ordinary query',action:{type:'search',query:'ordinary query'}}:
    {type:'mcpToolCall',id:'ordinary-item',server:'yijie_web',tool:e.first,arguments:arguments_,status:'completed',result:{content:[]}};
  const events=e.first==='none'?[]:['item/started','item/completed'].map(method=>({method,params:{threadId:thread,turnId:turn,item:structuredClone(item)}}));
  events.push({method:'turn/completed',params:{threadId:thread,turn:{id:turn,status}}});
  return {schema_version:2,feature:'FEAT-175',package_sha256:packageDigest(),sampling_sha256:digest(readFileSync(path.join(root,'sampling.json'))),kind:'synthetic',mode,
    observations:[{model:plan.models[0].model,case_id:id,submitted_prompt_sha256:digest(c.prompt),thread_id:thread,turn_id:turn,events,hosted_visibility:'complete',query_relevance:'pass',
      sampling:{trigger:{root_thread_id:thread,thread_id:thread,turn_id:turn,item_id:item.id},boundary:{closed:true,sequence:3},stop_result:status==='interrupted'?'interrupted':'natural_completion_race'}}]};
}
const first=(e,id='Q2-03')=>evaluate(e).rows.find(r=>r.model===plan.models[0].model&&r.case_id===id);

test('v2 keeps v1 sources and natural prompts frozen and has no paid entry',()=>{
  assert.equal(preflight().predecessor_preserved,true);assert.equal(preflight().paid_entry_enabled,false);
  assert.equal(policy().base_cases_sha256,packageDigest());
});
test('first choice can match while native interruption is never full-task success',()=>{
  const row=first(sample());assert.equal(row.first_route_verdict,'MATCH');assert.equal(row.task_execution,'interrupted');assert.equal(row.full_task_success,'NOT_EVALUATED_SAMPLED');
});
test('natural-completion race still does not grant full-task success for sampling',()=>{
  const row=first(sample('Q2-03','first_action','completed'));assert.equal(row.sampling,'observed_and_stopped');assert.equal(row.full_task_success,'NOT_EVALUATED_SAMPLED');
});
test('completed synthetic turns and failed full turns remain separate',()=>{
  assert.equal(first(sample('Q2-03','full_turn','completed')).full_task_success,'NOT_EVALUATED_SYNTHETIC');
  const row=first(sample('Q2-03','full_turn','failed'));assert.equal(row.first_route_verdict,'MATCH');assert.equal(row.full_task_success,'FAILED');
});
test('no-retrieval absence requires natural completion',()=>{
  assert.equal(first(sample('Q2-01','first_action','interrupted'),'Q2-01').first_route_verdict,'INCONCLUSIVE');
  assert.equal(first(sample('Q2-01','first_action','completed'),'Q2-01').first_route_verdict,'MATCH');
});
test('a boundary for another native item cannot qualify sampling',()=>{
  const e=sample();e.observations[0].sampling.trigger.item_id='different-ordinary-item';assert.equal(first(e).first_route_verdict,'INCONCLUSIVE');
});
test('a native child first choice retains root ownership and child terminal evidence',()=>{
  const e=sample(),o=e.observations[0],child='ordinary-child',childTurn='ordinary-child-turn';
  o.additional_turns=[{thread_id:child,turn_id:childTurn,parent_thread_id:o.thread_id}];
  for(const event of o.events.slice(0,2)){event.params.threadId=child;event.params.turnId=childTurn;}
  o.events.splice(2,0,{method:'turn/completed',params:{threadId:child,turn:{id:childTurn,status:'interrupted'}}});
  o.sampling.trigger.thread_id=child;o.sampling.trigger.turn_id=childTurn;
  assert.equal(first(e).first_route_verdict,'MATCH');
  o.sampling.trigger.root_thread_id='different-root';assert.equal(first(e).first_route_verdict,'INCONCLUSIVE');
});
test('first-choice argument changes at completion are not a later correction',()=>{
  const e=sample();e.observations[0].events[1].params.item.arguments.query='different ordinary query';assert.throws(()=>evaluate(e),/arguments changed/);
});
test('a wrong first tool remains wrong after later recovery',()=>{
  const e=sample('Q2-03','full_turn','completed'),o=e.observations[0];
  const wrong=o.events.slice(0,2).map(v=>structuredClone(v));
  for(const event of wrong){event.params.item.id='ordinary-wrong-first';event.params.item.tool='kimi_web_search';event.params.item.arguments={query:'ordinary public documentation'};}
  o.events.unshift(...wrong);
  const row=first(e);assert.equal(row.first_route_verdict,'FAIL');assert.equal(row.later_recovery,true);
  assert.notEqual(row.full_task_success,'PASS_REVIEWED');
});
test('missing starts and incomplete visibility never pass',()=>{
  const e=sample();e.observations[0].events.shift();assert.equal(first(e).first_route_verdict,'INCONCLUSIVE');
  const partial=sample();partial.observations[0].hosted_visibility='unknown';assert.equal(first(partial).first_route_verdict,'INCONCLUSIVE');
});
test('hosted repeats are counted also in an ordinary native-search task',()=>{
  const e=sample('Q2-02','full_turn','completed'),o=e.observations[0];
  const repeat=o.events.slice(0,2).map(v=>structuredClone(v));for(const r of repeat)r.params.item.id='second-search';o.events.splice(2,0,...repeat);
  const row=first(e,'Q2-02');assert.equal(row.hosted_calls,2);assert.equal(row.additional_hosted_after_first,1);assert.equal(row.extra_hosted_on_independent_task,false);
});
test('missing samples remain in both six-task denominators',()=>{
  const r=evaluate(sample());assert.equal(r.rows.length,12);assert.equal(r.per_model[0].planned_tasks,6);assert.equal(r.per_model[0].missing,5);assert.equal(r.per_model[1].missing,6);
});
test('real v1 failure replays read-only and keeps route match distinct from failed task',()=>{
  const refs=path.resolve(hostRoot,'../yijie/docs/features/FEAT-175-builtin-web-retrieval/references');
  const receipt=JSON.parse(readFileSync(path.join(refs,'q2-first-live-result.json')));
  const rawPath=path.resolve(hostRoot,'..',receipt.raw_run_relative,'evidence.json');
  const bytes=readFileSync(rawPath),original=digest(bytes),review=JSON.parse(readFileSync(path.join(refs,'q2-first-live-manual-review.json')));
  const r=evaluate(JSON.parse(bytes),{evidenceBytes:bytes,manualReview:review});
  assert.equal(r.sample_kind,'recorded-native');assert.equal(r.scorer_calls_provider,false);
  assert.equal(r.rows[0].full_task_success,'PASS_REVIEWED');assert.equal(r.rows[1].first_route_verdict,'MATCH');assert.equal(r.rows[1].full_task_success,'FAILED');
  assert.equal(r.rows[1].hosted_before_first_mcp_result,2);assert.equal(r.rows[1].additional_hosted_after_first,2);assert.equal(r.per_model[1].missing,6);
  assert.equal(digest(readFileSync(rawPath)),original);
  assert.throws(()=>evaluate(JSON.parse(bytes),{evidenceBytes:bytes,manualReview:{...review,raw_evidence_sha256:'different'}}),/another recording/);
});
test('v2 scorer does not have a live execution command',()=>{
  assert.throws(()=>execFileSync(process.execPath,[path.join(hostRoot,'scripts/feat175-q2-v2.mjs'),'live'],{stdio:'pipe'}));
});
