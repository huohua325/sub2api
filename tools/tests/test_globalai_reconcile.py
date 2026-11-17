"""Exact historical settlement planning and secret-free bounded collection."""
from copy import deepcopy
from decimal import Decimal
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location('globalai_reconcile', Path(__file__).parents[1] / 'globalai_reconcile.py')
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


def evidence(logs=None):
    return {'schema': 1, 'provider': 'globalai.vip', 'quota_per_yuan': 500000,
            'coverage': 'supplier_export', 'logs': logs if logs is not None else [
                {'account_id': 8, 'type': 2, 'request_id': 'up-1', 'quota': 10247,
                 'token_id': 100, 'created_at': 1794934956}]}


def charge(**changes):
    row = {'id': 1, 'request_id': 'local-1', 'api_key_id': 43, 'user_id': 3,
           'account_id': 8, 'upstream_request_id': 'up-1',
           'started_at': '2026-11-01T00:00:00+08:00', 'state': 'pending', 'amount': None}
    row.update(changes)
    return row


def snapshot(charges=None):
    return {'schema': 1, 'charges': charges if charges is not None else [charge()]}


class PlanTests(unittest.TestCase):
    def test_exact_cost_and_original_identity(self):
        plan = MODULE.build_plan(evidence(), snapshot())
        self.assertEqual(plan['total_amount_yuan'], '0.020494')
        self.assertEqual(plan['entries'][0]['started_at'], '2026-11-01T00:00:00+08:00')
        self.assertEqual(plan['entries'][0]['user_id'], 3)
        self.assertEqual(plan['entries'][0]['quota'], 10247)
        self.assertEqual(len(plan['evidence_sha256']), 64)

    def test_globalai_request_id_precedes_its_provider_hop_request_id(self):
        src = evidence()
        src['logs'][0]['upstream_request_id'] = 'different-provider-hop'
        plan = MODULE.build_plan(src, snapshot())
        self.assertEqual(len(plan['entries']), 1)
        self.assertEqual(plan['entries'][0]['upstream_request_id'], 'up-1')
        normalized = MODULE.normalize_evidence(src)['logs'][0]
        self.assertEqual(normalized['provider_upstream_request_id'], 'different-provider-hop')
        self.assertEqual(MODULE.normalize_log(normalized, 8)['upstream_request_id'], 'up-1')

    def test_multikey_multiuser_totals_never_allocate_account_balance(self):
        logs = evidence()['logs'] + [{'account_id': 15, 'type': 2, 'request_id': 'up-2', 'quota': 999999}]
        plan = MODULE.build_plan(evidence(logs), snapshot([charge(), charge(id=2, account_id=15,
                                                user_id=8, upstream_request_id='up-2')]))
        self.assertEqual(plan['user_amounts_yuan'], {'3': '0.020494', '8': '1.999998'})
        self.assertEqual(Decimal(plan['total_amount_yuan']), Decimal('2.020492'))

    def test_settled_equal_is_skipped(self):
        plan = MODULE.build_plan(evidence(), snapshot([charge(state='settled', amount='0.020494')]))
        self.assertEqual(plan['already_settled'], 1)
        self.assertEqual(plan['entries'], [])

    def test_settled_difference_is_not_overwritten(self):
        with self.assertRaisesRegex(MODULE.ReconcileError, 'settled'):
            MODULE.build_plan(evidence(), snapshot([charge(state='settled', amount='0.02')]))

    def test_unmatched_evidence_is_not_assigned_to_user(self):
        plan = MODULE.build_plan(evidence(), snapshot([charge(account_id=13)]))
        self.assertEqual(plan['entries'], [])
        self.assertEqual(len(plan['unmatched_evidence']), 1)
        self.assertEqual(plan['remaining_unpriced_requests'], 1)

    def test_duplicate_exact_evidence_only_counts_once(self):
        logs = evidence()['logs'] * 2
        self.assertEqual(len(MODULE.build_plan(evidence(logs), snapshot())['entries']), 1)

    def test_conflicting_duplicate_evidence_rejected(self):
        logs = evidence()['logs'] + [{**evidence()['logs'][0], 'quota': 999}]
        with self.assertRaisesRegex(MODULE.ReconcileError, 'Conflicting'):
            MODULE.build_plan(evidence(logs), snapshot())

    def test_one_upstream_request_multiple_users_is_ambiguous(self):
        with self.assertRaisesRegex(MODULE.ReconcileError, 'multiple local'):
            MODULE.build_plan(evidence(), snapshot([charge(), charge(id=2, user_id=8)]))

    def test_duplicate_local_charge_id_rejected(self):
        with self.assertRaisesRegex(MODULE.ReconcileError, 'Duplicate local'):
            MODULE.build_plan(evidence(), snapshot([charge(), charge()]))

    def test_pending_with_price_or_unresolved_state_rejected(self):
        for change in ({'amount': '1'}, {'state': 'unresolved'}):
            with self.subTest(change=change), self.assertRaises(MODULE.ReconcileError):
                MODULE.build_plan(evidence(), snapshot([charge(**change)]))

    def test_original_timestamp_required(self):
        for stamp in (None, 'wrong', '2026-11-01T00:00:00'):
            with self.subTest(stamp=stamp), self.assertRaises(MODULE.ReconcileError):
                MODULE.build_plan(evidence(), snapshot([charge(started_at=stamp)]))

    def test_zero_consume_amount_can_settle(self):
        src = evidence(); src['logs'][0]['quota'] = 0
        self.assertEqual(MODULE.build_plan(src, snapshot())['entries'][0]['amount_yuan'], '0')

    def test_non_consume_logs_do_not_become_costs(self):
        src = evidence(); src['logs'][0]['type'] = 1
        self.assertEqual(MODULE.build_plan(src, snapshot())['entries'], [])

    def test_invalid_quota_and_request_id_rejected(self):
        for changes in ({'quota': -1}, {'quota': True}, {'quota': 1.5}, {'request_id': ''},
                        {'request_id': "x';DROP TABLE users"}):
            src = evidence(); src['logs'][0].update(changes)
            with self.subTest(changes=changes), self.assertRaises(MODULE.ReconcileError):
                MODULE.build_plan(src, snapshot())

    def test_currency_provider_and_coverage_are_explicit(self):
        for changes in ({'provider': 'other.example'}, {'quota_per_yuan': 1}, {'coverage': 'complete_assumed'}):
            src = evidence(); src.update(changes)
            with self.subTest(changes=changes), self.assertRaises(MODULE.ReconcileError):
                MODULE.build_plan(src, snapshot())

    def test_legacy_snapshot_capture_is_supported(self):
        src = [{'account_id': 8, 'path': '/api/log/token', 'status': 200,
                'logs': evidence()['logs']}]
        self.assertEqual(MODULE.build_plan(src, snapshot())['coverage'], 'recent_only')

    def test_pending_not_in_recent_window_stays_unresolved(self):
        plan = MODULE.build_plan(evidence(), snapshot([charge(), charge(id=2, upstream_request_id='older')]))
        self.assertEqual(plan['remaining_unpriced_requests'], 1)


class CollectionTests(unittest.TestCase):
    def account(self, **changes):
        row = {'id': 8, 'credentials': {'base_url': 'https://globalai.vip/v1', 'api_key': 'sk-private-never-save'}}
        row.update(changes)
        return row

    def test_projection_discards_sensitive_fields_and_never_serializes_key(self):
        def fetch(_opener, _origin, path, _key):
            if path == '/api/log/token':
                row = {**evidence()['logs'][0], 'api_key': 'sk-leaked', 'content': 'private prompt',
                       'ip': '10.0.0.1', 'username': 'private user'}
                return {'status': 200, 'body': {'success': True, 'data': [row]}}
            return {'status': 200, 'body': {'total_used': 999, 'api_key': 'sk-leaked'}}
        result = MODULE.collect([self.account()], None, delay=0, fetch=fetch)
        raw = json.dumps(result)
        for secret in ('sk-private-never-save', 'sk-leaked', 'private prompt', '10.0.0.1', 'private user'):
            self.assertNotIn(secret, raw)
        self.assertEqual(result['logs'][0]['quota'], 10247)

    def test_rate_limit_stops_without_retry_or_reading_more_accounts(self):
        calls = []
        def fetch(_opener, _origin, path, _key):
            calls.append(path)
            return {'status': 429, 'retry_after': '120'}
        result = MODULE.collect([self.account(), self.account(id=13)], None, delay=0, fetch=fetch)
        self.assertEqual(len(calls), 1)
        self.assertTrue(result['stopped_on_rate_limit'])
        self.assertEqual(result['endpoints'][0]['retry_after'], '120')

    def test_only_exact_https_globalai_origin_allowed(self):
        for url in ('http://globalai.vip', 'https://globalai.vip.attacker.test',
                    'https://key@globalai.vip', 'https://example.test'):
            row = self.account(); row['credentials']['base_url'] = url
            with self.subTest(url=url), self.assertRaises(MODULE.ReconcileError):
                MODULE.collect([row], None, delay=0)

    def test_same_key_multiple_account_ids_needs_identity_resolution(self):
        def fetch(*args):
            return {'status': 200, 'body': [] if args[2] == '/api/log/token' else {}}
        # Use the expected wrapped list for log responses.
        def wrapped(*args):
            return {'status': 200, 'body': {'data': [] if args[2] == '/api/log/token' else {}}}
        with self.assertRaisesRegex(MODULE.ReconcileError, 'share a Key'):
            MODULE.collect([self.account(), self.account(id=13)], None, delay=0, fetch=wrapped)

    def test_invalid_supplier_structure_is_not_success(self):
        def fetch(*args):
            return {'status': 200, 'body': {'data': {'unexpected': 'shape'}}}
        with self.assertRaises(MODULE.ReconcileError):
            MODULE.collect([self.account()], None, delay=0, fetch=fetch)


class HistoryTests(unittest.TestCase):
    start = '2026-11-01T00:00:00+08:00'
    end = '2026-11-19T00:00:00+08:00'

    def row(self, request='up-1', token=100):
        return {'type': 2, 'request_id': request, 'token_id': token, 'quota': 100,
                'created_at': 1794934956, 'content': 'private prompt', 'api_key': 'never-save'}

    def response(self, rows, total=1):
        return {'status': 200, 'body': {'success': True, 'data': {'items': rows, 'total': total}}}

    def test_paginated_history_preserves_only_bound_consume_evidence(self):
        responses = [self.response([self.row()], 2), self.response([self.row('up-2')], 2)]
        paths = []
        def fetch(_opener, origin, path, token):
            self.assertEqual(origin, 'https://globalai.vip')
            self.assertEqual(token, 'private-session-token')
            paths.append(path)
            return responses.pop(0)
        result = MODULE.fetch_history('private-session-token', evidence(), None, self.start, self.end,
                                      page_size=1, delay=0, fetch=fetch)
        self.assertTrue(result['collection_complete'])
        self.assertEqual(result['returned_rows'], 2)
        self.assertEqual(len(result['logs']), 2)
        self.assertIn('/api/log/self?p=2', paths[1])
        raw = json.dumps(result)
        for text in ('private-session-token', 'private prompt', 'never-save'):
            self.assertNotIn(text, raw)

    def test_unknown_supplier_token_is_retained_without_local_assignment(self):
        result = MODULE.fetch_history('token', evidence(), None, self.start, self.end, delay=0,
                                      fetch=lambda *a: self.response([self.row(token=999)]))
        self.assertEqual(result['logs'], [])
        self.assertEqual(result['unmapped_token_ids'], {'999': 1})
        self.assertEqual(result['unmapped_logs'][0]['token_id'], 999)
        self.assertNotIn('account_id', result['unmapped_logs'][0])

    def test_session_expiration_and_429_are_partial_and_never_retried(self):
        for status, reason in ((401, 'session_auth_required'), (429, 'supplier_rate_limit')):
            calls = []
            def fetch(*args):
                calls.append(args)
                return {'status': status}
            result = MODULE.fetch_history('token', evidence(), None, self.start, self.end, delay=0, fetch=fetch)
            self.assertFalse(result['collection_complete'])
            self.assertEqual(result['stop_reason'], reason)
            self.assertEqual(len(calls), 1)

    def test_mutating_total_is_not_declared_complete(self):
        responses = [self.response([self.row()], 2), self.response([self.row('up-2')], 3)]
        result = MODULE.fetch_history('token', evidence(), None, self.start, self.end,
                                      page_size=1, delay=0, fetch=lambda *a: responses.pop(0))
        self.assertFalse(result['collection_complete'])
        self.assertEqual(result['stop_reason'], 'history_total_changed')

    def test_repeated_page_cannot_fake_full_history(self):
        result = MODULE.fetch_history('token', evidence(), None, self.start, self.end,
                                      page_size=1, delay=0, fetch=lambda *a: self.response([self.row()], 2))
        self.assertFalse(result['collection_complete'])
        self.assertEqual(result['stop_reason'], 'history_page_overlap')

    def test_fixed_time_range_is_verified(self):
        row = self.row(); row['created_at'] = 1
        with self.assertRaisesRegex(MODULE.ReconcileError, 'fixed time range'):
            MODULE.fetch_history('token', evidence(), None, self.start, self.end, delay=0,
                                 fetch=lambda *a: self.response([row]))

    def test_empty_missing_page_is_partial(self):
        result = MODULE.fetch_history('token', evidence(), None, self.start, self.end, delay=0,
                                      fetch=lambda *a: self.response([], 1))
        self.assertEqual(result['stop_reason'], 'history_page_missing')

    def test_maximum_page_bound_is_enforced(self):
        result = MODULE.fetch_history('token', evidence(), None, self.start, self.end,
                                      max_pages=1, page_size=1, delay=0,
                                      fetch=lambda *a: self.response([self.row()], 2))
        self.assertFalse(result['collection_complete'])
        self.assertEqual(result['stop_reason'], 'max_pages_reached')

    def test_expired_session_resumes_only_the_unread_page(self):
        responses = [self.response([self.row()], 2), {'status': 401}]
        partial = MODULE.fetch_history('old-token', evidence(), None, self.start, self.end,
                                       page_size=1, delay=0, fetch=lambda *a: responses.pop(0))
        paths = []
        def fetch(*args):
            paths.append(args[2])
            self.assertEqual(args[3], 'new-token')
            return self.response([self.row('up-2')], 2)
        result = MODULE.fetch_history('new-token', evidence(), None, self.start, self.end,
                                      page_size=1, delay=0, fetch=fetch, resume=partial)
        self.assertTrue(result['collection_complete'])
        self.assertEqual(result['returned_rows'], 2)
        self.assertEqual(len(result['logs']), 2)
        self.assertIn('p=2&', paths[0])
        self.assertEqual(len(paths), 1)

    def test_resume_cannot_change_bounds_or_identity(self):
        partial = MODULE.fetch_history('token', evidence(), None, self.start, self.end,
                                       delay=0, fetch=lambda *a: {'status': 401})
        with self.assertRaisesRegex(MODULE.ReconcileError, 'same fixed range'):
            MODULE.fetch_history('token', evidence(), None, self.start, '2026-11-20T00:00:00+08:00',
                                 delay=0, resume=partial)
        different = evidence(); different['logs'][0]['token_id'] += 1
        with self.assertRaisesRegex(MODULE.ReconcileError, 'same fixed range'):
            MODULE.fetch_history('token', different, None, self.start, self.end,
                                 delay=0, resume=partial)

    def test_credential_file_line_and_bearer_label_are_explicit(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / 'private.txt'
            path.write_text('other secret\n\nAuthorization: Bearer eyJabc.def.ghi\n', encoding='utf-8')
            self.assertEqual(MODULE.read_session_credential(path, 3), 'eyJabc.def.ghi')
            with self.assertRaises(MODULE.ReconcileError):
                MODULE.read_session_credential(path, 2)
            with self.assertRaises(MODULE.ReconcileError):
                MODULE.read_session_credential(path, 4)

    def test_model_key_is_not_accepted_as_session(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / 'private.txt'; path.write_text('sk-secret', encoding='utf-8')
            with self.assertRaisesRegex(MODULE.ReconcileError, 'website session'):
                MODULE.read_session_credential(path)

    def test_apply_envelope_is_bound_to_full_evidence(self):
        src = evidence(); plan = MODULE.build_plan(src, snapshot())
        payload = MODULE.apply_payload(src, plan)
        self.assertEqual(payload['schema'], 1)
        src['logs'][0]['quota'] += 1
        with self.assertRaisesRegex(MODULE.ReconcileError, 'no longer matches'):
            MODULE.apply_payload(src, plan)

    def test_execute_requires_verified_backup_and_fixed_program(self):
        src = evidence(); plan = MODULE.build_plan(src, snapshot())
        for backup in (None, {'path': '/tmp/backup', 'sha256': 'a'*64},
                       {'path': '/opt/sub2api/backups/../config', 'sha256': 'a'*64}):
            with self.subTest(backup=backup), self.assertRaises(MODULE.ReconcileError):
                MODULE.execute_plan('host', src, plan, 'b'*40, backup=backup, apply=True)

    def test_execute_reuses_staged_payload_and_sends_only_small_controls(self):
        from tools.globalai_stage import artifact, compact, digest
        src = evidence(); plan = MODULE.build_plan(src, snapshot())
        manifest, _ = artifact(MODULE.apply_payload(src, plan))
        staged = {'status': 'ready', 'manifest': manifest, 'manifest_sha256': digest(compact(manifest))}
        returned = SimpleNamespace(returncode=0, stdout='{"status":"planned"}', stderr='')
        with patch.object(MODULE, 'stage_payload', side_effect=AssertionError('Existing staging must be reused')):
            with patch.object(MODULE.subprocess, 'run', return_value=returned) as run:
                result = MODULE.execute_plan('host', src, plan, 'b'*40, staged=staged)
        self.assertEqual(result['status'], 'planned')
        code = run.call_args.kwargs['input']
        self.assertLess(len(code.encode()), 15000)
        import ast, base64
        encoded = next(line.split('=', 1)[1] for line in code.splitlines() if line.startswith('encoded='))
        control = json.loads(base64.b64decode(ast.literal_eval(encoded)))
        self.assertEqual(set(control), {'staged', 'source_commit', 'backup', 'apply'})
        self.assertEqual(control['staged']['blob_sha256'], manifest['blob_sha256'])
        self.assertNotIn('payload', control)


class SnapshotPartitionTests(unittest.TestCase):
    def test_each_completed_partition_is_persisted_without_credentials(self):
        calls = []
        def read(_host, ids, _database, after_id=0, limit=None):
            calls.append((ids[0], after_id))
            rows = [charge(id=after_id+1)] if after_id < 2 else []
            return [{'id': 8, 'credentials': {'api_key': 'never-save'}}], {'schema': 1, 'charges': rows, 'observed_at': 'now'}
        with tempfile.TemporaryDirectory() as folder:
            accounts, result = MODULE.partitioned_snapshot('host', [8], folder, batch_size=1, read=read)
            self.assertEqual(len(result['charges']), 2)
            self.assertEqual(accounts[0]['credentials']['api_key'], 'never-save')
            self.assertEqual(calls, [(8, 0), (8, 1), (8, 2)])
            self.assertEqual(len(list(Path(folder).glob('*.json'))), 3)
            self.assertNotIn('never-save', ''.join(p.read_text(encoding='utf-8') for p in Path(folder).glob('*.json')))

    def test_resumed_cache_must_match_selected_account_and_cursor(self):
        with tempfile.TemporaryDirectory() as folder:
            target = Path(folder) / 'account-8-after-0.json'
            MODULE.write_json(target, {'schema': 1, 'charges': [charge(account_id=13)]})
            def read(*args, **kwargs):
                return [{'id': 8, 'credentials': {'api_key': 'private'}}], {'schema': 1, 'charges': []}
            with self.assertRaisesRegex(MODULE.ReconcileError, 'partition identity'):
                MODULE.partitioned_snapshot('host', [8], folder, resume=True, read=read)


if __name__ == '__main__':
    unittest.main()
