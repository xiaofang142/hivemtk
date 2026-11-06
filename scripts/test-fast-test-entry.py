#!/usr/bin/env python3
"""Exercise Makefile fast targets without Docker, Go, or database side effects."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class FastTestEntryTest(unittest.TestCase):
    def run_entry(self, target, docker_failure=False, empty_password=False, preflight_failure=False):
        with tempfile.TemporaryDirectory(prefix="test-fast-entry-") as directory:
            folder = Path(directory)
            docker = folder / "docker"
            docker.write_text("""#!/bin/sh
case "$*" in
  *POSTGRES_PASSWORD*)
    [ "$DOCKER_FAILURE" = 1 ] && exit 17
    [ "$EMPTY_PASSWORD" = 1 ] || echo fixture-password
    ;;
  *POSTGRES_USER*) echo fixture-role ;;
  *) exit 18 ;;
esac
""")
            go = folder / "go"
            go.write_text("""#!/bin/sh
printf '%s|%s|%s|%s|%s|%s' "$POSTGRES_TEST_HOST" "$POSTGRES_TEST_PORT" "$POSTGRES_TEST_USER" "$POSTGRES_TEST_PASSWORD" "$POSTGRES_TEST_DBNAME" "$POSTGRES_TEST_DSN" > "$CAPTURE"
case "$*" in
  *TestTestDBAuthenticationPreflight*) [ "$PREFLIGHT_FAILURE" = 1 ] && exit 19 ;;
  *) echo suite >> "$CAPTURE.suites" ;;
esac
exit 0
""")
            docker.chmod(0o755)
            go.chmod(0o755)
            capture = folder / "capture"
            env = dict(os.environ, PATH=f"{folder}:{os.environ['PATH']}",
                       DOCKER_FAILURE=str(int(docker_failure)), EMPTY_PASSWORD=str(int(empty_password)),
                       PREFLIGHT_FAILURE=str(int(preflight_failure)),
                       CAPTURE=str(capture), POSTGRES_TEST_DSN="host=business port=8202 dbname=user_db",
                       POSTGRES_TEST_HOST="business", POSTGRES_TEST_PORT="8202", POSTGRES_TEST_USER="wrong",
                       POSTGRES_TEST_DBNAME="user_db", USER_POSTGRES_TEST_HOST_PORT="8227")
            result = subprocess.run(["make", "--no-print-directory", target], cwd=ROOT, env=env,
                                    capture_output=True, text=True, timeout=15)
            suites = folder / "capture.suites"
            return result, capture.read_text() if capture.exists() else None, suites.exists()

    def test_credential_failure_stops_before_go(self):
        for target in ("test-go-fast", "test-service-fast"):
            with self.subTest(target=target):
                result, capture, _ = self.run_entry(target, docker_failure=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIsNone(capture, "credential-read failure must never run go test")

    def test_empty_password_stops_before_go(self):
        result, capture, _ = self.run_entry("test-service-fast", empty_password=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(capture)

    def test_fast_profile_ignores_inherited_business_dsn(self):
        for target in ("test-go-fast", "test-service-fast"):
            with self.subTest(target=target):
                result, capture, suite = self.run_entry(target)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(capture, "127.0.0.1|8227|fixture-role|fixture-password|user_db_test|")
                self.assertNotIn("fixture-password", result.stdout + result.stderr)
                self.assertTrue(suite)

    def test_authentication_failure_does_not_run_full_suite(self):
        for target in ("test-go-fast", "test-service-fast", "test-go", "test-service"):
            with self.subTest(target=target):
                result, _, suite = self.run_entry(target, preflight_failure=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(suite, "authentication failure must stop before the full suite")


if __name__ == "__main__":
    unittest.main()
