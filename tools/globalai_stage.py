"""Private resumable SSH staging for exact GlobalAI reconciliation evidence.

Only the evidence/plan payload is staged. Execution controls, credentials,
database connection strings and backup choices never enter the large artifact.
"""
from __future__ import annotations

import base64
from datetime import datetime, timezone
import hashlib
import json
import re
import subprocess
import time
import zlib


class StageError(Exception):
    """A bounded safe reconciliation transport error."""


def compact(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(',', ':')).encode()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def artifact(payload, chunk_bytes=256 << 10):
    if not isinstance(payload, dict) or set(payload) != {'schema', 'evidence_base64', 'plan'} or payload['schema'] != 1:
        raise StageError('Only the exact evidence/plan payload may be staged')
    if chunk_bytes not in (256 << 10, 512 << 10):
        raise StageError('Staging chunks must be 256 or 512 KiB')
    raw = compact(payload)
    if not raw or len(raw) > 64 << 20:
        raise StageError('Reconciliation payload exceeds the formal CLI size bound')
    blob = zlib.compress(raw)
    chunks = [blob[i:i+chunk_bytes] for i in range(0, len(blob), chunk_bytes)]
    manifest = {'schema': 1, 'encoding': 'zlib', 'payload_sha256': digest(raw), 'payload_bytes': len(raw),
                'blob_sha256': digest(blob), 'blob_bytes': len(blob), 'chunk_bytes': chunk_bytes,
                'chunks': [{'index': i, 'bytes': len(chunk), 'sha256': digest(chunk)} for i, chunk in enumerate(chunks)]}
    return manifest, chunks


# Shared by stage RPCs and the small final execution control script. All paths
# are fixed or lowercase SHA256 components. Parent paths and each artifact file
# reject links; private directories exclude other users before writes begin.
REMOTE_LIBRARY = r'''
import hashlib,json,os,pathlib,re,stat,time,uuid,zlib
STAGING_ROOT=pathlib.Path('/opt/sub2api/reconciliation')
def _hash(data):return hashlib.sha256(data).hexdigest()
def _compact(value):return json.dumps(value,ensure_ascii=True,sort_keys=True,separators=(',',':')).encode()
def _link(path):return path.is_symlink() or (hasattr(path,'is_junction') and path.is_junction())
def _private_dir(path):
 if _link(path):raise ValueError('Staging directory cannot be a link')
 if path.exists() and not path.is_dir():raise ValueError('Staging directory is not a directory')
 path.mkdir(mode=0o700,exist_ok=True)
 if os.name!='nt' and path.stat().st_uid!=os.getuid():raise ValueError('Staging directory owner changed')
 os.chmod(path,0o700)
def _regular(path):
 if _link(path):raise ValueError('Staging file cannot be a link')
 if not path.exists():return False
 if not stat.S_ISREG(path.stat().st_mode):raise ValueError('Staging file is not regular')
 if os.name!='nt' and path.stat().st_uid!=os.getuid():raise ValueError('Staging file owner changed')
 os.chmod(path,0o600)
 return True
def _write(path,data):
 _regular(path)
 temporary=path.with_name('.'+path.name+'.'+uuid.uuid4().hex+'.tmp')
 try:
  fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL|getattr(os,'O_NOFOLLOW',0),0o600)
  with os.fdopen(fd,'wb') as output:output.write(data);output.flush();os.fsync(output.fileno())
  _regular(path)
  os.replace(temporary,path);os.chmod(path,0o600)
 finally:
  if temporary.exists():temporary.unlink()
def _validate_manifest(manifest):
 if manifest.get('schema')!=1 or manifest.get('encoding')!='zlib':raise ValueError('Invalid staging schema')
 for key in ('blob_sha256','payload_sha256'):
  if not re.fullmatch('[0-9a-f]{64}',manifest.get(key,'')):raise ValueError('Invalid staging digest')
 for key in ('payload_bytes','blob_bytes'):
  if type(manifest.get(key)) is not int or not 0<manifest[key]<=64<<20:raise ValueError('Invalid staging size')
 size=manifest.get('chunk_bytes');chunks=manifest.get('chunks')
 if size not in (256<<10,512<<10) or not isinstance(chunks,list) or not 1<=len(chunks)<=256:raise ValueError('Invalid staging chunk layout')
 total=0
 for i,item in enumerate(chunks):
  if item.get('index')!=i or type(item.get('bytes')) is not int or not 0<item['bytes']<=size or not re.fullmatch('[0-9a-f]{64}',item.get('sha256','')):raise ValueError('Invalid staging chunk')
  if i<len(chunks)-1 and item['bytes']!=size:raise ValueError('Non-final staging chunk is short')
  total+=item['bytes']
 if total!=manifest['blob_bytes']:raise ValueError('Staging chunk total differs')
def _directory(blob_sha):
 if not re.fullmatch('[0-9a-f]{64}',blob_sha):raise ValueError('Invalid staging path identity')
 for parent in [STAGING_ROOT,*STAGING_ROOT.parents]:
  if _link(parent):raise ValueError('Staging parent cannot be a link')
 if not STAGING_ROOT.parent.is_dir():raise ValueError('Expected deployment root is missing')
 _private_dir(STAGING_ROOT)
 folder=STAGING_ROOT/blob_sha;_private_dir(folder)
 return folder
def _manifest(manifest):
 _validate_manifest(manifest);folder=_directory(manifest['blob_sha256']);path=folder/'manifest.json';encoded=_compact(manifest)
 if _regular(path):
  if path.stat().st_size>1<<20 or path.read_bytes()!=encoded:raise ValueError('Staging manifest conflicts with existing artifact')
 else:_write(path,encoded)
 return folder
def _chunk_path(folder,index):return folder/('chunk-%06d.bin'%index)
def _chunk_ok(folder,item):
 path=_chunk_path(folder,item['index'])
 return _regular(path) and path.stat().st_size==item['bytes'] and _hash(path.read_bytes())==item['sha256']
def _verified_blob(folder,manifest):
 path=folder/'payload.zlib'
 if not _regular(path) or path.stat().st_size!=manifest['blob_bytes']:raise ValueError('Verified staged payload is missing or truncated')
 blob=path.read_bytes()
 if _hash(blob)!=manifest['blob_sha256']:raise ValueError('Staged compressed payload digest differs')
 decompressor=zlib.decompressobj();output=decompressor.decompress(blob,manifest['payload_bytes']+1)
 if len(output)!=manifest['payload_bytes'] or not decompressor.eof or decompressor.unused_data or decompressor.unconsumed_tail:raise ValueError('Staged payload size or zlib stream differs')
 if _hash(output)!=manifest['payload_sha256']:raise ValueError('Staged JSON payload digest differs')
 return output
def _prepare(manifest):
 folder=_manifest(manifest);missing=[item['index'] for item in manifest['chunks'] if not _chunk_ok(folder,item)]
 ready=False
 if _regular(folder/'payload.zlib'):
  try:_verified_blob(folder,manifest);ready=True
  except ValueError:pass
 return {'missing':missing,'ready':ready}
def _put(manifest,index,data):
 folder=_manifest(manifest)
 if type(index) is not int or not 0<=index<len(manifest['chunks']):raise ValueError('Invalid staging chunk index')
 item=manifest['chunks'][index]
 if len(data)!=item['bytes'] or _hash(data)!=item['sha256']:raise ValueError('Uploaded staging chunk digest differs')
 reused=_chunk_ok(folder,item)
 if not reused:_write(_chunk_path(folder,index),data)
 if not _chunk_ok(folder,item):raise ValueError('Staging chunk verification failed')
 return {'index':index,'bytes':len(data),'reused':reused}
def _finish(manifest):
 folder=_manifest(manifest)
 if any(not _chunk_ok(folder,item) for item in manifest['chunks']):raise ValueError('Staging chunks are incomplete')
 path=folder/'payload.zlib';temporary=folder/('.payload.'+uuid.uuid4().hex+'.tmp')
 try:
  fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL|getattr(os,'O_NOFOLLOW',0),0o600)
  total=hashlib.sha256();size=0
  with os.fdopen(fd,'wb') as output:
   for item in manifest['chunks']:
    data=_chunk_path(folder,item['index']).read_bytes();total.update(data);size+=len(data);output.write(data)
   output.flush();os.fsync(output.fileno())
  if size!=manifest['blob_bytes'] or total.hexdigest()!=manifest['blob_sha256']:raise ValueError('Assembled staging payload digest differs')
  _regular(path);os.replace(temporary,path);os.chmod(path,0o600)
 finally:
  if temporary.exists():temporary.unlink()
 _verified_blob(folder,manifest)
 _write(folder/'ready.json',_compact({'schema':1,'blob_sha256':manifest['blob_sha256'],'payload_sha256':manifest['payload_sha256'],'verified_at_unix':int(time.time())}))
 return {'ready':True,'directory':str(folder),'blob_bytes':manifest['blob_bytes'],'payload_bytes':manifest['payload_bytes']}
def _load_reference(reference):
 folder=_directory(reference.get('blob_sha256',''));path=folder/'manifest.json'
 if not _regular(path) or path.stat().st_size>1<<20:raise ValueError('Staging manifest is missing')
 raw=path.read_bytes()
 if _hash(raw)!=reference.get('manifest_sha256'):raise ValueError('Staging manifest digest differs from reviewed artifact')
 manifest=json.loads(raw);_validate_manifest(manifest)
 if manifest['blob_sha256']!=reference['blob_sha256'] or manifest['payload_sha256']!=reference.get('payload_sha256'):raise ValueError('Staged payload reference conflicts')
 return _verified_blob(folder,manifest)
'''


def remote_script(action, manifest, chunk=None, index=None):
    data = {'action': action, 'manifest': manifest}
    if chunk is not None:
        data.update(index=index, chunk=base64.b64encode(chunk).decode())
    # This is Python stdin, never a shell command. repr preserves one literal
    # JSON string and avoids a second base64 expansion of each binary chunk.
    return REMOTE_LIBRARY + "\nimport base64\nrequest=json.loads(" + repr(compact(data).decode()) + ")\n" + r'''
try:
 action=request['action'];manifest=request['manifest']
 if action=='prepare':result=_prepare(manifest)
 elif action=='chunk':result=_put(manifest,request['index'],base64.b64decode(request['chunk'],validate=True))
 elif action=='finish':result=_finish(manifest)
 else:raise ValueError('Unknown staging action')
 print(json.dumps({'ok':True,'result':result},sort_keys=True))
except Exception as error:
 print(json.dumps({'ok':False,'error':str(error)},sort_keys=True))
 raise SystemExit(1)
'''


def ssh_rpc(host, action, manifest, chunk=None, index=None, timeout=45):
    code = remote_script(action, manifest, chunk, index)
    try:
        result = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=12', host, 'python3 -'],
                                input=code, text=True, encoding='utf-8', capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired as error:
        raise StageError('Private staging RPC timed out; verified chunks are retained for resume') from error
    try:
        response = json.loads(result.stdout)
    except ValueError as error:
        raise StageError('Private staging RPC did not return a verified result; resume existing chunks') from error
    if result.returncode or not response.get('ok'):
        raise StageError('Private staging rejected the artifact: ' + str(response.get('error', 'transport failure')))
    return response['result']


def stage_payload(host, payload, chunk_bytes=256 << 10, progress=None, rpc=ssh_rpc, max_new_chunks=None):
    manifest, chunks = artifact(payload, chunk_bytes)
    started = time.monotonic()
    report = {'schema': 1, 'status': 'staging', 'started_at': datetime.now(timezone.utc).isoformat(),
              'manifest': manifest, 'manifest_sha256': digest(compact(manifest)), 'uploaded_chunks': [],
              'directory': '/opt/sub2api/reconciliation/' + manifest['blob_sha256']}
    def checkpoint():
        report['elapsed_seconds'] = round(time.monotonic() - started, 3)
        if progress:
            progress(report)
    checkpoint()
    try:
        prepared = rpc(host, 'prepare', manifest)
        missing = prepared['missing']
        if not isinstance(missing, list) or any(type(i) is not int or not 0 <= i < len(chunks) for i in missing):
            raise StageError('Remote staging returned invalid missing chunk indices')
        report['reused_chunks'] = len(chunks) - len(missing)
        checkpoint()
        if prepared.get('ready'):
            report['status'] = 'ready'
            checkpoint()
            return report
        for number, index in enumerate(missing):
            if max_new_chunks is not None and number >= max_new_chunks:
                report['status'] = 'partial'
                checkpoint()
                return report
            began = time.monotonic()
            result = rpc(host, 'chunk', manifest, chunks[index], index)
            if result.get('index') != index or result.get('bytes') != len(chunks[index]):
                raise StageError('Remote chunk acknowledgement differs')
            report['uploaded_chunks'].append({'index': index, 'bytes': len(chunks[index]),
                                              'elapsed_seconds': round(time.monotonic() - began, 3)})
            checkpoint()
        finished = rpc(host, 'finish', manifest)
        if finished.get('ready') is not True:
            raise StageError('Private staging did not verify the assembled payload')
        report['status'] = 'ready'
        checkpoint()
        return report
    except StageError:
        report['status'] = 'partial'
        checkpoint()
        raise


def stage_reference(report, payload):
    if report.get('status') != 'ready' or not isinstance(report.get('manifest'), dict):
        raise StageError('Execution needs a verified ready staging report')
    manifest = report['manifest']
    if (manifest.get('payload_sha256') != digest(compact(payload))
            or report.get('manifest_sha256') != digest(compact(manifest))
            or not re.fullmatch(r'[0-9a-f]{64}', manifest.get('blob_sha256', ''))):
        raise StageError('Staged artifact no longer matches the exact evidence and plan')
    return {'blob_sha256': manifest['blob_sha256'], 'payload_sha256': manifest['payload_sha256'],
            'manifest_sha256': report['manifest_sha256']}
