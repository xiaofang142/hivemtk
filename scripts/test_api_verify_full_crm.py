#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""api_verify_full CRM/SC-001 纯 Python 回归（不连 DB、不发 HTTP）。"""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("api_verify_full.py")
os.environ.setdefault("HIVEMTK_DB_PASSWORD", "unit-test-placeholder")
os.environ.setdefault("HIVEMTK_ADMIN_PASS", "unit-test-placeholder")
SPEC = importlib.util.spec_from_file_location("api_verify_full_crm_test_target", SCRIPT)
MOD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MOD)


class SC001SampleTests(unittest.TestCase):
    def test_sample_has_exactly_100_frozen_pairs_and_200_unique_customers(self):
        sample = MOD.build_sc001_sample("unitrun")
        self.assertEqual(len(sample["pairs"]), 100)
        self.assertEqual(len(sample["customers"]), 200)
        keys = [row["key"] for row in sample["customers"]]
        self.assertEqual(len(keys), len(set(keys)))
        initial_phones = [row["initial_phone"] for row in sample["customers"]]
        self.assertEqual(len(initial_phones), len(set(initial_phones)))

        counts = {}
        truths = {}
        for pair in sample["pairs"]:
            counts[pair["category"]] = counts.get(pair["category"], 0) + 1
            truths[pair["ground_truth"]] = truths.get(pair["ground_truth"], 0) + 1
        self.assertEqual(counts, {
            "P01": 15, "P02": 10, "P03": 10, "P04": 5,
            "P05": 5, "P06": 5,
            "N01": 20, "N02": 5, "N03": 15, "N04": 5, "N05": 5,
        })
        self.assertEqual(truths, {"duplicate": 50, "nonduplicate": 50})

    def test_sample_is_deterministic_for_same_run_and_rejects_bad_run_id(self):
        self.assertEqual(MOD.build_sc001_sample("unitrun"), MOD.build_sc001_sample("unitrun"))
        with self.assertRaises(ValueError):
            MOD.build_sc001_sample("bad run id")

    def test_positive_pairs_share_expected_identity_without_sharing_unified_seed(self):
        sample = MOD.build_sc001_sample("unitrun")
        customers = {row["key"]: row for row in sample["customers"]}
        for pair in sample["pairs"]:
            left = customers[pair["left_key"]]
            right = customers[pair["right_key"]]
            self.assertNotEqual(left["initial_phone"], right["initial_phone"])
            if pair["category"] in ("P01", "P02"):
                self.assertEqual(left["phone"], right["phone"])
            elif pair["category"] in ("P03", "P04"):
                self.assertEqual(left["email"], right["email"])
            elif pair["category"] == "P05":
                self.assertEqual(left["external_id"], right["external_id"])
            elif pair["category"] == "P06":
                self.assertGreaterEqual(MOD.NameSimilarityScore(left["name"], right["name"]), 85)
                self.assertNotEqual(left["phone"], right["phone"])
                self.assertNotEqual(left["email"], right["email"])

    def test_cross_account_categories_use_two_accounts(self):
        sample = MOD.build_sc001_sample("unitrun")
        customers = {row["key"]: row for row in sample["customers"]}
        for pair in sample["pairs"]:
            if pair["category"] in ("P02", "P04"):
                self.assertNotEqual(
                    customers[pair["left_key"]]["source_account_id"],
                    customers[pair["right_key"]]["source_account_id"],
                )

    def test_phone_fixtures_are_run_scoped_and_prefix_negative_stays_below_threshold(self):
        first = MOD.build_sc001_sample("runA")
        second = MOD.build_sc001_sample("runB")
        first_phones = {row[field] for row in first["customers"] for field in ("initial_phone", "phone")}
        second_phones = {row[field] for row in second["customers"] for field in ("initial_phone", "phone")}
        self.assertTrue(first_phones.isdisjoint(second_phones))

        customers = {row["key"]: row for row in first["customers"]}
        for pair in first["pairs"]:
            left, right = customers[pair["left_key"]], customers[pair["right_key"]]
            if pair["category"] == "N03":
                self.assertLess(MOD.NameSimilarityScore(left["name"], right["name"]), 85)
            if pair["category"] == "N04":
                self.assertEqual(left["phone"][:7], right["phone"][:7])
                self.assertNotEqual(left["phone"], right["phone"])


class SC001EvidenceTests(unittest.TestCase):
    def test_writer_persists_frozen_fixture_labels_scan_and_summary(self):
        writer = getattr(MOD, "write_sc001_evidence", None)
        self.assertTrue(callable(writer), "缺少 SC-001 取证文件写入器")
        sample = MOD.build_sc001_sample("evidence")
        id_map = {row["key"]: f"id-{index}" for index, row in enumerate(sample["customers"])}
        candidates = []
        for pair in sample["pairs"]:
            if pair["ground_truth"] == "duplicate":
                candidates.append({
                    "left_id": id_map[pair["left_key"]], "right_id": id_map[pair["right_key"]],
                    "action": "auto", "rule": "fixture", "score": 100,
                })
        result = MOD.score_sc001(sample["pairs"], candidates, id_map=id_map)
        cleanup = {"deleted": True, "remaining": 0}
        with tempfile.TemporaryDirectory() as directory:
            written = writer(directory, sample, id_map, {"list": candidates}, result, cleanup)
            self.assertEqual(Path(written), Path(directory))
            self.assertEqual(len(json.loads((Path(directory) / "labels.json").read_text())), 100)
            self.assertEqual(len((Path(directory) / "labels.csv").read_text().splitlines()), 101)
            self.assertEqual(json.loads((Path(directory) / "scan-response.json").read_text()), {"list": candidates})
            summary = json.loads((Path(directory) / "summary.json").read_text())
            self.assertEqual(summary["accuracy"], 1.0)
            self.assertEqual(summary["cleanup"], cleanup)


class CRMSafetyTests(unittest.TestCase):
    def test_external_ai_calls_are_opt_in(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            self.assertFalse(MOD.external_ai_calls_allowed())
        with mock.patch.dict(os.environ, {"HIVEMTK_ALLOW_EXTERNAL_AI": "1"}, clear=True):
            self.assertTrue(MOD.external_ai_calls_allowed())

    def test_operation_log_cleanup_is_scoped_to_captured_ids(self):
        cleaner = getattr(MOD, "cleanup_crm_operation_logs", None)
        self.assertTrue(callable(cleaner), "缺少 CRM 审计日志安全清理器")
        calls = []

        def execute(sql, params):
            calls.append((sql, params))

        cleaner(10, ["customer-1"], [11, 12], execute)
        self.assertEqual(len(calls), 1)
        sql, params = calls[0]
        self.assertIn("id = ANY(%s)", sql)
        self.assertIn("resource_id = ANY(%s)", sql)
        self.assertNotIn("action", sql.lower())
        self.assertEqual(params, ([11, 12], 10, ["customer-1"]))


class CRMAICallTests(unittest.TestCase):
    def test_ai_profile_request_is_skipped_without_explicit_opt_in(self):
        client = mock.Mock()
        old_degraded = MOD.DEGRADED[:]
        MOD.DEGRADED.clear()
        try:
            with mock.patch.dict(os.environ, {}, clear=True):
                MOD.verify_crm_ai_profile(client, "customer-1")
            client.req.assert_not_called()
            self.assertTrue(any("未设置 HIVEMTK_ALLOW_EXTERNAL_AI=1" in item for item in MOD.DEGRADED))
        finally:
            MOD.DEGRADED[:] = old_degraded

    def test_ai_profile_request_requires_explicit_opt_in(self):
        client = mock.Mock()
        old_report, old_degraded = MOD.REPORT[:], MOD.DEGRADED[:]
        MOD.REPORT.clear()
        MOD.DEGRADED.clear()
        client.req.return_value = (None, False)
        try:
            with mock.patch.dict(os.environ, {"HIVEMTK_ALLOW_EXTERNAL_AI": "1"}, clear=True):
                MOD.verify_crm_ai_profile(client, "customer-1")
            client.req.assert_called_once_with(
                "POST", "/api/manage/crm/ai/analyze", "C16 AI画像(依赖不可用则降级)",
                ok_status=(200, 503), json={"customer_id": "customer-1"},
            )
        finally:
            MOD.REPORT[:] = old_report
            MOD.DEGRADED[:] = old_degraded


class SC001CleanupTests(unittest.TestCase):
    def run_scenario(self, create_limit, cleanup_failure):
        sample = MOD.build_sc001_sample("cleanup")
        id_map = {}

        def request(method, path, *args, **kwargs):
            if method == "POST" and path == "/api/manage/crm/customers":
                if len(id_map) >= create_limit:
                    return None, False
                key = sample["customers"][len(id_map)]["key"]
                id_map[key] = f"customer-{len(id_map)}"
                return {"code": 0, "data": {"id": id_map[key]}}, True
            if method == "PUT":
                return {"code": 0, "data": {}}, True
            if method == "POST" and path == "/api/manage/crm/dedupe/scan":
                candidates = [{
                    "left_id": id_map[pair["left_key"]],
                    "right_id": id_map[pair["right_key"]],
                    "action": "auto", "rule": "fixture", "score": 100,
                } for pair in sample["pairs"] if pair["ground_truth"] == "duplicate"]
                return {"code": 0, "data": {"list": candidates}}, True
            self.fail(f"未预期的请求: {method} {path}")

        def execute(sql, params=()):
            if cleanup_failure and sql.startswith("DELETE"):
                raise RuntimeError("cleanup failed")

        old_report, old_degraded = MOD.REPORT[:], MOD.DEGRADED[:]
        MOD.REPORT.clear()
        MOD.DEGRADED.clear()
        output = io.StringIO()
        try:
            with mock.patch.object(MOD, "login_admin", return_value="unit-token"), \
                    mock.patch.object(MOD, "APIClient", return_value=mock.Mock(req=request)), \
                    mock.patch.object(MOD, "build_sc001_sample", return_value=sample), \
                    mock.patch.object(MOD, "q1", return_value={"id": 0}), \
                    mock.patch.object(MOD, "qexec", side_effect=execute) as qexec, \
                    contextlib.redirect_stdout(output):
                rc = MOD.run_sc001(keep=False)
            return rc, MOD.REPORT[:], output.getvalue(), qexec.call_args_list
        finally:
            MOD.REPORT[:] = old_report
            MOD.DEGRADED[:] = old_degraded

    def test_cleanup_failure_is_included_in_exit_code(self):
        rc, report, output, _ = self.run_scenario(200, cleanup_failure=True)
        self.assertEqual(rc, 1)
        self.assertTrue(any(name.startswith("SC001-CLEAN") and not ok for name, ok, _ in report))
        self.assertTrue(all(ok for name, ok, _ in report if not name.startswith("SC001-CLEAN")))
        self.assertIn("  ✗ SC001-CLEAN", output)

    def test_partial_create_cleanup_failure_is_included_in_summary(self):
        rc, report, output, calls = self.run_scenario(1, cleanup_failure=True)
        self.assertEqual(rc, 1)
        self.assertTrue(any(name.startswith("SC001-CLEAN") and not ok for name, ok, _ in report))
        self.assertEqual(calls[0].args[1], (0, ["customer-0"]))
        self.assertIn("  ✗ SC001-CLEAN", output)

    def test_all_creates_failed_skips_sql_and_reports_cleanup_before_summary(self):
        rc, report, output, calls = self.run_scenario(0, cleanup_failure=True)
        self.assertEqual(rc, 1)
        self.assertEqual(calls, [])
        self.assertTrue(any(name.startswith("SC001-CLEAN") and ok for name, ok, _ in report))
        self.assertLess(output.index("SC001-CLEAN"), output.index("【链路验收报告】"))


class SC001ScoringTests(unittest.TestCase):
    def test_perfect_candidate_classification_scores_100(self):
        sample = MOD.build_sc001_sample("score")
        candidates = []
        for pair in sample["pairs"]:
            if pair["ground_truth"] == "duplicate":
                candidates.append({
                    "left_id": pair["left_key"], "right_id": pair["right_key"],
                    "action": "auto", "rule": "fixture", "score": 100,
                })
        result = MOD.score_sc001(sample["pairs"], candidates)
        self.assertEqual((result["tp"], result["fp"], result["tn"], result["fn"]), (50, 0, 50, 0))
        self.assertEqual(result["accuracy"], 1.0)
        self.assertIn("50/50", MOD.format_sc001_summary(result))

    def test_review_predictions_are_counted_and_broken_out(self):
        sample = MOD.build_sc001_sample("review")
        candidates = []
        for pair in sample["pairs"]:
            action = "review" if pair["category"] in ("P06", "N02") else "auto"
            if pair["ground_truth"] == "duplicate" or pair["category"] == "N02":
                candidates.append({
                    "left_id": pair["left_key"], "right_id": pair["right_key"],
                    "action": action, "rule": "fixture", "score": 90,
                })
        result = MOD.score_sc001(sample["pairs"], candidates)
        self.assertEqual(result["review"]["tp"], 5)
        self.assertEqual(result["review"]["fp"], 5)
        self.assertEqual(result["auto"]["tp"], 45)
        self.assertEqual(result["auto"]["fp"], 0)
        self.assertEqual(result["accuracy"], 0.95)

    def test_scorer_rejects_unknown_action_and_missing_id_mapping(self):
        sample = MOD.build_sc001_sample("errors")
        pair = sample["pairs"][0]
        with self.assertRaises(ValueError):
            MOD.score_sc001(sample["pairs"], [{
                "left_id": pair["left_key"], "right_id": pair["right_key"],
                "action": "maybe", "rule": "fixture", "score": 1,
            }])
        with self.assertRaises(KeyError):
            MOD.score_sc001(sample["pairs"], [], id_map={})


class CRMPayloadTests(unittest.TestCase):
    def test_customer_create_payload_uses_seed_identity_and_never_sends_external_id(self):
        spec = MOD.build_sc001_sample("payload")["customers"][0]
        payload = MOD.crm_customer_create_payload(spec)
        self.assertEqual(payload["phone"], spec["initial_phone"])
        self.assertNotIn("external_id", payload)
        self.assertNotIn("unified_id", payload)
        self.assertEqual(payload["source_account_id"], spec["source_account_id"])

    def test_customer_update_payload_applies_frozen_final_identity(self):
        sample = MOD.build_sc001_sample("payload")
        pair = next(row for row in sample["pairs"] if row["category"] == "P01")
        spec = next(row for row in sample["customers"] if row["key"] == pair["right_key"])
        payload = MOD.crm_customer_update_payload(spec)
        self.assertEqual(payload["phone"], spec["phone"])
        self.assertEqual(payload["email"], spec["email"])
        self.assertNotIn("external_id", payload)

    def test_entity_payload_has_valid_contact_info_json(self):
        payload = MOD.crm_entity_payload("customer-1", "CRM-E2E-contact", "unitrun")
        self.assertEqual(payload["customer_id"], "customer-1")
        self.assertEqual(json.loads(payload["contact_info"])["source"], "crm-e2e")
        self.assertIn("unitrun", payload["source_ref"])


class CRMCLITests(unittest.TestCase):
    def test_parser_accepts_crm_subcommand(self):
        parser = MOD.build_arg_parser()
        self.assertEqual(parser.parse_args(["crm"]).cmd, "crm")

    def test_parser_accepts_sc001_subcommand(self):
        parser = MOD.build_arg_parser()
        self.assertEqual(parser.parse_args(["sc001"]).cmd, "sc001")

    def test_main_dispatches_sc001_without_network(self):
        with mock.patch.object(MOD, "run_sc001", return_value=0, create=True) as run_sc001, \
                mock.patch.object(MOD.sys, "argv", ["api_verify_full.py", "sc001"]):
            with self.assertRaises(SystemExit) as caught:
                MOD.main()
        self.assertEqual(caught.exception.code, 0)
        run_sc001.assert_called_once_with(keep=False)

    def test_main_dispatches_crm_without_network(self):
        with mock.patch.object(MOD, "run_crm", return_value=0, create=True) as run_crm, \
                mock.patch.object(MOD.sys, "argv", ["api_verify_full.py", "crm"]):
            with self.assertRaises(SystemExit) as caught:
                MOD.main()
        self.assertEqual(caught.exception.code, 0)
        run_crm.assert_called_once_with(keep=False)

    def test_api_deep_calls_crm_deep(self):
        class FakeClient:
            def req(self, *args, **kwargs):
                return None, False

        old_report, old_degraded = MOD.REPORT[:], MOD.DEGRADED[:]
        MOD.REPORT.clear()
        MOD.DEGRADED.clear()
        try:
            with mock.patch.object(MOD, "APIClient", return_value=FakeClient()), \
                    mock.patch.object(MOD, "q1", return_value=None), \
                    mock.patch.object(MOD, "qexec"), \
                    mock.patch.object(MOD, "_crm_deep", create=True) as crm_deep, \
                    contextlib.redirect_stdout(io.StringIO()):
                MOD._api_deep("unit-token")
            crm_deep.assert_called_once_with("unit-token")
        finally:
            MOD.REPORT[:] = old_report
            MOD.DEGRADED[:] = old_degraded


class QuickstartContractTests(unittest.TestCase):
    TASK_DIR = Path(__file__).resolve().parents[1] / ".specify/custom-task-crm-lead-channel-20261124"

    def test_quickstart_uses_current_root_isolation_commands_and_routes(self):
        text = (self.TASK_DIR / "quickstart.md").read_text(encoding="utf-8")
        required = (
            "hivemtk-user/", "HIVEMTK_DB_PORT=8217", "POSTGRES_TEST_PORT=8217",
            "python3 scripts/api_verify_full.py api",
            "python3 scripts/api_verify_full.py crm",
            "python3 scripts/api_verify_full.py sc001",
            "POST /api/identify-codes/:channel_id/rotate",
            "POST /api/manage/crm/dedupe/scan",
            "GET /api/manage/crm/merge/audit",
            "GET /api/manage/crm/360/:id",
            "GET /api/manage/crm/cross-industry",
            "POST /api/manage/crm/ai/analyze",
            "same_account_only: false", "expect_reply:false", "HIVEMTK_EVIDENCE_DIR",
            "不得运行 `all`/`chain`",
        )
        for item in required:
            self.assertIn(item, text)
        forbidden = (
            "内层仓 `hivemtk/`", "/api/manage/channels", "/api/manage/channel-accounts",
            "/api/manage/channel-identify-codes", "/api/manage/crm/customers/dedupe",
            "/api/manage/crm/resources/search", "/api/manage/crm/customers/:id/360",
            "/api/manage/crm/customers/:id/ai-profile", "`python3 scripts/api_verify_full.py`",
        )
        for item in forbidden:
            self.assertNotIn(item, text)

    def test_tasks_keep_blocked_runtime_checks_unchecked_and_use_api_subcommand(self):
        text = (self.TASK_DIR / "tasks.md").read_text(encoding="utf-8")
        t066 = next(line for line in text.splitlines() if "T066" in line)
        t070 = next(line for line in text.splitlines() if "T070" in line)
        self.assertIn("- [ ] T066", t066)
        self.assertIn("python3 scripts/api_verify_full.py api", t066)
        self.assertIn("8217", t066)
        self.assertIn("- [ ] T070", t070)
        self.assertIn("quickstart.md", t070)


if __name__ == "__main__":
    unittest.main()
