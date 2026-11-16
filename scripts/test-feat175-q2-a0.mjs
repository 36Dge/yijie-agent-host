import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import path from 'node:path';
import {preflight,visibility,evaluate,root} from './feat175-q2-a0.mjs';
import {digest} from './feat175-q2-v2.mjs';
import {packageDigest,packageRoot} from './feat175-q2.mjs';
const plan=JSON.parse(readFileSync(path.join(packageRoot,'cases.json')));
function observation(){
  const item={type:'webSearch',id:'first',query:'ordinary Python release',action:{type:'search',query:'ordinary Python release'}};
  return {model:'MiniMax-M3',case_id:'Q2-02',thread_id:'ordinary-root',turn_id:'ordinary-turn',submitted_prompt_sha256:digest(plan.cases[1].prompt),
    query_relevance:'pass',hosted_visibility:'unknown',capture_complete:true,native_ancestry_verified:true,native_barrier:{active:0,pending:0},
    sampling:{trigger:{root_thread_id:'ordinary-root',thread_id:'ordinary-root',turn_id:'ordinary-turn',item_id:'first'},boundary:{closed:true,sequence:2},stop_result:'interrupted'},
    events:[...['item/started','item/completed'].map(method=>({method,params:{threadId:'ordinary-root',turnId:'ordinary-turn',item}})),{method:'turn/completed',params:{threadId:'ordinary-root',turn:{id:'ordinary-turn',status:'interrupted'}}}],
    meter_settlement:{task:{late_bindings:0},requests:[{status:'settled',stream_complete:true,observer_valid:true,hosted_item_ids:['first'],provider_events:[{item_id:'first',sequence:1,after_cutoff:false}]}]}};
}
function evidence(o){return {schema_version:3,feature:'FEAT-175',package_sha256:packageDigest(),policy_sha256:digest(readFileSync(path.join(root,'policy.json'))),kind:'synthetic',mode:'first_action',observations:[o]};}
const result=o=>evaluate(evidence(o)).rows.find(r=>r.model===o.model&&r.case_id===o.case_id);
test('A0 sources preserve both predecessors',()=>assert.equal(preflight().new_paid_allowance,0));
test('complete native/provider inventory qualifies first evidence only',()=>{
  const row=result(observation());assert.equal(row.first_route_verdict,'MATCH');assert.equal(row.full_task_success,'NOT_EVALUATED_SAMPLED');
});
test('a late provider tail is charged and retained without inventing native items',()=>{
  const o=observation(),r=o.meter_settlement.requests[0];r.hosted_item_ids.push('tail');r.provider_events.push({item_id:'tail',sequence:3,after_cutoff:true});
  const before=JSON.stringify(o.events),row=result(o);assert.equal(row.first_route_verdict,'MATCH');assert.equal(row.visibility.native_visibility,'sampled_prefix');
  assert.equal(row.visibility.provider_hosted_count,2);assert.deepEqual(row.visibility.unconsumed_provider_tail,['tail']);assert.equal(JSON.stringify(o.events),before);
});
test('unmatched pre-cutoff provider items keep order inconclusive',()=>{
  const o=observation(),r=o.meter_settlement.requests[0];r.hosted_item_ids.unshift('earlier');r.provider_events.unshift({item_id:'earlier',sequence:0,after_cutoff:false});
  assert.equal(result(o).first_route_verdict,'INCONCLUSIVE');
});
test('missing terminal usage/eof never becomes complete billing evidence',()=>{
  const o=observation();o.meter_settlement.requests[0].stream_complete=false;assert.equal(visibility(o).provider_complete,false);assert.equal(result(o).first_route_verdict,'INCONCLUSIVE');
});
test('missing starts or an unclosed native barrier cannot pass',()=>{
  const a=observation();a.events.shift();assert.equal(result(a).first_route_verdict,'INCONCLUSIVE');
  const b=observation();b.native_barrier.active=1;assert.equal(result(b).first_route_verdict,'INCONCLUSIVE');
});
