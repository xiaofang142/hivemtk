# Bug Fix: 裸协程门自测的固定读数过期

- **Slug**: sys01-local-gates-20261120-3
- **Fixed**: 2026-11-20
- **Assessment**: [assessment.md](./assessment.md)
- **Status**: applied

`scripts/check-bare-go.test.sh` 的 B15 精确读数及成功提示由 83/83 同步降低至 81/81。保留全部 8 组控制、退出码断言与真实门总数精确断言；没有修改扫描器、提高基线或关闭测试。

修复前脚本真实退出 1 的日志为 `sys01-closure/bare-go-controls.log`。修复后重跑、shellcheck 和 bash 3.2 解析结果见 test.md。与评估无偏差；不提交、不发布。
