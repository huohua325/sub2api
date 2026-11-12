"""Package only successful, current delivery evidence; never rebuild while packaging."""
import gzip
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess

from delivery_core import DeliveryError, contained, digest, tree_hash, utcnow, write_json


def require_committed_inputs(root):
    status = subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all', '--',
                                      'frontend', 'backend', 'tools', 'AGENTS.md'], cwd=root, text=True, timeout=30)
    if status.strip():
        raise DeliveryError('Release inputs must be committed, including untracked files. Use a clean release checkout; preserve user-owned files.')


def package_release(root, evidence, destination, binary, version, baseline_sha256, baseline_commit, *, full_upload=False, baseline_binary=None):
    root = Path(root).resolve(); evidence = Path(evidence).resolve(); destination = Path(destination).resolve(); binary = Path(binary).resolve()
    if destination.exists(): raise DeliveryError('Release directory already exists; immutable packages are never overwritten')
    if not destination.is_relative_to(root / 'output'):
        # Isolated test repositories may use a different ignored evidence directory.
        if not destination.is_relative_to(evidence): raise DeliveryError('Package must stay inside the evidence directory or output/')
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}', version): raise DeliveryError('Invalid version')
    if not re.fullmatch(r'[0-9a-f]{64}', baseline_sha256) or not re.fullmatch(r'[0-9a-f]{40}', baseline_commit):
        raise DeliveryError('Explicit fixed baseline commit and SHA-256 required')
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=root, text=True, timeout=30).strip()
    require_committed_inputs(root)
    source = git('rev-parse', 'HEAD')
    if git('diff', '--name-only', baseline_commit, source, '--', 'backend'):
        raise DeliveryError('Backend changes require the full upgrade runbook; this lane is frontend-only')
    data = json.loads((evidence / 'manifest.json').read_text())
    if data.get('schema') != 1 or data.get('root') != str(root): raise DeliveryError('Evidence belongs to another checkout')
    stages = {s['name']: s for s in data['steps']}
    for name in ['lint', 'tests', 'types', 'frontend-build', 'artifact-browser', 'quota-race', 'backend-build']:
        stage = stages.get(name)
        if not stage or stage['status'] not in ('passed', 'reused'):
            raise DeliveryError('Missing successful evidence: ' + name)
        if stage['inputs'] != tree_hash(root, list(stage['inputs']), inputs=True):
            raise DeliveryError('Stale input evidence: ' + name)
        if stage['artifacts'] != tree_hash(root, list(stage['artifacts'])) or digest(contained(evidence, stage['log'])) != stage['log_sha256']:
            raise DeliveryError('Corrupt artifact or log evidence: ' + name)
    quota = stages['quota-race'].get('quota_tests', {})
    if not quota.get('passed') or quota.get('skipped') != 0 or quota.get('failed') != 0:
        raise DeliveryError('Real quota tests with zero skips are required')
    if stages['backend-build'].get('version') != version:
        raise DeliveryError('Package version differs from the verified program build')
    built_source = stages['backend-build'].get('source_commit', '')
    if not re.fullmatch(r'[0-9a-f]{40}', built_source):
        raise DeliveryError('Build must record its committed source')
    try:
        git('merge-base', '--is-ancestor', built_source, source)
    except subprocess.CalledProcessError:
        raise DeliveryError('Build source is not an ancestor of the selected release') from None
    if git('diff', '--name-only', built_source, source, '--', 'frontend', 'backend'):
        raise DeliveryError('Program build source differs from current application source')
    browser = stages['artifact-browser']
    results = [contained(root, name) for name in browser['artifacts'] if name.endswith('/artifact-browser/result.json')]
    if len(results) != 1:
        raise DeliveryError('Production browser result is required')
    review = json.loads(results[0].read_text())
    if not review.get('ok') or not {'chromium', 'webkit'}.issubset(review.get('engines', {})):
        raise DeliveryError('Both browser engines must pass on the production artifact')
    binary_relative = binary.relative_to(root).as_posix()
    if stages['backend-build']['artifacts'].get(binary_relative) != digest(binary):
        raise DeliveryError('Program does not match verified backend build')
    dist = root / 'backend/internal/web/dist'
    if stages['frontend-build']['artifacts'].get('backend/internal/web/dist') != tree_hash(root, ['backend/internal/web/dist'])['backend/internal/web/dist']:
        raise DeliveryError('Frontend artifacts do not match verified build')
    delta = None
    if not full_upload:
        if baseline_binary is None or digest(baseline_binary) != baseline_sha256:
            raise DeliveryError('Verified old binary is required for delta packaging; full upload must be explicit')
        try:
            import bsdiff4
        except ImportError:
            raise DeliveryError('Install tools/requirements-delivery.txt for delta packaging') from None
        old = Path(baseline_binary).read_bytes(); new = binary.read_bytes()
        delta = bsdiff4.diff(old, new)
        if bsdiff4.patch(old, delta) != new: raise DeliveryError('Local delta reconstruction failed')
    destination.mkdir(parents=True)
    if delta is None:
        with (destination / 'sub2api.gz').open('wb') as out:
            with gzip.GzipFile(filename='', mode='wb', fileobj=out, mtime=0, compresslevel=6) as zipped:
                with binary.open('rb') as src: shutil.copyfileobj(src, zipped)
        payload = 'sub2api.gz'
    else:
        (destination / 'app.bsdiff').write_bytes(delta); payload = 'app.bsdiff'
    files = {p.relative_to(dist).as_posix(): digest(p) for p in sorted(dist.rglob('*')) if p.is_file()}
    manifest = {'schema': 1, 'version': version, 'source_commit': built_source,
                'baseline_commit': baseline_commit, 'baseline_sha256': baseline_sha256, 'sha256': digest(binary),
                'bytes': binary.stat().st_size, 'payload': payload, 'payload_sha256': digest(destination / payload),
                'payload_bytes': (destination / payload).stat().st_size, 'backend_unchanged': True,
                'quota_tests': quota, 'created_at': utcnow(), 'frontend_files': files,
                'evidence_sha256': digest(evidence / 'manifest.json')}
    kit = Path(__file__).resolve().parent
    for name in ['release_server.py', 'delivery_core.py', 'quota_snapshot.py', 'apply_delta.py']:
        shutil.copyfile(kit / name, destination / name)
    write_json(destination / 'manifest.json', manifest)
    return manifest
