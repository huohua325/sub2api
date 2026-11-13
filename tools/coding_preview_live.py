"""Inspect or provision a dedicated administrator-only preview through existing APIs."""
import argparse
import base64
import hashlib
import hmac
import json
from pathlib import Path
import subprocess
import time
import urllib.request

PREVIEW_NAME = 'MILE Lab Unified Coding - Admin Preview'


class Live:
    def __init__(self):
        def env(container):
            info = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
            return dict(v.split('=', 1) for v in info['Config']['Env'] if '=' in v)
        self.app, self.pg = env('sub2api'), env('sub2api-postgres')
        self.users = self.sql("SELECT COALESCE(json_agg(u),'[]') FROM (SELECT id,email,role,password_hash FROM users WHERE deleted_at IS NULL AND status='active' ORDER BY id) u")
        self.admin = next(u for u in self.users if u['role'] == 'admin')

    def sql(self, query):
        command = ['docker', 'exec', '-i', 'sub2api-postgres', 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-U', self.pg['POSTGRES_USER'], '-d', self.pg['POSTGRES_DB']]
        text = 'BEGIN READ ONLY; SET LOCAL statement_timeout=\'15s\'; ' + query + '; COMMIT;'
        return json.loads(subprocess.check_output(command, input=text, text=True, timeout=25))

    def api(self, user, path, payload=None, method=None):
        def b64(value): return base64.urlsafe_b64encode(value).decode().rstrip('=')
        version = int.from_bytes(hashlib.sha256((user['email'].strip().lower() + '\n' + user['password_hash']).encode()).digest()[:8], 'big') & 0x7fffffffffffffff
        claims = {'user_id': user['id'], 'email': user['email'], 'role': user['role'], 'token_version': version, 'iat': int(time.time()), 'nbf': int(time.time()) - 1, 'exp': int(time.time()) + 120}
        content = b64(b'{"alg":"HS256","typ":"JWT"}') + '.' + b64(json.dumps(claims).encode())
        token = content + '.' + b64(hmac.new(self.app['JWT_SECRET'].encode(), content.encode(), hashlib.sha256).digest())
        request = urllib.request.Request('http://127.0.0.1:8080/api/v1/' + path, data=None if payload is None else json.dumps(payload).encode(), method=method, headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
        with urllib.request.urlopen(request, timeout=30) as response: return json.load(response)['data']

    def inventory(self):
        return self.sql("""SELECT json_build_object(
          'groups',(SELECT COALESCE(json_agg(s),'[]') FROM (SELECT id,name,platform,usage_kind,is_exclusive,status,subscription_type FROM groups WHERE deleted_at IS NULL ORDER BY id) s),
          'accounts',(SELECT COALESCE(json_agg(s),'[]') FROM (SELECT a.id,a.platform,a.type,a.status,a.parent_account_id,a.credentials->'model_mapping' AS model_mapping,array_agg(ag.group_id ORDER BY ag.group_id) AS groups FROM accounts a JOIN account_groups ag ON ag.account_id=a.id WHERE a.deleted_at IS NULL GROUP BY a.id ORDER BY a.id) s))""")

    def original_bindings(self):
        return self.sql("SELECT json_build_object('keys',(SELECT COALESCE(json_agg(s),'[]') FROM (SELECT id,user_id,group_id FROM api_keys WHERE deleted_at IS NULL ORDER BY id) s),'grants',(SELECT COALESCE(json_agg(s),'[]') FROM (SELECT user_id,group_id FROM user_allowed_groups ORDER BY user_id,group_id) s))")

    def setup(self, grok_model):
        before = self.inventory()
        bindings = self.original_bindings()
        existing = [g for g in before['groups'] if g['name'] == PREVIEW_NAME]
        if len(existing) > 1: raise RuntimeError('Ambiguous preview groups')
        source_ids = []
        for platform in ['openai', 'grok']:
            groups = [g for g in before['groups'] if g['platform'] == platform and g['usage_kind'] == 'coding' and g['status'] == 'active' and g['subscription_type'] == 'standard']
            candidates = [g for g in groups if any(a['platform'] == platform and a['type'] in ['oauth', 'setup-token'] and a['status'] == 'active' and g['id'] in a['groups'] for a in before['accounts'])]
            if not candidates: raise RuntimeError('No active subscription source group for ' + platform)
            source_ids.append(candidates[0]['id'])
        if existing:
            raise RuntimeError('Preview group already exists; inspect it manually instead of overwriting or assuming its configuration')
        else:
            group = self.api(self.admin, 'admin/groups', {'name': PREVIEW_NAME, 'description': 'Administrator preview; invite testers explicitly after validation.', 'platform': 'composite', 'usage_kind': 'coding', 'is_exclusive': True, 'subscription_type': 'standard', 'rate_multiplier': 1, 'require_oauth_only': True, 'copy_accounts_from_group_ids': source_ids, 'model_allowlist': {'enabled': True, 'models': ['gpt-6.1-sol', grok_model]}})
        group_id = group['id']
        try:
            path = 'admin/groups/' + str(group_id) + '/composite-routes'
            routes = self.api(self.admin, path)
            if isinstance(routes, dict): routes = routes.get('items', [])
            for model, platform in [('gpt-6.1-sol', 'openai'), (grok_model, 'grok')]:
                found = [r for r in routes if r['public_model'] == model]
                if found:
                    if len(found) != 1 or found[0]['target_platform'] != platform or not found[0]['enabled'] or found[0]['upstream_model'] != model or found[0]['endpoint'] != 'responses' or found[0]['match_type'] != 'exact': raise RuntimeError('Conflicting preview model route')
                else:
                    self.api(self.admin, path, {'public_model': model, 'match_type': 'exact', 'endpoint': 'responses', 'target_platform': platform, 'upstream_model': model, 'priority': 0, 'enabled': True})
            checks = []
            for user in self.users:
                available = self.api(user, 'groups/available')
                visible = any(g['id'] == group_id for g in available)
                if visible != (user['role'] == 'admin'): raise RuntimeError('Preview visibility is not administrator-only')
                checks.append({'user_id': user['id'], 'role': user['role'], 'preview_visible': visible})
            after = self.inventory()
            if [g for g in after['groups'] if g['id'] != group_id] != [g for g in before['groups'] if g['id'] != group_id]: raise RuntimeError('Existing group configuration changed')
            if self.original_bindings() != bindings: raise RuntimeError('Existing Key bindings or invitations changed')
            return {'group_id': group_id, 'group_name': PREVIEW_NAME, 'private': True, 'source_group_ids': source_ids, 'models': ['gpt-6.1-sol', grok_model], 'users': checks, 'old_groups_keys_grants_preserved': True, 'inference_calls': 0}
        except Exception:
            self.api(self.admin, 'admin/groups/' + str(group_id), {'status': 'inactive'}, method='PUT')
            raise


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--setup', action='store_true')
    parser.add_argument('--grok-model', default='grok-4.6')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    live = Live()
    result = live.setup(args.grok_model) if args.setup else live.inventory()
    Path(args.output).write_text(json.dumps(result, indent=2), encoding='utf-8')
    print(json.dumps(result, indent=2))
