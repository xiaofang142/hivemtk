# Bug Assessment: 帮助中心失败态与异步请求隔离

- Slug: help-center-request-state
- Source: 全页面审计，延续已批准任务
- Verdict: valid（代码与接口契约确认；真实数据服务缺失，受控复现）
- Severity: medium

## Symptom / Root Cause

HelpCenter.vue 原生 fetch 不检查 HTTP/业务 code，把错误和网络失败转换成 null 后显示“暂无公开文章”。搜索、分类和文章详情请求没有代次保护，旧响应可覆盖新结果，切换分类后迟到详情可重新打开。搜索防抖计时器在卸载后没有清理。

## Reproduction

真实前端 /#/help-center 首次加载只见后端非预期响应提示（本机 API 未运行）；不能据此声称已验证文章业务。组件测试使用真实 {code:0,data:{list,total}} 与 HTTP 400/503，验证错误区别于空态、搜索迟到隔离和返回列表后的详情隔离。

## Proposed Remediation

保持公开同源 fetch 和 tenant_code 约定；统一验证 HTTP/信封，页面常驻错误与重试。列表/详情按版本隔离，搜索输入立即废弃旧请求，分类/返回取消旧详情，卸载清理定时器和代次。分类读取失败独立显示；不因成功文章读取清掉分类错误。

## Files likely to change

- user-web/src/views/public/HelpCenter.vue
- user-web/tests/unit/help_center_request_state.test.js
- user-web/tests/fixtures/page-audit/{main.js,server.mjs}（受控页面新增入口和公开接口合成响应）
- user-web/docs/audit/user-web-interaction-audit/ 报告与覆盖台账

## Tests

HTTP 400/503 和非法信封显示错误、不显示空态；重试恢复；新搜索结果不被旧搜索覆盖；离开详情/切分类后旧详情不重开；卸载后防抖不发请求。保留同源公开请求，不写任何真实业务数据。

## Risks

不能以 mock 通过代替真实租户配置/文章数据验收。新增提示复用既有 i18n 词条，服务端业务文案原样显示。原有并行会话修改不触碰。不提交、不发布。
