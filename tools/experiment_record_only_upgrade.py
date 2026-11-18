"""Release the reviewed GlobalAI admission change with no database migrations."""
import subprocess

from delivery_core import DeliveryError
from experiment_accounting_upgrade import ExperimentAccountingUpgrade, main as accounting_main

BASELINE = '078c24a63fdcdd28b32990d254f7dde6bc61c0a4'
APPLICATION = 'e89082b998815b499464a852b07efdde3acf323c'


def contract_for_release():
    return dict(name='experiment-record-only-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations={}, database_migrations={})


class ExperimentRecordOnlyUpgrade(ExperimentAccountingUpgrade):
    def __init__(self, contract):
        if contract != contract_for_release():
            raise DeliveryError('Only the pinned experiment accounting release is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Experiment accounting source ancestry verification failed') from None
        if baseline != BASELINE:
            raise DeliveryError('Unexpected live experiment accounting baseline')
        git('merge-base', '--is-ancestor', BASELINE, APPLICATION)
        git('merge-base', '--is-ancestor', APPLICATION, source)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed experiment accounting change')
        if git('diff', '--name-only', baseline, source, '--', 'backend/migrations'):
            raise DeliveryError('Experiment accounting release must not change migrations')

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned experiment accounting manifest required')

    def can_rollback(self, runtime):
        # Standby2 reads the same attempts, charges and settlement events.
        # Rollback restores its admission rules and keeps the current database.
        return True


if __name__ == '__main__':
    accounting_main(ExperimentRecordOnlyUpgrade, contract_for_release, BASELINE)
