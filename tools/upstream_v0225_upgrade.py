"""Deploy the reviewed v0.2.25 merge and its one additive migration."""
import json
import subprocess
from pathlib import Path

from delivery_core import DeliveryError, digest
from experiment_accounting_upgrade import main
from experiment_account_standby_upgrade import ExperimentAccountStandbyRuntime, ExperimentAccountStandbyUpgrade

BASELINE = 'a2179319e5a1fdc81944a09e26d53c2aecd82fff'
APPLICATION = '17b38b898097e0d51214723d9bb96ede11cdb928'
PARENTS = ['b784a03bf9befd58b481f7f1ba7341cd4256e9c0', '348997267d756eab5729bb6b78a275a15d18796a']
MIGRATION = '252_channel_pricing_ultrafast_multiplier.sql'
FILE_SHA256 = '01eb249a933e5c3bef3a8e2106098abf56d20a5f2890fcc6490b1f83ec05687f'
DB_SHA256 = '458752b3523254ee60ab4a821261ab64df9216a6f208c1960e7b816f8c7cfd1d'
CONTRACT = 'upstream-additive-v0225-v1'


def contract_for_release():
    return dict(name=CONTRACT, baseline_commit=BASELINE, source_commit=APPLICATION,
                migrations={MIGRATION: FILE_SHA256}, database_migrations={MIGRATION: DB_SHA256})


class UpstreamV0225Upgrade(ExperimentAccountStandbyUpgrade):
    baseline = BASELINE
    rollback_block_reason = 'A channel Ultra Fast multiplier has been set. Keep the database and fix forward.'

    def __init__(self, contract):
        if len(APPLICATION) != 40 or contract != contract_for_release():
            raise DeliveryError('Only the pinned v0.2.25 upstream release is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Reviewed v0.2.25 source ancestry verification failed') from None

        if baseline != BASELINE or git('show', '-s', '--format=%P', APPLICATION).split() != PARENTS:
            raise DeliveryError('Unexpected v0.2.25 baseline or merge parents')
        git('merge-base', '--is-ancestor', BASELINE, APPLICATION)
        git('merge-base', '--is-ancestor', APPLICATION, source)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed v0.2.25 merge')
        path = 'backend/migrations/' + MIGRATION
        if git('diff', '--name-status', BASELINE, source, '--', 'backend/migrations') != 'A\t' + path:
            raise DeliveryError('Only the reviewed v0.2.25 additive migration is accepted')
        if digest(Path(root) / path) != FILE_SHA256:
            raise DeliveryError('v0.2.25 migration checksum changed')

    def assert_preserved(self, before, after):
        super().assert_preserved(before, after)
        if before['channel_ultrafast'] != after['channel_ultrafast']:
            raise DeliveryError('Channel Ultra Fast pricing changed during deployment')

    def can_rollback(self, runtime):
        return not runtime.channel_ultrafast_snapshot()


class UpstreamV0225Runtime(ExperimentAccountStandbyRuntime):
    def channel_ultrafast_snapshot(self):
        # to_jsonb also works on the baseline, where the column is absent.
        query = """BEGIN READ ONLY;
        SET LOCAL statement_timeout='15s';
        SELECT COALESCE(json_agg(json_build_array(id, to_jsonb(p)->'ultrafast_multiplier') ORDER BY id), '[]')
        FROM channel_model_pricing p WHERE to_jsonb(p)->>'ultrafast_multiplier' IS NOT NULL;
        COMMIT;"""
        return json.loads(subprocess.check_output(self._command(), input=query, text=True, timeout=20))

    def snapshot(self, as_of):
        data = super().snapshot(as_of)
        data['channel_ultrafast'] = self.channel_ultrafast_snapshot()
        return data


if __name__ == '__main__':
    main(UpstreamV0225Upgrade, contract_for_release, BASELINE, UpstreamV0225Runtime)
