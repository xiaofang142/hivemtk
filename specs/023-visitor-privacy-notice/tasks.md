# tasks · 023-visitor-privacy-notice

^- [x] T1 config.js：`privacyNotice` 配置（typedef/DEFAULTS/data-* 简写/形状消毒）
^- [x] T2 consent.js：ConsentGate（needsConsent/show/destroy + localStorage 读写守卫 + 双语默认文案）
^- [x] T3 widget.js：open()/toggle() 接闸（同意前不 show 面板）+ onPrivacyConsent 事件 + destroy 清理
^- [x] T4 styles.css：`mcw-consent` 层样式
^- [x] T5 index.d.ts / README.md：类型与配置文档同步
^- [x] T6 unit.test.mjs 新增用例（缺省关闭/未同意不建 iframe/同意落存储/拒绝不落/version 重弹）
^- [x] T7 `npm run build` + `npm run test:all` 全绿；converge 结论（2026-11-18 回勾：converge.md 已 Converged，unit 116/116 + boundary 62/62 绿 + 真浏览器 S1-S5 回归过）
^- [ ] T8（Deviations 挂账，不在本 spec）：管理端渠道开启页「已知悉平台规则」勾选留痕——
      user-web/src/i18n 并行泳道冲突已随 6637e216c 落地消解（2026-11-18 复核），可开工；iframe 内首条消息前二次确认同理。已登记主清单 §3.22 BQ-2
