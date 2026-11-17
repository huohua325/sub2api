import copy
import json
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError, digest, json_hash, write_json
from runtime_resources import Settings, ResourceRuntime, candidate_override, assert_allowed_change, change_resources

MIB = 1024 * 1024


class FakeRuntime:
    def __init__(self, root):
        self.root = root
        self.app = 'sub2api'
        self.project = 'mile'
        self.calls = []
        self.fail_backup = self.fail_health = self.fail_preservation = False
        self.health_hook = lambda: None
        self.after_backup = lambda: None
        self.base = {'name': 'mile', 'services': {
            'sub2api': {'mem_limit': 420 * MIB, 'memswap_limit': 700 * MIB,
                'environment': {'KEEP': 'private-value', 'SUB2API_CODING_QUOTA_OPENAI_MODE': 'shadow',
                                'SUB2API_CODING_QUOTA_GROK_MODE': 'shadow'}, 'restart': 'unless-stopped'},
            'postgres': {'image': 'unchanged'}}}
        self.running = self.effective_model()
        self.views = {'as_of': '', 'clocks': ['original'], 'migrations': ['original'],
                      'immutable': {'audits': [{'id': 1}]}, 'durable_ids': {'jobs': [1]},
                      'requests': [{'id': 1, 'state': 'settled', 'points': '10'}]}

    def host_memory_bytes(self): return 2048 * MIB

    def read_override(self, path): return json.loads(path.read_text())

    def candidate_model(self, path):
        model = copy.deepcopy(self.base)
        for name, config in self.read_override(path).get('services', {}).items():
            target = model['services'].setdefault(name, {})
            for key, value in config.items():
                if key == 'environment': target.setdefault(key, {}).update(value)
                else: target[key] = copy.deepcopy(value)
        return model

    def effective_model(self): return self.candidate_model(self.root / 'docker-compose.override.yml')

    def frozen_model(self, path): return json.loads(path.read_text())

    def preflight(self):
        self.calls.append('preflight')
        self.resolved_compose = self.effective_model()
        if self.resolved_compose != self.running: raise DeliveryError('compose drift')
        env = self.running['services']['sub2api']['environment']
        return {'modes': [env['SUB2API_CODING_QUOTA_OPENAI_MODE'], env['SUB2API_CODING_QUOTA_GROK_MODE']],
                'environment_hash': json_hash(env)}

    def inspect(self, name):
        app = self.running['services']['sub2api']
        return {'Image': 'same-image', 'HostConfig': {'Memory': app['mem_limit'],
                'MemorySwap': app['memswap_limit'], 'NanoCpus': 0},
                'Config': {'Env': [k + '=' + str(v) for k, v in app['environment'].items()]}}

    def backup(self, folder):
        self.calls.append('backup')
        if self.fail_backup: raise DeliveryError('archive verification failed')
        (folder / 'database.dump').write_bytes(b'verified backup')
        (folder / 'database-contents.txt').write_text('verified archive list')
        self.frozen_file = folder / 'frozen-compose.json'
        write_json(self.frozen_file, self.running)
        self.after_backup()

    def stop(self): self.calls.append('stop')

    def start(self):
        self.calls.append('start')
        self.running = self.frozen_model(self.frozen_file)

    def health(self):
        self.calls.append('health')
        self.health_hook()
        if self.fail_health and self.running['services']['sub2api']['mem_limit'] == 768 * MIB:
            raise DeliveryError('candidate unhealthy')

    def snapshot(self, at):
        self.calls.append('snapshot')
        view = copy.deepcopy(self.views)
        view['as_of'] = at
        if self.fail_preservation and self.calls.count('snapshot') > 1:
            view['requests'][0]['points'] = '0'
        return view


class RuntimeResourcesTests(unittest.TestCase):
    def setUp(self):
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        self.root = Path(folder.name)
        (self.root / 'bin').mkdir()
        (self.root / 'bin/sub2api').write_bytes(b'unchanged application')
        (self.root / 'docker-compose.yml').write_text('services: {}')
        self.override = self.root / 'docker-compose.override.yml'
        self.original = {'services': {'sub2api': {'environment': {'GOMEMLIMIT': '256MiB', 'GOGC': '50'},
                                                  'logging': {'options': {'max-size': '10m'}}}}}
        self.override.write_text(json.dumps(self.original))
        self.override.chmod(0o600)
        self.old_bytes = self.override.read_bytes()
        self.old_mode = stat.S_IMODE(self.override.stat().st_mode)
        self.runtime = FakeRuntime(self.root)
        self.sha = digest(self.root / 'bin/sub2api')

    def run_change(self, **kwargs):
        return change_resources(self.root, self.runtime, Settings(), self.sha, **kwargs)

    def test_parameters_reserve_host_memory_and_reject_invalid_swap_heap_and_body(self):
        Settings().validate(2048 * MIB)
        for settings, memory in [(Settings(), 1536 * MIB), (Settings(swap_mib=700), 2048 * MIB),
                                 (Settings(go_mib=768), 2048 * MIB),
                                 (Settings(body_mib=300), 2048 * MIB),
                                 (Settings(hard_mib=-1), 2048 * MIB)]:
            with self.subTest(settings=settings), self.assertRaises(DeliveryError): settings.validate(memory)

    def test_candidate_preserves_unknown_override_and_only_allows_resource_fields(self):
        before = self.runtime.effective_model()
        settings = Settings(body_mib=192)
        candidate = candidate_override(before, self.original, settings)
        self.assertEqual(candidate['services']['sub2api']['logging'], self.original['services']['sub2api']['logging'])
        self.override.write_text(json.dumps(candidate))
        after = self.runtime.effective_model()
        assert_allowed_change(before, after, settings)
        for change in ['secret', 'postgres', 'cpu', 'body-without-opt-in']:
            altered = copy.deepcopy(after)
            if change == 'secret': altered['services']['sub2api']['environment']['KEEP'] = 'changed'
            if change == 'postgres': altered['services']['postgres']['image'] = 'changed'
            if change == 'cpu': altered['services']['sub2api']['cpus'] = 4
            if change == 'body-without-opt-in':
                with self.assertRaises(DeliveryError): assert_allowed_change(before, altered, Settings())
                continue
            with self.subTest(change=change), self.assertRaises(DeliveryError):
                assert_allowed_change(before, altered, settings)

    def test_default_plan_does_not_backup_stop_or_rewrite_configuration(self):
        result = self.run_change()
        self.assertEqual(result['status'], 'planned')
        self.assertEqual(self.override.read_bytes(), self.old_bytes)
        self.assertNotIn('backup', self.runtime.calls)
        self.assertNotIn('stop', self.runtime.calls)
        self.assertFalse((self.root / 'backups').exists())
        self.assertNotIn('private-value', json.dumps(result))

    def test_apply_backs_up_override_and_preserves_program_and_business_data(self):
        result = self.run_change(apply=True)
        self.assertEqual(result['status'], 'applied')
        backup = Path(result['backup'])
        self.assertEqual((backup / 'docker-compose.override.yml').read_bytes(), self.old_bytes)
        self.assertEqual(digest(self.root / 'bin/sub2api'), self.sha)
        self.assertEqual(stat.S_IMODE(self.override.stat().st_mode), self.old_mode)
        self.assertEqual(self.runtime.running['services']['sub2api']['mem_limit'], 768 * MIB)
        self.assertLess(self.runtime.calls.index('backup'), self.runtime.calls.index('stop'))
        self.assertTrue(result['business_snapshot_preserved'])

    def test_failed_backup_or_drift_during_backup_never_stops_app(self):
        self.runtime.fail_backup = True
        with self.assertRaises(DeliveryError): self.run_change(apply=True)
        self.assertNotIn('stop', self.runtime.calls)
        self.runtime.fail_backup = False
        self.runtime.after_backup = lambda: self.override.write_text('{}')
        with self.assertRaises(DeliveryError): self.run_change(apply=True)
        self.assertNotIn('stop', self.runtime.calls)

    def test_health_or_preservation_failure_rolls_back_config_only(self):
        for failure in ['fail_health', 'fail_preservation']:
            with self.subTest(failure=failure):
                setattr(self.runtime, failure, True)
                with self.assertRaises(DeliveryError): self.run_change(apply=True)
                self.assertEqual(self.override.read_bytes(), self.old_bytes)
                self.assertEqual(stat.S_IMODE(self.override.stat().st_mode), self.old_mode)
                self.assertEqual(digest(self.root / 'bin/sub2api'), self.sha)
                self.assertEqual(self.runtime.running['services']['sub2api']['mem_limit'], 420 * MIB)
                self.assertNotIn('restore', self.runtime.calls)
                setattr(self.runtime, failure, False)
                self.runtime.calls.clear()

    def test_wrong_binary_or_non_shadow_rejected_before_backup(self):
        self.sha = '0' * 64
        with self.assertRaises(DeliveryError): self.run_change(apply=True)
        self.assertNotIn('backup', self.runtime.calls)
        self.sha = digest(self.root / 'bin/sub2api')
        self.runtime.base['services']['sub2api']['environment']['SUB2API_CODING_QUOTA_OPENAI_MODE'] = 'enforce'
        self.runtime.running = self.runtime.effective_model()
        with self.assertRaises(DeliveryError): self.run_change(apply=True)
        self.assertNotIn('backup', self.runtime.calls)

    def test_deploy_memory_is_updated_without_changing_other_deploy_limits(self):
        current = self.runtime.effective_model()
        current['services']['sub2api']['deploy'] = {'resources': {'limits': {'memory': '420m', 'cpus': '1'}}}
        partial = candidate_override(current, self.original, Settings())
        updated = copy.deepcopy(current)
        updated['services']['sub2api'].update({k: v for k, v in partial['services']['sub2api'].items() if k != 'deploy'})
        updated['services']['sub2api']['environment'] = dict(current['services']['sub2api']['environment'], **Settings().environment())
        updated['services']['sub2api']['deploy']['resources']['limits']['memory'] = 768 * MIB
        assert_allowed_change(current, updated, Settings())
        self.assertEqual(partial['services']['sub2api']['deploy']['resources']['limits']['memory'], 768 * MIB)

    def test_partial_override_parser_preserves_interpolation_and_skips_consistency_only(self):
        runtime = ResourceRuntime.__new__(ResourceRuntime)
        runtime.root, runtime.project = self.root, 'mile'
        payload = {'services': {'sub2api': {'environment': {'CUSTOM': '${KEEP}'}}}}
        with patch('runtime_resources.subprocess.check_output', return_value=json.dumps(payload)) as call:
            self.assertEqual(runtime.read_override(self.override), payload)
        command = call.call_args.args[0]
        for flag in ['--no-interpolate', '--no-normalize', '--no-consistency', '--no-path-resolution', '--no-env-resolution']:
            self.assertIn(flag, command)

    def test_external_override_edit_is_not_overwritten_during_failure_recovery(self):
        external = b'{"services":{"sub2api":{"environment":{"NEW_OWNER_SETTING":"keep"}}}}'
        def interfere():
            if self.runtime.running['services']['sub2api']['mem_limit'] == 768 * MIB:
                self.override.write_bytes(external)
                raise DeliveryError('health failed after external edit')
        self.runtime.health_hook = interfere
        with self.assertRaises(DeliveryError): self.run_change(apply=True)
        self.assertEqual(self.override.read_bytes(), external)
        self.assertEqual(self.runtime.running['services']['sub2api']['mem_limit'], 420 * MIB)
        record = json.loads(next((self.root / 'backups').glob('*/runtime-resources.json')).read_text())
        self.assertTrue(record['override_rollback_blocked'])
        self.assertTrue(record['original_frozen_runtime_restored'])


if __name__ == '__main__': unittest.main()
