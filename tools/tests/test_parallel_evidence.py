import copy
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import delivery_workflow
from delivery_core import DeliveryError, Runner, digest, utcnow, write_json


class ParallelEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'source').write_text('stable')
        self.out = self.root / 'output/change'
        self.started = utcnow()
        self.child = self.out / 'parallel/attempt/quota'
        self.runner = Runner(self.root, self.child)
        self.record = self.runner.run('quota-race', [sys.executable, '-c',
            'print(\'{"Action":"pass","Package":"repo","Test":"TestCodingQuotaDBExample"}\')'], ['source'], cache=False)
        self.record['quota_tests'] = {'passed': 1, 'skipped': 0, 'failed': 0}
        write_json(self.child / 'manifest.json', self.runner.manifest)

    def collect(self, children=None):
        function = getattr(delivery_workflow, 'collect_evidence', None)
        self.assertIsNotNone(function, 'Parallel evidence needs a verified collector, not manual manifest edits')
        return function(self.root, self.out, children or [self.child], started_at=self.started)

    def test_current_child_log_is_preserved_and_import_has_provenance(self):
        source_hash = digest(self.child / 'manifest.json')
        self.collect()
        data = json.loads((self.out / 'manifest.json').read_text())
        result = data['steps'][-1]
        self.assertEqual(result['quota_tests'], {'passed': 1, 'skipped': 0, 'failed': 0})
        self.assertEqual(digest(self.out / result['log']), self.record['log_sha256'])
        self.assertEqual(result['provenance']['sha256'], source_hash)
        self.assertTrue((self.child / self.record['log']).exists())

    def test_corrupt_log_and_source_edit_are_rejected_without_partial_import(self):
        for mutation in ['log', 'input']:
            with self.subTest(mutation=mutation):
                target = self.child / self.record['log'] if mutation == 'log' else self.root / 'source'
                original = target.read_bytes(); target.write_bytes(b'changed')
                with self.assertRaises(DeliveryError): self.collect()
                self.assertFalse((self.out / 'manifest.json').exists())
                target.write_bytes(original)

    def test_failed_or_old_child_cannot_be_imported(self):
        self.started = utcnow()
        with self.assertRaises(DeliveryError): self.collect()
        self.started = '2000-01-01T00:00:00+00:00'
        data = copy.deepcopy(self.runner.manifest); data['steps'][-1]['status'] = 'failed'
        write_json(self.child / 'manifest.json', data)
        with self.assertRaises(DeliveryError): self.collect()

    def test_success_label_cannot_hide_skipped_quota_in_raw_log(self):
        log = self.child / self.record['log']
        log.write_text('{"Action":"skip","Package":"repo","Test":"TestCodingQuotaDBExample"}\n')
        self.record['log_sha256'] = digest(log)
        write_json(self.child / 'manifest.json', self.runner.manifest)
        with self.assertRaises(DeliveryError): self.collect()

    def test_child_outside_delivery_or_from_other_checkout_is_rejected(self):
        with self.assertRaises(DeliveryError): self.collect([self.root / 'elsewhere'])
        data = copy.deepcopy(self.runner.manifest); data['root'] = str(self.root / 'other')
        write_json(self.child / 'manifest.json', data)
        with self.assertRaises(DeliveryError): self.collect()

    def test_all_children_verified_before_any_success_is_imported(self):
        other = self.out / 'parallel/attempt/browser'; other.mkdir(parents=True)
        data = copy.deepcopy(self.runner.manifest)
        data['steps'][-1]['status'] = 'failed'
        write_json(other / 'manifest.json', data)
        with self.assertRaises(DeliveryError): self.collect([self.child, other])
        self.assertFalse((self.out / 'manifest.json').exists())
