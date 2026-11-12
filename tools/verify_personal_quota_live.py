"""Read-only local-server verification; credentials stay in this process."""
import argparse
import base64
from datetime import datetime, timezone
import hashlib
import hmac
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request


def verify(output):
    def environment(container):
        info = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
        return dict(v.split('=', 1) for v in info['Config']['Env'] if '=' in v)
    app_env, pg_env = environment('sub2api'), environment('sub2api-postgres')
    command = ['docker', 'exec', '-i', 'sub2api-postgres', 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
               '-U', pg_env['POSTGRES_USER'], '-d', pg_env['POSTGRES_DB']]
    query = "BEGIN READ ONLY; SELECT json_agg(u) FROM (SELECT id,email,role,password_hash FROM users WHERE deleted_at IS NULL AND status='active' ORDER BY id) u; COMMIT;"
    users = json.loads(subprocess.check_output(command, input=query, text=True, timeout=30))
    admin = next(u for u in users if u['role'] == 'admin')
    def b64(value): return base64.urlsafe_b64encode(value).decode().rstrip('=')
    def api(user, path):
        version = int.from_bytes(hashlib.sha256((user['email'].strip().lower() + '\n' + user['password_hash']).encode()).digest()[:8], 'big') & 0x7fffffffffffffff
        claims = {'user_id': user['id'], 'email': user['email'], 'role': user['role'], 'token_version': version,
                  'iat': int(time.time()), 'nbf': int(time.time()) - 1, 'exp': int(time.time()) + 120}
        content = b64(b'{"alg":"HS256","typ":"JWT"}') + '.' + b64(json.dumps(claims).encode())
        token = content + '.' + b64(hmac.new(app_env['JWT_SECRET'].encode(), content.encode(), hashlib.sha256).digest())
        request = urllib.request.Request('http://127.0.0.1:8080/api/v1/' + path, headers={'Authorization': 'Bearer ' + token})
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)['data']
    result = {'checked_at': datetime.now(timezone.utc).isoformat(), 'users': [], 'business_writes': 0}
    for user in users:
        if user['role'] != 'user': continue
        path = 'admin/users/' + str(user['id']) + '/purpose-quota-resets'
        view = api(admin, path)
        assert {q['purpose'] for q in view['quotas']} == {'openai', 'grok', 'experiment'}, 'Missing purpose quota'
        student = api(user, 'user/platform-quotas')
        for quota in view['quotas']:
            allowance = next(q for q in student['purpose_allowances'] if q['purpose'] == quota['purpose'])
            assert datetime.fromisoformat(allowance['resets_at'].replace('Z', '+00:00')) == datetime.fromisoformat(quota['period_end'].replace('Z', '+00:00')), 'Admin/student period differs'
        try:
            api(user, path)
            raise AssertionError('Ordinary user can read admin reset data')
        except urllib.error.HTTPError as error:
            assert error.code == 403, 'Unexpected ordinary-user denial'
        result['users'].append({'user_id': user['id'], 'purposes': len(view['quotas']),
                                'events': len(view['events']), 'periods_match': True, 'ordinary_user_denied': True})
    assert result['users'], 'No ordinary users verified'
    catalog = api(admin, 'admin/settings/admin-api-keys')
    ids = {p['id'] for p in catalog['permissions']}
    assert {'users.purpose_quotas.read', 'users.purpose_quotas.reset'} <= ids
    result['permissions_present'] = True
    Path(output).write_text(json.dumps(result, indent=2), encoding='utf-8')
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True)
    verify(parser.parse_args().output)
