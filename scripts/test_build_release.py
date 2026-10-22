#!/usr/bin/env python3
"""Check archive/provenance packaging without cross-compiling in a unit test."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
import zipfile

BUILDER = Path(__file__).with_name('build-release.py')


class ReleasePackagingTests(unittest.TestCase):
    def test_matrix_contains_renamed_tools_legal_files_and_provenance(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts').mkdir()
            shutil.copyfile(BUILDER, root / 'scripts/build-release.py')
            for name in ('LICENSE', 'NOTICE', 'README.md'):
                (root / name).write_text(name)
            (root / 'third_party').mkdir()
            (root / 'third_party/LICENSE').write_text('fixture license')
            for args in (['init', '-q'], ['add', '.'], ['-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture']):
                subprocess.run(['git', *args], cwd=root, check=True)
            commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
            fake_go = root.parent / (root.name + '-go')
            fake_go.write_text('#!/usr/bin/env python3\nimport pathlib,sys\nif sys.argv[1] == "version": print("go version go1.27.3 linux/amd64")\nelse: pathlib.Path(sys.argv[sys.argv.index("-o")+1]).write_text(sys.argv[-1])\n')
            fake_go.chmod(0o755)
            try:
                output = root.parent / (root.name + '-out')
                subprocess.run(['python3', str(root / 'scripts/build-release.py'), '--version', 'v0.1.0', '--go', str(fake_go), '--output', str(output)], check=True, capture_output=True)
                source = json.loads((output / 'SOURCE.json').read_text())
                self.assertEqual(source['repository'], 'https://github.com/tetriz-ai/local-ci')
                self.assertEqual(source['commit'], commit)
                self.assertEqual(len(source['targets']), 6)
                for target in source['targets']:
                    system, arch = target.split('/')
                    stem = f'local-ci_0.1.0_{system}_{arch}'
                    if system == 'windows':
                        with zipfile.ZipFile(output / (stem + '.zip')) as archive:
                            entries = {name: archive.read(name) for name in archive.namelist()}
                        suffix = '.exe'
                    else:
                        with tarfile.open(output / (stem + '.tar.gz')) as archive:
                            entries = {member.name: archive.extractfile(member).read() for member in archive.getmembers()}
                        suffix = ''
                    self.assertEqual(entries['local-ci' + suffix], b'./cmd/local-ci')
                    self.assertEqual(entries['deps' + suffix], b'./cmd/deps')
                    self.assertEqual(entries['SOURCE.json'], (output / 'SOURCE.json').read_bytes())
                    self.assertIn('third_party/LICENSE', entries)
                for line in (output / 'checksums.txt').read_text().splitlines():
                    digest, name = line.split('  ', 1)
                    self.assertEqual(digest, hashlib.sha256((output / name).read_bytes()).hexdigest())
            finally:
                fake_go.unlink()
                shutil.rmtree(output, ignore_errors=True)


if __name__ == '__main__':
    unittest.main()
