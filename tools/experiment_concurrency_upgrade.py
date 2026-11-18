"""Pinned opt-in asynchronous experiment admission; never discard billing liabilities."""
from pathlib import Path
import re
import subprocess

from delivery_core import DeliveryError, digest
from experiment_accounting_upgrade import ExperimentAccountingUpgrade, main as accounting_main

BASELINE = '2898b7e4b478e581c1ccc34b65a898e98a1ed086'
APPLICATION = '11a1df54deff041bc68e6187c58ac0c54e3caa87'
MIGRATIONS = {'273_experiment_async_admission.sql': 'f5674cf8a5e8d4b4c48f9a1d94142cd447a6c47682a2e944fa01140961f60024'}
DATABASE_MIGRATIONS = {'273_experiment_async_admission.sql': 'e527e4862099d0655006285f1828a1880507978be939256711b7fcb7b7ef7281'}


def contract_for_release():
    return dict(name='experiment-concurrency-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations=MIGRATIONS.copy(),
                database_migrations=DATABASE_MIGRATIONS.copy())


class ExperimentConcurrencyUpgrade(ExperimentAccountingUpgrade):
    def __init__(self, contract):
        if (contract != contract_for_release() or not re.fullmatch(r'[0-9a-f]{40}', APPLICATION)
                or set(MIGRATIONS) != {'273_experiment_async_admission.sql'}
                or set(DATABASE_MIGRATIONS) != set(MIGRATIONS)
                or any(not re.fullmatch(r'[0-9a-f]{64}', value)
                       for value in (*MIGRATIONS.values(), *DATABASE_MIGRATIONS.values()))):
            raise DeliveryError('Only the pinned reviewed asynchronous admission release is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Asynchronous admission source ancestry verification failed') from None
        if baseline != BASELINE:
            raise DeliveryError('Unexpected live asynchronous admission baseline')
        git('merge-base', '--is-ancestor', BASELINE, APPLICATION)
        git('merge-base', '--is-ancestor', APPLICATION, source)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from reviewed asynchronous admission')
        changes = git('diff', '--name-status', baseline, source, '--', 'backend/migrations').splitlines()
        if changes != ['A\tbackend/migrations/273_experiment_async_admission.sql']:
            raise DeliveryError('Only additive migration 273 is accepted')
        for name, checksum in MIGRATIONS.items():
            if digest(Path(root) / 'backend/migrations' / name) != checksum:
                raise DeliveryError('Reviewed admission migration changed')

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned asynchronous admission manifest required')

    def can_rollback(self, runtime):
        # The pinned 2898b7e4 baseline already understands every attempt state,
        # charge and settlement event. Its migration runner skips registered 272
        # (verified with multiple open attempts), so it will not recreate the
        # unique index. Keeping 273 and the grant setting safely restores strict
        # admission while its existing reconciler settles the retained backlog.
        return True


if __name__ == '__main__':
    accounting_main(ExperimentConcurrencyUpgrade, contract_for_release, BASELINE)
