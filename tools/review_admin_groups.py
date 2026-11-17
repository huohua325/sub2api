"""Check OpenAI group tier controls with mocked business APIs in real browsers."""
import argparse
import json
from pathlib import Path

from review_guide import run
from delivery_core import tree_hash, write_json


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument('--base-url')
    target.add_argument('--artifact', action='store_true')
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--engines', nargs='+', choices=['chromium', 'webkit'], default=['chromium', 'webkit'])
    args = parser.parse_args()
    if args.artifact:
        from delivery import preview
        with preview(Path(__file__).resolve().parents[1] / 'backend/internal/web/dist') as url:
            args.base_url = url
            run(args, scenario='review_admin_groups.cjs')
        path = args.output / 'result.json'
        result = json.loads(path.read_text())
        result['frontend_sha256'] = tree_hash(Path(__file__).resolve().parents[1], ['backend/internal/web/dist'])['backend/internal/web/dist']
        write_json(path, result)
    else:
        run(args, scenario='review_admin_groups.cjs')


if __name__ == '__main__': main()
