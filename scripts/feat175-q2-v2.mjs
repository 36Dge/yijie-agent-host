// Q2-v2 offline scoring. v1 code, prompts, source lock and evidence stay intact.
import assert from 'node:assert/strict';
import {readFileSync, writeFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {isDeepStrictEqual} from 'node:util';
import {evaluate as v1Evaluate, preflight as v1Preflight, hostRoot, packageRoot, packageDigest} from './feat175-q2.mjs';

export const root = path.join(packageRoot, '../feat175-q2-v2');
const workspace = path.dirname(hostRoot);
const read = p => readFileSync(p);
const json = p => JSON.parse(read(p));
export const digest = value => createHash('sha256').update(value).digest('hex');
export const policy = () => json(path.join(root, 'sampling.json'));

export function freeze() {
  v1Preflight();
  const previous = json(path.join(packageRoot, 'source-lock.json'));
  const additions = [
    'scripts/feat175-q2-v2.mjs', 'scripts/feat175_q2_v2_meter.py', 'scripts/feat175_q2_v2_run.py',
    'scripts/test_feat175_q2_v2_meter.py', 'scripts/test-feat175-q2-v2.mjs', 'scripts/check-builtin-web-q2-v2.sh',
    'internal/codex/builtin_web_q2_v2_test.go', 'internal/codex/testdata/feat175-q2-v2/sampling.json',
    'internal/codex/testdata/feat175-q2/source-lock.json',
  ].map(p => 'yijie-agent-host/' + p);
  const lock = {...previous, schema_version:2, purpose:'Q2-v2 local-only sampling qualification',
    predecessor_source_lock_sha256:digest(read(path.join(packageRoot,'source-lock.json'))),
    sampling_sha256:digest(read(path.join(root,'sampling.json'))),
    sources:[...previous.sources, ...additions.map(file=>({file,sha256:digest(read(path.join(workspace,file)))}))]};
  writeFileSync(path.join(root,'source-lock.json'), JSON.stringify(lock,null,2)+'\n');
  return {sources:lock.sources.length, live_entry:false};
}

export function preflight() {
  v1Preflight(); const lock=json(path.join(root,'source-lock.json')), p=policy();
  assert.equal(p.base_cases_sha256,packageDigest());
  assert.equal(p.supplier_model_calls,0); assert.equal(p.supplier_rest_calls,0);
  assert.equal(lock.predecessor_source_lock_sha256,digest(read(path.join(packageRoot,'source-lock.json'))));
  assert.equal(lock.sampling_sha256,digest(read(path.join(root,'sampling.json'))));
  for(const row of lock.sources) {
    const file=path.resolve(workspace,row.file);assert(file.startsWith(workspace+path.sep));
    assert.equal(digest(read(file)),row.sha256,'Frozen source changed: '+row.file);
  }
  return {feature:'FEAT-175',revision:2,source_files:lock.sources.length,package_sha256:packageDigest(),
    predecessor_preserved:true,paid_entry_enabled:false,authorized_new_provider_calls:0};
}

// Keep first-route validation, canonical schema, argument checks and ancestry
// semantics in the existing scorer. Full-task success is a SEPARATE outcome.
export function evaluate(evidence, options={}) {
  preflight();
  assert([1,2].includes(evidence.schema_version));
  const replay=evidence.schema_version===1;
  if(!replay) {
    assert.equal(evidence.sampling_sha256,digest(read(path.join(root,'sampling.json'))));
    assert(['first_action','full_turn'].includes(evidence.mode));
  }
  const original=structuredClone(evidence);
  const adapted={...structuredClone(evidence),schema_version:1};
  for(const o of original.observations) {
    const args=new Map();
    for(const event of o.events) {
      const item=event.params?.item;
      if(item?.type!=='mcpToolCall')continue;
      const key=event.params.threadId+'/'+event.params.turnId+'/'+item.id;
      if(args.has(key))assert(isDeepStrictEqual(item.arguments,args.get(key)),'Native MCP arguments changed after first observation');
      else args.set(key,structuredClone(item.arguments));
    }
  }
  const manual=options.manualReview;
  if(manual) {
    assert(options.evidenceBytes, 'Review requires original evidence bytes');
    assert.equal(manual.raw_evidence_sha256,digest(options.evidenceBytes),'Review belongs to another recording');
    for(const review of manual.cases) {
      const o=adapted.observations.find(o=>o.model===review.model&&o.case_id===review.case_id);
      assert(o&&o.thread_id===review.thread_id&&o.turn_id===review.turn_id,'Review binding changed');
      if(review.query_relevance_review==='pass')o.query_relevance='pass';
    }
  }
  const base=v1Evaluate(adapted);
  const rows=base.rows.map(row=>{
    const o=original.observations.find(o=>o.model===row.model&&o.case_id===row.case_id);
    if(!o)return {...row,first_route_verdict:'MISSING',task_execution:'MISSING',full_task_success:'MISSING'};
    const first=o.events.find(e=>e.method==='item/started'&&['webSearch','mcpToolCall'].includes(e.params.item?.type));
    const starts=o.events.filter(e=>e.method==='item/started');
    const hosted=starts.filter(e=>e.params.item.type==='webSearch');
    const firstMCPResult=o.events.findIndex(e=>e.method==='item/completed'&&e.params.item?.type==='mcpToolCall');
    const hostedBeforeMCP=o.events.filter((e,i)=>(firstMCPResult<0||i<firstMCPResult)&&e.method==='item/started'&&e.params.item?.type==='webSearch').length;
    let route=row.verdict, sampling='not_requested';
    if(!replay&&original.mode==='first_action'&&first) {
      const s=o.sampling, trigger=s?.trigger;
      const bound=trigger&&trigger.root_thread_id===o.thread_id&&trigger.item_id===first.params.item.id&&trigger.thread_id===first.params.threadId&&trigger.turn_id===first.params.turnId;
      const normal=s?.stop_result==='interrupted'||s?.stop_result==='natural_completion_race';
      const closed=s?.boundary?.closed===true&&Number.isSafeInteger(s?.boundary?.sequence)&&s.boundary.sequence>0;
      sampling=bound&&normal&&closed?'observed_and_stopped':'unqualified_stop';
      if(sampling!=='observed_and_stopped')route='INCONCLUSIVE';
    } else if(!replay&&original.mode==='first_action')sampling=row.turn_status==='completed'?'natural_completion_no_retrieval':'no_action_inconclusive';
    const review=manual?.cases.find(r=>r.model===row.model&&r.case_id===row.case_id);
    let success=row.turn_status==='failed'?'FAILED':row.turn_status==='interrupted'?'INTERRUPTED':'REVIEW_REQUIRED';
    if(!replay&&original.mode==='first_action')success='NOT_EVALUATED_SAMPLED';
    else if(original.kind==='synthetic'&&row.turn_status==='completed')success='NOT_EVALUATED_SYNTHETIC';
    else if(row.turn_status==='completed'&&review?.task_outcome==='PASS')success='PASS_REVIEWED';
    // Missing starts, incomplete bindings/visibility or a failed review never
    // become a successful full task merely because Runtime said completed.
    if(success==='PASS_REVIEWED'&&route==='INCONCLUSIVE')success='INCONCLUSIVE';
    return {model:row.model,case_id:row.case_id,first_route:row.first_route,first_route_verdict:route,
      parameters_match:row.parameters_match,task_execution:row.turn_status,full_task_success:success,
      sampling,sampling_mode:replay?'full_turn_recording':original.mode,
      hosted_calls:hosted.length,additional_hosted_after_first:Math.max(0,hosted.length-1),
      hosted_before_first_mcp_result:hostedBeforeMCP,extra_hosted_on_independent_task:row.extra_hosted,
      first_mcp_result_observed:firstMCPResult>=0,
      mcp_items:row.mcp_items,unplanned_followups:row.unplanned_followups,later_recovery:row.later_recovery,
      tool_execution:row.execution_status,source_quality:row.source_quality};
  });
  return {feature:'FEAT-175',scorer_revision:2,sample_kind:original.kind,source_recording_schema:original.schema_version,
    scorer_calls_provider:false,new_provider_calls:0,rows,
    per_model:base.per_model.map(m=>({model:m.model,planned_tasks:6,observed:rows.filter(r=>r.model===m.model&&r.first_route_verdict!=='MISSING').length,
      first_route_matches:rows.filter(r=>r.model===m.model&&r.first_route_verdict==='MATCH').length,
      full_tasks_passed:rows.filter(r=>r.model===m.model&&r.full_task_success==='PASS_REVIEWED').length,
      missing:rows.filter(r=>r.model===m.model&&r.first_route_verdict==='MISSING').length})),
    qualification:'NOT GRANTED: separate routing, complete answers, real source quality and provider qualification required',
    original_recording_modified:false};
}

if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    const [cmd,input,review,extra]=process.argv.slice(2);assert(!extra);
    let result;
    if(cmd==='freeze'&&!input)result=freeze();
    else if(cmd==='preflight'&&!input)result=preflight();
    else if(cmd==='score'&&input) {
      const bytes=read(input);assert(bytes.length<=4<<20);
      result=evaluate(JSON.parse(bytes),{evidenceBytes:bytes,manualReview:review?json(review):undefined});
    } else throw Error('Offline only: freeze | preflight | score EVIDENCE [BOUND_REVIEW]. No live mode.');
    process.stdout.write(JSON.stringify(result,null,2)+'\n');
  } catch(error){process.stderr.write(error.message+'\n');process.exitCode=1;}
}
