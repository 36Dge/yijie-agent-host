#!/usr/bin/env python3
"""FEAT-164 retained-state qualification. Real canonical Runtime, loopback only.

No timeout/kill, automatic retry, database checkpoint, or home deletion. A failed
normal shutdown stops the run and retains all evidence for owner inspection.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import sqlite3
import subprocess
import sys

parser = argparse.ArgumentParser()
for flag in ('output', 'go-binary', 'runtime-binary', 'runtime-manifest', 'legacy-binary', 'legacy-manifest'):
    parser.add_argument('--' + flag, type=Path, required=True)
a = parser.parse_args()
repo = Path(__file__).resolve().parents[1]
paths = {name: getattr(a, name).resolve(strict=True) for name in ('go_binary', 'runtime_binary', 'runtime_manifest', 'legacy_binary', 'legacy_manifest')}
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
assert sha(paths['runtime_binary']) == '283d12e16b124942302fcdc8281c729a8a7f676498f06df1d2646292905f579e', 'Reviewed r5 required'
assert sha(paths['legacy_binary']) == '9766d7e10d25327f0595964a19ad67e1e1c1db0c22e5a65f9af66b547ab316c6', 'Reviewed historical producer required'
output = a.output.absolute()
output.mkdir(parents=True, exist_ok=False, mode=0o700)
output = output.resolve()
homes = output / 'homes'
homes.mkdir(mode=0o700)
env = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR', 'GOPATH', 'GOCACHE') if key in os.environ}
env.update(GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', YIJIE_FEAT164_S4D_OFFLINE='true', YIJIE_FEAT164_RETAIN_ROOT=str(homes))
for prefix, label in [('YIJIE_CODEX_INTEGRATION', 'runtime'), ('YIJIE_FEAT164_LEGACY', 'legacy')]:
    for suffix in ('binary', 'manifest'):
        env[prefix + '_' + suffix.upper()] = str(paths[label + '_' + suffix])
version = subprocess.check_output([str(paths['go_binary']), 'version'], env=env, text=True).strip()
assert 'go1.26.5 ' in version, 'Reviewed local Go toolchain required'
tracked = subprocess.check_output(['git', 'ls-files', '-z'], cwd=repo).decode().split('\0')
result = {'scope': 'Current r5 safe surfaces, auxiliary, child-terminal/replay and compaction subset; not App/real-provider/full TCONF', 'contract_impact': 'none', 'started_at': datetime.datetime.now().astimezone().isoformat(), 'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repo, text=True).strip(), 'status_before': subprocess.check_output(['git', 'status', '--porcelain'], cwd=repo, text=True).splitlines(), 'source_files': {n: sha(repo/n) for n in tracked if n and (repo/n).is_file()}, 'inputs': {k: {'path': str(v), 'sha256': sha(v)} for k, v in paths.items()}, 'toolchain': version, 'real_model_http_added': 0, 'checks': []}

def save():
    (output/'result.json').write_text(json.dumps(result, indent=2) + '\n')

def record_unhandled_failure(kind, value, traceback):
    result.update(status='FAIL', failure_class='qualification_or_data_audit', failure_type=kind.__name__)
    save()
    sys.__excepthook__(kind, value, traceback)

sys.excepthook = record_unhandled_failure

cases = [
    ('surfaces', '^TestFEAT164CallSurfacesRuntime$', 1, 1, 6),
    ('auxiliary', '^TestFEAT164(NativeAuxiliaryAgent|NativeAuxiliaryCompaction|NativeGuardianModel|AuxiliaryMeterLayer)$', 4, 3, 9),
    ('cutover', '^TestFEAT164(LoadedLegacyGraphCutover|LoadedRetiredAgentGuard|AutomaticCompaction|PreviousModelCompactionGuard|MigratedAutomaticCompaction)$', 7, 6, 23),
]
for name, selector, expected_passes, expected_roots, expected_http in cases:
    cmd = [str(paths['go_binary']), 'test', '-race', '-json', './internal/codex', '-run', selector, '-count=1', '-failfast', '-timeout=0']
    log = output/(name+'.log')
    with log.open('xb') as stream:
        done = subprocess.run(cmd, cwd=repo, env=env, stdout=stream, stderr=subprocess.STDOUT)
    events = [json.loads(line) for line in log.read_text().splitlines() if line.startswith('{')]
    passed = [e['Test'] for e in events if e.get('Action') == 'pass' and e.get('Test')]
    failed = [e.get('Test', 'package') for e in events if e.get('Action') == 'fail']
    skipped = [e.get('Test', 'package') for e in events if e.get('Action') == 'skip']
    chunks = {}
    for e in events:
        key = (e.get('Package'), e.get('Test'))
        chunks[key] = chunks.get(key, '') + e.get('Output', '')
    retained = []
    for text in chunks.values():
        for match in re.finditer(r'FEAT164_RETAINED_ROOT=(".*?"); normal_shutdown=(true|false); loopback_http=(\d+)', text):
            retained.append({'path': json.loads(match[1]), 'normal_shutdown': match[2] == 'true', 'loopback_http': int(match[3])})
    check = {'name': name, 'command': cmd, 'exit_code': done.returncode, 'passed': passed, 'failed': failed, 'skipped': skipped, 'retained': retained, 'log_sha256': sha(log)}
    result['checks'].append(check)
    ok = done.returncode == 0 and len(passed) == expected_passes and not failed and not skipped and len(retained) == expected_roots and sum(r['loopback_http'] for r in retained) == expected_http and all(r['normal_shutdown'] for r in retained)
    check['status'] = 'PASS_TESTS_PENDING_DATA_AUDIT' if ok else 'FAIL'
    result['status'] = 'IN_PROGRESS' if ok else 'FAIL'
    save()
    if not ok:
        print(name + ': FAIL; all files retained, no automatic retry', flush=True)
        raise SystemExit(1)
    for record in retained:
        original = Path(record['path'])
        assert original.parent == homes and original.is_dir(), 'Unexpected retained root'
        handles = subprocess.run(['lsof', '-nP', '+D', str(original)], capture_output=True, text=True)
        assert handles.returncode == 1 and not handles.stdout, 'Open Runtime home handles; stop without forced cleanup'
        snapshot = output/'snapshots'/name/original.name
        snapshot.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(original, snapshot)
        original_files = {str(p.relative_to(original)): sha(p) for p in original.rglob('*') if p.is_file()}
        assert original_files == {str(p.relative_to(snapshot)): sha(p) for p in snapshot.rglob('*') if p.is_file()}, 'Full snapshot differs'
        databases = {}
        for db in sorted((snapshot/'runtime').glob('*.sqlite')):
            wal = original/'runtime'/(db.name+'-wal')
            wal_bytes = wal.stat().st_size if wal.exists() else 0
            connection = sqlite3.connect(db.as_uri()+'?mode=ro', uri=True)
            integrity = connection.execute('PRAGMA integrity_check').fetchall()
            connection.close()
            databases[db.name] = {'wal_bytes': wal_bytes, 'integrity': integrity, 'sha256': original_files['runtime/'+db.name]}
        assert len(databases) == 4 and all(v['wal_bytes'] == 0 and v['integrity'] == [('ok',)] for v in databases.values()), 'Runtime normal-close data qualification incomplete'
        record.update(snapshot=str(snapshot), files=original_files, databases=databases, open_handles=False)
    check['status'] = 'PASS'
    save()
    print(name + ': PASS; complete homes retained', flush=True)
result.update(status='PASS_SCOPED', loopback_model_http_added=38, completed_at=datetime.datetime.now().astimezone().isoformat())
save()
print('12 checks; 10 retained Runtime homes; 38 loopback HTTP; zero real model HTTP', flush=True)
