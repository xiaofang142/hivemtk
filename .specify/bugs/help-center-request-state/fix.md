# Bug Fix: 帮助中心请求失败与竞态

- Status: applied
- Assessment: ./assessment.md

## Changes

HelpCenter.vue 检查 HTTP 与 code，拒绝非法 list 载荷；分类与内容错误分开常驻显示并提供重试。搜索输入立即使旧响应失效，分类切换/返回不允许迟到详情重新打开；卸载清理防抖。服务端文案保留，通用提示复用 i18n。

新增 tests/unit/help_center_request_state.test.js；受控 fixture 添加公开文章与可切换 503，仅合成数据。

## Verification

- 专项 7/7 通过：HTTP/业务失败与重试、非法载荷、搜索顺序、迟到详情、分类失败、卸载防抖。
- npm run lint:check 退出 0，无错误/警告。
- npm run build 退出 0，built in 24.50s。
- 真前端 API 不可用显示两区“加载失败 / 重试”，不显示暂无公开文章。
- 受控浏览器：列表→详情正文；503 显示“受控帮助服务暂不可用”；恢复后点重试重新出现文章。
- 日志位于 user-web/docs/audit/user-web-interaction-audit/logs/help-center-*.log，网络证据 help-center-network.json。

## Limits

真实租户文章数据与角色链未验。此前第一批全量命令的 1 项超时及独立通过结果保持原记录。本修复没有改变后端、凭据或真实业务数据。
