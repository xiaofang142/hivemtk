#!/usr/bin/env python3
"""Regression tests for the router-list generator CLI."""

import contextlib
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parent.parent
SCRIPT = ROOT / "scripts" / "gen_router_list.py"
SPEC = importlib.util.spec_from_file_location("gen_router_list", SCRIPT)
gen_router_list = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gen_router_list)

TABLE_HEADER = "| # | 状态 | 路径 | 名称 | 标题 | 需登录 | 组件 | 源文件 | 引导 |"
TABLE_SEPARATOR = "|---|---|---|---|---|---|---|---|---|"
SENTINEL = "canonical evidence sentinel\n"


class RouterListCliTests(unittest.TestCase):
    def test_import_does_not_modify_canonical_ledger(self):
        canonical = Path(gen_router_list.OUT)
        before = hashlib.sha256(canonical.read_bytes()).hexdigest()
        env = os.environ.copy()
        env["PYTHONDONTWRITEBYTECODE"] = "1"
        result = subprocess.run(
            [
                sys.executable,
                "-c",
                "import importlib.util; "
                f"spec = importlib.util.spec_from_file_location('gen_router_list', {str(SCRIPT)!r}); "
                "module = importlib.util.module_from_spec(spec); "
                "spec.loader.exec_module(module)",
            ],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
        )
        after = hashlib.sha256(canonical.read_bytes()).hexdigest()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(after, before, "导入生成器不得改写 canonical router-list.md")

    def test_help_exits_zero_without_modifying_default_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            default_out = Path(tmp) / "router-list.md"
            default_out.write_text(SENTINEL, encoding="utf-8")
            stdout = io.StringIO()
            with mock.patch.object(gen_router_list, "OUT", str(default_out)):
                with mock.patch.object(sys, "argv", ["gen_router_list.py", "--help"]):
                    with contextlib.redirect_stdout(stdout):
                        with self.assertRaises(SystemExit) as caught:
                            gen_router_list.main()
            self.assertEqual(caught.exception.code, 0)
            self.assertEqual(default_out.read_text(encoding="utf-8"), SENTINEL)
            self.assertIn("usage:", stdout.getvalue())
            self.assertIn("--out", stdout.getvalue())
            self.assertIn("-o", stdout.getvalue())

    def test_default_invocation_writes_default_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            default_out = Path(tmp) / "router-list.md"
            with mock.patch.object(gen_router_list, "OUT", str(default_out)):
                with mock.patch.object(sys, "argv", ["gen_router_list.py"]):
                    with contextlib.redirect_stdout(io.StringIO()):
                        gen_router_list.main()
            text = default_out.read_text(encoding="utf-8")
            self.assertTrue(text.startswith("# 前端页面路由清单"))
            self.assertIn(TABLE_HEADER, text)
            self.assertIn(TABLE_SEPARATOR, text)
            self.assertIn("## 进度", text)
            self.assertTrue(text.endswith("\n"))

    def test_custom_output_aliases_write_only_requested_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            default_out = Path(tmp) / "router-list.md"
            default_out.write_text(SENTINEL, encoding="utf-8")
            for option in ("--out", "-o"):
                with self.subTest(option=option):
                    custom_out = Path(tmp) / f"custom-{option.lstrip('-')}.md"
                    with mock.patch.object(gen_router_list, "OUT", str(default_out)):
                        with mock.patch.object(
                            sys, "argv", ["gen_router_list.py", option, str(custom_out)]
                        ):
                            with contextlib.redirect_stdout(io.StringIO()):
                                gen_router_list.main()
                    self.assertTrue(custom_out.is_file(), "自定义输出文件未生成")
                    self.assertIn(TABLE_HEADER, custom_out.read_text(encoding="utf-8"))
                    self.assertEqual(
                        default_out.read_text(encoding="utf-8"),
                        SENTINEL,
                        "自定义输出不得改写默认 canonical 文件",
                    )

    def test_generated_header_rows_and_evidence_are_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            router_dir = root / "user-web" / "src" / "router"
            module_dir = router_dir / "modules"
            module_dir.mkdir(parents=True)
            (router_dir / "index.js").write_text(
                """export default [
  { path: '/alpha', name: 'Alpha', component: () => import('@/views/Alpha.vue'), meta: { title: 'Alpha', requiresAuth: true } },
  { path: '/old', name: 'Old', redirect: '/alpha' },
]
""",
                encoding="utf-8",
            )
            (module_dir / "beta.js").write_text(
                """export default {
  path: '/group', name: 'Group', component: () => import('@/views/Group.vue'),
  meta: { title: 'Group', requiresAuth: false }, children: [
    { path: 'child', name: 'Child', component: () => import('@/views/Child.vue'), meta: { title: 'Child', requiresAuth: false } },
  ],
}
""",
                encoding="utf-8",
            )
            out = root / "router-list.md"
            out.write_text(
                "# Evidence header\n\nManual note\n\n"
                "## 清单\n\n"
                f"{TABLE_HEADER}\n{TABLE_SEPARATOR}\n"
                "| 1 | [x] | `/alpha` | StaleName | Stale title | 是 | `stale.vue` | `stale.js` | kept evidence |\n",
                encoding="utf-8",
            )
            with mock.patch.multiple(
                gen_router_list,
                ROOT=str(root),
                ROUTER_DIR=str(router_dir),
                MOD_DIR=str(module_dir),
                OUT=str(out),
            ):
                with mock.patch.object(sys, "argv", ["gen_router_list.py"]):
                    with contextlib.redirect_stdout(io.StringIO()):
                        gen_router_list.main()

            text = out.read_text(encoding="utf-8")
            self.assertIn("# Evidence header\n\nManual note\n\n## 清单", text)
            self.assertIn(f"{TABLE_HEADER}\n{TABLE_SEPARATOR}", text)
            rows = [line for line in text.splitlines() if re.match(r"^\| \d+ \|", line)]
            self.assertEqual(len(rows), 4)
            cells_by_path = {}
            for index, row in enumerate(rows, 1):
                cells = [cell.strip() for cell in row.strip("|").split("|")]
                self.assertEqual(len(cells), 9, f"第 {index} 行不是 9 列")
                self.assertEqual(cells[0], str(index))
                path = cells[2].strip("`")
                self.assertNotIn(path, cells_by_path, f"路由路径重复：{path}")
                cells_by_path[path] = cells
            self.assertEqual(
                list(cells_by_path),
                ["/alpha", "/old", "/group", "/group/child"],
            )
            self.assertEqual(
                cells_by_path["/alpha"],
                ["1", "[x]", "`/alpha`", "Alpha", "Alpha", "是", "`views/Alpha.vue`", "`src/router/index.js`", "kept evidence"],
            )
            self.assertEqual(cells_by_path["/old"][6], "`-`")
            self.assertEqual(cells_by_path["/group/child"][3], "Child")
            self.assertEqual(cells_by_path["/group/child"][5], "否")
            self.assertIn("- 已验证无错误 `[x]`：**1** 条", text)
            self.assertIn("- 待点验 `[ ]`：**3** 条", text)
            self.assertIn("- 已点出缺陷 `[!]`：**0** 条", text)
            self.assertTrue(text.endswith("\n"))


if __name__ == "__main__":
    unittest.main()
