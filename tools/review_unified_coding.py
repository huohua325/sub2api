"""Review unified coding with synthetic credentials and fully mocked APIs."""
import argparse
import json
from pathlib import Path

from delivery_core import tree_hash, write_json
from review_guide import run

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument('--base-url')
    target.add_argument('--artifact', action='store_true')
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--engines', nargs='+', choices=['chromium', 'webkit'], default=['chromium', 'webkit'])
    args = parser.parse_args()
    if args.artifact:
        from delivery import preview
        root = Path(__file__).resolve().parents[1]
        with preview(root / 'backend/internal/web/dist') as url:
            args.base_url = url
            run(args, scenario='review_unified_coding.cjs')
        result = args.output / 'result.json'
        evidence = json.loads(result.read_text())
        evidence['frontend_sha256'] = tree_hash(root, ['backend/internal/web/dist'])['backend/internal/web/dist']
        write_json(result, evidence)
    else:
        run(args, scenario='review_unified_coding.cjs')
