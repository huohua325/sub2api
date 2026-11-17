"""Promote identical, already-tested frontend bytes to a clean checkout.

This imports four successful frontend records, never executes checks, builds an
artifact, or imports quota/browser/Go evidence. Original cwd, command, tool and
environment identity remain unchanged and the cross-checkout origin is explicit.
"""
import argparse
import copy
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

from delivery_core import DeliveryError, contained, digest, exclusive, json_hash, tree_hash, utcnow, write_json
from delivery_workflow import verify_record

STEPS = ('lint', 'tests', 'types', 'frontend-build')
INPUTS = ('frontend', 'tools/delivery.py', 'tools/delivery_core.py', 'tools/delivery_workflow.py',
          'tools/review_guide.py', 'tools/review_guide.cjs', 'tools/browser_fixtures.cjs',
          'AGENTS.md', 'frontend/node_modules/.modules.yaml')
ARTIFACT = 'backend/internal/web/dist'
HEX = re.compile(r'[0-9a-f]{64}')


def _git(root, *args):
    try:
        return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE, timeout=30).strip()
    except subprocess.CalledProcessError:
        raise DeliveryError('Cannot verify committed checkout inputs') from None


def _committed(root):
    if Path(_git(root, 'rev-parse', '--show-toplevel')).resolve() != root:
        raise DeliveryError('Select the actual Git checkout root')
    paths = [name for name in INPUTS if name != 'frontend/node_modules/.modules.yaml']
    status = _git(root, 'status', '--porcelain', '--untracked-files=all', '--', *paths)
    if status:
        raise DeliveryError('Frontend and relevant delivery inputs must be committed, including untracked frontend files')
    for path in paths:
        _git(root, 'ls-files', '--error-unmatch', '--', path)
    return _git(root, 'rev-parse', 'HEAD'), _git(root, 'rev-parse', 'HEAD:frontend')


def _runtime_tools(root, pnpm):
    def version(command):
        try:
            return subprocess.check_output(command, cwd=root, text=True, stderr=subprocess.PIPE, timeout=30).strip()
        except (OSError, subprocess.CalledProcessError):
            raise DeliveryError('Cannot verify the existing frontend toolchain') from None
    return {'node': version(['node', '--version']), 'pnpm': version([pnpm, '--version']),
            'python': sys.version, 'platform': sys.platform}


def _check_record_metadata(source_root, record):
    if (not HEX.fullmatch(record.get('key', '')) or not HEX.fullmatch(record.get('environment_hash', ''))
            or record.get('cwd') != str(source_root / 'frontend') or not isinstance(record.get('tools'), dict)
            or not isinstance(record.get('command'), list) or not record['command']
            or not all(isinstance(arg, str) for arg in record['command'])):
        raise DeliveryError('Incomplete original command identity: ' + record['name'])
    if record.get('reuse_kind') == 'immutable-frontend-promotion':
        raise DeliveryError('Promote directly from original frontend evidence, not another promotion')
    commands = {'tests': ['run', 'test:run'], 'types': ['exec', 'vue-tsc', '-b'],
                'frontend-build': ['exec', 'vite', 'build', '--config', 'vite.config.ts']}
    arguments = record['command'][1:]
    if record['name'] == 'lint':
        valid = arguments == ['run', 'lint:check'] or (
            len(arguments) == 7 and arguments[:6] == ['run', 'lint:check', '--cache', '--cache-strategy', 'content', '--cache-location'])
    else:
        valid = arguments == commands[record['name']]
    if not valid:
        raise DeliveryError('Original step does not represent the full frontend check: ' + record['name'])
    executable = shutil.which(record['command'][0])
    if not executable:
        raise DeliveryError('Original frontend executable is no longer available')
    identity = dict(inputs=record['inputs'], argv=record['command'], cwd=record['cwd'], tools=record['tools'],
                    executable=digest(executable), environment_hash=record['environment_hash'])
    if json_hash(identity) != record['key']:
        raise DeliveryError('Original command/tool identity changed: ' + record['name'])
    log = Path(record['log'])
    if log.suffix != '.log' or not log.name.startswith(record['name'] + '-'):
        raise DeliveryError('Only the original step logs may be promoted')


def adopt_frontend_evidence(root, source_root, source_output, output):
    root, source_root = Path(root).resolve(), Path(source_root).resolve()
    source_output, output = Path(source_output).resolve(), Path(output).resolve()
    if root == source_root:
        raise DeliveryError('Use this tool only for a different clean checkout')
    if (not output.is_relative_to(root / 'output') or output == root / 'output'
            or not source_output.is_relative_to(source_root / 'output') or source_output == source_root / 'output'):
        raise DeliveryError('Evidence must stay in each checkout output directory')
    if (output / 'manifest.json').exists():
        raise DeliveryError('Destination evidence already exists; it will not be overwritten')
    source_manifest = source_output / 'manifest.json'
    source_manifest_hash = digest(source_manifest)
    original = json.loads(source_manifest.read_text(encoding='utf-8'))
    if original.get('schema') != 1 or original.get('root') != str(source_root) or not isinstance(original.get('steps'), list):
        raise DeliveryError('Source manifest belongs to a different checkout or format')
    validated = original.get('validated_inputs')
    if not isinstance(validated, dict) or set(validated) != set(INPUTS):
        raise DeliveryError('Source must contain the complete supported frontend validation scope')
    if not (source_root / 'frontend/pnpm-lock.yaml').is_file() or not (root / 'frontend/pnpm-lock.yaml').is_file():
        raise DeliveryError('Both checkouts need their committed frontend lockfile')
    for selected in (source_root, root):
        if not (selected / 'frontend/node_modules/.modules.yaml').is_file():
            raise DeliveryError('Both checkouts need their existing dependency metadata; install is not inferred')
    source_commit, source_tree = _committed(source_root)
    destination_commit, destination_tree = _committed(root)
    app_commit = original.get('source_commit', '')
    if not re.fullmatch(r'[0-9a-f]{40}', app_commit) or _git(source_root, 'rev-parse', app_commit + ':frontend') != source_tree:
        raise DeliveryError('Source frontend differs from its validated application commit')
    if source_tree != destination_tree:
        raise DeliveryError('Source and destination committed frontend trees differ')
    expected_artifacts = {ARTIFACT: tree_hash(source_root, [ARTIFACT])[ARTIFACT]}
    if original.get('frontend_artifacts') != expected_artifacts or tree_hash(root, [ARTIFACT]) != expected_artifacts:
        raise DeliveryError('Destination must already contain the identical immutable frontend artifact')
    if tree_hash(source_root, INPUTS, inputs=True) != validated or tree_hash(root, INPUTS, inputs=True) != validated:
        raise DeliveryError('Source or destination frontend, dependencies or delivery tools changed')
    latest = {record.get('name'): record for record in original['steps']}
    selected = []
    for name in STEPS:
        record = latest.get(name)
        if not isinstance(record, dict) or record.get('inputs') != validated:
            raise DeliveryError('Missing current full frontend evidence: ' + name)
        if record.get('artifacts') != (expected_artifacts if name == 'frontend-build' else {}):
            raise DeliveryError('Unexpected artifacts for frontend step: ' + name)
        verify_record(source_root, source_output, record)
        _check_record_metadata(source_root, record)
        selected.append(record)
    tools = _runtime_tools(root, selected[0]['command'][0])
    if any(record['tools'] != tools for record in selected):
        raise DeliveryError('Existing frontend toolchain differs from original validation')
    stamp = utcnow()
    origin = {'root': str(source_root), 'output': str(source_output), 'manifest_sha256': source_manifest_hash,
              'source_commit': source_commit, 'source_app_commit': app_commit, 'frontend_git_tree': source_tree}
    with exclusive(output):
        if (output / 'manifest.json').exists():
            raise DeliveryError('Destination evidence appeared while checking inputs')
        records = []
        for original_record in selected:
            record = copy.deepcopy(original_record)
            source_log = contained(source_output, record['log'])
            relative_log = f"adopted-frontend/{record['name']}-{record['log_sha256']}.log"
            destination_log = contained(output, relative_log)
            destination_log.parent.mkdir(parents=True, exist_ok=True)
            if destination_log.exists():
                raise DeliveryError('Promoted log already exists; use a fresh output directory')
            shutil.copyfile(source_log, destination_log)
            if digest(destination_log) != record['log_sha256']:
                raise DeliveryError('Log changed while copying: ' + record['name'])
            record.update(status='reused', log=relative_log, elapsed_seconds=0,
                          reuse_kind='immutable-frontend-promotion', adopted_at=stamp,
                          origin={**origin, 'key': record['key'], 'log': original_record['log'],
                                  'record_sha256': json_hash(original_record), 'status': original_record['status'],
                                  'elapsed_seconds': original_record.get('elapsed_seconds')})
            verify_record(root, output, record)
            records.append(record)
        if digest(source_manifest) != source_manifest_hash:
            raise DeliveryError('Source manifest changed during adoption')
        for record in selected:
            verify_record(source_root, source_output, record)
        if (_committed(source_root) != (source_commit, source_tree)
                or _committed(root) != (destination_commit, destination_tree)
                or tree_hash(root, INPUTS, inputs=True) != validated
                or tree_hash(root, [ARTIFACT]) != expected_artifacts):
            raise DeliveryError('Checkout inputs or artifacts changed during adoption')
        manifest = {'schema': 1, 'root': str(root), 'created_at': stamp, 'updated_at': stamp,
                    'source_commit': destination_commit, 'steps': records, 'validated_inputs': validated,
                    'frontend_artifacts': expected_artifacts, 'frontend_evidence_origin': origin,
                    'promotion': {'kind': 'immutable-frontend-bytes', 'destination_checks_executed': False,
                                  'destination_dependency_installation_claimed': False}}
        write_json(output / 'manifest.json', manifest)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--source-root', type=Path, required=True)
    parser.add_argument('--source-output', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    adopt_frontend_evidence(args.root, args.source_root, args.source_output, args.output)
    print('Promoted identical frontend bytes and four original check records; quota, browsers and backend build must run in the destination checkout.')


if __name__ == '__main__': main()
