"""Deploy account-based experiment standby without migrations."""
import json
import subprocess

from delivery_core import DeliveryError
from experiment_accounting_upgrade import ExperimentAccountingRuntime, main
from experiment_key_standby_upgrade import ExperimentKeyStandbyUpgrade

BASELINE = 'a8669b4ae750a062e0ac119907ad51f6d9c98e75'
APPLICATION = '0e74614210b2a7005d11d24ab3b1a54e1d338c91'


def contract_for_release():
    return dict(name='experiment-account-standby-v1', baseline_commit=BASELINE,
                source_commit=APPLICATION, migrations={}, database_migrations={})


class ExperimentAccountStandbyUpgrade(ExperimentKeyStandbyUpgrade):
    baseline = BASELINE
    application = APPLICATION
    require_disabled_standby = False
    rollback_block_reason = 'Coding standby accounts have been configured. Keep the database and fix forward.'

    def __init__(self, contract):
        if contract != contract_for_release():
            raise DeliveryError('Only the reviewed experiment account standby release is supported')
        self.contract = contract

    def assert_preserved(self, before, after):
        super().assert_preserved(before, after)
        if before['account_standby'] != after['account_standby']:
            raise DeliveryError('Standby account configuration changed during deployment')

    def can_rollback(self, runtime):
        return not runtime.account_standby_snapshot()


class ExperimentAccountStandbyRuntime(ExperimentAccountingRuntime):
    def account_standby_snapshot(self):
        query = """BEGIN READ ONLY;
        SELECT COALESCE(json_agg(json_build_array(id, extra->'coding_standby_group_ids') ORDER BY id), '[]')
        FROM accounts WHERE deleted_at IS NULL AND extra ? 'coding_standby_group_ids'
          AND extra->'coding_standby_group_ids' <> '[]'::jsonb;
        COMMIT;"""
        return json.loads(subprocess.check_output(self._command(), input=query, text=True, timeout=20))

    def snapshot(self, as_of):
        data = super().snapshot(as_of)
        data['account_standby'] = self.account_standby_snapshot()
        return data


if __name__ == '__main__':
    main(ExperimentAccountStandbyUpgrade, contract_for_release, BASELINE, ExperimentAccountStandbyRuntime)
