import copy
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from coding_preview_live import Live, PREVIEW_NAME


class FakeLive(Live):
    def __init__(self, leak=False):
        self.users = [{'id': 1, 'role': 'admin'}, {'id': 2, 'role': 'user'}]
        self.admin = self.users[0]
        self.groups = [{'id': i, 'platform': platform, 'usage_kind': 'coding', 'status': 'active', 'subscription_type': 'standard', 'name': platform} for i, platform in [(10, 'openai'), (20, 'grok')]]
        self.accounts = [{'id': i, 'platform': platform, 'type': 'oauth', 'status': 'active', 'groups': [i]} for i, platform in [(10, 'openai'), (20, 'grok')]]
        self.grants, self.routes, self.writes = [], [], []
        self.leak = leak

    def inventory(self): return copy.deepcopy({'groups': self.groups, 'accounts': self.accounts})
    def original_bindings(self): return {'keys': [{'id': 1, 'group_id': 10}], 'grants': list(self.grants)}
    def api(self, user, path, payload=None, method=None):
        if payload is not None: self.writes.append((path, copy.deepcopy(payload)))
        if path == 'admin/groups':
            self.groups.append({**payload, 'id': 30, 'status': 'active'})
            return self.groups[-1]
        if path.endswith('/composite-routes'):
            if payload: self.routes.append(payload)
            return self.routes
        if path == 'groups/available':
            return self.groups if user['role'] == 'admin' or self.leak else self.groups[:2]
        if method == 'PUT':
            self.groups[-1]['status'] = payload['status']
            return self.groups[-1]
        raise AssertionError(path)


class PreviewSetupTests(unittest.TestCase):
    def test_only_a_new_private_group_and_routes_are_written(self):
        live = FakeLive()
        result = live.setup('grok-4.6')
        self.assertTrue(result['old_groups_keys_grants_preserved'])
        self.assertEqual(result['inference_calls'], 0)
        self.assertTrue(live.groups[-1]['is_exclusive'])
        self.assertTrue(live.groups[-1]['require_oauth_only'])
        self.assertEqual(live.groups[-1]['copy_accounts_from_group_ids'], [10, 20])
        self.assertEqual(len(live.writes), 3)

    def test_visibility_failure_disables_only_the_new_preview(self):
        live = FakeLive(leak=True)
        with self.assertRaises(RuntimeError): live.setup('grok-4.6')
        self.assertEqual(live.groups[-1]['status'], 'inactive')
        self.assertEqual([g['status'] for g in live.groups[:2]], ['active', 'active'])

    def test_existing_testers_are_never_removed_or_overwritten(self):
        live = FakeLive()
        live.groups.append({'id': 30, 'name': PREVIEW_NAME, 'platform': 'composite', 'usage_kind': 'coding', 'is_exclusive': True, 'status': 'active'})
        live.grants = [{'user_id': 2, 'group_id': 30}]
        with self.assertRaises(RuntimeError): live.setup('grok-4.6')
        self.assertEqual(live.writes, [])


if __name__ == '__main__': unittest.main()
