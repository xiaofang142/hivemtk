#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""api_verify_full CRM/SC-001 纯 Python 回归（不连 DB、不发 HTTP）。"""

import ast
import contextlib
import importlib.util
import inspect
import io
import json
import os
import re
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
    def test_write_runners_refuse_non_8217_database(self):
        old_report = MOD.REPORT[:]
        MOD.REPORT.clear()
        try:
            with mock.patch.dict(MOD.DB, {"port": 8232}), \
                    mock.patch.object(MOD, "login_admin") as login, \
                    mock.patch.object(MOD, "APIClient", return_value=mock.Mock(
                        req=mock.Mock(return_value=(None, False)))), \
                    mock.patch.object(MOD, "q1", return_value={"id": 0}), \
                    mock.patch.object(MOD, "qall", return_value=[]), \
                    mock.patch.object(MOD, "qexec"), \
                    mock.patch.object(MOD.tempfile, "mkdtemp", return_value="/unit-evidence"), \
                    mock.patch.object(MOD, "write_sc001_evidence", return_value="/unit-evidence"), \
                    contextlib.redirect_stdout(io.StringIO()):
                crm_rc = MOD.run_crm(keep=False)
                sc001_rc = MOD.run_sc001(keep=False)
                api_rc = MOD.run_api(keep=False)
            self.assertEqual((crm_rc, sc001_rc, api_rc), (1, 1, 1))
            login.assert_not_called()
            self.assertTrue(any(name.startswith("DB-SAFE") and not ok for name, ok, _ in MOD.REPORT))
        finally:
            MOD.REPORT[:] = old_report

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
        self.assertIn("resource = 'customer'", sql)
        self.assertIn("resource_id = ANY(%s)", sql)
        self.assertNotIn("action", sql.lower())
        self.assertEqual(params, ([11, 12], 10, ["customer-1"]))

    def test_crm_deep_does_not_keep_unscoped_action_cleanup(self):
        source = SCRIPT.read_text(encoding="utf-8")
        self.assertNotIn("action IN ('customer_import','customer_export','customer_merge','merge')", source)
        self.assertIn("cleanup_crm_operation_logs(audit_floor, customer_ids, audit_ids, qexec)", source)
        self.assertIn("cleanup_crm_operation_logs(audit_floor, customer_ids, [], qexec)", source)

    def test_crm_deep_uses_exact_audit_capture_for_import_and_exports(self):
        source = inspect.getsource(MOD._crm_deep)
        self.assertNotIn("capture_audit_ids(", source)
        self.assertIn('capture_crm_audits(\n            "customer_import"', source)
        self.assertEqual(source.count('capture_crm_audits(\n            "customer_export"'), 2)
        self.assertIn('detail_match={"created": 1, "total": 1}', source)

    def test_crm_deep_exercises_csv_export_and_safe_archive_contracts(self):
        source = inspect.getsource(MOD._crm_deep)
        self.assertIn('verify_crm_export_csv(adm, csv_params, imported_id,', source)
        self.assertIn('verify_crm_archive(\n            adm, primary_id, run,', source)
        self.assertIn('"format": "csv"', source)

    def test_crm_deep_cleanup_count_uses_customer_resource_scope(self):
        source = inspect.getsource(MOD._crm_deep)
        self.assertIn('log_clauses.append(crm_customer_audit_clause())', source)
        self.assertNotIn('log_clauses.append("(id > %s AND resource_id = ANY(%s))")', source)


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
            with tempfile.TemporaryDirectory() as evidence_dir, \
                    mock.patch.object(MOD.tempfile, "mkdtemp", return_value=evidence_dir), \
                    mock.patch.dict(MOD.DB, {"port": 8217}), \
                    mock.patch.object(MOD, "login_admin", return_value="unit-token"), \
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

    def test_successful_run_writes_evidence_after_cleanup(self):
        with mock.patch.dict(os.environ, {"HIVEMTK_EVIDENCE_DIR": "/unit-evidence"}, clear=False), \
                mock.patch.object(MOD, "write_sc001_evidence", return_value="/unit-evidence") as writer:
            rc, report, output, _ = self.run_scenario(200, cleanup_failure=False)
        self.assertEqual(rc, 0)
        self.assertTrue(all(ok for _, ok, _ in report))
        writer.assert_called_once()
        evidence_dir, sample, id_map, scan_response, result, cleanup = writer.call_args.args
        self.assertEqual(evidence_dir, "/unit-evidence")
        self.assertEqual(len(sample["pairs"]), 100)
        self.assertEqual(len(id_map), 200)
        self.assertTrue((scan_response or {}).get("data", {}).get("list"))
        self.assertEqual(result["accuracy"], 1.0)
        self.assertEqual(cleanup["remaining"], 0)
        self.assertLess(output.index("SC001-CLEAN"), output.index("SC001-EVIDENCE"))


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


class CRMHTTPContractTests(unittest.TestCase):
    def setUp(self):
        self.old_report = MOD.REPORT[:]
        MOD.REPORT.clear()

    def tearDown(self):
        MOD.REPORT[:] = self.old_report

    def test_raw_csv_request_returns_text_and_checks_content_type(self):
        class Response:
            status_code = 200
            headers = {"Content-Type": "text/csv; charset=utf-8"}
            text = "id,name\ncustomer-1,Name\n"

            def json(self):
                raise ValueError("not json")

        class Session:
            def request(self, *args, **kwargs):
                self.call = (args, kwargs)
                return Response()

        client = MOD.APIClient()
        session = Session()
        client.s = session
        with contextlib.redirect_stdout(io.StringIO()):
            body, ok = client.req("GET", "/api/manage/crm/export", "CSV", raw=True,
                                  expect_content_type="text/csv", params={"format": "csv"})
        self.assertTrue(ok)
        self.assertEqual(body, Response.text)
        self.assertEqual(session.call[1]["params"], {"format": "csv"})

    def test_raw_csv_request_rejects_wrong_content_type(self):
        class Response:
            status_code = 200
            headers = {"Content-Type": "application/json"}
            text = '{"code":0}'

            def json(self):
                raise ValueError("not json")

        class Session:
            def request(self, *args, **kwargs):
                return Response()

        client = MOD.APIClient()
        client.s = Session()
        with contextlib.redirect_stdout(io.StringIO()):
            body, ok = client.req("GET", "/api/manage/crm/export", "CSV", raw=True,
                                  expect_content_type="text/csv")
        self.assertFalse(ok)
        self.assertEqual(body, Response.text)

    def test_crm_csv_export_requires_exact_header_and_matches_json_rows(self):
        header = "id,name,phone,email,industry,source_account_id,owner_user_id,tags"
        csv_text = header + '\ncustomer-1,Name,19000000001,a@example.invalid,e2e,acct,,"[""crm-e2e""]"\n'
        json_rows = [{
            "id": "customer-1", "name": "Name", "phone": "19000000001",
            "email": "a@example.invalid", "industry": "e2e", "source_account_id": "acct",
        }]
        self.assertEqual(MOD.crm_csv_export_errors(csv_text, json_rows, "customer-1"), [])

        bad_header = csv_text.replace("owner_user_id", "owner", 1)
        self.assertTrue(any("CSV header" in item for item in
                            MOD.crm_csv_export_errors(bad_header, json_rows, "customer-1")))
        missing = csv_text.replace("customer-1", "customer-2", 1)
        self.assertTrue(any("导入客户" in item for item in
                            MOD.crm_csv_export_errors(missing, json_rows, "customer-1")))
        mismatch = csv_text + "customer-3,Other,19000000002,b@example.invalid,e2e,other,,\n"
        self.assertTrue(any("ID 集合" in item for item in
                            MOD.crm_csv_export_errors(mismatch, json_rows, "customer-1")))

    def test_csv_export_compares_identity_fields_for_every_row(self):
        header = "id,name,phone,email,industry,source_account_id,owner_user_id,tags"
        json_rows = [
            {"id": "customer-1", "name": "Imported", "phone": "19000000001",
             "email": "imported@example.invalid", "industry": "e2e",
             "source_account_id": "acct-1", "owner_user_id": "", "tags": "[]"},
            {"id": "customer-2", "name": "Existing", "phone": "19000000002",
             "email": "existing@example.invalid", "industry": "e2e",
             "source_account_id": "acct-2", "owner_user_id": "", "tags": "[]"},
        ]
        csv_text = header + (
            '\ncustomer-1,Imported,19000000001,imported@example.invalid,e2e,acct-1,,"[]"'
            '\ncustomer-2,Existing,19000009999,existing@example.invalid,e2e,acct-2,,"[]"\n'
        )
        errors = MOD.crm_csv_export_errors(csv_text, json_rows, "customer-1")
        self.assertTrue(any("customer-2" in item and "phone" in item for item in errors), errors)

    def test_csv_export_step_uses_raw_response_and_reports_parity(self):
        class Client:
            def __init__(self):
                self.calls = []

            def req(self, *args, **kwargs):
                self.calls.append((args, kwargs))
                return ("id,name,phone,email,industry,source_account_id,owner_user_id,tags\n"
                        "customer-1,Name,19000000001,a@example.invalid,e2e,acct,,\n", True)

        client = Client()
        params = {"keyword": "CRM-E2E-run", "industry": "e2e", "format": "csv"}
        json_rows = [{"id": "customer-1", "phone": "19000000001",
                      "email": "a@example.invalid", "industry": "e2e"}]
        errors = MOD.verify_crm_export_csv(client, params, "customer-1", json_rows)
        self.assertEqual(errors, [])
        args, kwargs = client.calls[0]
        self.assertEqual(args[:2], ("GET", "/api/manage/crm/export"))
        self.assertTrue(kwargs["raw"])
        self.assertEqual(kwargs["expect_content_type"], "text/csv")
        self.assertEqual(kwargs["params"], params)

    def test_archive_links_customer_without_notification_route_and_asserts_db_audit(self):
        class Client:
            def __init__(self):
                self.calls = []

            def req(self, *args, **kwargs):
                self.calls.append((args, kwargs))
                return ({"code": 0, "data": {
                    "customer_id": "customer-1", "linked": True, "notified": True,
                }}, True)

        client = Client()
        query_one = mock.Mock(return_value={"n": 0})
        query_all = mock.Mock(return_value=[{
            "id": 44, "action": "clue_archive_link_customer", "module": "crm",
            "resource": "customer", "resource_id": "customer-1", "username": "",
            "detail": json.dumps({"clue_id": "crm-e2e-run", "customer_id": "customer-1"}),
        }])
        with contextlib.redirect_stdout(io.StringIO()):
            audit = MOD.verify_crm_archive(client, "customer-1", "run", 40, query_one, query_all)
        self.assertEqual(audit["id"], 44)
        args, kwargs = client.calls[0]
        self.assertEqual(args[:2], ("POST", "/api/manage/crm/clue/archive"))
        payload = kwargs["json"]
        self.assertEqual(payload["clue_id"], "crm-e2e-run")
        self.assertEqual(payload["customer_id"], "customer-1")
        for forbidden in ("source_channel", "source_account_id", "owner_user_id"):
            self.assertNotIn(forbidden, payload)
        self.assertFalse(any(not ok for _, ok, _ in MOD.REPORT))

    def test_archive_refuses_to_call_api_when_tenant_has_enabled_notification_route(self):
        client = mock.Mock()
        with contextlib.redirect_stdout(io.StringIO()):
            audit = MOD.verify_crm_archive(
                client, "customer-1", "run", 40,
                mock.Mock(return_value={"n": 1}), mock.Mock(return_value=[]),
            )
        self.assertIsNone(audit)
        client.req.assert_not_called()
        self.assertTrue(any(not ok for _, ok, _ in MOD.REPORT))

    def test_archive_fails_when_expected_audit_is_missing(self):
        class Client:
            def req(self, *args, **kwargs):
                return ({"code": 0, "data": {"customer_id": "customer-1", "linked": True}}, True)

        with contextlib.redirect_stdout(io.StringIO()):
            audit = MOD.verify_crm_archive(
                Client(), "customer-1", "run", 40,
                mock.Mock(return_value={"n": 0}), mock.Mock(return_value=[]),
            )
        self.assertIsNone(audit)
        self.assertTrue(any("审计" in name and not ok for name, ok, _ in MOD.REPORT))

    def test_audit_capture_filters_detail_and_requires_exactly_one_match(self):
        row = {"id": 12, "action": "customer_import", "module": "crm", "resource": "customer",
               "username": "e2e_admin", "detail": json.dumps({"created": 1, "total": 1})}
        query_all = mock.Mock(return_value=[row, dict(row, id=13, detail='{"created":2}')])
        reporter = mock.Mock(return_value=True)
        rows = MOD.capture_crm_audits(
            "customer_import", 10, query_all, username="e2e_admin",
            detail_match={"created": 1, "total": 1}, report=reporter,
        )
        self.assertEqual([item["id"] for item in rows], [12])
        self.assertTrue(reporter.call_args.args[1])
        sql, params = query_all.call_args.args
        self.assertIn("module = 'crm'", sql)
        self.assertIn("resource = 'customer'", sql)
        self.assertIn("username = %s", sql)
        self.assertEqual(params, (10, "customer_import", "e2e_admin"))

        for candidates in ([], [row, dict(row, id=14)]):
            reporter = mock.Mock(return_value=True)
            rows = MOD.capture_crm_audits(
                "customer_import", 10, mock.Mock(return_value=candidates),
                username="e2e_admin", detail_match={"created": 1}, report=reporter,
            )
            self.assertEqual(rows, [])
            self.assertFalse(reporter.call_args.args[1])


class CRMRouteCoverageTests(unittest.TestCase):
    def test_deep_contract_covers_every_router_method_and_path(self):
        product_root = Path(__file__).resolve().parents[1]
        route_source = product_root / "user-server/internal/router/crm_routes.go"
        script_source = product_root / "scripts/api_verify_full.py"
        route_text = route_source.read_text(encoding="utf-8")
        router_routes = {
            (method, f"/api/manage/crm{path}")
            for method, path in re.findall(r'\bg\.(GET|POST|PUT|DELETE)\("([^"]+)"', route_text)
        }
        self.assertTrue(router_routes, "未从 CRM 路由文件解析到任何路由")

        tree = ast.parse(script_source.read_text(encoding="utf-8"))
        entity_paths = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.Assign) and any(
                isinstance(target, ast.Name) and target.id == "entity_specs"
                for target in node.targets
            ):
                entity_paths.update(spec[0] for spec in ast.literal_eval(node.value))
        self.assertEqual(
            entity_paths, {"contacts", "suppliers", "service-providers"},
            "动态实体路径集合变化，路由覆盖解析需同步更新",
        )

        def path_options(node):
            if isinstance(node, ast.Constant) and isinstance(node.value, str):
                return {node.value}
            if not isinstance(node, ast.JoinedStr):
                raise AssertionError(f"非静态请求路径无法核对: {ast.dump(node)}")
            options = {""}
            for part in node.values:
                if isinstance(part, ast.Constant):
                    values = {part.value}
                elif isinstance(part, ast.FormattedValue):
                    expression = part.value
                    if isinstance(expression, ast.Name) and expression.id == "path":
                        values = set(entity_paths)
                    elif isinstance(expression, (ast.Name, ast.Subscript)):
                        values = {":id"}
                    else:
                        raise AssertionError(f"未知动态路径表达式: {ast.dump(expression)}")
                else:
                    raise AssertionError(f"未知 f-string 片段: {ast.dump(part)}")
                options = {prefix + str(value) for prefix in options for value in values}
            return options

        covered_routes = set()
        for call in ast.walk(tree):
            if not (
                isinstance(call, ast.Call)
                and isinstance(call.func, ast.Attribute)
                and call.func.attr == "req"
                and len(call.args) >= 2
                and isinstance(call.args[0], ast.Constant)
                and isinstance(call.args[1], (ast.Constant, ast.JoinedStr))
            ):
                continue
            method = call.args[0].value.upper()
            for path in path_options(call.args[1]):
                if path.startswith("/api/manage/crm/"):
                    covered_routes.add((method, path))

        self.assertEqual(
            covered_routes - router_routes, set(),
            f"验收脚本请求了不存在的 CRM 路由: {sorted(covered_routes - router_routes)}",
        )
        self.assertEqual(
            router_routes - covered_routes, set(),
            f"CRM 路由未被验收脚本显式覆盖: {sorted(router_routes - covered_routes)}",
        )


class CRMRunnerIsolationTests(unittest.TestCase):
    def test_crm_runner_lock_is_exclusive_and_released(self):
        old_report = MOD.REPORT[:]
        MOD.REPORT.clear()
        try:
            with tempfile.TemporaryDirectory() as directory:
                path = str(Path(directory) / "crm.lock")
                with contextlib.redirect_stdout(io.StringIO()):
                    with MOD.crm_runner_lock(path) as first:
                        self.assertTrue(first)
                        with MOD.crm_runner_lock(path) as second:
                            self.assertFalse(second)
                    with MOD.crm_runner_lock(path) as third:
                        self.assertTrue(third)
        finally:
            MOD.REPORT[:] = old_report

    def test_runner_guard_checks_db_and_lock_before_body(self):
        body = mock.Mock(return_value="done")
        guarded = MOD.isolated_crm_runner(body)
        with mock.patch.object(MOD, "require_isolated_test_db", return_value=False), \
                mock.patch.object(MOD, "finish", return_value="db-blocked"):
            self.assertEqual(guarded(keep=False), "db-blocked")
        body.assert_not_called()

        with mock.patch.object(MOD, "require_isolated_test_db", return_value=True), \
                mock.patch.object(MOD, "crm_runner_lock", return_value=contextlib.nullcontext(False)), \
                mock.patch.object(MOD, "finish", return_value="blocked"):
            self.assertEqual(guarded(keep=False), "blocked")
        body.assert_not_called()

        with mock.patch.object(MOD, "require_isolated_test_db", return_value=True), \
                mock.patch.object(MOD, "crm_runner_lock", return_value=contextlib.nullcontext(True)):
            self.assertEqual(guarded(keep=True), "done")
        body.assert_called_once_with(keep=True)

    def test_all_write_runners_use_isolation_guard(self):
        for runner in (MOD.run_api, MOD.run_crm, MOD.run_sc001):
            self.assertTrue(hasattr(runner, "__wrapped__"), runner.__name__)


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
            "HIVEMTK_DB_PASSWORD", "POSTGRES_TEST_PASSWORD", "HIVEMTK_ADMIN_PASS",
            "${TMPDIR:-/tmp}", "不得运行 `all`/`chain`",
            ': "${POSTGRES_PASSWORD:?', ': "${HIVEMTK_ADMIN_PASS:?',
            "mktemp -d", "同一测试库", "串行运行", "归档到",
        )
        for item in required:
            self.assertIn(item, text)
        forbidden = (
            "内层仓 `hivemtk/`", "/api/manage/channels", "/api/manage/channel-accounts",
            "/api/manage/channel-identify-codes", "/api/manage/crm/customers/dedupe",
            "/api/manage/crm/resources/search", "/api/manage/crm/customers/:id/360",
            "/api/manage/crm/customers/:id/ai-profile", "`python3 scripts/api_verify_full.py`",
            'HIVEMTK_EVIDENCE_DIR="$PWD/.artifacts/sc001"', "\ncd hivemtk-user\n",
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
