"""Release administrator self-reset with no database migrations."""
from delivery_core import DeliveryError
from experiment_account_standby_upgrade import ExperimentAccountStandbyRuntime, ExperimentAccountStandbyUpgrade
from experiment_accounting_upgrade import main

BASELINE = 'd9bc47cb0381a3d3f463ef8aa224acb5fd867fba'
APPLICATION = '7fd982051b883c877971716cca9976eefaaf3d15'


def contract_for_release():
    return dict(name='admin-quota-self-reset-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations={}, database_migrations={})


class AdminQuotaSelfResetUpgrade(ExperimentAccountStandbyUpgrade):
    baseline = BASELINE
    application = APPLICATION

    def __init__(self, contract):
        if contract != contract_for_release():
            raise DeliveryError('Only the reviewed administrator self-reset release is supported')
        self.contract = contract

    def can_rollback(self, runtime):
        # The baseline already understands both standby configuration and every
        # personal reset ledger entry; it just does not offer the new operation.
        return True


if __name__ == '__main__':
    main(AdminQuotaSelfResetUpgrade, contract_for_release, BASELINE, ExperimentAccountStandbyRuntime)
