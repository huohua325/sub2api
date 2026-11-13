"""Pinned, migration-free deployment of the reviewed unified coding preview."""
import argparse
import json
from pathlib import Path
import subprocess

from delivery_core import DeliveryError
from release_server import DockerRuntime, assert_preserved, deploy, stage_release

BASELINE = 'd0322099cb9d47f80c21fa24d1000f12b82c6739'
APPLICATION = '17377e59a0af8d4ba0577f68be5547cf61110323'
BACKEND_FILES = [
    'backend/internal/handler/gateway_handler.go',
    'backend/internal/server/middleware/api_key_auth.go',
    'backend/internal/server/middleware/api_key_auth_google.go',
    'backend/internal/server/middleware/unified_coding.go',
    'backend/internal/server/routes/gateway.go',
    'backend/internal/service/admin_group.go',
    'backend/internal/service/admin_group_duplicate.go',
    'backend/internal/service/api_key_service.go',
    'backend/internal/service/coding_quota_gateway.go',
    'backend/internal/service/coding_quota_runtime.go',
    'backend/internal/service/composite_route_resolver.go',
    'backend/internal/service/gateway_service.go',
    'backend/internal/service/gateway_usage_billing.go',
    'backend/internal/service/purpose_allowance.go',
    'backend/internal/service/user.go',
]


def contract_for_release():
    return {'name': 'unified-coding-preview-v1', 'baseline_commit': BASELINE,
            'source_commit': APPLICATION, 'backend_files': list(BACKEND_FILES), 'migrations': {}}


class UnifiedCodingUpgrade:
    rollback_block_reason = 'Unified coding groups exist; keep the database and fix forward instead of installing a program that cannot interpret them.'

    def __init__(self, contract):
        if contract != contract_for_release():
            raise DeliveryError('Only the pinned, migration-free unified coding preview is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            return subprocess.check_output(['git', *args], cwd=root, text=True, timeout=30).strip()
        if baseline != BASELINE or git('diff', '--name-only', APPLICATION, source, '--', 'frontend', 'backend'):
            raise DeliveryError('Application differs from the reviewed coding preview')
        if git('diff', '--name-only', baseline, source, '--', 'backend/migrations'):
            raise DeliveryError('Coding preview cannot change migrations')
        changed = git('diff', '--name-only', baseline, source, '--', 'backend').splitlines()
        if sorted(p for p in changed if not p.endswith('_test.go')) != BACKEND_FILES:
            raise DeliveryError('Unexpected backend changes in coding preview')

    def validate(self, manifest):
        if manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract or manifest.get('baseline_commit') != BASELINE:
            raise DeliveryError('Explicit pinned coding preview manifest required')

    def assert_preserved(self, before, after):
        assert_preserved(before, after)

    def can_rollback(self, runtime):
        check = getattr(runtime, 'coding_preview_active', None)
        return callable(check) and not check()


class CodingRuntime(DockerRuntime):
    def coding_preview_active(self):
        query = "BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; SELECT count(*) FROM groups WHERE platform='composite' AND usage_kind='coding' AND deleted_at IS NULL; COMMIT;"
        result = subprocess.check_output(['docker', 'exec', '-i', self.database, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                                         '-U', self.db_env['POSTGRES_USER'], '-d', self.db_env['POSTGRES_DB']], input=query, text=True, timeout=20)
        return int(result.strip()) > 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    package = commands.add_parser('package')
    for name in ['output', 'destination', 'baseline-binary']:
        package.add_argument('--' + name, type=Path, required=True)
    package.add_argument('--version', required=True)
    package.add_argument('--baseline-sha256', required=True)
    server = commands.add_parser('deploy')
    server.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    server.add_argument('--release', type=Path, required=True)
    server.add_argument('--stage', action='store_true')
    server.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if args.command == 'package':
        from release_package import package_release
        root = Path(__file__).resolve().parents[1]
        result = package_release(root, args.output, args.destination, args.output / 'sub2api-linux-amd64', args.version,
                                 args.baseline_sha256, BASELINE, baseline_binary=args.baseline_binary,
                                 upgrade_contract=contract_for_release())
    else:
        manifest = json.loads((args.release / 'manifest.json').read_text())
        policy = UnifiedCodingUpgrade(manifest.get('upgrade_contract', {}))
        policy.validate(manifest)
        if args.stage:
            stage_release(args.root, args.release)
            return
        result = deploy(args.root, args.release, CodingRuntime(args.root, args.release), apply=args.apply, upgrade=policy)
    print(json.dumps(result, indent=2))


if __name__ == '__main__': main()
