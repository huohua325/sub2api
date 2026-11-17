"""Collect minimal GlobalAI billing evidence and prepare exact pending-charge plans.

This tool never infers an individual charge from an account balance or token total.
Collection is read-only. Plans contain only matched supplier consume records, and
are not database writes. Credentials remain in memory and are never serialized.
"""
from __future__ import annotations

import argparse
import base64
from collections import Counter, defaultdict
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import zlib


PROVIDER = 'globalai.vip'
QUOTA_PER_YUAN = 500000
REQUEST_ID = re.compile(r'[A-Za-z0-9._:-]{1,256}\Z')


class ReconcileError(Exception):
    """A safe, user-readable evidence or transport error."""


def utcnow():
    return datetime.now(timezone.utc).isoformat()


def canonical(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(',', ':')).encode()


def content_hash(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def integer(value, name, minimum=0):
    if isinstance(value, bool) or not isinstance(value, int) or value < minimum:
        raise ReconcileError(f'{name} must be an integer >= {minimum}')
    return value


def amount(value):
    try:
        result = Decimal(str(value))
    except (ValueError, InvalidOperation) as error:
        raise ReconcileError('Invalid local charge amount') from error
    if not result.is_finite() or result < 0:
        raise ReconcileError('Invalid local charge amount')
    return result


def normalize_log(row, account_id):
    """Project a consume log without retaining key, prompt, IP, or user fields."""
    if not isinstance(row, dict) or row.get('type') != 2:
        return None
    # Supplier request_id is the ID in our X-Oneapi-Request-Id header.
    # Its upstream_request_id names GlobalAI's own provider call, another hop.
    request_id = row.get('request_id') or row.get('upstream_request_id', '')
    if not isinstance(request_id, str) or not REQUEST_ID.fullmatch(request_id):
        raise ReconcileError('Consume log has an invalid upstream request ID')
    result = {
        'account_id': integer(account_id, 'account_id', 1),
        'upstream_request_id': request_id,
        'quota': integer(row.get('quota'), 'quota'),
        'type': 2,
    }
    for name in ('token_id', 'created_at'):
        if row.get(name) is not None:
            result[name] = integer(row[name], name)
    model = row.get('model_name')
    if model is not None:
        if not isinstance(model, str) or len(model) > 256:
            raise ReconcileError('Invalid consume model name')
        result['model_name'] = model
    if row.get('request_id') and row.get('upstream_request_id'):
        nested = row['upstream_request_id']
        if not isinstance(nested, str) or not REQUEST_ID.fullmatch(nested):
            raise ReconcileError('Invalid supplier downstream-hop request ID')
        result['provider_upstream_request_id'] = nested
    return result


def normalize_evidence(source):
    """Accept this tool's envelope or the retained November audit capture."""
    if isinstance(source, list):
        # Historical capture contains only a projection of the supplier response.
        logs = []
        for endpoint in source:
            if endpoint.get('path') != '/api/log/token' or endpoint.get('status') != 200:
                continue
            for row in endpoint.get('logs', []):
                value = normalize_log(row, endpoint.get('account_id'))
                if value is not None:
                    logs.append(value)
        return {'schema': 1, 'provider': PROVIDER, 'quota_per_yuan': QUOTA_PER_YUAN,
                'coverage': 'recent_only', 'logs': logs, 'source_sha256': content_hash(source)}
    if not isinstance(source, dict) or source.get('schema') != 1:
        raise ReconcileError('Unsupported supplier evidence schema')
    if source.get('provider') != PROVIDER or source.get('quota_per_yuan') != QUOTA_PER_YUAN:
        raise ReconcileError('Unapproved provider or quota unit')
    if source.get('coverage') not in ('recent_only', 'supplier_export'):
        raise ReconcileError('Evidence must state its historical coverage')
    result = {k: source[k] for k in ('schema', 'provider', 'quota_per_yuan', 'coverage')}
    result['source_sha256'] = content_hash(source)
    result['logs'] = []
    for row in source.get('logs', []):
        value = normalize_log(row, row.get('account_id'))
        if value is not None:
            result['logs'].append(value)
    return result


def build_plan(source, snapshot):
    """Return an exact plan; ambiguity or settled disagreement fails closed."""
    evidence = normalize_evidence(source)
    if not isinstance(snapshot, dict) or snapshot.get('schema') != 1:
        raise ReconcileError('Unsupported local snapshot schema')
    proof = {}
    for row in evidence['logs']:
        key = (row['account_id'], row['upstream_request_id'])
        old = proof.get(key)
        if old is not None and old != row:
            raise ReconcileError('Conflicting supplier evidence for one upstream request')
        proof[key] = row
    local = defaultdict(list)
    charge_ids = set()
    for charge in snapshot.get('charges', []):
        cid = integer(charge.get('id'), 'charge id', 1)
        if cid in charge_ids:
            raise ReconcileError('Duplicate local charge ID in snapshot')
        charge_ids.add(cid)
        aid = integer(charge.get('account_id'), 'local account_id', 1)
        integer(charge.get('user_id'), 'local user_id', 1)
        integer(charge.get('api_key_id'), 'local api_key_id', 1)
        if charge.get('upstream_request_id'):
            local[(aid, charge['upstream_request_id'])].append(charge)
    entries, unmatched, settled = [], [], 0
    totals = defaultdict(Decimal)
    for key, row in sorted(proof.items()):
        matches = local.get(key, [])
        if not matches:
            unmatched.append({'account_id': key[0], 'upstream_request_id': key[1]})
            continue
        if len(matches) != 1:
            raise ReconcileError('Upstream request maps to multiple local charges')
        charge = matches[0]
        yuan = Decimal(row['quota']) / QUOTA_PER_YUAN
        if charge.get('state') == 'settled':
            if amount(charge.get('amount')) != yuan:
                raise ReconcileError('Supplier amount disagrees with a settled local charge')
            settled += 1
            continue
        if charge.get('state') != 'pending' or charge.get('amount') is not None:
            raise ReconcileError('Matched local charge is not an unpriced pending request')
        started = charge.get('started_at')
        try:
            stamp = datetime.fromisoformat(started.replace('Z', '+00:00'))
            if stamp.tzinfo is None:
                raise ValueError('missing timezone')
        except (ValueError, AttributeError) as error:
            raise ReconcileError('Local request needs its original timestamp and timezone') from error
        # All identity fields are frozen. Apply must re-read and compare them.
        entry = {name: charge[name] for name in
                 ('id', 'request_id', 'api_key_id', 'user_id', 'account_id',
                  'upstream_request_id', 'started_at')}
        entry.update(quota=row['quota'], amount_yuan=format(yuan, 'f'),
                     supplier_log_sha256=content_hash(row))
        entries.append(entry)
        totals[charge['user_id']] += yuan
    return {'schema': 1, 'provider': PROVIDER, 'quota_per_yuan': QUOTA_PER_YUAN,
            'created_at': utcnow(), 'evidence_sha256': evidence['source_sha256'],
            'snapshot_sha256': content_hash(snapshot), 'coverage': evidence['coverage'],
            'entries': entries, 'already_settled': settled, 'unmatched_evidence': unmatched,
            'remaining_unpriced_requests': sum(c.get('state') != 'settled' for c in snapshot.get('charges', [])) - len(entries),
            'user_amounts_yuan': {str(k): format(v, 'f') for k, v in sorted(totals.items())},
            'total_amount_yuan': format(sum(totals.values(), Decimal(0)), 'f')}


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def remote_snapshot(host, account_ids, database='sub2api-postgres', after_id=0, limit=None):
    """Read credentials into memory and return a non-secret local charge snapshot."""
    ids = ','.join(str(integer(i, 'account ID', 1)) for i in account_ids)
    if not ids or not re.fullmatch(r'[A-Za-z0-9_.-]+', database):
        raise ReconcileError('Account IDs and a valid database container are required')
    integer(after_id, 'snapshot cursor')
    if limit is not None:
        integer(limit, 'snapshot batch size', 1)
    bound = (' AND id>' + str(after_id) + ' ORDER BY id LIMIT ' + str(limit)) if limit is not None else ' ORDER BY id'
    # The SSH command contains no credentials. The database connection parameters
    # come from docker inspect inside the remote process and are not printed.
    script = r'''
import base64,json,subprocess,zlib
container=CONTAINER
info=json.loads(subprocess.check_output(['docker','inspect',container]))[0]
env=dict(x.split('=',1) for x in info['Config']['Env'] if '=' in x)
query="""BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='15s';
SELECT json_build_object('section','account','id',id,'credentials',credentials) FROM accounts WHERE id IN(IDS) ORDER BY id;
SELECT json_build_object('section','charge','id',id,'request_id',request_id,'api_key_id',api_key_id,'user_id',user_id,'account_id',account_id,'upstream_request_id',upstream_request_id,'started_at',started_at,'amount',amount::text,'state',state) FROM experiment_quota_charges WHERE account_id IN(IDS)BOUND;
SELECT json_build_object('section','time','at',now()); COMMIT;"""
result=subprocess.run(['docker','exec','-i',container,'psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U',env.get('POSTGRES_USER','postgres'),'-d',env.get('POSTGRES_DB','sub2api')],input=query,text=True,encoding='utf-8',capture_output=True,timeout=25)
if result.returncode: raise SystemExit('Read-only billing snapshot failed')
data=[json.loads(line) for line in result.stdout.splitlines() if line.strip()]
print(base64.b64encode(zlib.compress(json.dumps(data).encode())).decode())
'''.replace('CONTAINER', repr(database)).replace('IDS', ids).replace('BOUND', bound)
    try:
        process = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=12',
                                  host, 'python3 -'], input=script, text=True, encoding='utf-8',
                                 capture_output=True, timeout=90)
        if process.returncode:
            raise ReconcileError('Read-only remote snapshot failed; no partial snapshot accepted')
        rows = json.loads(zlib.decompress(base64.b64decode(process.stdout)).decode())
    except (subprocess.TimeoutExpired, ValueError, zlib.error) as error:
        raise ReconcileError('Read-only remote snapshot failed') from error
    accounts = [{k: v for k, v in row.items() if k != 'section'} for row in rows
                if row.get('section') == 'account']
    charges = [{k: v for k, v in row.items() if k != 'section'} for row in rows
               if 'upstream_request_id' in row]
    observed_at = next((row['at'] for row in rows if 'at' in row), utcnow())
    return accounts, {'schema': 1, 'observed_at': observed_at, 'charges': charges}


def partitioned_snapshot(host, account_ids, directory, database='sub2api-postgres', resume=False,
                         batch_size=4000, read=remote_snapshot):
    """Persist bounded account/ID partitions before reading the next batch."""
    integer(batch_size, 'snapshot batch size', 1)
    folder = Path(directory)
    folder.mkdir(parents=True, exist_ok=True)
    credentials, charges, batches = {}, [], []
    seen = set()
    for account in account_ids:
        integer(account, 'snapshot account', 1)
        cursor = 0
        while True:
            target = folder / f'account-{account}-after-{cursor}.json'
            if resume and target.exists():
                snapshot = json.loads(target.read_text(encoding='utf-8'))
                if account not in credentials:
                    current, _ = read(host, [account], database, after_id=9223372036854775807, limit=1)
                    credentials.update({row['id']: row for row in current})
            else:
                current, snapshot = read(host, [account], database, after_id=cursor, limit=batch_size)
                credentials.update({row['id']: row for row in current})
                write_json(target, snapshot)
            rows = snapshot.get('charges', [])
            if snapshot.get('schema') != 1 or len(rows) > batch_size:
                raise ReconcileError('Invalid cached local snapshot partition')
            for row in rows:
                if row.get('account_id') != account or integer(row.get('id'), 'partition charge ID', 1) <= cursor or row['id'] in seen:
                    raise ReconcileError('Local snapshot partition identity or cursor conflict')
                seen.add(row['id'])
            charges.extend(rows)
            batches.append({'account_id': account, 'after_id': cursor, 'rows': len(rows),
                            'observed_at': snapshot.get('observed_at'), 'sha256': content_hash(snapshot)})
            if len(rows) < batch_size:
                break
            cursor = max(row['id'] for row in rows)
    return list(credentials.values()), {'schema': 1, 'captured_batches': batches, 'charges': charges}


def read_session_credential(path, line=None):
    """Read an explicitly supplied GlobalAI session access token, never cookies."""
    try:
        raw = Path(path).read_text(encoding='utf-8-sig')
        if line is not None:
            integer(line, 'credential line', 1)
            lines = raw.splitlines()
            if line > len(lines):
                raise ReconcileError('Credential file does not contain the selected line')
            raw = lines[line - 1]
        raw = raw.strip()
    except OSError as error:
        raise ReconcileError('Cannot read the supplied session credential file') from error
    if raw.startswith('{'):
        try:
            obj = json.loads(raw)
            raw = obj.get('access_token', obj.get('data', {}).get('access_token', ''))
        except (ValueError, AttributeError) as error:
            raise ReconcileError('Session credential JSON needs access_token') from error
    if isinstance(raw, str):
        bearer = re.fullmatch(r'(?i)(?:authorization\s*:\s*)?bearer\s+([^\s]+)', raw)
        if bearer:
            raw = bearer.group(1)
        elif line is not None:
            # A selected line may contain a human label copied with the header.
            # Accept exactly one complete JWT, never search other lines/files.
            tokens = re.findall(r'(?<![A-Za-z0-9_-])(eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)(?![A-Za-z0-9_-])', raw)
            if len(tokens) == 1:
                raw = tokens[0]
    if not isinstance(raw, str) or not raw or len(raw) > 16000 or any(c.isspace() for c in raw):
        raise ReconcileError('Invalid session access token in the supplied file')
    if raw.startswith('sk-'):
        raise ReconcileError('History needs a GlobalAI website session token, not a model API Key')
    return raw


def token_account_map(identity_source):
    """Bind supplier token IDs to accounts using retained current-Key evidence."""
    identities = {}
    for row in normalize_evidence(identity_source)['logs']:
        token = row.get('token_id')
        if not token:
            continue
        if token in identities and identities[token] != row['account_id']:
            raise ReconcileError('One supplier token maps to multiple local accounts')
        identities[token] = row['account_id']
    if not identities:
        raise ReconcileError('Historical import needs supplier token_id to local account evidence')
    return identities


def unix_timestamp(value):
    try:
        stamp = datetime.fromisoformat(value.replace('Z', '+00:00'))
    except (ValueError, AttributeError) as error:
        raise ReconcileError('History bounds need ISO timestamps with timezones') from error
    if stamp.tzinfo is None:
        raise ReconcileError('History bounds need ISO timestamps with timezones')
    return int(stamp.timestamp())


def fetch_history(access_token, identity_source, opener, start, end, max_pages=500,
                  page_size=100, delay=1.0, fetch=None, resume=None, token_name=None):
    """Read the exact authenticated usage-page API with bounded pagination.

    The time interval is fixed and every page is retained only after identity
    projection. Missing/expired auth or changing pagination is partial evidence,
    never an assertion that all old charges have been recovered.
    """
    fetch = fetch or fetch_json
    start_at, end_at = unix_timestamp(start), unix_timestamp(end)
    if start_at >= end_at or not 1 <= page_size <= 100 or not 1 <= max_pages <= 1000:
        raise ReconcileError('Invalid historical time range or page bound')
    identities = token_account_map(identity_source)
    result = {'schema': 1, 'provider': PROVIDER, 'quota_per_yuan': QUOTA_PER_YUAN,
              'created_at': utcnow(), 'coverage': 'supplier_export',
              'collection_complete': False, 'start_timestamp': start_at,
              'end_timestamp': end_at, 'identity_evidence_sha256': content_hash(identity_source),
              'pages': [], 'logs': [], 'unmapped_logs': [], 'unmapped_token_ids': {}}
    if token_name is not None:
        if not isinstance(token_name, str) or not token_name or len(token_name) > 256:
            raise ReconcileError('Invalid supplier token-name selector')
        result['token_name_sha256'] = hashlib.sha256(token_name.encode()).hexdigest()
    unknown_tokens = Counter()
    seen = set()
    expected_total, returned = None, 0
    first_page = 1
    if resume is not None:
        if (resume.get('schema') != 1 or resume.get('provider') != PROVIDER
                or resume.get('start_timestamp') != start_at or resume.get('end_timestamp') != end_at
                or resume.get('identity_evidence_sha256') != content_hash(identity_source)
                or resume.get('token_name_sha256') != result.get('token_name_sha256')
                or resume.get('stop_reason') not in ('session_auth_required', 'supplier_rate_limit',
                                                     'supplier_read_failed', 'max_pages_reached')):
            raise ReconcileError('Resume needs the same fixed range, identity evidence, and a safely interrupted history')
        result = json.loads(json.dumps(resume))
        result['pages'] = [row for row in result['pages'] if row.get('status') == 200]
        result.pop('stop_reason', None)
        result['resumed_at'] = utcnow()
        returned = integer(result.get('returned_rows'), 'prior returned_rows')
        first_page = len(result['pages']) + 1
        if result['pages']:
            expected_total = integer(result['pages'][0].get('total'), 'prior total')
        for row in result['logs'] + result['unmapped_logs']:
            seen.add((row['token_id'], row.get('upstream_request_id', row.get('request_id'))))
        unknown_tokens.update({int(k): v for k, v in result['unmapped_token_ids'].items()})
    for page in range(first_page, max_pages + 1):
        query = {'p': page, 'page_size': page_size, 'type': 2,
                 'start_timestamp': start_at, 'end_timestamp': end_at}
        if token_name is not None:
            query['token_name'] = token_name
        params = urllib.parse.urlencode(query)
        path = '/api/log/self?' + params
        response = fetch(opener, 'https://' + PROVIDER, path, access_token)
        info = {'page': page, 'status': response.get('status')}
        if response.get('retry_after'):
            info['retry_after'] = str(response['retry_after'])[:120]
        result['pages'].append(info)
        if response.get('status') != 200:
            result['stop_reason'] = ('session_auth_required' if response.get('status') in (401, 403)
                                     else 'supplier_rate_limit' if response.get('status') == 429
                                     else 'supplier_read_failed')
            break
        body = response.get('body')
        if not isinstance(body, dict) or body.get('success') is not True or not isinstance(body.get('data'), dict):
            raise ReconcileError('Unexpected authenticated GlobalAI history envelope')
        data = body['data']
        items = data.get('items')
        total = integer(data.get('total'), 'history total')
        if not isinstance(items, list) or len(items) > page_size:
            raise ReconcileError('Invalid authenticated GlobalAI history page')
        info.update(total=total, returned_rows=len(items))
        if expected_total is None:
            expected_total = total
        elif total != expected_total:
            result['stop_reason'] = 'history_total_changed'
            break
        if not items and returned < total:
            result['stop_reason'] = 'history_page_missing'
            break
        for row in items:
            if not isinstance(row, dict) or row.get('type') != 2:
                raise ReconcileError('History endpoint returned a non-consume row')
            # A supplier request identity must not appear on multiple pages.
            token = integer(row.get('token_id'), 'historical supplier token_id', 1)
            request = row.get('request_id')
            if not isinstance(request, str) or not REQUEST_ID.fullmatch(request):
                raise ReconcileError('History consume record has no valid request ID')
            identity = (token, request)
            if identity in seen:
                result['stop_reason'] = 'history_page_overlap'
                break
            seen.add(identity)
            created = integer(row.get('created_at'), 'historical created_at')
            if not start_at <= created <= end_at:
                raise ReconcileError('History endpoint ignored the fixed time range')
            if token not in identities:
                unknown_tokens[token] += 1
                allowed = ('type', 'request_id', 'quota', 'token_id', 'created_at', 'model_name')
                result['unmapped_logs'].append({name: row[name] for name in allowed if name in row})
            else:
                result['logs'].append(normalize_log(row, identities[token]))
        if result.get('stop_reason'):
            break
        returned += len(items)
        if returned > total:
            raise ReconcileError('History returned more rows than its declared total')
        if returned == total:
            result['collection_complete'] = True
            break
        if delay:
            time.sleep(delay)
    else:
        result['stop_reason'] = 'max_pages_reached'
    result['returned_rows'] = returned
    result['unmapped_token_ids'] = {str(k): v for k, v in sorted(unknown_tokens.items())}
    return result


def fetch_json(opener, origin, path, key):
    request = urllib.request.Request(origin + path, headers={'Authorization': 'Bearer ' + key,
                                                            'Accept': 'application/json'})
    try:
        with opener.open(request, timeout=15) as response:
            body = response.read(4 << 20)
            return {'status': response.status, 'body': json.loads(body)}
    except urllib.error.HTTPError as error:
        return {'status': error.code, 'retry_after': error.headers.get('Retry-After')}
    except (urllib.error.URLError, TimeoutError, ValueError):
        return {'status': None, 'error': 'supplier_read_failed'}


def apply_payload(source, plan):
    """Bind an exact reviewed plan to the complete retained supplier evidence."""
    if plan.get('evidence_sha256') != content_hash(source):
        raise ReconcileError('Reviewed plan no longer matches its supplier evidence')
    return {'schema': 1, 'evidence_base64': base64.b64encode(canonical(source)).decode(), 'plan': plan}


def execute_plan(host, source, plan, source_commit, backup=None, apply=False):
    """Run the deployed program's shared settlement implementation over SSH.

    Backup verification is performed remotely before any settlement. Its exact
    archive digest is passed as a non-secret guard; the database DSN exists only
    in the child process environment, never argv or a generated file.
    """
    if not re.fullmatch(r'[0-9a-f]{40}', source_commit):
        raise ReconcileError('The exact deployed source commit is required')
    if apply and (not isinstance(backup, dict) or set(backup) != {'path', 'sha256'}):
        raise ReconcileError('Apply needs the verified database backup archive path and SHA256')
    if backup:
        path = backup.get('path', '')
        if not re.fullmatch(r'/opt/sub2api/backups/[A-Za-z0-9._/-]+', path) or '/..' in path:
            raise ReconcileError('Backup archive must be within /opt/sub2api/backups')
        if not re.fullmatch(r'[0-9a-f]{64}', backup.get('sha256', '')):
            raise ReconcileError('The exact verified database archive digest is required')
    value = {'payload': apply_payload(source, plan), 'source_commit': source_commit,
             'backup': backup, 'apply': bool(apply)}
    encoded = base64.b64encode(zlib.compress(canonical(value))).decode()
    script = "encoded=" + repr(encoded) + '\n' + r'''
import base64,hashlib,json,os,pathlib,re,subprocess,urllib.parse,zlib
data=json.loads(zlib.decompress(base64.b64decode(encoded)))
version=subprocess.run(['/opt/sub2api/bin/sub2api','-version'],text=True,encoding='utf-8',capture_output=True,timeout=10)
match=re.search(r'commit:\s*([0-9a-f]{40})',version.stdout+version.stderr)
if version.returncode or not match or match.group(1)!=data['source_commit']:raise SystemExit('Deployed program source does not match the reviewed reconciliation program')
if data['apply']:
 archive=pathlib.Path(data['backup']['path']).resolve(strict=True)
 root=pathlib.Path('/opt/sub2api/backups').resolve(strict=True)
 if root not in archive.parents or not archive.is_file():raise SystemExit('Invalid verified backup archive')
 digest=hashlib.sha256()
 with archive.open('rb') as source:
  for chunk in iter(lambda:source.read(1024*1024),b''):digest.update(chunk)
 if digest.hexdigest()!=data['backup']['sha256']:raise SystemExit('Database backup digest changed')
 # pg_restore --list checks archive structure without restoring any database.
 with archive.open('rb') as source:
  listed=subprocess.run(['docker','exec','-i','sub2api-postgres','pg_restore','--list'],stdin=source,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=30)
 if listed.returncode:raise SystemExit('Database backup archive verification failed')
info=json.loads(subprocess.check_output(['docker','inspect','sub2api-postgres']))[0]
settings=dict(x.split('=',1) for x in info['Config']['Env'] if '=' in x)
check=subprocess.run(['docker','exec','-i','sub2api-postgres','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U',settings.get('POSTGRES_USER','postgres'),'-d',settings.get('POSTGRES_DB','sub2api')],input="BEGIN READ ONLY;SELECT to_regclass('experiment_quota_settlement_events') IS NOT NULL;COMMIT;",text=True,encoding='utf-8',capture_output=True,timeout=15)
if check.returncode or check.stdout.strip()!='t':raise SystemExit('Required accounting audit migration has not been deployed')
networks=info.get('NetworkSettings',{}).get('Networks',{})
addresses=[x.get('IPAddress') for x in networks.values() if x.get('IPAddress')]
if not addresses:raise SystemExit('Cannot locate the isolated PostgreSQL container network')
quote=urllib.parse.quote
dsn='postgresql://'+quote(settings.get('POSTGRES_USER','postgres'),safe='')+':'+quote(settings.get('POSTGRES_PASSWORD',''),safe='')+'@'+addresses[0]+':5432/'+quote(settings.get('POSTGRES_DB','sub2api'),safe='')+'?sslmode=disable'
env=dict(os.environ);env['SUB2API_RECONCILE_DSN']=dsn
args=['/opt/sub2api/bin/sub2api','reconcile-experiment','--expected-source-commit',data['source_commit']]
if data['apply']:args+=['--apply','--backup-sha256',data['backup']['sha256']]
result=subprocess.run(args,input=json.dumps(data['payload']),text=True,encoding='utf-8',env=env,capture_output=True,timeout=330)
if result.stdout:print(result.stdout,end='')
if result.returncode:
 # The Go CLI emits only bounded safe labels; no SQL driver/DSN errors.
 if result.stderr:print(result.stderr,end='',file=__import__('sys').stderr)
 raise SystemExit(result.returncode)
'''
    try:
        result = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=12', host, 'python3 -'],
                                input=script, text=True, encoding='utf-8', capture_output=True, timeout=360)
    except subprocess.TimeoutExpired as error:
        raise ReconcileError('Settlement command timed out; retain the plan and inspect durable progress before rerun') from error
    if result.returncode:
        raise ReconcileError('Remote settlement did not complete; retain the unchanged plan and inspect the server result')
    try:
        return json.loads(result.stdout)
    except ValueError as error:
        raise ReconcileError('Remote settlement returned no valid result') from error


def collect(accounts, opener, delay=1.0, fetch=fetch_json):
    """One bounded consume and usage read per unique current API credential."""
    evidence = {'schema': 1, 'provider': PROVIDER, 'quota_per_yuan': QUOTA_PER_YUAN,
                'created_at': utcnow(), 'coverage': 'recent_only', 'accounts': [],
                'endpoints': [], 'logs': []}
    seen = set()
    for account in accounts:
        aid = integer(account['id'], 'account_id', 1)
        credentials = account['credentials']
        parsed = urllib.parse.urlsplit(credentials.get('base_url', ''))
        if parsed.hostname != PROVIDER or parsed.scheme != 'https' or parsed.username or parsed.password:
            raise ReconcileError('Only explicit HTTPS GlobalAI accounts can be collected')
        key = credentials.get('api_key', '')
        if not isinstance(key, str) or not key:
            raise ReconcileError('GlobalAI account has no API credential')
        fingerprint = hashlib.sha256(key.encode()).hexdigest()
        if fingerprint in seen:
            raise ReconcileError('Two selected accounts share a Key; choose its authoritative account identity')
        seen.add(fingerprint)
        evidence['accounts'].append({'account_id': aid, 'credential_sha256': fingerprint})
        origin = parsed.scheme + '://' + parsed.netloc
        for path in ('/api/usage/token/', '/api/log/token'):
            response = fetch(opener, origin, path, key)
            safe = {'account_id': aid, 'path': path, 'status': response.get('status')}
            if response.get('retry_after'):
                safe['retry_after'] = str(response['retry_after'])[:120]
            body = response.get('body', {})
            data = body.get('data', body) if isinstance(body, dict) else None
            if response.get('status') == 200 and path == '/api/log/token':
                if not isinstance(data, list):
                    raise ReconcileError('Unexpected GlobalAI consume-log response')
                for row in data:
                    value = normalize_log(row, aid)
                    if value is not None:
                        evidence['logs'].append(value)
                safe['returned_rows'] = len(data)
            elif response.get('status') == 200 and isinstance(data, dict):
                fields = ('total_granted', 'total_used', 'total_available', 'unlimited_quota', 'expires_at')
                safe['usage'] = {k: data[k] for k in fields if k in data}
            evidence['endpoints'].append(safe)
            # Do not amplify supplier throttling with short retries. The caller
            # can retain partial evidence and rerun later after Retry-After.
            if response.get('status') == 429:
                evidence['stopped_on_rate_limit'] = True
                return evidence
            if delay:
                time.sleep(delay)
    return evidence


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    read = commands.add_parser('collect', help='Read recent supplier logs; never settles charges')
    read.add_argument('--host', required=True)
    read.add_argument('--accounts', nargs='+', type=int, required=True)
    read.add_argument('--database', default='sub2api-postgres')
    read.add_argument('--proxy')
    read.add_argument('--output', type=Path, required=True)
    read.add_argument('--resume-snapshot', action='store_true')
    history = commands.add_parser('fetch-history', help='Read website user history using an explicitly supplied session token')
    history.add_argument('--credential-file', type=Path, required=True)
    history.add_argument('--credential-line', type=int)
    history.add_argument('--identity-evidence', type=Path, required=True)
    history.add_argument('--snapshot', type=Path)
    history.add_argument('--start')
    history.add_argument('--end')
    history.add_argument('--resume', type=Path)
    history.add_argument('--token-name')
    history.add_argument('--max-pages', type=int, default=500)
    history.add_argument('--proxy')
    history.add_argument('--output', type=Path, required=True)
    plan = commands.add_parser('plan', help='Match exact supplier consume evidence with a local snapshot')
    plan.add_argument('--evidence', type=Path, required=True)
    plan.add_argument('--snapshot', type=Path, required=True)
    plan.add_argument('--output', type=Path, required=True)
    execute = commands.add_parser('execute', help='Verify or apply an unchanged exact plan through the deployed program')
    execute.add_argument('--host', required=True)
    execute.add_argument('--evidence', type=Path, required=True)
    execute.add_argument('--plan', type=Path, required=True)
    execute.add_argument('--source-commit', required=True)
    execute.add_argument('--backup-record', type=Path)
    execute.add_argument('--apply', action='store_true')
    execute.add_argument('--output', type=Path, required=True)
    snap = commands.add_parser('snapshot', help='Read local charge identities in resumable bounded partitions')
    snap.add_argument('--host', required=True)
    snap.add_argument('--accounts', nargs='+', type=int, required=True)
    snap.add_argument('--database', default='sub2api-postgres')
    snap.add_argument('--resume', action='store_true')
    snap.add_argument('--output', type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        if args.command == 'collect':
            accounts, snapshot = partitioned_snapshot(args.host, args.accounts, args.output / 'local-batches',
                                                       args.database, args.resume_snapshot)
            opener = urllib.request.build_opener(urllib.request.ProxyHandler(
                {'http': args.proxy, 'https': args.proxy} if args.proxy else {}))
            evidence = collect(accounts, opener)
            write_json(args.output / 'local-snapshot.json', snapshot)
            write_json(args.output / 'supplier-evidence.json', evidence)
            result = build_plan(evidence, snapshot)
            write_json(args.output / 'settlement-plan.json', result)
        elif args.command == 'fetch-history':
            token = read_session_credential(args.credential_file, args.credential_line)
            identities = json.loads(args.identity_evidence.read_text(encoding='utf-8'))
            snapshot = json.loads(args.snapshot.read_text(encoding='utf-8')) if args.snapshot else None
            opener = urllib.request.build_opener(urllib.request.ProxyHandler(
                {'http': args.proxy, 'https': args.proxy} if args.proxy else {}))
            previous = json.loads(args.resume.read_text(encoding='utf-8')) if args.resume else None
            start, end = args.start, args.end
            if previous:
                start = datetime.fromtimestamp(previous['start_timestamp'], timezone.utc).isoformat()
                end = datetime.fromtimestamp(previous['end_timestamp'], timezone.utc).isoformat()
            evidence = fetch_history(token, identities, opener, start, end, args.max_pages,
                                     resume=previous, token_name=args.token_name)
            write_json(args.output / 'supplier-history.json', evidence)
            if snapshot is None:
                print(json.dumps({'status': 'collected', 'complete': evidence['collection_complete'],
                                  'returned_rows': evidence['returned_rows'], 'stop_reason': evidence.get('stop_reason')}, ensure_ascii=True))
                return 0
            result = build_plan(evidence, snapshot)
            write_json(args.output / 'settlement-plan.json', result)
        elif args.command == 'snapshot':
            accounts, result = partitioned_snapshot(args.host, args.accounts, args.output / 'local-batches',
                                                     args.database, args.resume)
            del accounts  # The command exports no credentials or credential fingerprints.
            write_json(args.output / 'local-snapshot.json', result)
            print(json.dumps({'status': 'snapshot_complete', 'charges': len(result['charges'])}, ensure_ascii=True))
            return 0
        elif args.command == 'execute':
            result = execute_plan(args.host, json.loads(args.evidence.read_text(encoding='utf-8')),
                                  json.loads(args.plan.read_text(encoding='utf-8')), args.source_commit,
                                  json.loads(args.backup_record.read_text(encoding='utf-8')) if args.backup_record else None,
                                  args.apply)
            write_json(args.output, result)
            print(json.dumps(result, ensure_ascii=True))
            return 0
        else:
            result = build_plan(json.loads(args.evidence.read_text(encoding='utf-8')),
                                json.loads(args.snapshot.read_text(encoding='utf-8')))
            write_json(args.output, result)
        print(json.dumps({'status': 'planned', 'entries': len(result['entries']),
                          'total_amount_yuan': result['total_amount_yuan'],
                          'remaining_unpriced_requests': result['remaining_unpriced_requests']}, ensure_ascii=True))
        return 0
    except (ReconcileError, OSError, ValueError) as error:
        # Do not include raw HTTP bodies, SSH stderr, or credentials in errors.
        print('GlobalAI reconciliation failed: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
