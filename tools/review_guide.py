"""Generate parameterized Playwright CLI scenarios; isolate and close only our sessions."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import urllib.parse

from delivery_core import DeliveryError, digest, utcnow, write_json

TOOLS = Path(__file__).resolve().parent


def run(args):
    origin = urllib.parse.urlsplit(args.base_url)
    if origin.scheme not in ('http', 'https') or not origin.netloc or origin.path not in ('', '/') or origin.query or origin.fragment or origin.username:
        raise DeliveryError('Browser base URL must be an HTTP(S) origin')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    npx = shutil.which('npx.cmd' if os.name == 'nt' else 'npx')
    if not npx:
        raise DeliveryError('npx is required; install @playwright/cli and its browsers first')
    fixture = (TOOLS / 'browser_fixtures.cjs').read_text(encoding='utf-8')
    template = (TOOLS / 'review_guide.cjs').read_text(encoding='utf-8')
    result = {'base_url': args.base_url, 'checked_at': utcnow(), 'engines': {}, 'ok': False,
              'script_sha256': digest(TOOLS / 'review_guide.cjs'), 'fixtures_sha256': digest(TOOLS / 'browser_fixtures.cjs')}
    write_json(output / 'result.json', result)
    try:
        for engine in args.engines:
            session = f'mile-{os.getpid()}-{engine}-{time.time_ns()}'
            prefix = [npx, '--no-install', '--package', '@playwright/cli', 'playwright-cli', '-s=' + session]
            options = {'base': args.base_url.rstrip('/'), 'output': str(output / engine).replace('\\', '/'),
                       'clipboard': engine == 'chromium'}
            (output / engine).mkdir(exist_ok=True)
            script = template.replace('__MILE_REVIEW_OPTIONS__', json.dumps(options)).replace('__MILE_FIXTURES__', fixture.strip())
            generated = output / (engine + '.cjs')
            generated.write_text(script, encoding='utf-8')
            def invoke(argv, label):
                log = output / (engine + '-' + label + '.log')
                process = subprocess.run(prefix + argv, cwd=TOOLS.parent, capture_output=True,
                                         encoding='utf-8', errors='replace', timeout=150)
                text = process.stdout + process.stderr
                log.write_text(text, encoding='utf-8')
                if process.returncode or '### Error' in text:
                    raise DeliveryError('Browser ' + label + ' failed; see ' + str(log))
                return text
            try:
                invoke(['open', 'about:blank', '--browser', 'chrome' if engine == 'chromium' else 'webkit'], 'open')
                text = invoke(['run-code', '--filename', str(generated)], 'scenario')
                if '### Result\n' not in text:
                    raise DeliveryError('Browser CLI did not return structured evidence')
                payload = text.split('### Result\n', 1)[1].split('\n### ', 1)[0].strip()
                data = json.loads(payload)
                if data['errors'] or data['apiMutations']:
                    raise DeliveryError('Browser reported errors or attempted business mutations')
                result['engines'][engine] = data
            finally:
                invoke(['close'], 'close')
        result['ok'] = True
    finally:
        write_json(output / 'result.json', result)
    print('browser regression: passed (' + ', '.join(args.engines) + ')')
