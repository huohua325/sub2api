import json
from pathlib import Path
import sys
import tempfile
import unittest
from argparse import Namespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
from delivery_workflow import change_plan, freeze_source, require_frozen, parse_go_events


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for name in ['frontend/src/app.ts', 'backend/internal/service/app.go']:
            path = self.root / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('source')
        self.out = self.root / 'output/change'

    def test_tools_and_docs_do_not_rebuild_or_retest_application(self):
        for files in [['tools/release_server.py'], ['AGENTS.md', 'docs/runbook.md']]:
            plan = change_plan(files)
            self.assertFalse(plan['application_release'])
            self.assertFalse(plan['frontend_full'])
            self.assertFalse(plan['quota_postgres_race'])
        self.assertTrue(change_plan(['tools/release_server.py'])['early_preflight'])

    def test_frontend_release_retains_full_checks_and_real_quota_race(self):
        plan = change_plan(['frontend/src/views/App.vue'])
        self.assertTrue(plan['frontend_full'])
        self.assertTrue(plan['quota_postgres_race'])
        self.assertTrue(plan['feature_path_required'])
        self.assertFalse(plan['broad_go'])

    def test_quota_targets_related_packages_and_core_billing_escalates(self):
        quota = change_plan(['backend/internal/repository/personal_quota_reset.go', 'backend/migrations/260_personal_quota_resets.sql'])
        self.assertTrue(quota['quota_postgres_race'])
        self.assertIn('./internal/repository', quota['go_packages'])
        self.assertFalse(quota['broad_go'])
        self.assertTrue(quota['migration_sandbox'])
        self.assertTrue(change_plan(['backend/internal/service/usage_billing.go'])['broad_go'])
        self.assertTrue(change_plan(['docs/upstream.md'], upstream=True)['broad_go'])

    def test_freeze_requires_review_and_rejects_edits_before_checks(self):
        with self.assertRaises(DeliveryError): freeze_source(self.root, self.out, reviewed=False)
        freeze_source(self.root, self.out, reviewed=True)
        require_frozen(self.root, self.out)
        (self.root / 'frontend/src/app.ts').write_text('new code')
        with self.assertRaisesRegex(DeliveryError, 'changed'): require_frozen(self.root, self.out)

    def test_generated_artifacts_docs_and_tooling_do_not_change_app_freeze(self):
        freeze_source(self.root, self.out, reviewed=True)
        for name in ['backend/internal/web/dist/index.html', 'frontend/vite.config.js', 'docs/report.md', 'tools/release_server.py']:
            p = self.root / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text('generated or separate change')
        require_frozen(self.root, self.out)
        (self.root / 'backend/internal/service/extra.go').write_text('new source')
        with self.assertRaises(DeliveryError): require_frozen(self.root, self.out)

    def test_go_summary_preserves_failures_skips_and_raw_protocol(self):
        text = '\n'.join(json.dumps(e) for e in [
            {'Action': 'pass', 'Package': 'p', 'Test': 'TestGood'},
            {'Action': 'fail', 'Package': 'p', 'Test': 'TestBad/edge'},
            {'Action': 'fail', 'Package': 'p', 'Test': 'TestBad'},
            {'Action': 'skip', 'Package': 'p', 'Test': 'TestExternal'},
            {'Action': 'fail', 'Package': 'p'}])
        summary = parse_go_events(text)
        self.assertEqual(summary['passed'], 1)
        self.assertEqual(summary['skipped'], 1)
        self.assertEqual(summary['failed'], 2)
        self.assertEqual(summary['failure_roots'], ['p:TestBad'])
        self.assertEqual(summary['failed_packages'], ['p'])
        with self.assertRaises(DeliveryError): parse_go_events('not Go JSON evidence')

    def test_go_runner_keeps_json_failure_even_when_the_command_exits_zero(self):
        import delivery
        fake = self.root / 'fake_go.py'
        fake.write_text("import sys,json\nif 'version' in sys.argv: print('go-test-tool');sys.exit(0)\nprint(json.dumps({'Action':'fail','Package':'p','Test':'TestBroken'}))\nprint(json.dumps({'Action':'fail','Package':'p'}))\n")
        args = Namespace(go_command=json.dumps([sys.executable, str(fake)]), packages=['./internal/service'],
                         tags='unit', race=False, run='TestBroken', output=self.out)
        with patch.object(delivery, 'ROOT', self.root):
            with self.assertRaises(DeliveryError): delivery.run_go_check(args)
        report = json.loads((self.out / 'go-summary.json').read_text())
        self.assertEqual(report['failure_roots'], ['p:TestBroken'])
        self.assertIn('TestBroken', (self.out / report['log']).read_text())
        self.assertEqual(json.loads((self.out / 'manifest.json').read_text())['steps'][-1]['status'], 'failed')

    def test_final_validation_rejects_an_unfrozen_tree_before_any_tool_runs(self):
        import delivery
        args = Namespace(output=self.out)
        with patch.object(delivery, 'ROOT', self.root), patch.object(delivery, 'version') as version:
            with self.assertRaisesRegex(DeliveryError, 'Freeze'): delivery.validate(args)
            version.assert_not_called()

    def test_failed_preflight_does_not_leave_an_old_success_report(self):
        import delivery
        from types import SimpleNamespace
        for name in ['preflight_server.py', 'release_server.py', 'delivery_core.py']:
            p = self.root / 'tools' / name; p.parent.mkdir(exist_ok=True); p.write_text('# fixture')
        self.out.mkdir(parents=True)
        (self.out / 'preflight.json').write_text('{"ok":true}')
        args = Namespace(host='root@example.invalid', expected_sha256=None, deploy_root='/opt/sub2api', output=self.out)
        with patch.object(delivery, 'ROOT', self.root), patch.object(delivery.subprocess, 'run', return_value=SimpleNamespace(returncode=1, stdout='', stderr='offline')):
            with self.assertRaises(DeliveryError): delivery.run_preflight(args)
        self.assertFalse(json.loads((self.out / 'preflight.json').read_text())['ok'])

    def test_early_preflight_rejects_wrong_baseline_before_docker_access(self):
        import preflight_server
        p = self.root / 'bin/sub2api'; p.parent.mkdir(); p.write_bytes(b'current program')
        with patch.object(preflight_server, 'DockerRuntime') as runtime:
            with self.assertRaisesRegex(DeliveryError, 'baseline'): preflight_server.check(self.root, '0' * 64)
            runtime.assert_not_called()
