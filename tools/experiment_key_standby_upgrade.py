"""Release experiment-key standby without changing migrations or billing history."""
from delivery_core import DeliveryError
from experiment_accounting_upgrade import ExperimentAccountingUpgrade, main as accounting_main

BASELINE = 'c6f4cc0a27d67fb5b8e455a97b5218c17262c103'
APPLICATION = 'b4c6f0537e2b025c43bf53a342c424498d2f8093'


def contract_for_release():
    return dict(name='experiment-key-standby-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations={}, database_migrations={})


class ExperimentKeyStandbyUpgrade(ExperimentAccountingUpgrade):
    rollback_block_reason = 'An experiment standby policy was saved. Keep the current database and fix forward.'

    def __init__(self, contract):
        if contract != contract_for_release():
            raise DeliveryError('Only the reviewed experiment-key standby release is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        import subprocess

        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Experiment standby source ancestry verification failed') from None

        if baseline != BASELINE:
            raise DeliveryError('Unexpected live experiment standby baseline')
        git('merge-base', '--is-ancestor', BASELINE, APPLICATION)
        git('merge-base', '--is-ancestor', APPLICATION, source)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed experiment-key standby change')
        if git('diff', '--name-only', baseline, source, '--', 'backend/migrations'):
            raise DeliveryError('Experiment standby release must not change migrations')

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned experiment-key standby manifest required')

    def can_rollback(self, runtime):
        snapshot = runtime.fallback_snapshot()
        return not any(row['config'].get('experiment_key_id')
                       for table in ('api_fallback_policies', 'api_fallback_policy_versions')
                       for row in snapshot[table])


if __name__ == '__main__':
    accounting_main(ExperimentKeyStandbyUpgrade, contract_for_release, BASELINE)
