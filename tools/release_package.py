"""Package only successful, current delivery evidence; never rebuild while packaging."""
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

from delivery_core import DeliveryError, contained, digest, exclusive, tree_hash, utcnow, write_json


def verified_delta(root, baseline_binary, binary):
    try:
        import bsdiff4
    except ImportError:
        raise DeliveryError('Install tools/requirements-delivery.txt for delta packaging') from None
    old, new = Path(baseline_binary).read_bytes(), Path(binary).read_bytes()
    before, after = hashlib.sha256(old).hexdigest(), hashlib.sha256(new).hexdigest()
    cache = Path(root) / 'output/delivery-cache/deltas' / (before + '-' + after)
    with exclusive(cache):
        payload = cache / 'app.bsdiff'
        metadata = cache / 'manifest.json'
        if payload.exists() and metadata.exists():
            try:
                info = json.loads(metadata.read_text())
                delta = payload.read_bytes()
                if (info == {'schema': 1, 'baseline_sha256': before, 'sha256': after,
                             'payload_sha256': hashlib.sha256(delta).hexdigest()}
                        and bsdiff4.patch(old, delta) == new):
                    return delta
            except (OSError, ValueError):
                pass  # A corrupt cache is recomputed, never treated as proof.
        delta = bsdiff4.diff(old, new)
        if bsdiff4.patch(old, delta) != new:
            raise DeliveryError('Local delta reconstruction failed')
        temporary = cache / 'app.bsdiff.tmp'
        temporary.write_bytes(delta)
        os.replace(temporary, payload)
        write_json(metadata, {'schema': 1, 'baseline_sha256': before, 'sha256': after,
                              'payload_sha256': hashlib.sha256(delta).hexdigest()})
        return delta


def require_committed_inputs(root):
    status = subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all', '--',
                                      'frontend', 'backend', 'tools', 'AGENTS.md'], cwd=root, text=True, timeout=30)
    if status.strip():
        raise DeliveryError('Release inputs must be committed, including untracked files. Use a clean release checkout; preserve user-owned files.')


def package_release(root, evidence, destination, binary, version, baseline_sha256, baseline_commit, *, full_upload=False, baseline_binary=None, upgrade_contract=None):
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
    backend_changes = git('diff', '--name-only', baseline_commit, source, '--', 'backend').splitlines()
    if upgrade_contract is None and any(not path.endswith('_test.go') for path in backend_changes):
        raise DeliveryError('Backend changes require the full upgrade runbook; this lane is frontend-only')
    if upgrade_contract is not None:
        if upgrade_contract.get('name') == 'unified-coding-preview-v1':
            from coding_upgrade import UnifiedCodingUpgrade
            UnifiedCodingUpgrade(upgrade_contract).validate_sources(root, baseline_commit, source)
        else:
            from full_upgrade import PersonalQuotaUpgrade
            PersonalQuotaUpgrade(upgrade_contract)
            migration_changes = git('diff', '--name-only', baseline_commit, source, '--', 'backend/migrations').splitlines()
            if migration_changes != ['backend/migrations/260_personal_quota_resets.sql']:
                raise DeliveryError('Full personal-reset upgrade requires exactly the reviewed additive migration')
            if upgrade_contract['migrations']['260_personal_quota_resets.sql'] != digest(root / migration_changes[0]):
                raise DeliveryError('Reviewed migration checksum changed')
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
    if upgrade_contract is not None:
        coding_preview = upgrade_contract.get('name') == 'unified-coding-preview-v1'
        result = evidence / ('artifact-feature-browser/result.json' if coding_preview else 'feature-browser/result.json')
        feature = json.loads(result.read_text())
        if not feature.get('ok') or not {'chromium', 'webkit'}.issubset(feature.get('engines', {})):
            raise DeliveryError('Full upgrade requires the actual reset flow in both browser engines')
        if feature.get('frontend_sha256') != tree_hash(root, ['backend/internal/web/dist'])['backend/internal/web/dist']:
            raise DeliveryError('Feature browser evidence is for another frontend artifact')
        if feature.get('script_sha256') != digest(root / ('tools/review_unified_coding.cjs' if coding_preview else 'tools/review_personal_quota.cjs')):
            raise DeliveryError('Feature browser scenario changed after verification')
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
        delta = verified_delta(root, baseline_binary, binary)
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
                'payload_bytes': (destination / payload).stat().st_size, 'backend_unchanged': upgrade_contract is None,
                'quota_tests': quota, 'created_at': utcnow(), 'frontend_files': files,
                'evidence_sha256': digest(evidence / 'manifest.json')}
    kit = Path(__file__).resolve().parent
    for name in ['release_server.py', 'delivery_core.py', 'quota_snapshot.py', 'apply_delta.py']:
        shutil.copyfile(kit / name, destination / name)
    if upgrade_contract is not None:
        manifest['upgrade_contract'] = upgrade_contract
        shutil.copyfile(kit / 'full_upgrade.py', destination / 'full_upgrade.py')
        if upgrade_contract.get('name') == 'unified-coding-preview-v1':
            shutil.copyfile(kit / 'coding_upgrade.py', destination / 'coding_upgrade.py')
            shutil.copyfile(kit / 'coding_preview_live.py', destination / 'coding_preview_live.py')
    write_json(destination / 'manifest.json', manifest)
    return manifest
