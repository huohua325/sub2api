"""Scope, source freeze and structured test evidence for the delivery workflow."""
import json
import os
from pathlib import Path
import subprocess

from delivery_core import DeliveryError, digest, json_hash, tree_hash, utcnow, write_json


def changed_files(root, base):
    tracked = subprocess.check_output(['git', 'diff', '--name-only', '-z', base, '--'], cwd=root)
    untracked = subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', '-z'], cwd=root)
    return sorted(set(p.decode('utf-8') for p in (tracked + untracked).split(b'\0') if p))


def change_plan(files, *, upstream=False):
    files = sorted(set(files))
    frontend = any(p.startswith('frontend/') for p in files)
    backend = [p for p in files if p.startswith('backend/')]
    application = frontend or bool(backend) or upstream
    sensitive = any(any(s in p.lower() for s in ['billing', '/auth', '/jwt']) for p in backend)
    migrations = any(p.startswith('backend/migrations/') for p in backend)
    quota = any('quota' in p.lower() or 'purpose_allowance' in p for p in backend)
    packages = {str(Path(p).parent.relative_to('backend')).replace('\\', '/') for p in backend if p.endswith('.go')}
    if quota:
        packages.update(['internal/repository', 'internal/service', 'internal/handler/admin', 'internal/pkg/adminaccess'])
    if migrations:
        packages.update(['internal/repository', 'migrations'])
    return {'schema': 1, 'files': files, 'application_release': application,
            'frontend_full': application, 'quota_postgres_race': application,
            'feature_path_required': application, 'broad_go': upstream or sensitive,
            'migration_sandbox': migrations,
            'go_packages': sorted('./' + p for p in packages),
            'tool_tests': any(p.startswith('tools/') for p in files),
            'early_preflight': application or any(p.startswith('tools/') and any(s in p for s in ['release', 'upgrade', 'preflight']) for p in files),
            'known_failures': 'Report known baseline failures; unrelated broad Go suites belong to periodic/upstream checks, not every small delivery.'}


def application_snapshot(root):
    root = Path(root)
    paths = []
    backend = root / 'backend'
    if backend.exists():
        for folder, directories, names in os.walk(backend):
            directories[:] = [d for d in directories if d not in {'.git', 'node_modules', '__pycache__'}
                              and (Path(folder) / d).relative_to(root).as_posix() != 'backend/internal/web/dist']
            paths.extend((Path(folder) / name).relative_to(root).as_posix() for name in names)
    return {'frontend': tree_hash(root, ['frontend'], inputs=True)['frontend'],
            'backend': json_hash(tree_hash(root, sorted(paths), inputs=True))}


def freeze_source(root, output, *, reviewed):
    if not reviewed:
        raise DeliveryError('Finish the feature path and review, then freeze with --reviewed')
    data = {'schema': 1, 'root': str(Path(root).resolve()), 'reviewed': True,
            'frozen_at': utcnow(), 'application': application_snapshot(root)}
    write_json(Path(output) / 'freeze.json', data)
    return data


def require_frozen(root, output):
    path = Path(output) / 'freeze.json'
    if not path.exists():
        raise DeliveryError('Freeze reviewed source before final checks: delivery.py freeze --reviewed --output ...')
    data = json.loads(path.read_text(encoding='utf-8'))
    if data.get('schema') != 1 or data.get('root') != str(Path(root).resolve()) or data.get('reviewed') is not True:
        raise DeliveryError('Source freeze belongs to another checkout or was not reviewed')
    if data.get('application') != application_snapshot(root):
        raise DeliveryError('Application source changed after freeze; finish targeted checks/review and freeze again')
    return data


def parse_go_events(text):
    events = []
    for line in text.splitlines():
        try:
            value = json.loads(line)
            if isinstance(value, dict) and 'Action' in value and 'Package' in value:
                events.append(value)
        except ValueError:
            pass
    if not events:
        raise DeliveryError('No Go JSON test events; keep the original log and inspect the toolchain failure')
    tests = [e for e in events if e.get('Test')]
    return {'passed': sum(e['Action'] == 'pass' for e in tests),
            'skipped': sum(e['Action'] == 'skip' for e in tests),
            'failed': sum(e['Action'] == 'fail' for e in tests),
            'failure_roots': sorted({e['Package'] + ':' + e['Test'].split('/')[0] for e in tests if e['Action'] == 'fail'}),
            'failed_packages': sorted({e['Package'] for e in events if not e.get('Test') and e['Action'] == 'fail'})}


def select_build_source(root, current, version, previous):
    source = previous.get('source_commit')
    if previous.get('version') != version or not source:
        return current
    ancestor = subprocess.run(['git', 'merge-base', '--is-ancestor', source, current], cwd=root, capture_output=True)
    if ancestor.returncode:
        return current
    changed = subprocess.check_output(['git', 'diff', '--name-only', source, current, '--', 'frontend', 'backend'], cwd=root)
    return current if changed.strip() else source
