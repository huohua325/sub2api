"""Pinned v0.2.22 upgrade with two reviewed additive migrations.

Keeps the older migration-free lane intact. Never restores the database.
"""
import argparse
import copy
import json
from pathlib import Path
import subprocess

from delivery_core import DeliveryError, digest
from release_server import DockerRuntime, assert_preserved, deploy, stage_release

BASELINE = '539e4ba8b44eaf90ef29f30c0c51e86d23b3aba6'
MERGE = '45c1cfda1c738a60cd278cd06071305670380252'
PARENTS = ['31f27a36ef10c3ea3bb911c073c7da99995c8118',
           '15871f904056f548dd874a1d069ffc6cdf777b78']
MIGRATIONS = {
    '251_group_force_openai_ultrafast.sql': 'f91352f2d3b1bffbbea5f6b7fc17e37c5ad64f04c3426cb7168778d10135a7bf',
    '251_group_usage_rollup_nonblocking_build.sql': 'da504646104cec14aa7bcf5a0af6f06f5cd40883459379ae915db27c1d4cec8b',
}
DATABASE_MIGRATIONS = {
    '251_group_force_openai_ultrafast.sql': 'd78e605d4fc2038c57f620dff398022823a850ea8994b129b50774ac49e20af1',
    '251_group_usage_rollup_nonblocking_build.sql': '5b6e9b57f9b354a96c6870185c034db8adc7def944efcddf654e39cd97770718',
}


def contract_for_release():
    return dict(name='upstream-additive-v0222-v1', baseline_commit=BASELINE,
                merge_commit=MERGE, merge_parents=PARENTS.copy(),
                migrations=MIGRATIONS.copy(), database_migrations=DATABASE_MIGRATIONS.copy())


class AdditiveUpstreamUpgrade:
    rollback_block_reason = 'Ultra Fast has been enabled; preserve the database and fix forward.'

    def __init__(self, contract):
        if contract != contract_for_release():
            raise DeliveryError('Only the fixed reviewed v0.2.22 additive upgrade is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=60).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Reviewed source ancestry verification failed') from None
        if baseline != BASELINE or git('rev-list', '--parents', '-n', '1', MERGE).split() != [MERGE, *PARENTS]:
            raise DeliveryError('Fixed production baseline and reviewed two-parent merge required')
        git('merge-base', '--is-ancestor', BASELINE, MERGE)
        git('merge-base', '--is-ancestor', MERGE, source)
        if git('diff', '--name-only', MERGE, source, '--', 'frontend', 'backend'):
            raise DeliveryError('Application differs from the reviewed merge')
        changes = git('diff', '--name-status', BASELINE, source, '--', 'backend/migrations/*.sql').splitlines()
        if sorted(changes) != sorted('A\tbackend/migrations/' + name for name in MIGRATIONS):
            raise DeliveryError('Only the two reviewed additive SQL files may differ')
        for name, checksum in MIGRATIONS.items():
            if digest(Path(root) / 'backend/migrations' / name) != checksum:
                raise DeliveryError('Reviewed migration file changed: ' + name)

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False
                or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned additive upstream manifest required')

    def assert_preserved(self, before, after):
        prior = {m['filename'] for m in before['migrations']}
        added = [m for m in after['migrations'] if m['filename'] not in prior]
        if {m['filename']: m['checksum'] for m in added} != DATABASE_MIGRATIONS:
            raise DeliveryError('Unexpected migration addition or database checksum')
        adjusted = copy.deepcopy(after)
        adjusted['migrations'] = [m for m in after['migrations'] if m['filename'] in prior]
        assert_preserved(before, adjusted)
        # Configuration is exact, including no new grants/bindings/routes.
        if before['access'] != after['access']:
            raise DeliveryError('Existing groups, Key bindings or tester permissions changed')
        if after['ultrafast_enabled']:
            raise DeliveryError('Upgrade must not enable Ultra Fast automatically')

    def can_rollback(self, runtime):
        # Old source supports personal resets/unified coding. Extra columns and
        # replacement rollup triggers are backward compatible, but enabling the
        # new tier requires the new program to continue interpreting it.
        return not runtime.access_snapshot()['ultrafast_enabled']


ACCESS_QUERY = """BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='15s';
SELECT json_build_object(
 'groups',(SELECT COALESCE(json_agg(s ORDER BY s.id),'[]') FROM
   (SELECT id,platform,usage_kind,is_exclusive,status,subscription_type,force_openai_fast,
           model_allowlist,require_oauth_only FROM groups WHERE deleted_at IS NULL) s),
 'keys',(SELECT COALESCE(json_agg(s ORDER BY s.id),'[]') FROM
   (SELECT id,user_id,group_id FROM api_keys WHERE deleted_at IS NULL) s),
 'grants',(SELECT COALESCE(json_agg(s ORDER BY s.user_id,s.group_id),'[]') FROM
   (SELECT user_id,group_id FROM user_allowed_groups) s),
 'routes',(SELECT COALESCE(json_agg(s ORDER BY s.id),'[]') FROM composite_model_routes s),
 'ultrafast_enabled', EXISTS(SELECT 1 FROM groups
   WHERE deleted_at IS NULL AND COALESCE((to_jsonb(groups)->>'force_openai_ultrafast')::boolean,false))
);
COMMIT;"""


class AdditiveRuntime(DockerRuntime):
    def access_snapshot(self):
        command = ['docker', 'exec', '-i', self.database, 'psql', '-X', '-qAt',
                   '-v', 'ON_ERROR_STOP=1', '-U', self.db_env['POSTGRES_USER'], '-d', self.db_env['POSTGRES_DB']]
        return json.loads(subprocess.check_output(command, input=ACCESS_QUERY, text=True, timeout=25))

    def snapshot(self, as_of):
        result = super().snapshot(as_of)
        access = self.access_snapshot()
        result['ultrafast_enabled'] = access.pop('ultrafast_enabled')
        result['access'] = access
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
    package.add_argument('--admin-browser', type=Path, required=True)
    server = commands.add_parser('deploy')
    server.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    server.add_argument('--release', type=Path, required=True)
    server.add_argument('--stage', action='store_true')
    server.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if args.command == 'package':
        from release_package import package_release
        from delivery_core import tree_hash
        root = Path(__file__).resolve().parents[1]
        review = json.loads(args.admin_browser.read_text())
        if (not review.get('ok') or not {'chromium', 'webkit'}.issubset(review.get('engines', {}))
                or review.get('frontend_sha256') != tree_hash(root, ['backend/internal/web/dist'])['backend/internal/web/dist']
                or review.get('script_sha256') != digest(root / 'tools/review_admin_groups.cjs')
                or review.get('fixtures_sha256') != digest(root / 'tools/browser_fixtures.cjs')):
            raise DeliveryError('Current dual-browser administrator tier evidence required')
        result = package_release(root, args.output, args.destination, args.output / 'sub2api-linux-amd64',
                                 args.version, args.baseline_sha256, BASELINE,
                                 baseline_binary=args.baseline_binary, full_upload=args.full_upload,
                                 upgrade_contract=contract_for_release())
    else:
        manifest = json.loads((args.release / 'manifest.json').read_text())
        policy = AdditiveUpstreamUpgrade(manifest.get('upgrade_contract', {}))
        policy.validate(manifest)
        if args.stage:
            stage_release(args.root, args.release)
            return
        result = deploy(args.root, args.release, AdditiveRuntime(args.root, args.release),
                        apply=args.apply, upgrade=policy)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
