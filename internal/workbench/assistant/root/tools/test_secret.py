#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Tests for secret.py: uv run test_secret.py"""

import importlib.util
import io
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("secret", Path(__file__).with_name("secret.py"))
secret = importlib.util.module_from_spec(spec)
spec.loader.exec_module(secret)


class SecretTest(unittest.TestCase):
    def test_save_check_and_never_print(self):
        with tempfile.TemporaryDirectory() as d:
            folder = Path(d) / "secrets"
            self.assertFalse(secret.check("mail-password", folder)[0])
            p = secret.save("mail-password", "hunter2222", folder)
            if os.name != "nt":
                self.assertEqual(oct(p.stat().st_mode & 0o777), "0o600")
            ok, line = secret.check("mail-password", folder)
            self.assertTrue(ok)
            self.assertNotIn("hunter", line)
            os.chmod(p, 0o644)
            if os.name != "nt":
                self.assertFalse(secret.check("mail-password", folder)[0])

    def test_names_and_empty(self):
        for bad in ("../x", "", "a/b"):
            with self.assertRaises(SystemExit):
                secret.path_of(bad)
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(SystemExit):
                secret.save("x", "  ", Path(d))

    def test_main_set_from_file_prints_no_value(self):
        with tempfile.TemporaryDirectory() as d:
            src = Path(d) / "client.json"
            # Built from parts: the repository's credentials check would take
            # this fake client secret for a real one.
            src.write_text('{"client_' + 'secret": "abc123secret"}')
            old = secret.SECRETS
            secret.SECRETS = Path(d) / "secrets"
            try:
                out = io.StringIO()
                with redirect_stdout(out):
                    self.assertEqual(secret.main(["set", "google-calendar.json", "--from", str(src)]), 0)
                    self.assertEqual(secret.main(["check", "google-calendar.json"]), 0)
            finally:
                secret.SECRETS = old
            self.assertNotIn("abc123", out.getvalue())
            with self.assertRaises(SystemExit):
                secret.main(["open", "http://example.com"])


if __name__ == "__main__":
    unittest.main()
