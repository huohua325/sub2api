import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from verify_coding_preview_live import verify


class FakeLive:
    users = [{'id': 1, 'role': 'admin'}, {'id': 2, 'role': 'user'}]
    def api(self, user, path):
        if path == 'groups/available': return [{'id': 17}] if user['role'] == 'admin' else []
        return {'purpose_allowances': [{'purpose': p} for p in ['openai', 'grok', 'experiment']]}
    def sql(self, query): return [{'id': 1, 'key': 'sk-synthetic-never-print', 'group_id': 9, 'ip_whitelist': [], 'ip_blacklist': []}]


class Response:
    status = 200
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def read(self): return b'{"data":{}}'


class VerifyPreviewTests(unittest.TestCase):
    def test_verifies_old_key_without_inference_or_credential_output(self):
        with patch('verify_coding_preview_live.Live', FakeLive), patch('verify_coding_preview_live.urllib.request.urlopen', return_value=Response()) as http:
            result = verify(17)
        self.assertEqual(result['old_key_groups'], [{'group_id': 9, 'status': 200}])
        self.assertEqual(result['inference_calls'], 0)
        self.assertNotIn('sk-synthetic', json.dumps(result))
        self.assertTrue(http.call_args.args[0].full_url.endswith('/v1/sub2api/billing'))

    def test_uninvited_user_visibility_is_rejected(self):
        class Leaking(FakeLive):
            def api(self, user, path): return [{'id': 17}] if path == 'groups/available' else super().api(user, path)
        with patch('verify_coding_preview_live.Live', Leaking):
            with self.assertRaises(AssertionError): verify(17)


if __name__ == '__main__': unittest.main()
