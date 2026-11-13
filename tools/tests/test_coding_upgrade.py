import copy
import sys
from pathlib import Path
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
from coding_upgrade import UnifiedCodingUpgrade, contract_for_release, BASELINE, BACKEND_FILES


class CodingUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.contract = contract_for_release()
        self.policy = UnifiedCodingUpgrade(self.contract)
        self.before = {'as_of': 'fixed', 'clocks': [], 'migrations': [{'filename': '260.sql', 'checksum': 'existing'}],
                       'immutable': {'personal_resets': [{'id': 'retained'}]}, 'durable_ids': {}, 'requests': []}

    def test_only_pinned_application_and_baseline_are_accepted(self):
        changed = copy.deepcopy(self.contract)
        changed['source_commit'] = '0' * 40
        with self.assertRaises(DeliveryError): UnifiedCodingUpgrade(changed)
        changed = copy.deepcopy(self.contract)
        changed['baseline_commit'] = '0' * 40
        with self.assertRaises(DeliveryError): UnifiedCodingUpgrade(changed)

    def test_schema_and_persistent_data_must_remain_unchanged(self):
        self.policy.assert_preserved(self.before, copy.deepcopy(self.before))
        after = copy.deepcopy(self.before)
        after['migrations'].append({'filename': 'unexpected.sql', 'checksum': 'new'})
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, after)
        after = copy.deepcopy(self.before)
        after['immutable']['personal_resets'] = []
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, after)

    def test_profile_does_not_grant_frontend_lane_permission(self):
        manifest = {'backend_unchanged': True, 'upgrade_contract': self.contract, 'source_commit': self.contract['source_commit'], 'baseline_commit': self.contract['baseline_commit']}
        with self.assertRaises(DeliveryError): self.policy.validate(manifest)
        manifest['backend_unchanged'] = False
        self.policy.validate(manifest)

    def test_rollback_requires_no_new_preview_groups(self):
        class Runtime:
            def coding_preview_active(inner): return False
        self.assertTrue(self.policy.can_rollback(Runtime()))
        Runtime.coding_preview_active = lambda inner: True
        self.assertFalse(self.policy.can_rollback(Runtime()))
        self.assertFalse(self.policy.can_rollback(object()), 'missing evidence must block rollback')

    def test_source_guard_rejects_changed_application_schema_and_extra_backend_files(self):
        replies = ['', '', '\n'.join(BACKEND_FILES)]
        with patch('coding_upgrade.subprocess.check_output', side_effect=replies):
            self.policy.validate_sources(Path('.'), BASELINE, 'a' * 40)
        for replies in [['frontend/changed.vue'], ['', 'backend/migrations/999.sql'], ['', '', '\n'.join(BACKEND_FILES + ['backend/extra.go'])]]:
            with self.subTest(replies=replies), patch('coding_upgrade.subprocess.check_output', side_effect=replies):
                with self.assertRaises(DeliveryError): self.policy.validate_sources(Path('.'), BASELINE, 'a' * 40)


if __name__ == '__main__': unittest.main()
