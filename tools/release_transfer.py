"""Transfer an immutable payload through a private draft asset when SSH is lossy.

Uses the existing Git credential in memory. Never publishes a release, forwards
the Git credential, or logs the temporary asset URL. Always deletes its draft.
"""
import argparse
import json
from pathlib import Path
import re
import shlex
import subprocess
import urllib.error
import urllib.parse
import urllib.request

from delivery_core import DeliveryError, digest, utcnow, write_json

DOWNLOAD = '''import hashlib,json,os,signal,sys,urllib.request
from pathlib import Path
signal.alarm(150)
j=json.load(sys.stdin)
p=Path(j['destination']); temporary=p.with_name(p.name+'.transfer')
try:
 h=hashlib.sha256(); size=0
 with urllib.request.urlopen(j['url'],timeout=30) as source, temporary.open('wb') as target:
  while chunk:=source.read(1024*1024):
   size+=len(chunk)
   if size>j['bytes']: raise ValueError('Unexpected payload size')
   h.update(chunk);target.write(chunk)
 if size!=j['bytes'] or h.hexdigest()!=j['sha256']: raise ValueError('Payload checksum mismatch')
 os.replace(temporary,p)
 print(json.dumps({'sha256':h.hexdigest(),'bytes':size}))
except Exception:
 temporary.unlink(missing_ok=True)
 print('Private payload download failed',file=sys.stderr)
 sys.exit(1)
'''


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def transfer(package, host, remote, proxy, output):
    package = Path(package).resolve()
    manifest = json.loads((package / 'manifest.json').read_text())
    payload = package / manifest['payload']
    if manifest['payload'] not in ('sub2api.gz', 'app.bsdiff') or digest(payload) != manifest['payload_sha256']:
        raise DeliveryError('Verified immutable payload required')
    if (not re.fullmatch(r'(?:[\w.-]+@)?[\w.-]+', host)
            or not re.fullmatch(r'/opt/sub2api/releases/[\w.-]+', remote)
            or remote.rsplit('/', 1)[-1] in ('.', '..')):
        raise DeliveryError('Explicit SSH host and contained release directory required')
    credential = subprocess.run(['git', 'credential', 'fill'],
                                input='protocol=https\nhost=github.com\n\n', text=True,
                                capture_output=True, timeout=20)
    if credential.returncode:
        raise DeliveryError('Existing Git credential unavailable')
    fields = dict(line.split('=', 1) for line in credential.stdout.splitlines() if '=' in line)
    token = fields.get('password')
    if not token: raise DeliveryError('Existing Git credential unavailable')
    handlers = [urllib.request.ProxyHandler({'https': proxy})] if proxy else []
    client = urllib.request.build_opener(*handlers)
    redirects = urllib.request.build_opener(*handlers, NoRedirect())
    repo = 'https://api.github.com/repos/huohua325/sub2api'
    def request(url, *, data=None, method=None, content_type='application/json', accept='application/vnd.github+json'):
        return urllib.request.Request(url, data=data, method=method, headers={
            'Authorization': 'Bearer ' + token, 'User-Agent': 'mile-private-delivery',
            'Content-Type': content_type, 'Accept': accept})
    report = dict(started_at=utcnow(), payload_sha256=manifest['payload_sha256'],
                  bytes=payload.stat().st_size, draft_deleted=False, status='running')
    release_id = None
    try:
        body = dict(tag_name='private-delivery-' + package.name, target_commitish=manifest['source_commit'],
                    name='Private deployment transfer', draft=True, prerelease=False)
        with client.open(request(repo + '/releases', data=json.dumps(body).encode()), timeout=30) as response:
            draft = json.load(response)
        release_id = draft['id']; report['draft_id'] = release_id
        write_json(output, report)
        print('Private transfer draft created', flush=True)
        upload = 'https://uploads.github.com/repos/huohua325/sub2api/releases/' + str(release_id) + '/assets?name=' + payload.name
        with client.open(request(upload, data=payload.read_bytes(), content_type='application/octet-stream'), timeout=180) as response:
            asset = json.load(response)
        print('Payload uploaded; downloading to server', flush=True)
        try:
            with redirects.open(request(repo + '/releases/assets/' + str(asset['id']), accept='application/octet-stream'), timeout=30):
                raise DeliveryError('Expected a temporary asset redirect')
        except urllib.error.HTTPError as error:
            if error.code != 302: raise
            url = error.headers['Location']
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme != 'https' or not parsed.hostname.endswith('.githubusercontent.com'):
            raise DeliveryError('Unexpected temporary asset host')
        command = ['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', host,
                   'python3 -c ' + shlex.quote(DOWNLOAD)]
        result = subprocess.run(command, input=json.dumps(dict(url=url, destination=remote + '/' + payload.name,
                                bytes=payload.stat().st_size, sha256=manifest['payload_sha256'])),
                                text=True, capture_output=True, timeout=170)
        if result.returncode:
            raise DeliveryError('Server download or checksum verification failed')
        verified = json.loads(result.stdout)
        if verified != {'sha256': manifest['payload_sha256'], 'bytes': payload.stat().st_size}:
            raise DeliveryError('Unexpected transfer verification')
        report['status'] = 'passed'
    except BaseException:
        report['status'] = 'failed'
        raise
    finally:
        if release_id is not None:
            try:
                with client.open(request(repo + '/releases/' + str(release_id), method='DELETE'), timeout=30): pass
                report['draft_deleted'] = True
            except Exception:
                report['status'] = 'cleanup-required'
        report['finished_at'] = utcnow()
        write_json(output, report)
    if not report['draft_deleted']: raise DeliveryError('Temporary private draft requires cleanup')
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--host', required=True)
    parser.add_argument('--remote-release', required=True)
    parser.add_argument('--proxy')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(transfer(args.package, args.host, args.remote_release, args.proxy, args.output), indent=2))
    except Exception as error:
        # Exception text can include a signed URL. Keep only the safe type/status.
        print('Private transfer failed: ' + type(error).__name__)
        raise SystemExit(1)
