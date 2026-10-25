#!/usr/bin/env python3
"""Approved repaired Runtime checks, including native child failure and cold history."""
import argparse,datetime,hashlib,json,os,re,subprocess
from pathlib import Path
p=argparse.ArgumentParser()
for name in ('output','go-binary','runtime-binary','runtime-manifest','legacy-binary','legacy-manifest'):p.add_argument('--'+name,type=Path,required=True)
a=p.parse_args();root=Path(__file__).resolve().parents[1]
paths={n:getattr(a,n).resolve(strict=True) for n in ('go_binary','runtime_binary','runtime_manifest','legacy_binary','legacy_manifest')}
a.output.mkdir(parents=True,exist_ok=False)
env={k:os.environ[k] for k in ('PATH','HOME','TMPDIR','GOPATH','GOCACHE') if k in os.environ}
env.update(GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off',YIJIE_FEAT164_S4D_OFFLINE='true')
for prefix,key in [('YIJIE_CODEX_INTEGRATION','runtime'),('YIJIE_FEAT164_LEGACY','legacy')]:
 for suffix in ('binary','manifest'):env[prefix+'_'+suffix.upper()]=str(paths[key+'_'+suffix])
version=subprocess.check_output([str(paths['go_binary']),'version'],env=env,text=True).strip()
assert 'go1.26.5 ' in version,'Use the reviewed installed toolchain, not a network upgrade'
selector='^TestFEAT164(LoadedLegacyGraphCutover|LoadedRetiredAgentGuard|AutomaticCompaction|PreviousModelCompactionGuard|MigratedAutomaticCompaction)$'
command=[str(paths['go_binary']),'test','-race','-json','./internal/codex','-run',selector,'-count=1','-failfast','-timeout=0']
r={'scope':'S4e repaired native terminal subset; not complete TCONF or App acceptance','full_tconf_pass':False,'started_at':datetime.datetime.now().astimezone().isoformat(),'command':command,'toolchain':version,'head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'source_status':subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).splitlines(),'inputs':{k:{'path':str(v),'sha256':hashlib.sha256(v.read_bytes()).hexdigest()} for k,v in paths.items() if k!='go_binary'},'test_source_sha256':hashlib.sha256((root/'internal/codex/feat164_cutover_integration_test.go').read_bytes()).hexdigest(),'real_model_http_added':0}
log=a.output/'checks.log'
with log.open('wb') as f:done=subprocess.run(command,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT)
events=[json.loads(x) for x in log.read_text().splitlines() if x.startswith('{')]
r['exit_code']=done.returncode;r['passed']=[x['Test'] for x in events if x.get('Action')=='pass' and x.get('Test')];r['failed']=[x['Test'] for x in events if x.get('Action')=='fail' and x.get('Test')];r['skipped']=[x['Test'] for x in events if x.get('Action')=='skip' and x.get('Test')]
ledgers=[]
for x in events:
 text=x.get('Output','');m=re.search(r'loopback ledger=(.*); real_provider_http=0',text)
 if m:ledgers.append(json.loads(m[1]))
r['loopback_requests']=sum(map(len,ledgers));r['completed_fixture_ledgers']=len(ledgers)
ok=done.returncode==0 and len(r['passed'])==7 and not r['failed'] and not r['skipped'] and r['loopback_requests']==23 and len(ledgers)==6
r['status']='PASS_FOR_NATIVE_TERMINAL_SUBSET' if ok else 'FAIL';r['verified_at']=datetime.datetime.now().astimezone().isoformat();r['log_sha256']=hashlib.sha256(log.read_bytes()).hexdigest()
(a.output/'result.json').write_text(json.dumps(r,indent=2)+'\n');print(r['status'],flush=True)
raise SystemExit(0 if ok else 1)
