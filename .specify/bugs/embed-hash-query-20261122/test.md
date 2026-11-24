# Bug Verification: FA-25 iframe hash query

- Tested: 2026-11-22
- Result: verified（真实Vue Router解析+SDK构建测试；真实iframe页面未验）

| Check | Result | Notes |
|---|---|---|
| 复现 | fail 3/3 | welcome/lang丢失 |
| 修复后契约 | pass 3/3 | 特殊字符只编码一次，参数完整 |
| SDK build | pass | iife/esm构建成功 |
| SDK unit/boundary | pass 116/62 | 0 fail |
| 管理端全量 | pass | 129文件/1228用例；lint rc=0 |

真实浏览器欢迎语与语言渲染留G-UI验收，不以本记录冒充。
