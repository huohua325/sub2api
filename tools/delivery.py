"""MILE single-server delivery entry point. See tools/DELIVERY.md."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from functools import partial
from http.server import ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import threading

from delivery_core import DeliveryError, Runner, digest, exclusive, tree_hash, utcnow, write_json

ROOT = Path(__file__).resolve().parents[1]
FRONTEND_INPUTS = ['frontend', 'tools/delivery.py', 'tools/delivery_core.py', 'tools/review_guide.py',
                   'tools/review_guide.cjs', 'tools/browser_fixtures.cjs', 'AGENTS.md']


def version(command):
    return subprocess.check_output(command, cwd=ROOT, text=True, encoding='utf-8',
                                   errors='replace', timeout=30).strip()


def validate(args):
    from delivery_workflow import require_frozen
    require_frozen(ROOT, args.output)
    pnpm = shutil.which('pnpm.cmd' if os.name == 'nt' else 'pnpm')
    if not pnpm:
        raise DeliveryError('pnpm is not installed')
    tools = {'node': version(['node', '--version']), 'pnpm': version([pnpm, '--version']),
             'python': sys.version, 'platform': sys.platform}
    output = args.output.resolve()
    with exclusive(output):
        runner = Runner(ROOT, output, tools=tools)
        # Refresh the dependency graph before allowing reuse; never install with a mutable lockfile.
        runner.run('install', [pnpm, 'install', '--frozen-lockfile'],
                   ['frontend/package.json', 'frontend/pnpm-lock.yaml'], cwd='frontend', cache=False)
        inputs = FRONTEND_INPUTS + ['frontend/node_modules/.modules.yaml']
        snapshot = tree_hash(ROOT, inputs, inputs=True)
        runner.run('early-browser', [sys.executable, 'tools/delivery.py', 'browser',
                   '--base-url', args.review_url, '--output', str(output / 'early-browser'),
                   '--engines', 'chromium'], inputs, cache=False, timeout=180)
        # Full tests already contain check:i18n. Use exactly the same -b typecheck as pnpm build.
        jobs = [
            ('lint', [pnpm, 'run', 'lint:check']),
            ('tests', [pnpm, 'run', 'test:run']),
            ('types', [pnpm, 'exec', 'vue-tsc', '-b']),
        ]
        def run(job):
            name, command = job
            return runner.run(name, command, inputs, cwd='frontend', cache=not args.fresh)
        with ThreadPoolExecutor(max_workers=args.jobs) as executor:
            # Resolve every future: no hidden failure in a parallel check.
            results = [executor.submit(run, job) for job in jobs]
            errors = []
            for future in results:
                try:
                    future.result()
                except DeliveryError as error:
                    errors.append(str(error))
            if errors:
                raise DeliveryError('; '.join(errors))
        if snapshot != tree_hash(ROOT, inputs, inputs=True):
            raise DeliveryError('Inputs changed during validation; rerun on a stable snapshot')
        runner.run('frontend-build', [pnpm, 'exec', 'vite', 'build', '--config', 'vite.config.ts'], inputs,
                   artifacts=['backend/internal/web/dist'], cwd='frontend', cache=not args.fresh)
        runner.manifest['source_commit'] = version(['git', 'rev-parse', 'HEAD'])
        runner.manifest['validated_inputs'] = snapshot
        runner.manifest['frontend_artifacts'] = tree_hash(ROOT, ['backend/internal/web/dist'])
        write_json(output / 'manifest.json', runner.manifest)
        require_frozen(ROOT, output)
        print('Validated frontend; review the production artifact before packaging. ' + str(output / 'manifest.json'))


def quota(args):
    if not os.environ.get('SUB2API_QUOTA_TEST_DSN'):
        raise DeliveryError('SUB2API_QUOTA_TEST_DSN must point to an isolated PostgreSQL database')
    prefix = [args.go_executable] if getattr(args, 'go_executable', None) else json.loads(args.go_command)
    if not isinstance(prefix, list) or not prefix or not all(isinstance(v, str) for v in prefix):
        raise DeliveryError('--go-command must be a JSON argument array')
    command = list(prefix)
    command += ['test', '-json', '-race', './internal/repository', '-run', 'TestCodingQuotaDB', '-count=1']
    with exclusive(args.output):
        runner = Runner(ROOT, args.output, tools={'go': version(prefix + ['version'])})
        result = runner.run('quota-race', command, ['backend', 'tools/delivery.py', 'tools/delivery_core.py', 'AGENTS.md'], cwd='backend', cache=False, timeout=1800)
        actions = []
        for line in (runner.output / result['log']).read_text(encoding='utf-8', errors='replace').splitlines():
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if event.get('Test', '').startswith('TestCodingQuotaDB'):
                actions.append(event.get('Action'))
        summary = {'passed': actions.count('pass'), 'skipped': actions.count('skip'), 'failed': actions.count('fail')}
        if not summary['passed'] or summary['skipped'] or summary['failed']:
            result['status'] = 'failed'
            write_json(runner.output / 'manifest.json', runner.manifest)
            raise DeliveryError('Quota tests did not actually pass with zero skips: ' + json.dumps(summary))
        result['quota_tests'] = summary
        write_json(runner.output / 'manifest.json', runner.manifest)
        print(json.dumps(summary))


@contextmanager
def preview(dist):
    from serve_frontend_review import Handler
    server = ThreadingHTTPServer(('127.0.0.1', 0), partial(Handler, directory=str(dist)))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield 'http://127.0.0.1:' + str(server.server_port)
    finally:
        server.shutdown(); server.server_close(); thread.join(timeout=3)


def artifact_review(args):
    output = args.output.resolve()
    with exclusive(output):
        runner = Runner(ROOT, output)
        if not output.is_relative_to(ROOT): raise DeliveryError('Evidence output must be inside the checkout')
        result_path = output / 'artifact-browser/result.json'
        with preview(ROOT / 'backend/internal/web/dist') as url:
            runner.run('artifact-browser', [sys.executable, 'tools/delivery.py', 'browser',
                       '--base-url', url, '--output', str(result_path.parent), '--engines', 'chromium', 'webkit'],
                       FRONTEND_INPUTS + ['backend/internal/web/dist'],
                       artifacts=[result_path.relative_to(ROOT).as_posix()], cache=False, timeout=360)


def build_backend(args):
    from release_package import require_committed_inputs
    require_committed_inputs(ROOT)
    if not __import__('re').fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}', args.version):
        raise DeliveryError('Invalid version')
    output = args.output.resolve()
    if not output.is_relative_to(ROOT): raise DeliveryError('Evidence output must be inside the checkout')
    prefix = [args.go_executable] if getattr(args, 'go_executable', None) else json.loads(args.go_command)
    if not isinstance(prefix, list) or not prefix or not all(isinstance(v, str) for v in prefix):
        raise DeliveryError('--go-command must be a JSON argument array')
    from delivery_workflow import select_build_source
    prior_manifest = output / 'manifest.json'
    previous = json.loads(prior_manifest.read_text()) if prior_manifest.exists() else {}
    previous = next((s for s in reversed(previous.get('steps', [])) if s['name'] == 'backend-build' and s['status'] in ('passed', 'reused')), {})
    source = select_build_source(ROOT, version(['git', 'rev-parse', 'HEAD']), args.version, previous)
    date = version(['git', 'show', '-s', '--format=%cI', source])
    binary = output / 'sub2api-linux-amd64'
    with exclusive(output):
        env = {**os.environ, 'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}
        runner = Runner(ROOT, output, tools={'go': version(prefix + ['version'])}, env=env)
        flags = f'-s -w -X main.Version={args.version} -X main.Commit={source} -X main.Date={date} -X main.BuildType=release'
        record = runner.run('backend-build', prefix + ['build', '-tags', 'embed', '-trimpath', '-ldflags', flags,
                            '-o', str(binary), './cmd/server'], ['backend', 'tools/delivery.py', 'tools/delivery_core.py', 'tools/release_package.py', 'AGENTS.md'],
                            artifacts=[binary.relative_to(ROOT).as_posix()], cwd='backend')
        record['source_commit'] = source
        record['version'] = args.version
        write_json(output / 'manifest.json', runner.manifest)


def package(args):
    from release_package import package_release
    manifest = package_release(ROOT, args.output, args.destination,
                               args.output / 'sub2api-linux-amd64', args.version,
                               args.baseline_sha256, args.baseline_commit,
                               full_upload=args.full_upload, baseline_binary=args.baseline_binary)
    print(json.dumps(manifest, indent=2))


def run_go_check(args):
    from delivery_workflow import parse_go_events
    prefix = [args.go_executable] if getattr(args, 'go_executable', None) else json.loads(args.go_command)
    if not isinstance(prefix, list) or not prefix or not all(isinstance(v, str) for v in prefix):
        raise DeliveryError('--go-command must be a JSON argument array')
    if not args.packages or any(not p.startswith('./') or '..' in Path(p).parts for p in args.packages):
        raise DeliveryError('Use explicit relative Go package paths')
    command = prefix + ['test', '-json', '-count=1']
    if args.tags: command += ['-tags', args.tags]
    if args.race: command.append('-race')
    if args.run: command += ['-run', args.run]
    command += args.packages
    with exclusive(args.output):
        runner = Runner(ROOT, args.output, tools={'go': version(prefix + ['version'])})
        write_json(runner.output / 'go-summary.json', {'status': 'running', 'passed': 0, 'skipped': 0, 'failed': 0})
        def validate_log(path):
            report = parse_go_events(path.read_text(encoding='utf-8', errors='replace'))
            if not report['passed'] or report['failed'] or report['failed_packages']:
                raise DeliveryError('Go JSON reports failures or no actually passed tests')
        failure = None
        try:
            result = runner.run('go-check', command, ['backend', 'tools/delivery.py', 'tools/delivery_workflow.py'],
                                cwd='backend', cache=False, timeout=1800, log_validator=validate_log)
        except DeliveryError as error:
            failure = error
            result = runner.manifest['steps'][-1]
        try:
            report = parse_go_events((runner.output / result['log']).read_text(encoding='utf-8', errors='replace'))
        except DeliveryError as error:
            write_json(runner.output / 'go-summary.json', {'status': 'failed', 'log': result['log'],
                       'exit_code': result['exit_code'], 'error': str(error)})
            raise failure or error
        report.update(log=result['log'], log_sha256=result['log_sha256'], exit_code=result['exit_code'],
                      packages=args.packages, run=args.run, checked_at=result['finished_at'],
                      status='failed' if failure or report['failed'] or report['failed_packages'] else 'passed')
        write_json(runner.output / 'go-summary.json', report)
        if report['failed'] or report['failed_packages']:
            result['status'] = 'failed'
            write_json(runner.output / 'manifest.json', runner.manifest)
            failure = failure or DeliveryError('Go JSON protocol reports failed tests')
        print(json.dumps(report, indent=2))
        if failure: raise failure


def run_preflight(args):
    import base64
    import io
    import re
    import zipfile
    if not re.fullmatch(r'(?:[A-Za-z0-9_.-]+@)?[A-Za-z0-9][A-Za-z0-9_.:-]*', args.host):
        raise DeliveryError('Invalid SSH host')
    if args.expected_sha256 is not None and not re.fullmatch('[0-9a-f]{64}', args.expected_sha256):
        raise DeliveryError('Invalid baseline SHA-256')
    args.output.mkdir(parents=True, exist_ok=True)
    write_json(args.output / 'preflight.json', {'ok': False, 'status': 'running', 'host': args.host,
                                              'checked_at': utcnow()})
    data = io.BytesIO()
    with zipfile.ZipFile(data, 'w', zipfile.ZIP_DEFLATED) as archive:
        for name in ['preflight_server.py', 'release_server.py', 'delivery_core.py']:
            archive.writestr(name, (ROOT / 'tools' / name).read_bytes())
        archive.writestr('options.json', json.dumps({'root': args.deploy_root, 'expected_sha256': args.expected_sha256}))
    code = """import base64,io,json,pathlib,sys,tempfile,zipfile
with tempfile.TemporaryDirectory(prefix='mile-preflight-code-') as folder:
 with zipfile.ZipFile(io.BytesIO(base64.b64decode(PAYLOAD))) as archive: archive.extractall(folder)
 sys.path.insert(0,folder)
 from preflight_server import check
 options=json.loads((pathlib.Path(folder)/'options.json').read_text())
 print(json.dumps(check(**options)))
""".replace('PAYLOAD', repr(base64.b64encode(data.getvalue()).decode('ascii')))
    result = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', args.host, 'python3', '-'],
                            input=code, text=True, encoding='utf-8', capture_output=True, timeout=180)
    (args.output / 'preflight.log').write_text(result.stdout + result.stderr, encoding='utf-8')
    if result.returncode:
        write_json(args.output / 'preflight.json', {'ok': False, 'status': 'failed', 'host': args.host,
                                                  'checked_at': utcnow()})
        raise DeliveryError('Read-only preflight failed; inspect preflight.log')
    report = json.loads(result.stdout)
    if report.get('ok') is not True: raise DeliveryError('Preflight did not pass')
    report['host'] = args.host
    write_json(args.output / 'preflight.json', report)
    print(json.dumps(report, indent=2))


def summary(args):
    manifest = json.loads((args.output / 'manifest.json').read_text())
    lines = ['# Delivery evidence', '', '| Step | Result | Seconds |', '| --- | --- | --- |']
    for step in manifest['steps']:
        lines.append(f"| {step['name']} | {step['status']} | {step['elapsed_seconds']} |")
    (args.output / 'summary.md').write_text('\n'.join(lines) + '\n', encoding='utf-8')
    print('\n'.join(lines))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    plan = sub.add_parser('plan', help='classify changed paths without running unrelated application checks')
    plan.add_argument('--base', required=True)
    plan.add_argument('--upstream', action='store_true')
    def plan_action(args):
        from delivery_workflow import changed_files, change_plan
        result = change_plan(changed_files(ROOT, args.base), upstream=args.upstream)
        write_json(args.output / 'plan.json', result)
        print(json.dumps(result, indent=2))
    plan.set_defaults(func=plan_action)
    freeze = sub.add_parser('freeze', help='record reviewed application source before final checks')
    freeze.add_argument('--reviewed', action='store_true')
    def freeze_action(args):
        from delivery_workflow import freeze_source
        print(json.dumps(freeze_source(ROOT, args.output, reviewed=args.reviewed), indent=2))
    freeze.set_defaults(func=freeze_action)
    go_check = sub.add_parser('go-check', help='capture targeted or broad Go tests as JSON, retaining every failure')
    go_check.add_argument('--go-command', default='["go"]')
    go_check.add_argument('--go-executable', help='single Go path; avoids PowerShell JSON quoting')
    go_check.add_argument('--packages', nargs='+', required=True)
    go_check.add_argument('--run')
    go_check.add_argument('--tags', default='unit')
    go_check.add_argument('--race', action='store_true')
    go_check.set_defaults(func=run_go_check)
    early = sub.add_parser('preflight', help='read-only remote baseline, capacity and frozen Compose check before building')
    early.add_argument('--host', required=True)
    early.add_argument('--deploy-root', default='/opt/sub2api')
    early.add_argument('--expected-sha256')
    early.set_defaults(func=run_preflight)
    check = sub.add_parser('validate', help='early browser -> full checks -> one frontend build')
    check.add_argument('--review-url', required=True, help='running local development/review server')
    check.add_argument('--jobs', type=int, choices=[1, 2], default=2)
    check.add_argument('--fresh', action='store_true', help='rerun checks even with unchanged inputs')
    check.set_defaults(func=validate)
    q = sub.add_parser('quota', help='real isolated PostgreSQL race suite; no skips accepted')
    q.add_argument('--go-command', default='["go"]', help='JSON argv prefix; no shell string')
    q.add_argument('--go-executable', help='single Go path; avoids PowerShell JSON quoting')
    q.set_defaults(func=quota)
    s = sub.add_parser('summary', help='generate release-note timing evidence without rebuilding')
    s.set_defaults(func=summary)
    b = sub.add_parser('browser', help='mock-only browser regression; no production mutations')
    b.add_argument('--base-url', required=True)
    b.add_argument('--engines', nargs='+', choices=['chromium', 'webkit'], default=['chromium', 'webkit'])
    b.set_defaults(func=lambda a: __import__('review_guide').run(a))
    artifact = sub.add_parser('review-artifact', help='serve the built assets and verify Chromium + WebKit')
    artifact.set_defaults(func=artifact_review)
    build = sub.add_parser('build-backend', help='compile the embedded Linux program once, with evidence')
    build.add_argument('--go-command', default='["go"]')
    build.add_argument('--go-executable', help='single Go path; avoids PowerShell JSON quoting')
    build.add_argument('--version', required=True)
    build.set_defaults(func=build_backend)
    p = sub.add_parser('package', help='verify all evidence and create an immutable upload kit; no rebuild')
    p.add_argument('--version', required=True)
    p.add_argument('--destination', type=Path, required=True)
    p.add_argument('--baseline-commit', required=True)
    p.add_argument('--baseline-sha256', required=True)
    p.add_argument('--baseline-binary', type=Path)
    p.add_argument('--full-upload', action='store_true', help='explicitly choose full gzip instead of delta')
    p.set_defaults(func=package)
    for command in [check, q, s, b, artifact, build, p, plan, freeze, go_check, early]:
        command.add_argument('--output', type=Path, default=ROOT / 'output/delivery/current')
    args = parser.parse_args()
    try:
        args.func(args)
    except (DeliveryError, OSError, subprocess.SubprocessError, ValueError) as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
