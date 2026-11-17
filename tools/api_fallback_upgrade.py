"""Pinned administrator-only API standby upgrade; never restores a database."""
import argparse
import copy
import json
from pathlib import Path
import subprocess

from delivery_core import DeliveryError, digest
from release_server import assert_preserved, deploy, stage_release
from upstream_additive_upgrade import AdditiveRuntime

BASELINE = '33e5d3d99cac471f82339f4a507582e0e338954b'
APPLICATION = 'ad6f53065eceeafbb9490558557b5288f112f57a'
MIGRATIONS = {
    '270_api_fallback_metering.sql': 'bffd68827dacf48074291445992a4e49459c92e963124d82f02762f8083af6bf',
    '271_api_fallback_control.sql': '296c89f2b2de5728cda31b971e05a9ebc25cc55399ee5f7a7bb02c578eab2cf6',
}
DATABASE_MIGRATIONS = {
    '270_api_fallback_metering.sql': '10014d9a5a0b50a64760b806f0d9c5a684021318e5c9d95b7274d3108c5e2959',
    '271_api_fallback_control.sql': '5d0841ddddec0e92b08706eb0538166599159e6154a71e64d7037e8d00a49e5f',
}
TABLES = ('api_fallback_policies', 'api_fallback_policy_versions', 'api_fallback_attempts', 'api_fallback_events')


def contract_for_release():
    return dict(name='api-fallback-admin-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations=MIGRATIONS.copy(),
                database_migrations=DATABASE_MIGRATIONS.copy())


class APIFallbackUpgrade:
    rollback_block_reason = 'API standby backup roles, charges or durable attempts exist; retain the compatible program and fix forward. Never restore an older database.'

    def __init__(self, contract):
        if contract != contract_for_release() or len(APPLICATION) != 40 or len(MIGRATIONS) != 2:
            raise DeliveryError('Only the pinned reviewed administrator standby upgrade is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            return subprocess.check_output(['git', *args], cwd=root, text=True, timeout=30).strip()
        if baseline != BASELINE:
            raise DeliveryError('Unexpected live source baseline')
        git('merge-base', '--is-ancestor', APPLICATION, source)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed administrator standby source')
        changes = git('diff', '--name-status', baseline, source, '--', 'backend/migrations').splitlines()
        if sorted(changes) != sorted('A\tbackend/migrations/' + name for name in MIGRATIONS):
            raise DeliveryError('Only the two reviewed additive standby migrations are accepted')
        for name, checksum in MIGRATIONS.items():
            if digest(Path(root) / 'backend/migrations' / name) != checksum:
                raise DeliveryError('Reviewed standby migration changed: ' + name)

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False
                or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned administrator standby manifest required')

    def assert_preserved(self, before, after):
        prior = {m['filename'] for m in before['migrations']}
        added = [m for m in after['migrations'] if m['filename'] not in prior]
        if {m['filename']: m['checksum'] for m in added} != DATABASE_MIGRATIONS:
            raise DeliveryError('Unexpected migration addition or checksum')
        adjusted = copy.deepcopy(after)
        adjusted['migrations'] = [m for m in after['migrations'] if m['filename'] in prior]
        assert_preserved(before, adjusted)
        if before['access'] != after['access'] or after['ultrafast_enabled']:
            raise DeliveryError('Existing access or Ultra Fast configuration changed')
        if before['fallback'] != after['fallback']:
            raise DeliveryError('Upgrade changed standby policy, spend evidence or fixed charges')
        if any(p.get('enabled') or p.get('config', {}).get('enabled') for p in after['fallback']['api_fallback_policies']):
            raise DeliveryError('Initial standby deployment must remain disabled')

    def can_rollback(self, runtime):
        snapshot = runtime.fallback_snapshot()
        if snapshot['api_fallback_attempts'] or snapshot['api_fallback_requests']:
            return False
        # Even a disabled draft can reserve an API account for standby only.
        # The old program ignores these roles and could schedule it normally.
        for table in ('api_fallback_policies', 'api_fallback_policy_versions'):
            for row in snapshot[table]:
                config = row.get('config', {})
                if not isinstance(config, dict) or config.get('backup_account_ids'):
                    return False
        return True


class APIFallbackRuntime(AdditiveRuntime):
    def fallback_snapshot(self):
        from quota_snapshot import stream_rows
        command = ['docker', 'exec', '-i', self.database, 'psql', '-X', '-qAt',
                   '-v', 'ON_ERROR_STOP=1', '-U', self.db_env['POSTGRES_USER'], '-d', self.db_env['POSTGRES_DB']]
        existing = json.loads(subprocess.check_output(command, input="SELECT COALESCE(json_agg(tablename),'[]') FROM pg_tables WHERE schemaname='public';", text=True, timeout=20))
        result = {table: [] for table in (*TABLES, 'api_fallback_requests')}
        sql = ["BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='20s';"]
        for table in TABLES:
            if table in existing:
                sql.append("SELECT json_build_array('" + table + "',to_jsonb(t)) FROM " + table + " t ORDER BY to_jsonb(t)::text;")
        if 'api_fallback_attempts' in existing:
            sql.append("SELECT json_build_array('api_fallback_requests',to_jsonb(q)) FROM coding_quota_requests q WHERE metering_source='api_fallback' ORDER BY id;")
        sql.append('COMMIT;')
        for line in stream_rows(command, '\n'.join(sql)):
            if line.strip():
                key, row = json.loads(line)
                result[key].append(row)
        return result

    def preflight(self):
        result = super().preflight()
        data = self.fallback_snapshot()
        if data['api_fallback_attempts'] or data['api_fallback_requests']:
            raise DeliveryError('Initial standby lane cannot replace an already-used standby runtime')
        if any(p.get('enabled') or p.get('config', {}).get('enabled') for p in data['api_fallback_policies']):
            raise DeliveryError('Disable standby before this initial upgrade')
        return result

    def snapshot(self, as_of):
        result = super().snapshot(as_of)
        result['fallback'] = self.fallback_snapshot()
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    package = commands.add_parser('package')
    for name in ['output', 'destination']:
        package.add_argument('--' + name, type=Path, required=True)
    package.add_argument('--version', required=True)
    package.add_argument('--baseline-sha256', required=True)
    package.add_argument('--baseline-binary', type=Path)
    package.add_argument('--full-upload', action='store_true')
    server = commands.add_parser('deploy')
    server.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    server.add_argument('--release', type=Path, required=True)
    server.add_argument('--stage', action='store_true')
    server.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if args.command == 'package':
        from release_package import package_release
        result = package_release(Path(__file__).resolve().parents[1], args.output, args.destination,
                                 args.output / 'sub2api-linux-amd64', args.version, args.baseline_sha256, BASELINE,
                                 baseline_binary=args.baseline_binary, full_upload=args.full_upload,
                                 upgrade_contract=contract_for_release())
    else:
        manifest = json.loads((args.release / 'manifest.json').read_text())
        policy = APIFallbackUpgrade(manifest.get('upgrade_contract', {}))
        policy.validate(manifest)
        if args.stage:
            stage_release(args.root, args.release)
            return
        result = deploy(args.root, args.release, APIFallbackRuntime(args.root, args.release), apply=args.apply, upgrade=policy)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
