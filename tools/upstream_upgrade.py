"""Deploy a reviewed, clean upstream merge with unchanged database migrations."""
import argparse
import json
from pathlib import Path
import re
import subprocess

from delivery_core import DeliveryError
from release_server import DockerRuntime, assert_preserved, deploy, stage_release

BASELINE = 'f3b217911923de35658969994b1d0f44ad070c38'
UPSTREAM = '530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484'


def contract_for_release(source):
    if not re.fullmatch('[0-9a-f]{40}', source):
        raise DeliveryError('Fixed reviewed merge SHA required')
    return dict(name='upstream-merge-no-migrations-v1', baseline_commit=BASELINE,
                upstream_commit=UPSTREAM, source_commit=source, migrations={})


class UpstreamUpgrade:
    def __init__(self, contract):
        if contract != contract_for_release(contract.get('source_commit', '')):
            raise DeliveryError('Only the pinned migration-free upstream merge is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=60).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Upstream source verification failed') from None
        reviewed = self.contract['source_commit']
        parents = git('rev-list', '--parents', '-n', '1', reviewed).split()
        if baseline != BASELINE or parents != [reviewed, BASELINE, UPSTREAM]:
            raise DeliveryError('Expected the reviewed true merge with both fixed parents')
        git('merge-base', '--is-ancestor', reviewed, source)
        if git('diff', '--name-only', reviewed, source, '--', 'frontend', 'backend'):
            raise DeliveryError('Application changed after the reviewed merge')
        if git('diff', '--name-only', BASELINE, source, '--', 'backend/migrations'):
            raise DeliveryError('Upstream lane cannot change existing or add new migrations')
        # Fail closed on conflicts or manual application edits. Tool/docs additions are allowed.
        expected = git('merge-tree', '--write-tree', BASELINE, UPSTREAM).splitlines()[0]
        if git('diff', '--name-only', expected, source, '--', 'frontend', 'backend'):
            raise DeliveryError('Application differs from the clean reviewed upstream merge')

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False
                or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE
                or manifest.get('source_commit') != self.contract['source_commit']):
            raise DeliveryError('Explicit reviewed upstream manifest required')

    def assert_preserved(self, before, after):
        assert_preserved(before, after)

    def can_rollback(self, runtime):
        # Both programs share schema and custom business semantics; database is never restored.
        return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    package = commands.add_parser('package')
    for name in ['output', 'destination', 'baseline-binary']:
        package.add_argument('--' + name, type=Path, required=True)
    package.add_argument('--version', required=True)
    package.add_argument('--baseline-sha256', required=True)
    package.add_argument('--source-commit', required=True)
    server = commands.add_parser('deploy')
    server.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    server.add_argument('--release', type=Path, required=True)
    server.add_argument('--stage', action='store_true')
    server.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if args.command == 'package':
        from release_package import package_release
        root = Path(__file__).resolve().parents[1]
        result = package_release(root, args.output, args.destination,
                                 args.output / 'sub2api-linux-amd64', args.version,
                                 args.baseline_sha256, BASELINE, baseline_binary=args.baseline_binary,
                                 upgrade_contract=contract_for_release(args.source_commit))
    else:
        manifest = json.loads((args.release / 'manifest.json').read_text())
        policy = UpstreamUpgrade(manifest.get('upgrade_contract', {}))
        policy.validate(manifest)
        if args.stage:
            stage_release(args.root, args.release)
            return
        result = deploy(args.root, args.release, DockerRuntime(args.root, args.release),
                        apply=args.apply, upgrade=policy)
    print(json.dumps(result, indent=2))


if __name__ == '__main__': main()
