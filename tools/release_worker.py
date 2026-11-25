"""Controlled server-side release worker with strict dev/release privilege separation.

The development account (`t3workspace`) may only drop immutable submission requests
referencing a verified GitHub Actions candidate run and binary SHA-256.
The privileged release worker fetches, verifies, stages, and runs read-only
preflight; production apply requires explicit root administrator approval and
delegates to the existing quota-preserving `release_server.deploy` guardrails.
"""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import urllib.error
import urllib.request
import zipfile

from delivery_core import DeliveryError, digest, utcnow, write_json

HEX40_RE = re.compile(r'^[0-9a-f]{40}$')
HEX64_RE = re.compile(r'^[0-9a-f]{64}$')
VERSION_RE = re.compile(r'^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$')
REPO_RE = re.compile(r'^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$')
RELEASE_ID_RE = re.compile(r'^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$')


def validate_submission(payload):
    if not isinstance(payload, dict):
        raise DeliveryError('Submission must be a JSON object')
    version = payload.get('version', '')
    commit = payload.get('source_commit', '')
    sha256 = payload.get('expected_binary_sha256', '')
    run_id = payload.get('workflow_run_id')
    if not isinstance(version, str) or not VERSION_RE.fullmatch(version):
        raise DeliveryError('Invalid release version')
    if not isinstance(commit, str) or not HEX40_RE.fullmatch(commit):
        raise DeliveryError('Invalid 40-char source_commit')
    if not isinstance(sha256, str) or not HEX64_RE.fullmatch(sha256):
        raise DeliveryError('Invalid 64-char expected_binary_sha256')
    if not isinstance(run_id, int) or isinstance(run_id, bool) or run_id <= 0:
        raise DeliveryError('Positive integer workflow_run_id required')
    return {
        'schema': 1,
        'version': version,
        'source_commit': commit,
        'expected_binary_sha256': sha256,
        'workflow_run_id': run_id,
        'submitted_by': str(payload.get('submitted_by', 't3workspace'))[:64],
        'submitted_at': str(payload.get('submitted_at') or utcnow()),
    }


def submit_candidate(spool_dir, version, source_commit, expected_sha256, run_id, submitted_by='t3workspace'):
    record = validate_submission({
        'version': version,
        'source_commit': source_commit,
        'expected_binary_sha256': expected_sha256,
        'workflow_run_id': int(run_id),
        'submitted_by': submitted_by,
        'submitted_at': utcnow(),
    })
    submissions = Path(spool_dir) / 'submissions'
    submissions.mkdir(parents=True, exist_ok=True)
    release_id = f'{record["version"]}-{record["source_commit"][:8]}'
    if not RELEASE_ID_RE.fullmatch(release_id):
        raise DeliveryError('Derived release identifier is invalid')
    target = submissions / f'{release_id}.json'
    if target.exists():
        raise DeliveryError(f'Submission already exists: {target.name}')
    write_json(target, record)
    return {'release_id': release_id, 'submission_file': str(target), 'submission': record}


def safe_extract_zip(zip_bytes, destination):
    destination = Path(destination).resolve()
    destination.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(io.BytesIO(zip_bytes)) as archive:
        for info in archive.infolist():
            if info.is_dir():
                continue
            name = Path(info.filename)
            if name.is_absolute() or '..' in name.parts:
                raise DeliveryError(f'Unsafe archive member path: {info.filename}')
            if stat.S_ISLNK(info.external_attr >> 16):
                raise DeliveryError(f'Symlink archive member forbidden: {info.filename}')
            resolved = (destination / name).resolve()
            if not resolved.is_relative_to(destination):
                raise DeliveryError(f'Archive member escapes destination: {info.filename}')
            resolved.parent.mkdir(parents=True, exist_ok=True)
            with archive.open(info, 'r') as src, resolved.open('wb') as dst:
                shutil.copyfileobj(src, dst)


def verify_and_unpack_candidate(kit_dir, submission):
    kit_dir = Path(kit_dir).resolve()
    manifest_path = kit_dir / 'manifest.json'
    if not manifest_path.is_file():
        raise DeliveryError('Candidate kit missing manifest.json')
    manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
    if manifest.get('kind') != 'mile-ci-release-candidate' or manifest.get('schema') != 1:
        raise DeliveryError('Unexpected candidate manifest schema')
    if manifest.get('version') != submission['version']:
        raise DeliveryError('Candidate version mismatch')
    if manifest.get('source_commit') != submission['source_commit']:
        raise DeliveryError('Candidate source_commit mismatch')
    if int(manifest.get('workflow_run_id', -1)) != submission['workflow_run_id']:
        raise DeliveryError('Candidate workflow_run_id mismatch')
    if manifest.get('sha256') != submission['expected_binary_sha256']:
        raise DeliveryError('Candidate manifest binary SHA-256 does not match submission')
    quota = manifest.get('quota_tests') or {}
    if not quota.get('passed') or quota.get('skipped') or quota.get('failed'):
        raise DeliveryError('Candidate failed zero-skip isolated PostgreSQL quota race requirement')
    payload_name = manifest.get('payload')
    if payload_name != 'sub2api.gz':
        raise DeliveryError('Only verified gzip immutable candidates are accepted from CI')
    payload_path = kit_dir / payload_name
    if not payload_path.is_file() or digest(payload_path) != manifest.get('payload_sha256'):
        raise DeliveryError('Candidate payload checksum mismatch')

    binary_path = kit_dir / 'sub2api'
    tmp_path = kit_dir / '.sub2api.unpack'
    hasher = hashlib.sha256()
    size = 0
    try:
        with gzip.open(payload_path, 'rb') as src, tmp_path.open('wb') as dst:
            while chunk := src.read(1024 * 1024):
                size += len(chunk)
                if size > int(manifest['bytes']):
                    raise DeliveryError('Unpacked binary exceeds declared manifest size')
                hasher.update(chunk)
                dst.write(chunk)
        if size != int(manifest['bytes']) or hasher.hexdigest() != submission['expected_binary_sha256']:
            raise DeliveryError('Unpacked binary SHA-256 mismatch')
        os.chmod(tmp_path, 0o755)
        os.replace(tmp_path, binary_path)
    finally:
        tmp_path.unlink(missing_ok=True)
    return manifest, binary_path


def download_ci_artifact(repo, run_id, commit, token, proxy=None):
    if not REPO_RE.fullmatch(repo):
        raise DeliveryError('Invalid GitHub repository coordinate')
    if not token:
        raise DeliveryError('GITHUB_RELEASE_READ_TOKEN is required to fetch CI artifacts')
    handlers = [urllib.request.ProxyHandler({'https': proxy})] if proxy else []
    opener = urllib.request.build_opener(*handlers)
    headers = {
        'Authorization': 'Bearer ' + token,
        'Accept': 'application/vnd.github+json',
        'User-Agent': 'mile-release-worker',
    }
    list_url = f'https://api.github.com/repos/{repo}/actions/runs/{run_id}/artifacts'
    req = urllib.request.Request(list_url, headers=headers)
    with opener.open(req, timeout=30) as resp:
        data = json.load(resp)
    expected_name = f'mile-release-candidate-{commit}'
    artifact = next((a for a in data.get('artifacts', []) if a.get('name') == expected_name and not a.get('expired')), None)
    if artifact is None:
        raise DeliveryError(f'Artifact {expected_name} not found in run {run_id}')
    dl_req = urllib.request.Request(artifact['archive_download_url'], headers=headers)
    with opener.open(dl_req, timeout=180) as resp:
        return resp.read()


def stage_from_submission(submission_file, deploy_root, spool_dir, repo, token, proxy=None, local_zip=None, run_preflight=True):
    submission = validate_submission(json.loads(Path(submission_file).read_text(encoding='utf-8')))
    release_id = f'{submission["version"]}-{submission["source_commit"][:8]}'
    release_dir = Path(deploy_root) / 'releases' / release_id
    if release_dir.exists():
        raise DeliveryError(f'Release directory already exists: {release_dir}')

    if local_zip is not None:
        zip_bytes = Path(local_zip).read_bytes()
    else:
        zip_bytes = download_ci_artifact(repo, submission['workflow_run_id'], submission['source_commit'], token, proxy=proxy)

    tmp_dir = Path(deploy_root) / 'releases' / f'.staging-{release_id}'
    if tmp_dir.exists():
        shutil.rmtree(tmp_dir)
    try:
        safe_extract_zip(zip_bytes, tmp_dir)
        manifest, binary_path = verify_and_unpack_candidate(tmp_dir, submission)
        os.replace(tmp_dir, release_dir)
    except BaseException:
        if tmp_dir.exists():
            shutil.rmtree(tmp_dir, ignore_errors=True)
        raise

    preflight_result = None
    if run_preflight:
        from preflight_server import check
        preflight_result = check(root=str(deploy_root))

    staged_dir = Path(spool_dir) / 'staged'
    staged_dir.mkdir(parents=True, exist_ok=True)
    record = {
        'schema': 1,
        'status': 'staged-awaiting-approval',
        'release_id': release_id,
        'release_dir': str(release_dir),
        'binary_sha256': manifest['sha256'],
        'version': manifest['version'],
        'source_commit': manifest['source_commit'],
        'workflow_run_id': manifest['workflow_run_id'],
        'quota_tests': manifest['quota_tests'],
        'migrations_count': manifest['migrations_count'],
        'migrations_sha256': manifest['migrations_sha256'],
        'preflight': preflight_result,
        'staged_at': utcnow(),
    }
    write_json(staged_dir / f'{release_id}.json', record)
    return record


def apply_approved_release(release_id, deploy_root, spool_dir, approve=False):
    if not approve:
        raise DeliveryError('Refusing production apply without explicit --approve confirmation')
    if not RELEASE_ID_RE.fullmatch(release_id):
        raise DeliveryError('Invalid release_id')
    staged_file = Path(spool_dir) / 'staged' / f'{release_id}.json'
    if not staged_file.is_file():
        raise DeliveryError(f'Staged candidate record not found: {staged_file}')
    record = json.loads(staged_file.read_text(encoding='utf-8'))
    release_dir = Path(record['release_dir'])
    manifest_path = release_dir / 'manifest.json'
    binary_path = release_dir / 'sub2api'
    if not manifest_path.is_file() or not binary_path.is_file():
        raise DeliveryError('Staged release directory is incomplete')
    if digest(binary_path) != record['binary_sha256']:
        raise DeliveryError('Staged binary SHA-256 changed after staging')

    # Verify live baseline matches before delegating to quota-preserving deploy.
    current_bin = Path(deploy_root) / 'bin' / 'sub2api'
    live_sha = digest(current_bin)
    manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
    manifest['baseline_sha256'] = live_sha
    write_json(manifest_path, manifest)

    from release_server import deploy
    from upstream_v0225_upgrade import UpstreamV0225Runtime
    result = deploy(Path(deploy_root), release_dir, UpstreamV0225Runtime(Path(deploy_root), release_dir), apply=True)
    record['status'] = 'deployed'
    record['deployed_at'] = utcnow()
    record['deploy_result'] = result
    write_json(staged_file, record)
    return record


def worker_status(deploy_root, spool_dir):
    spool = Path(spool_dir)
    submissions = sorted(p.name for p in (spool / 'submissions').glob('*.json')) if (spool / 'submissions').is_dir() else []
    staged = sorted(p.name for p in (spool / 'staged').glob('*.json')) if (spool / 'staged').is_dir() else []
    current_bin = Path(deploy_root) / 'bin' / 'sub2api'
    return {
        'checked_at': utcnow(),
        'deploy_root': str(deploy_root),
        'live_binary_sha256': digest(current_bin) if current_bin.is_file() else None,
        'submissions': submissions,
        'staged': staged,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)

    p_submit = sub.add_parser('submit', help='drop an immutable candidate submission into the spool')
    p_submit.add_argument('--spool', type=Path, default=Path('/var/spool/mile-release'))
    p_submit.add_argument('--version', required=True)
    p_submit.add_argument('--source-commit', required=True)
    p_submit.add_argument('--expected-sha256', required=True)
    p_submit.add_argument('--run-id', type=int, required=True)
    p_submit.add_argument('--submitted-by', default=os.environ.get('USER', 't3workspace'))

    p_stage = sub.add_parser('stage-ci', help='fetch, verify, unpack, and preflight a CI release candidate')
    p_stage.add_argument('--submission', type=Path, required=True)
    p_stage.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    p_stage.add_argument('--spool', type=Path, default=Path('/var/spool/mile-release'))
    p_stage.add_argument('--repo', default=os.environ.get('MILE_RELEASE_REPO', 'huohua325/sub2api'))
    p_stage.add_argument('--proxy', default=os.environ.get('HTTPS_PROXY'))
    p_stage.add_argument('--local-zip', type=Path)
    p_stage.add_argument('--skip-preflight', action='store_true')

    p_apply = sub.add_parser('apply', help='apply an explicitly approved staged release candidate')
    p_apply.add_argument('--release-id', required=True)
    p_apply.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    p_apply.add_argument('--spool', type=Path, default=Path('/var/spool/mile-release'))
    p_apply.add_argument('--approve', action='store_true')

    p_status = sub.add_parser('status', help='report spool submissions, staged candidates, and live binary hash')
    p_status.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    p_status.add_argument('--spool', type=Path, default=Path('/var/spool/mile-release'))

    args = parser.parse_args()
    try:
        if args.command == 'submit':
            out = submit_candidate(args.spool, args.version, args.source_commit, args.expected_sha256,
                                   args.run_id, submitted_by=args.submitted_by)
        elif args.command == 'stage-ci':
            token = os.environ.get('GITHUB_RELEASE_READ_TOKEN', '').strip()
            out = stage_from_submission(args.submission, args.root, args.spool, args.repo, token,
                                        proxy=args.proxy, local_zip=args.local_zip,
                                        run_preflight=not args.skip_preflight)
        elif args.command == 'apply':
            out = apply_approved_release(args.release_id, args.root, args.spool, approve=args.approve)
        else:
            out = worker_status(args.root, args.spool)
        print(json.dumps(out, indent=2))
    except (DeliveryError, OSError, urllib.error.URLError, subprocess.SubprocessError, ValueError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

