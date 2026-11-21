# Bug Assessment: SEO 预览失败隔离与状态

- Verdict: valid（静态确认，受控测试复现）
- Severity: medium
- Source: 全页面审计，已批准自动修复

## Root cause

SeoInfra.vue 在 Promise.all 全部成功后才赋值；llms/robots 任一失败丢掉另一成功值。失败被空 catch 吞掉，初始无 loading/error/empty/retry。sitemap 按钮无请求锁。右侧清单固定 ✅，容易把部署前提误看成实测通过。

## Files likely to change

user-web/src/views/geo/SeoInfra.vue；tests/unit/seo_infra_states.test.js；tests/fixtures/page-audit/{main.js,server.mjs}；审计报告与静态覆盖表。

## Remediation and tests

三种预览各自 loading/error/content，分开赋值，失败显示可重试，响应不是 content 文本则拒绝。相同请求未完成不重复发，卸载丢弃结果。清单用中性项目描述，说明部署项需实际验证。复用 i18n 加载失败/重试/暂无数据。测试部分失败保留成功结果、恢复、空文本、非法数据和生成去重；浏览器按标签逐个验。仅只读 preview API，不部署、推送或访问外部 URL。
