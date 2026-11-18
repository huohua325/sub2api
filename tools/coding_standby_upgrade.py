"""Pinned coding standby plus opt-in experiment concurrency; retain continuation fix."""
from pathlib import Path
import re
import subprocess

from delivery_core import DeliveryError, digest
from experiment_accounting_upgrade import ExperimentAccountingUpgrade, main as accounting_main
from experiment_concurrency_upgrade import MIGRATIONS, DATABASE_MIGRATIONS

BASELINE = '16ba3ae098dfca67bc3e8b900b4e30702181d4ef'
HOTFIX = '7624e4b236f85775ebd841c2058458a36b6af2f3'
APPLICATION = 'f3128c0353430660240a46c147a0216a1c5298b2'


def contract_for_release():
    return dict(name='coding-standby-v2', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations={}, database_migrations={})


class CodingStandbyUpgrade(ExperimentAccountingUpgrade):
    rollback_block_reason = 'Coding standby roles or paid attempts exist; retain the compatible program and fix forward. Never restore a database.'

    def __init__(self, contract):
        if (contract != contract_for_release() or not re.fullmatch(r'[0-9a-f]{40}', APPLICATION)
                or set(MIGRATIONS) != {'273_experiment_async_admission.sql'}
                or set(DATABASE_MIGRATIONS) != set(MIGRATIONS)
                or any(not re.fullmatch(r'[0-9a-f]{64}', value)
                       for value in (*MIGRATIONS.values(), *DATABASE_MIGRATIONS.values()))):
            raise DeliveryError('Only the pinned reviewed coding standby extension is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Coding standby source ancestry verification failed') from None
        if baseline != BASELINE:
            raise DeliveryError('Unexpected deployed experiment concurrency baseline')
        for ancestor, descendant in ((HOTFIX, BASELINE), (BASELINE, APPLICATION), (APPLICATION, source)):
            git('merge-base', '--is-ancestor', ancestor, descendant)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed coding standby extension')
        if git('diff', '--name-only', baseline, source, '--', 'backend/migrations'):
            raise DeliveryError('Coding standby must preserve all deployed migrations including 273')
        for name, checksum in MIGRATIONS.items():
            if digest(Path(root) / 'backend/migrations' / name) != checksum:
                raise DeliveryError('Reviewed admission migration changed')

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned coding standby manifest required')

    def can_rollback(self, runtime):
        # The pinned baseline already understands all experiment settlements.
        # A newly assigned standby role still cannot be ignored by that program.
        fallback = runtime.fallback_snapshot()
        if fallback['api_fallback_attempts'] or fallback['api_fallback_requests']:
            return False
        for table in ('api_fallback_policies', 'api_fallback_policy_versions'):
            for row in fallback[table]:
                config = row.get('config', {})
                if not isinstance(config, dict) or config.get('backup_account_ids'):
                    return False
        return True


if __name__ == '__main__':
    accounting_main(CodingStandbyUpgrade, contract_for_release, BASELINE)
