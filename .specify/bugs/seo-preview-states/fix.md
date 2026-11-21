# Bug Fix: SEO 预览状态隔离

- Assessment: ./assessment.md
- Status: applied

## Changes

SeoInfra.vue 为 llms/robots/sitemap 分别管理 content/loading/error，失败不再连带丢弃另一预览。错误可见且可重试，content 不是文本明确拒绝；空内容显示空态；生成请求未完成时不重复提交；卸载后不写入组件。规范清单不再固定展示 ✅，部署前提写明需实际验证。

## Verification

- seo_infra_states.test.js：3 passed；与 help_center_request_state.test.js 合计 10 passed。
- npm run lint:check 退出 0，无错误/警告。
- npm run build 退出 0，built in 33.85s。
- 受控浏览器逐元素：llms 显示“# 受控 SEO 站点 / 审计站点说明”；robots 显示 User-agent: * / Allow: /；sitemap 点生成显示 `<?xml version="1.0"?><urlset />`。

## Limits

未验证真实站点内容和部署响应头。接口代码核对：GenerateAndSubmitSitemap 当前只读文章并生成 XML，未外发。使用真实组件与受控只读接口，未部署或推送。
