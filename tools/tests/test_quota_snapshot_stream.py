import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
from quota_snapshot import snapshot, build_snapshot_script


class SnapshotStreamTests(unittest.TestCase):
    def test_script_emits_rows_in_one_readonly_transaction_without_aggregates(self):
        script, _ = build_snapshot_script('2026-11-17T00:00:00+00:00', set())
        self.assertEqual(script.count('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY'), 1)
        self.assertIn('SET LOCAL jit=off', script)
        for forbidden in ['jsonb_agg', 'json_agg', 'jsonb_build_object', 'json_build_object']:
            self.assertNotIn(forbidden, script.lower())
        self.assertIn('coding_quota_reservation_events', script)
        self.assertTrue(script.endswith('COMMIT;'))

    def test_empty_tables_keep_all_existing_sections(self):
        with patch('quota_snapshot.subprocess.check_output', return_value='f'), patch('quota_snapshot.stream_rows', return_value=iter(())) as stream:
            result = snapshot('isolated', {'POSTGRES_USER': 'test', 'POSTGRES_DB': 'test'}, '2026-11-17T00:00:00Z')
        self.assertIn('FETCH_COUNT=1000', stream.call_args.args[0])
        self.assertEqual(result['as_of'], '2026-11-17T00:00:00+00:00')
        self.assertEqual(result['requests'], [])
        self.assertEqual(result['immutable']['capacity_events'], [])
        self.assertEqual(result['immutable']['personal_resets'], [])
        self.assertEqual(result['durable_ids']['coding_quota_async_jobs'], [])

    def test_rows_keep_types_and_partial_failures_never_return_a_snapshot(self):
        row = {'id': 1, 'weight': '0.12345678', 'points': '10', 'state': 'settled'}
        with patch('quota_snapshot.subprocess.check_output', return_value='f'), patch('quota_snapshot.stream_rows', return_value=iter([json.dumps(['requests', row])])):
            result = snapshot('isolated', {'POSTGRES_USER': 'test', 'POSTGRES_DB': 'test'}, '2026-11-17T00:00:00Z')
        self.assertEqual(result['requests'], [row])
        def broken():
            yield json.dumps(['requests', row])
            raise DeliveryError('backend closed')
        with patch('quota_snapshot.subprocess.check_output', return_value='f'), patch('quota_snapshot.stream_rows', return_value=broken()):
            with self.assertRaises(DeliveryError): snapshot('isolated', {'POSTGRES_USER': 'test', 'POSTGRES_DB': 'test'}, '2026-11-17T00:00:00Z')


if __name__ == '__main__': unittest.main()
