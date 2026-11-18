"""Pinned migration-free coding standby extension on the deployed continuation fix."""
import re
import subprocess

from delivery_core import DeliveryError
from experiment_accounting_upgrade import ExperimentAccountingUpgrade, main as accounting_main

BASELINE = '2898b7e4b478e581c1ccc34b65a898e98a1ed086'
HOTFIX = '7624e4b236f85775ebd841c2058458a36b6af2f3'
APPLICATION = 'fabfc074150430c4e47a9d9c13897820b9c3ba4a'


def contract_for_release():
    return dict(name='coding-standby-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations={}, database_migrations={})


class CodingStandbyUpgrade(ExperimentAccountingUpgrade):
    rollback_block_reason = 'Coding standby roles or paid attempts exist; retain the compatible program and fix forward. Never restore a database.'

    def __init__(self, contract):
        if contract != contract_for_release() or not re.fullmatch(r'[0-9a-f]{40}', APPLICATION):
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
            raise DeliveryError('Unexpected deployed continuation baseline')
        for ancestor, descendant in ((HOTFIX, BASELINE), (BASELINE, APPLICATION), (APPLICATION, source)):
            git('merge-base', '--is-ancestor', ancestor, descendant)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed coding standby extension')
        if git('diff', '--name-only', baseline, source, '--', 'backend/migrations'):
            raise DeliveryError('Coding standby extension must not change any migration')

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
