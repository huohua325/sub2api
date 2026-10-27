"""Serve a local production build with SPA fallback for browser review.

Usage: python tools/serve_frontend_review.py backend/internal/web/dist 4173
"""
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import sys

class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        requested = Path(self.translate_path(self.path.split('?', 1)[0]))
        if not requested.exists() and not Path(self.path.split('?', 1)[0]).suffix:
            self.path = '/index.html'
        super().do_GET()

    def log_message(self, *args):
        pass

if __name__ == '__main__':
    directory = Path(sys.argv[1]).resolve()
    port = int(sys.argv[2])
    server = ThreadingHTTPServer(('127.0.0.1', port), partial(Handler, directory=str(directory)))
    print(f'SPA review: http://127.0.0.1:{port} ({directory})', flush=True)
    server.serve_forever()
