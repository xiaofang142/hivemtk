# tasks · 023-visitor-privacy-notice

^- [ ] T1 config.js：`privacyNotice` 配置（typedef/DEFAULTS/data-* 简写/形状消毒）
^- [ ] T2 consent.js：ConsentGate（needsConsent/show/destroy + localStorage 读写守卫 + 双语默认文案）
^- [ ] T3 widget.js：open()/toggle() 接闸（同意前不 show 面板）+ onPrivacyConsent 事件 + destroy 清理
^- [ ] T4 styles.css：`mcw-consent` 层样式
^- [ ] T5 index.d.ts / README.md：类型与配置文档同步
^- [ ] T6 unit.test.mjs 新增用例（缺省关闭/未同意不建 iframe/同意落存储/拒绝不落/version 重弹）
^- [ ] T7 `npm run build` + `npm run test:all` 全绿；converge 结论
^- [x] T8（Deviations 挂账，不在本 spec）：管理端渠道开启页「已知悉平台规则」勾选留痕——
      user-web/src/i18n 并行泳道在途，单独任务；iframe 内首条消息前二次确认同理
