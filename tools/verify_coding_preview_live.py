"""Read-only checks of old API Keys and administrator-only preview visibility."""
import argparse
import json
from pathlib import Path
import urllib.request

from coding_preview_live import Live


def verify(group_id):
    live = Live()
    result = {'preview_group_id': group_id, 'users': [], 'old_key_groups': [], 'inference_calls': 0}
    for user in live.users:
        groups = live.api(user, 'groups/available')
        visible = any(g['id'] == group_id for g in groups)
        assert visible == (user['role'] == 'admin'), 'Unexpected preview visibility'
        quotas = live.api(user, 'user/platform-quotas')
        assert {'openai', 'grok', 'experiment'} <= {q['purpose'] for q in quotas['purpose_allowances']}
        result['users'].append({'id': user['id'], 'role': user['role'], 'preview_visible': visible, 'quota_api_ok': True})
    keys = live.sql("""SELECT COALESCE(json_agg(s),'[]') FROM (
      SELECT k.id,k.key,k.group_id,k.ip_whitelist,k.ip_blacklist FROM api_keys k
      JOIN users u ON u.id=k.user_id JOIN groups g ON g.id=k.group_id
      WHERE k.deleted_at IS NULL AND k.status='active' AND (k.expires_at IS NULL OR k.expires_at>now())
      AND u.deleted_at IS NULL AND u.status='active' AND u.role='user'
      AND g.deleted_at IS NULL AND g.status='active' AND g.platform!='composite'
      ORDER BY k.group_id,k.id) s""")
    checked = set()
    for key in keys:
        if key['group_id'] in checked or key['ip_whitelist'] or key['ip_blacklist']: continue
        request = urllib.request.Request('http://127.0.0.1:8080/v1/sub2api/billing', headers={'Authorization': 'Bearer ' + key['key']})
        with urllib.request.urlopen(request, timeout=20) as response:
            assert response.status == 200
            json.load(response)
        checked.add(key['group_id'])
        result['old_key_groups'].append({'group_id': key['group_id'], 'status': 200})
    assert checked, 'No original API Key authenticated successfully'
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--group-id', type=int, required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    result = verify(args.group_id)
    Path(args.output).write_text(json.dumps(result, indent=2), encoding='utf-8')
    print(json.dumps(result, indent=2))
