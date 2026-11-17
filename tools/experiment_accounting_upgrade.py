"""Pinned GlobalAI accounting repair; preserve evidence and never restore a database."""
import argparse
import copy
from datetime import datetime, timedelta, timezone
from decimal import Decimal, InvalidOperation
import json
from pathlib import Path
import re
import subprocess

from api_fallback_upgrade import (APIFallbackRuntime, APPLICATION as STANDBY_APPLICATION,
                                  BASELINE, DATABASE_MIGRATIONS as STANDBY_DATABASE_MIGRATIONS,
                                  MIGRATIONS as STANDBY_MIGRATIONS)
from delivery_core import DeliveryError, digest
from release_server import assert_preserved, deploy, stage_release

# Complete reviewed application; later tool/document commits may only retain its
# exact frontend/backend tree. Program identity still comes from its real build.
APPLICATION = '5e7a4cdecf77eec7b107771c63e203d7d92dc576'
MIGRATIONS = {**STANDBY_MIGRATIONS,
              '272_experiment_accounting_guard.sql': 'd44ccb7429fed949416b4c1c2349b5e639a99f2a79060ba91c7228f45b6bbf54'}
DATABASE_MIGRATIONS = {**STANDBY_DATABASE_MIGRATIONS,
                       '272_experiment_accounting_guard.sql': 'f75901a522e7ce0b78c94f3bad166cc2d20375532c57c4617bc7133da0450d81'}
TABLES = ('experiment_quota_charges', 'experiment_quota_attempts', 'experiment_quota_settlement_events')
CHARGE_RETRY_FIELDS = {'attempts', 'next_retry_at', 'last_error', 'updated_at'}
USAGE_COST_FIELDS = ('input_cost', 'total_cost', 'actual_cost')


def contract_for_release():
    return dict(name='experiment-accounting-v1', baseline_commit=BASELINE, source_commit=APPLICATION,
                migrations=MIGRATIONS.copy(), database_migrations=DATABASE_MIGRATIONS.copy())


def _instant(value):
    try:
        parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
        if parsed.tzinfo is None:
            raise ValueError('timezone required')
        return parsed
    except (TypeError, AttributeError, ValueError):
        raise DeliveryError('Accounting evidence requires an explicit timestamp and timezone') from None


def _amount(value):
    try:
        number = Decimal(str(value))
        if not number.is_finite() or number < 0:
            raise InvalidOperation
        return number
    except (InvalidOperation, TypeError, ValueError):
        raise DeliveryError('Invalid accounting amount in persistence evidence') from None


def _indexed(rows, field='id'):
    result = {row[field]: row for row in rows}
    if len(result) != len(rows):
        raise DeliveryError('Duplicate identities in accounting snapshot')
    return result


def _same_time(left, right):
    return _instant(left) == _instant(right)


def _wallet_start(row, at):
    start = _instant(row['window_start'])
    if row['period'] == 'month':
        local = at.astimezone(timezone(timedelta(hours=8)))
        month = local.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        return max(start, month)
    if row['period'] == 'week':
        return start + max(0, (at - start) // timedelta(days=7)) * timedelta(days=7)
    raise DeliveryError('Unrecognized experiment wallet period')


def explain_experiment_settlements(before, after):
    """Return a copy whose monetary deltas have all been proved by immutable events."""
    result = copy.deepcopy(after)
    old, new = before['experiment'], after['experiment']
    if old.get('policy') != new.get('policy'):
        raise DeliveryError('Experiment allowance policy changed during replacement')
    prior_events = _indexed(old['experiment_quota_settlement_events'])
    events = _indexed(new['experiment_quota_settlement_events'])
    for identity, row in prior_events.items():
        if events.get(identity) != row:
            raise DeliveryError('Original experiment settlement evidence changed or disappeared')
    added = [row for identity, row in events.items() if identity not in prior_events]
    additions = _indexed(added, 'charge_id')
    old_charges, charges = _indexed(old['experiment_quota_charges']), _indexed(new['experiment_quota_charges'])
    reference = _instant(before['as_of'])
    for event in added:
        charge = charges.get(event['charge_id'])
        if charge is None or charge['state'] != 'settled' or _amount(charge['amount']) != _amount(event['amount']):
            raise DeliveryError('Settlement evidence does not match the final experiment charge')
        if _instant(event['created_at']) < reference or not re.fullmatch(r'[0-9a-f]{64}', event.get('evidence_sha256', '')):
            raise DeliveryError('New settlement lacks current immutable source evidence')
        if not event.get('evidence_source') or 'original_usage_costs' not in event:
            raise DeliveryError('Settlement must preserve its source and original usage costs')
        for field in ('request_id', 'api_key_id', 'user_id', 'account_id', 'upstream_request_id'):
            if event[field] != charge[field]:
                raise DeliveryError('Settlement charge identity changed: ' + field)
        if not _same_time(event['started_at'], charge['started_at']):
            raise DeliveryError('Settlement moved the original request time')
        prior = old_charges.get(event['charge_id'])
        if prior is not None and (prior['state'] not in ('pending', 'unresolved')
                                  or event['previous_state'] != prior['state']
                                  or event.get('previous_amount') != prior.get('amount')):
            raise DeliveryError('Settlement cannot rewrite an original settled charge')
    for identity, row in old_charges.items():
        following = charges.get(identity)
        if following is None:
            raise DeliveryError('Original experiment charge disappeared')
        immutable = set(row) - CHARGE_RETRY_FIELDS - {'state', 'amount'}
        if any(row[field] != following.get(field) for field in immutable):
            raise DeliveryError('Original experiment request or billing identity changed')
        if (row['state'], row.get('amount')) != (following['state'], following.get('amount')) and identity not in additions:
            raise DeliveryError('Experiment charge changed without a new settlement event')
    for identity, charge in charges.items():
        if identity not in old_charges and charge['state'] == 'settled' and identity not in additions:
            raise DeliveryError('New settled experiment charge has no immutable settlement evidence')

    old_usage, usage = _indexed(old['usage']), _indexed(new['usage'])
    by_request = {(row['request_id'], row['api_key_id']): row for row in added}
    for identity, row in old_usage.items():
        following = usage.get(identity)
        if following is None or any(row[field] != following.get(field) for field in
                                     ('request_id', 'api_key_id', 'immutable_hash')):
            raise DeliveryError('Original usage metadata, tokens or timestamps changed')
        event = by_request.get((row['request_id'], row['api_key_id']))
        changed_price = any(row[field] != following.get(field) for field in USAGE_COST_FIELDS)
        if event is not None or changed_price:
            if event is None or any(_amount(row[field]) != 0 or _amount(following[field]) != _amount(event['amount'])
                                    for field in USAGE_COST_FIELDS):
                raise DeliveryError('Usage price changed without the same zero-to-known settlement')

    for table in ('experiment_quota_attempts',):
        prior, current = _indexed(old[table]), _indexed(new[table])
        immutable = ('id', 'request_id', 'api_key_id', 'user_id', 'account_id', 'started_at', 'created_at')
        for identity, row in prior.items():
            if identity not in current or any(row[field] != current[identity].get(field) for field in immutable):
                raise DeliveryError('A durable experiment attempt disappeared or changed identity')

    old_anchors = {(row['id'], row['purpose']): row for row in before['immutable']['anchors']}
    for following in result['immutable']['anchors']:
        prior = old_anchors.get((following['id'], following['purpose']))
        if following['purpose'] != 'experiment' or prior is None:
            continue
        user_events = [row for row in added if row['user_id'] == following['id']]
        active_start = _wallet_start(prior, reference)
        delta = sum((_amount(row['amount']) for row in user_events if _instant(row['started_at']) >= active_start), Decimal(0))
        expected_start = active_start if delta else _instant(prior['window_start'])
        expected_used = (_amount(prior['used_amount']) if active_start == _instant(prior['window_start']) else Decimal(0)) + delta
        if not delta:
            expected_used = _amount(prior['used_amount'])
        # Legacy wallets are float8; compare at the established eight-decimal
        # billing unit rather than accepting binary floating-point residue as
        # a real correction. Original raw values are retained in the evidence.
        precision = Decimal('0.00000001')
        if (_amount(following['used_amount']).quantize(precision) != expected_used.quantize(precision)
                or _instant(following['window_start']) != expected_start):
            raise DeliveryError('Experiment wallet delta is not exactly explained by same-user, original-period settlement evidence')
        following['used_amount'], following['window_start'] = prior['used_amount'], prior['window_start']
    return result


class ExperimentAccountingUpgrade:
    rollback_block_reason = 'Experiment attempts or settlement evidence exist; keep the compatible program and fix forward. Never restore an older database.'

    def __init__(self, contract):
        hashes = [*MIGRATIONS.values(), *DATABASE_MIGRATIONS.values()]
        if (contract != contract_for_release() or not re.fullmatch(r'[0-9a-f]{40}', APPLICATION)
                or len(MIGRATIONS) != 3 or any(not re.fullmatch(r'[0-9a-f]{64}', value) for value in hashes)):
            raise DeliveryError('Only the fixed, reviewed experiment accounting repair is supported')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('Reviewed accounting source ancestry verification failed') from None
        if baseline != BASELINE:
            raise DeliveryError('Unexpected live source baseline')
        git('merge-base', '--is-ancestor', BASELINE, APPLICATION)
        git('merge-base', '--is-ancestor', STANDBY_APPLICATION, APPLICATION)
        git('merge-base', '--is-ancestor', APPLICATION, source)
        if git('diff', '--name-only', APPLICATION, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application differs from the reviewed accounting repair')
        changes = git('diff', '--name-status', baseline, source, '--', 'backend/migrations').splitlines()
        if sorted(changes) != sorted('A\tbackend/migrations/' + name for name in MIGRATIONS):
            raise DeliveryError('Only the three reviewed additive migrations are accepted')
        for name, checksum in MIGRATIONS.items():
            if digest(Path(root) / 'backend/migrations' / name) != checksum:
                raise DeliveryError('Reviewed accounting migration changed: ' + name)

    def validate(self, manifest):
        if (manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract
                or manifest.get('baseline_commit') != BASELINE):
            raise DeliveryError('Explicit pinned experiment accounting manifest required')

    def assert_preserved(self, before, after):
        prior = {row['filename'] for row in before['migrations']}
        added = [row for row in after['migrations'] if row['filename'] not in prior]
        if {row['filename']: row['checksum'] for row in added} != DATABASE_MIGRATIONS:
            raise DeliveryError('Unexpected accounting migration addition or database checksum')
        adjusted = explain_experiment_settlements(before, after)
        adjusted['migrations'] = [row for row in after['migrations'] if row['filename'] in prior]
        assert_preserved(before, adjusted)
        if before['access'] != after['access'] or after['ultrafast_enabled']:
            raise DeliveryError('Existing access or Ultra Fast configuration changed')
        if before['fallback'] != after['fallback'] or any(
                row.get('enabled') or row.get('config', {}).get('enabled')
                for row in after['fallback']['api_fallback_policies']):
            raise DeliveryError('Accounting repair must preserve disabled standby and all prior paid evidence')

    def can_rollback(self, runtime):
        fallback, experiment = runtime.fallback_snapshot(), runtime.experiment_snapshot()
        return not (fallback['api_fallback_attempts'] or fallback['api_fallback_requests']
                    or experiment['experiment_quota_attempts'] or experiment['experiment_quota_settlement_events'])


class ExperimentAccountingRuntime(APIFallbackRuntime):
    def _command(self):
        return ['docker', 'exec', '-i', self.database, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                '-v', 'FETCH_COUNT=1000', '-U', self.db_env['POSTGRES_USER'], '-d', self.db_env['POSTGRES_DB']]

    def _tables(self):
        return json.loads(subprocess.check_output(self._command(), input="SELECT COALESCE(json_agg(tablename),'[]') FROM pg_tables WHERE schemaname='public';", text=True, timeout=20))

    @staticmethod
    def _experiment_queries(names, prefix=''):
        sql = []
        for table in TABLES:
            if table in names:
                sql.append("SELECT json_build_array('" + prefix + table + "',to_jsonb(t)) FROM " + table + ' t ORDER BY id;')
        if 'experiment_quota_charges' in names:
            sql.append("SELECT json_build_array('" + prefix + "usage',json_build_object('id',u.id,'request_id',u.request_id," + """
 'api_key_id',u.api_key_id,'input_cost',u.input_cost::text,'total_cost',u.total_cost::text,'actual_cost',u.actual_cost::text,
 'immutable_hash',md5((to_jsonb(u)-ARRAY['input_cost','total_cost','actual_cost'])::text)))
 FROM usage_logs u WHERE EXISTS(SELECT 1 FROM experiment_quota_charges c
 WHERE c.request_id=u.request_id AND c.api_key_id=u.api_key_id) ORDER BY u.id;""")
        sql.append("SELECT json_build_array('" + prefix + "policy',json_build_object('key',key,'value',value)) FROM settings WHERE key='purpose_allowance_policy';")
        return sql

    def experiment_snapshot(self):
        from quota_snapshot import stream_rows
        result = {table: [] for table in TABLES}
        result.update(usage=[], policy=[])
        sql = ["BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='20s'; SET LOCAL jit=off;"]
        sql += self._experiment_queries(self._tables())
        sql.append('COMMIT;')
        for line in stream_rows(self._command(), '\n'.join(sql)):
            if line.strip():
                key, row = json.loads(line)
                result[key].append(row)
        return result

    def snapshot(self, as_of):
        # All wallets, charges and explanatory events must share one MVCC
        # snapshot. Independent SELECT transactions can straddle a settlement.
        from api_fallback_upgrade import TABLES as FALLBACK_TABLES
        from quota_snapshot import build_snapshot_script, stream_rows
        from upstream_additive_upgrade import ACCESS_QUERY
        stamp = _instant(as_of).isoformat()
        names = self._tables()
        optional = set(names) & {'coding_quota_capacity_events', 'personal_quota_resets', 'personal_quota_reset_requests'}
        script, result = build_snapshot_script(stamp, optional)
        result['experiment'] = {table: [] for table in TABLES}
        result['experiment'].update(usage=[], policy=[])
        result['fallback'] = {table: [] for table in (*FALLBACK_TABLES, 'api_fallback_requests')}
        sections = {key: result[key] for key in ('requests', 'clocks', 'migrations')}
        sections.update({'immutable.' + key: rows for key, rows in result['immutable'].items()})
        sections.update({'durable_ids.' + key: rows for key, rows in result['durable_ids'].items()})
        sections.update({'experiment.' + key: rows for key, rows in result['experiment'].items()})
        sections.update({'fallback.' + key: rows for key, rows in result['fallback'].items()})
        sql = [script.rsplit('COMMIT;', 1)[0]]
        access = ACCESS_QUERY[ACCESS_QUERY.index('SELECT json_build_object('):].rsplit('COMMIT;', 1)[0].strip().removesuffix(';')
        sql.append("SELECT json_build_array('access',t.value) FROM (" + access + ') AS t(value);')
        for table in FALLBACK_TABLES:
            if table in names:
                sql.append("SELECT json_build_array('fallback." + table + "',to_jsonb(t)) FROM " + table + ' t ORDER BY to_jsonb(t)::text;')
        if 'api_fallback_attempts' in names:
            sql.append("SELECT json_build_array('fallback.api_fallback_requests',to_jsonb(q)) FROM coding_quota_requests q WHERE metering_source='api_fallback' ORDER BY id;")
        sql += self._experiment_queries(names, 'experiment.')
        sql.append('COMMIT;')
        for line in stream_rows(self._command(), '\n'.join(sql)):
            if line.strip():
                key, row = json.loads(line)
                if key == 'access':
                    result['access'] = row
                    result['ultrafast_enabled'] = result['access'].pop('ultrafast_enabled')
                else:
                    sections[key].append(row)
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    package = commands.add_parser('package')
    for name in ('output', 'destination'):
        package.add_argument('--' + name, type=Path, required=True)
    package.add_argument('--version', required=True)
    package.add_argument('--baseline-sha256', required=True)
    package.add_argument('--baseline-binary', type=Path)
    package.add_argument('--full-upload', action='store_true')
    server = commands.add_parser('deploy')
    server.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    server.add_argument('--release', type=Path, required=True)
    server.add_argument('--stage', action='store_true')
    server.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if args.command == 'package':
        from release_package import package_release
        result = package_release(Path(__file__).resolve().parents[1], args.output, args.destination,
                                 args.output / 'sub2api-linux-amd64', args.version, args.baseline_sha256, BASELINE,
                                 baseline_binary=args.baseline_binary, full_upload=args.full_upload,
                                 upgrade_contract=contract_for_release())
    else:
        manifest = json.loads((args.release / 'manifest.json').read_text())
        policy = ExperimentAccountingUpgrade(manifest.get('upgrade_contract', {}))
        policy.validate(manifest)
        if args.stage:
            stage_release(args.root, args.release)
            return
        result = deploy(args.root, args.release, ExperimentAccountingRuntime(args.root, args.release),
                        apply=args.apply, upgrade=policy)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
