import bz2
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class DeltaTests(unittest.TestCase):
    def test_reconstruction_does_not_truncate_a_linked_temporary_name(self):
        with tempfile.TemporaryDirectory() as tmp:
            folder = Path(tmp); active = folder / 'active'; active.write_bytes(b'old binary')
            old_hash = hashlib.sha256(b'old binary').hexdigest()
            target = b'new binary'
            control = bz2.compress(bytes(8) + len(target).to_bytes(8, 'little') + bytes(8))
            difference = bz2.compress(b'')
            patch = b'BSDIFF40' + len(control).to_bytes(8, 'little') + len(difference).to_bytes(8, 'little') + len(target).to_bytes(8, 'little') + control + difference + bz2.compress(target)
            delta = folder / 'app.bsdiff'; delta.write_bytes(patch)
            os.link(active, folder / 'sub2api.reconstructed')
            process = subprocess.run([sys.executable, str(Path(__file__).resolve().parents[1] / 'apply_delta.py'),
                                      str(active), str(delta), str(folder / 'sub2api'), old_hash,
                                      hashlib.sha256(target).hexdigest()], capture_output=True, text=True)
            self.assertEqual(process.returncode, 0, process.stderr)
            self.assertEqual((folder / 'sub2api').read_bytes(), target)
            self.assertEqual(active.read_bytes(), b'old binary', 'Staging overwrote the active hardlinked program')


if __name__ == '__main__': unittest.main()
