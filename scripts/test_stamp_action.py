#!/usr/bin/env python3
"""Exercise the composite action's actual shell body with synthetic git notes."""
import base64
import json
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[1]
ACTION = ROOT / '.github/actions/local-ci-checker/action.yml'


def action_script(source):
    return textwrap.dedent(source.split('      run: |\n', 1)[1])


class StampActionTests(unittest.TestCase):
    def run_action(self, stamp, check='test', source=None):
        with tempfile.TemporaryDirectory() as directory:
            git = Path(directory) / 'git'
            git.write_text('#!/bin/sh\nprintf "%s" "$TEST_NOTE"\n')
            git.chmod(0o755)
            env = dict(os.environ, PATH=directory + os.pathsep + os.environ['PATH'],
                       TEST_NOTE=base64.b64encode(json.dumps(stamp).encode()).decode() if stamp else '',
                       HEAD_SHA='123abc', BASE_REF='main', LCI_CHECK=check)
            return subprocess.run(['bash', '-c', action_script(source or ACTION.read_text())],
                                  env=env, text=True, capture_output=True)

    def test_gate_rejects_missing_or_mismatched_records(self):
        valid = dict(commitSha='123abc', baseBranch='main', checksStatus={'test': True})
        for patch in ({}, {'commitSha': 'other'}, {'baseBranch': 'other'}, {'checksStatus': {'test': False}}):
            with self.subTest(patch=patch):
                stamp = dict(valid, **patch) if patch else None
                self.assertNotEqual(self.run_action(stamp).returncode, 0)
        self.assertEqual(self.run_action(valid).returncode, 0)

    def test_stamp_fields_cannot_inject_workflow_commands(self):
        payload = 'bad%\r\n::warning::injected'
        for field in ('commitSha', 'baseBranch', 'check'):
            with self.subTest(field=field):
                stamp = dict(commitSha='123abc', baseBranch='main', checksStatus={'test': True})
                if field != 'check':
                    stamp[field] = payload
                result = self.run_action(stamp, check=payload if field == 'check' else 'test')
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('\n::warning::', result.stdout)
                self.assertIn('bad%25%0D%0A::warning::injected', result.stdout)

    def test_success_does_not_echo_untrusted_check_name(self):
        check = 'test\n::warning::injected'
        result = self.run_action(dict(commitSha='123abc', baseBranch='main', checksStatus={check: True}), check=check)
        self.assertEqual(result.returncode, 0)
        self.assertNotIn('::warning::', result.stdout)


if __name__ == '__main__':
    unittest.main()
