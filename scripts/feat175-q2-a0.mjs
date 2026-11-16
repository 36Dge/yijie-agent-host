// A0 private evidence adapter. Canonical items/arguments/ancestry remain in v2/v1.
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync} from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {evaluate as priorEvaluate,preflight as priorPreflight,digest,root as priorRoot} from './feat175-q2-v2.mjs';
import {hostRoot,packageRoot,packageDigest} from './feat175-q2.mjs';
export const root=path.join(packageRoot,'../feat175-q2-a0');
const workspace=path.dirname(hostRoot),read=p=>readFileSync(p),json=p=>JSON.parse(read(p));
export const policy=()=>json(path.join(root,'policy.json'));

export function freeze(){
  priorPreflight();const prev=json(path.join(priorRoot,'source-lock.json'));
  const files=['scripts/feat175_q2_a0_policy.py','scripts/feat175_q2_a0_meter.py','scripts/feat175_q2_a0_transport.py',
    'scripts/feat175_q2_a0_run.py','scripts/feat175-q2-a0.mjs','scripts/test_feat175_q2_a0.py','scripts/test-feat175-q2-a0.mjs',
    'scripts/check-builtin-web-q2-a0.sh','internal/codex/builtin_web_q2_a0_test.go',
    'internal/codex/testdata/feat175-q2-a0/policy.json','internal/codex/testdata/feat175-q2-v2/source-lock.json'];
  const lock={...prev,schema_version:3,purpose:'Q2-v2A controlled first-action QA',
    predecessor_source_lock_sha256:digest(read(path.join(priorRoot,'source-lock.json'))),
    policy_sha256:digest(read(path.join(root,'policy.json'))),
    sources:[...prev.sources,...files.map(p=>({file:'yijie-agent-host/'+p,sha256:digest(read(path.join(hostRoot,p)))}))]};
  writeFileSync(path.join(root,'source-lock.json'),JSON.stringify(lock,null,2)+'\n');return {sources:lock.sources.length,paid_calls_authorized:0};
}
export function preflight(){
  priorPreflight();const lock=json(path.join(root,'source-lock.json'));
  assert.equal(lock.policy_sha256,digest(read(path.join(root,'policy.json'))));
  assert.equal(lock.predecessor_source_lock_sha256,digest(read(path.join(priorRoot,'source-lock.json'))));
  for(const row of lock.sources){const p=path.resolve(workspace,row.file);assert(p.startsWith(workspace+path.sep));assert.equal(digest(read(p)),row.sha256,'Source changed: '+row.file);}
  return {feature:'FEAT-175',revision:3,source_files:lock.sources.length,package_sha256:packageDigest(),policy_sha256:lock.policy_sha256,new_paid_allowance:0};
}

export function visibility(o){
  const rows=o.meter_settlement?.requests??[];
  const providerComplete=rows.length>0&&rows.every(r=>r.status==='settled'&&r.stream_complete===true&&r.observer_valid===true);
  const starts=o.events.filter(e=>e.method==='item/started'&&['webSearch','mcpToolCall'].includes(e.params.item?.type));
  const first=starts[0];const native=starts.filter(e=>e.params.item.type==='webSearch').map(e=>e.params.item.id);
  const events=rows.flatMap(r=>r.provider_events??[]).sort((a,b)=>a.sequence-b.sequence),items=new Map();
  for(const e of events)if(!items.has(e.item_id))items.set(e.item_id,e);
  const ids=[...items.keys()],terminal=rows.flatMap(r=>r.hosted_item_ids??[]);
  const inventory=providerComplete&&new Set(terminal).size===terminal.length&&ids.length===terminal.length&&ids.every(id=>terminal.includes(id));
  const subset=native.every(id=>items.has(id))&&new Set(native).size===native.length;
  const omitted=ids.filter(id=>!native.includes(id));
  const tail=omitted.filter(id=>items.get(id).after_cutoff===true);
  const nativeComplete=inventory&&subset&&omitted.length===0;
  let firstComplete=nativeComplete;
  if(!nativeComplete&&inventory&&subset&&first?.params.item.type==='webSearch'){
    firstComplete=ids[0]===first.params.item.id&&native.every((id,i)=>ids[i]===id)&&tail.length===omitted.length;
  }
  // No MCP-first inference from an unseen supplier search, even after cutoff.
  return {provider_complete:inventory,native_visibility:nativeComplete?'complete':'sampled_prefix',first_route_complete:firstComplete,
    provider_hosted_count:terminal.length,native_hosted_count:native.length,unconsumed_provider_tail:tail,unreconciled_provider_items:omitted.filter(id=>!tail.includes(id))};
}

function canonicalOrderComplete(o){
  const known=new Set(),ended=new Set();let ok=true;
  for(const e of o.events){
    if(e.method==='turn/completed')ended.add(e.params.threadId+'/'+e.params.turn.id);
    const item=e.params.item;if(!['webSearch','mcpToolCall'].includes(item?.type))continue;
    const key=e.params.threadId+'/'+e.params.turnId+'/'+item.id;
    if(!known.has(key)&&e.method!=='item/started')ok=false;
    known.add(key);
  }
  return ok&&[{thread_id:o.thread_id,turn_id:o.turn_id},...(o.additional_turns??[])].every(b=>ended.has(b.thread_id+'/'+b.turn_id));
}

export function evaluate(e){
  preflight();assert.equal(e.schema_version,3);assert.equal(e.policy_sha256,digest(read(path.join(root,'policy.json'))));
  assert.equal(e.mode,'first_action');assert(['synthetic','recorded-native'].includes(e.kind));
  // This is an immutable canonical validation projection, not a replacement
  // recording. Its old all-items visibility verdict is retained separately.
  const legacy=priorEvaluate({...structuredClone(e),schema_version:1});
  const plan=json(path.join(packageRoot,'cases.json'));
  const rows=legacy.rows.map(base=>{
    const o=e.observations.find(o=>o.model===base.model&&o.case_id===base.case_id);
    if(!o)return {...base,first_route_verdict:'MISSING',full_task_success:'MISSING'};
    const v=visibility(o),first=o.events.find(x=>x.method==='item/started'&&['webSearch','mcpToolCall'].includes(x.params.item?.type));
    const expected=plan.cases.find(c=>c.id===o.case_id).expected;
    const binding=o.sampling?.trigger,boundary=o.sampling?.boundary;
    const samplingOK=!first?base.task_execution==='completed':binding?.root_thread_id===o.thread_id&&binding.thread_id===first.params.threadId&&binding.turn_id===first.params.turnId&&binding.item_id===first.params.item.id&&boundary?.closed===true&&['interrupted','natural_completion_race'].includes(o.sampling.stop_result);
    const endOK=o.capture_complete===true&&o.native_ancestry_verified===true&&o.native_barrier?.active===0&&o.native_barrier?.pending===0&&canonicalOrderComplete(o);
    const firstOK=v.first_route_complete&&endOK&&samplingOK&&(!(!first&&o.meter_settlement.task.late_bindings));
    let verdict='INCONCLUSIVE';
    if(firstOK){verdict=base.first_route===expected.first&&base.parameters_match?'MATCH':'FAIL';if(o.query_relevance==='fail')verdict='FAIL';else if(verdict==='MATCH'&&expected.manual_query_review&&o.query_relevance!=='pass')verdict='REVIEW_REQUIRED';}
    return {...base,first_route_verdict:verdict,prior_all_items_verdict:base.first_route_verdict,
      full_task_success:'NOT_EVALUATED_SAMPLED',visibility:v,sampling_qualified:samplingOK,owner_barrier_qualified:endOK};
  });
  return {feature:'FEAT-175',revision:3,sample_kind:e.kind,scorer_calls_provider:false,rows,
    qualification:'First-action evidence only; no full-task or production qualification',raw_events_modified:false};
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
  try{const [cmd,file,extra]=process.argv.slice(2);assert(!extra);let result;
    if(cmd==='freeze'&&!file)result=freeze();else if(cmd==='preflight'&&!file)result=preflight();
    else if(cmd==='score'&&file){const data=read(file);assert(data.length<=4<<20);result=evaluate(JSON.parse(data));}
    else throw Error('Offline commands: freeze | preflight | score EVIDENCE');
    process.stdout.write(JSON.stringify(result,null,2)+'\n');
  }catch(error){process.stderr.write(error.message+'\n');process.exitCode=1;}
}
