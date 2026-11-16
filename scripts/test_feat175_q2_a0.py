"""Normal local state/stream/authorization tests. No provider credentials or I/O."""
import copy
import datetime as dt
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest

from feat175_q2_a0_meter import Meter, v1, v2
from feat175_q2_a0_policy import PACKAGE, POLICY, validate_grant, consume_grant
from feat175_q2_a0_transport import Observer, deadline
from test_feat175_q2_meter import observation


class A0Test(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix="feat175-a0-unit-")
        self.root=Path(self.temp.name);self.m=Meter(self.root,binding_wait=.02)
        self.m.state["generation"]="ordinary-generation";self.seq=0
    def tearDown(self):self.temp.cleanup()
    def arm(self):
        i=len(self.m.state["tasks"]);entry=POLICY["schedule"][i]
        self.m.arm(entry["model"],entry["case_id"],f"root-{i}")
        return i,entry
    def bind(self,index,thread=None,turn=None,state="started"):
        self.seq+=1;thread=thread or f"root-{index}";turn=turn or f"turn-{index}"
        return self.m.bind_native({"generation":"ordinary-generation","root_thread_id":f"root-{index}","thread_id":thread,"turn_id":turn,
                                  "state":state,"native_status":"completed" if state=="ended" else "inProgress","native_sequence":self.seq})
    def meta(self,index,thread=None,turn=None):
        return {"model":POLICY["schedule"][index]["model"],"thread_id":thread or f"root-{index}","turn_id":turn or f"turn-{index}","request_adjustments":[]}
    def settle(self,row):self.m.settle(row,observation(row["model"]))
    def sample(self,index):
        b={"root_thread_id":f"root-{index}","thread_id":f"root-{index}","turn_id":f"turn-{index}","item_id":"ordinary-item"}
        self.m.sample(b);self.m.acknowledge_interrupt(b);return b
    def close_review(self,index):
        self.m.close_root(f"root-{index}","natural_terminal");self.bind(index,state="ended");self.m.finish()
        e=POLICY["schedule"][index];return self.m.review(index,{**e,"qualified":True,"evidence_sha256":"f"*64,"review_kind":"local_scripted_protocol"})

    def test_unseen_identity_after_cutoff_has_no_active_task_fallback(self):
        i,_=self.arm();self.bind(i);self.settle(self.m.admit(self.meta(i)));self.sample(i)
        with self.assertRaisesRegex(v2.Denied,"binding_unverified"):
            self.m.admit(self.meta(i,"late-child","late-turn"))
        self.assertEqual(len(self.m.state["requests"]),1)
        self.assertIsNone(self.m.state["denials"][-1]["task_index"])

    def test_late_native_binding_is_a_closed_root_tombstone(self):
        i,_=self.arm();self.bind(i);self.settle(self.m.admit(self.meta(i)));self.sample(i)
        self.assertFalse(self.bind(i,"late-child","late-turn")["accepted"])
        with self.assertRaisesRegex(v2.Denied,"root_closed"):self.m.admit(self.meta(i,"late-child","late-turn"))
        self.assertEqual(self.m.state["denials"][-1]["task_index"],i)
        self.assertFalse(self.m.state["halted"])

    def test_late_child_cannot_use_next_task_budget(self):
        i,_=self.arm();self.bind(i);self.settle(self.m.admit(self.meta(i)));self.close_review(i)
        j,_=self.arm();self.bind(j)
        self.bind(i,"old-child","old-turn")
        with self.assertRaisesRegex(v2.Denied,"root_closed"):self.m.admit(self.meta(i,"old-child","old-turn"))
        self.assertEqual(self.m.counts(j)["model_task"],0)
        self.assertEqual(self.m.state["denials"][-1]["task_index"],i)
        self.assertEqual(self.m.state["halt_reason"],"late_binding_after_finish")

    def test_unknown_request_waits_for_verified_native_registration(self):
        self.m.binding_wait=1;i,_=self.arm();result=[]
        worker=threading.Thread(target=lambda:result.append(self.m.admit(self.meta(i))))
        worker.start();self.bind(i);worker.join(2)
        self.assertFalse(worker.is_alive());self.assertEqual(len(result),1);self.settle(result[0])

    def test_ended_turn_never_authorizes_dispatch(self):
        i,_=self.arm();self.bind(i,state="ended")
        with self.assertRaisesRegex(v2.Denied,"turn_inactive"):self.m.admit(self.meta(i))
        self.assertEqual(self.m.state["real_model_http_requests"],0)

    def test_no_review_no_phase_or_next_task(self):
        i,_=self.arm();self.bind(i);self.settle(self.m.admit(self.meta(i)))
        self.m.close_root(f"root-{i}","natural_terminal");self.bind(i,state="ended");self.m.finish()
        with self.assertRaisesRegex(v2.Denied,"review_required"):self.arm()

    def test_stage_upgrade_keeps_all_counts_and_costs(self):
        for i in range(4):
            actual,_=self.arm();self.assertEqual(actual,i);self.bind(i)
            self.settle(self.m.admit(self.meta(i)));before=self.m.state["observed_cost_upper_micro_cny"]
            self.close_review(i)
            self.assertEqual(self.m.state["observed_cost_upper_micro_cny"],before)
        self.assertEqual(self.m.state["phase"],"A2")
        self.assertEqual(len(self.m.state["requests"]),4)
        self.assertEqual(self.m.limits["soft_stop_micro_cny"],20_000_000)
        self.assertEqual(self.m.limits["model_http_requests"],48)

    def test_inflight_binding_prevents_finish_barrier(self):
        i,_=self.arm();self.bind(i);self.bind(i,"child","child-turn");self.m.close_root("root-0","first_native_action")
        self.bind(i,state="ended")
        with self.assertRaisesRegex(v2.Denied,"native_barrier_pending"):self.m.finish()

    def test_dispatch_commit_cannot_cross_sample_boundary(self):
        i,_=self.arm();self.bind(i);row=self.m.admit(self.meta(i));self.sample(i)
        with self.assertRaisesRegex(v2.Denied,"root_closed"):self.m.dispatch(row)
        self.assertFalse(row["dispatch_attempted"]);self.assertEqual(row["status"],"cancelled_before_dispatch")
        self.assertEqual(self.m.state["real_model_http_requests"],0)

    def grant(self):
        prices=json.loads((v1.PACKAGE/'pricing-snapshot.json').read_text());at=dt.datetime.fromisoformat(prices['checked_at'])+dt.timedelta(seconds=1)
        grant={"schema_version":3,"feature":"FEAT-175","purpose":POLICY['purpose'],"status":"approved",
               "authorization_id":"00000000-0000-4000-8000-000000000a00","approved_by":"段成威","approval_source":"current-user-reply:UNIT TEST DATA ONLY",
               "approved_at":at.isoformat(),"expires_at":(at+dt.timedelta(hours=1)).isoformat(),"stages":["A1","A2"],
               "money_mode":"soft_stop_after_observation","allow_inflight_overshoot":True,"supplier_rest_calls":0,
               "source_lock_sha256":v1.digest((PACKAGE/'source-lock.json').read_bytes()),"policy_sha256":v1.digest((PACKAGE/'policy.json').read_bytes()),
               "package_sha256":v1.digest((v1.PACKAGE/'cases.json').read_bytes()),"profile_sha256":v1.digest((v1.PACKAGE/'effective-profile.json').read_bytes()),
               "pricing_sha256":v1.shared.canonical_digest(prices),"schedule_sha256":v1.shared.canonical_digest(POLICY['schedule'])}
        return grant,at

    def test_grant_binds_sources_scope_stage_and_expiry(self):
        grant,at=self.grant();validate_grant(grant,at=at)
        for field,value in [("purpose","Q2-route-feasibility"),("allow_inflight_overshoot",False),("supplier_rest_calls",1),
                            ("source_lock_sha256","wrong"),("schedule_sha256","wrong"),("stages",["A2"]),("status","not_authorized")]:
            with self.subTest(field=field),self.assertRaises(ValueError):validate_grant({**grant,field:value},at=at)
        with self.assertRaises(ValueError):validate_grant(grant,at=at+dt.timedelta(hours=2))

    def test_live_refuses_absent_and_old_grants_before_keys_or_directory(self):
        out=self.root/'never-created';script=v1.HOST/'scripts/feat175_q2_a0_run.py'
        for extra in ([],['--authorization',str(v1.PACKAGE/'authorization-not-granted.json'),'--kimi-key-file','/nonexistent/unused','--minimax-key-file','/nonexistent/unused']):
            result=subprocess.run([sys.executable,'-B',str(script),'live','--output',str(out),*extra],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertFalse(out.exists())

    def test_one_shot_registry_and_stage_one_only(self):
        grant,_=self.grant();at=v1.now();grant['approved_at']=at.isoformat();grant['expires_at']=(at+dt.timedelta(hours=1)).isoformat();grant['stages']=['A1']
        # Pure file guard; no controller, credential loading or provider dispatch.
        used=consume_grant(grant,self.root,self.root/'not-run');self.assertTrue(used.exists())
        with self.assertRaises(FileExistsError):consume_grant(grant,self.root,self.root/'different-output')

    def test_incremental_sse_preserves_late_hosted_and_terminal_usage(self):
        i,_=self.arm();self.bind(i);row=self.m.admit(self.meta(i));obs=Observer(self.m,row)
        first={'type':'web_search_call','id':'ordinary-first','status':'completed','action':{'type':'search','query':'ordinary'}}
        early=('data: '+json.dumps({'type':'response.output_item.added','item':first})+'\n\n').encode()
        for part in [early[:9],early[9:]]:obs.feed(part)
        self.sample(i)
        late={**first,'id':'ordinary-late'}
        events=[{'type':'response.output_item.done','item':late},{'type':'response.completed','response':{'model':row['model'],'status':'completed','output':[first,late],'usage':{'input_tokens':100,'output_tokens':10}}}]
        for e in events:obs.feed(('data: '+json.dumps(e)+'\n\n').encode())
        self.assertEqual(obs.result['native_search_calls'],2)
        self.assertFalse(row['provider_events'][0]['after_cutoff'])
        self.assertTrue(next(e for e in row['provider_events'] if e['item_id']=='ordinary-late')['after_cutoff'])

    def test_missing_eof_is_unknown_not_refunded(self):
        i,_=self.arm();self.bind(i);row=self.m.admit(self.meta(i))
        self.m.settle(row,{**observation(row['model']),'stream_complete':False})
        self.assertEqual(row['status'],'outcome_unknown');self.assertIsNone(row['observed_cost_upper_micro_cny'])
        self.assertTrue(self.m.state['halted']);self.assertEqual(len(self.m.state['requests']),1)

    def test_absolute_and_drain_deadlines_do_not_reset(self):
        d=POLICY['deadlines_seconds'];self.assertEqual(deadline(10,None,d),250)
        self.assertEqual(deadline(10,20,d),80);self.assertEqual(deadline(10,220,d),250)


if __name__=='__main__':unittest.main()
