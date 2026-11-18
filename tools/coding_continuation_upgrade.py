"""Pinned migration-free continuation fix on the accounting-compatible production baseline."""
import re
import subprocess

from delivery_core import DeliveryError
from experiment_accounting_upgrade import ExperimentAccountingUpgrade, main as accounting_main

BASELINE = 'fa996675e4fc4543e13c2b3fa801566702f5236e'
APPLICATION = '7624e4b236f85775ebd841c2058458a36b6af2f3'
FILES = {'backend/internal/repository/api_fallback_attempt.go',
         'backend/internal/repository/api_fallback_db_test.go'}


def contract_for_release():
    return dict(name='coding-continuation-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations={}, database_migrations={})


class CodingContinuationUpgrade(ExperimentAccountingUpgrade):
    def __init__(self, contract):
        if contract != contract_for_release() or not re.fullmatch(r'[0-9a-f]{40}', APPLICATION):
            raise DeliveryError('Only the pinned reviewed continuation fix is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Continuation source ancestry verification failed') from None
        if baseline != BASELINE:
            raise DeliveryError('Unexpected live continuation baseline')
        git('merge-base', '--is-ancestor', BASELINE, APPLICATION)
        git('merge-base', '--is-ancestor', APPLICATION, source)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed continuation fix')
        changes = set(git('diff', '--name-status', baseline, source, '--', 'backend', 'frontend').splitlines())
        if changes != {'M\t' + name for name in FILES}:
            raise DeliveryError('Continuation fix must only modify the reviewed repository and regression test')

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned continuation manifest required')


if __name__ == '__main__':
    accounting_main(CodingContinuationUpgrade, contract_for_release, BASELINE)
