"""Summarize CLI browser reports and gzip sizes after a MILE Lab visual review."""
from pathlib import Path
import gzip
import json
import re
import statistics

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / 'output/playwright/design-system'

def load_cli(path):
    raw = path.read_bytes()
    text = raw.decode('utf-16' if raw.startswith(b'\xff\xfe') else 'utf-8-sig')
    if '### Result' not in text:
        return {'error': text[:1200]}
    text = text.split('### Result', 1)[1].lstrip()
    return json.JSONDecoder().raw_decode(text)[0]

def assets(directory):
    return [{'file': p.name, 'bytes': p.stat().st_size, 'gzip_bytes': len(gzip.compress(p.read_bytes(), mtime=0))}
            for p in sorted((directory / 'assets').iterdir()) if p.is_file()]

def entry_sizes(directory, inventory):
    html = (directory / 'index.html').read_text(encoding='utf-8')
    names = re.findall(r'(?:src|href)="/assets/([^"/]+)"', html)
    lookup = {a['file']: a['gzip_bytes'] for a in inventory}
    sizes = {extension: sum(lookup.get(n, 0) for n in names if n.endswith(extension)) for extension in ['.js', '.css']}
    for script in re.findall(r'<script[^>]+src="(/[^"/]+\.js)"', html):
        sizes['.js'] += len(gzip.compress((directory / script.lstrip('/')).read_bytes(), mtime=0))
    return sizes

def summarize():
    output = {}
    for engine in ['chromium', 'firefox', 'webkit']:
        path = OUTPUT / f'{engine}-review.log'
        if not path.exists():
            continue
        report = load_cli(path)
        path.with_suffix('.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
        if 'error' in report:
            output[engine] = report
            continue
        checks = report['checks']
        output[engine] = {
            'checks': len(checks), 'overflow': [c['name'] for c in checks if c.get('overflow')], 'errors': report['errors'],
            'page_error_banners': [c for c in checks if c.get('error')],
            'interaction': [c for c in checks if c['name'] in ['drawer-keyboard','dialog-keyboard','filter-response','dialog-response','reduced-motion','offscreen-animation']],
        }
        if report.get('measurements'):
            output[engine]['performance'] = []
            for build in ['before', 'after']:
                for path in ['/home', '/dashboard']:
                    samples = [m for m in report['measurements'] if m['build'] == build and m['path'] == path]
                    output[engine]['performance'].append({'build':build,'path':path,'median_lcp_ms':statistics.median(m['lcp'] for m in samples),'max_cls':max(m['cls'] for m in samples)})
            frames = next(c['frames'] for c in checks if c['name'] == 'animation-frames')
            output[engine]['frame_interval_p95_ms'] = sorted(frames)[int(len(frames) * .95) - 1]
    before_dir = OUTPUT / 'baseline-dist'
    after_dir = ROOT / 'backend/internal/web/dist'
    before = json.loads((OUTPUT / 'baseline-assets.json').read_text(encoding='utf-8-sig'))
    after = assets(after_dir)
    (OUTPUT / 'after-assets.json').write_text(json.dumps(after, indent=2), encoding='utf-8')
    output['entry_gzip_bytes'] = {'before':entry_sizes(before_dir,before),'after':entry_sizes(after_dir,after)}
    output['entry_delta_gzip_bytes'] = {k:output['entry_gzip_bytes']['after'][k]-output['entry_gzip_bytes']['before'][k] for k in ['.js','.css']}
    output['all_chunk_delta_gzip_bytes'] = {ext:sum(a['gzip_bytes'] for a in after if a['file'].endswith(ext))-sum(a['gzip_bytes'] for a in before if a['file'].endswith(ext)) for ext in ['.js','.css']}
    output['sora_bytes'] = (ROOT / 'frontend/src/assets/fonts/sora-latin-variable.woff2').stat().st_size
    (OUTPUT / 'summary.json').write_text(json.dumps(output, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(output, ensure_ascii=True, indent=2))

if __name__ == '__main__':
    summarize()
