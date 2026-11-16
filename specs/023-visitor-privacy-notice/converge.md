# converge · 023-visitor-privacy-notice

- 实现 → 收敛 → 实现 循环：1 轮收敛即达绿。首轮实跑暴露 1 处真缺陷：
  消毒只挂在 parseConfig，直接 `new MarketingChatWidget(config)` 的宿主接入形态
  绕过消毒 ⇒ 抽 `normalizePrivacyNotice` 导出，parseConfig 与构造器共用同一条路径。
- 验证：`npm run build` ✓；`npm run test`（unit）114/114 ✓（含新增 §9 共 19 断言：
  形状消毒×3、关闭形态零行为变更×3、未同意不建 iframe×4、同意落存储+建 iframe×5、
  同版本免询问×2、version 变更重问×2、拒绝不落存储×4）；`npm run test:boundary` 62/62 ✓。
- 覆盖 FR001-FR006 全部落地；Deviations：T8（管理端勾选页 / iframe 内二次确认）
  因 user-web/src i18n 并行泳道在途，挂账单独任务，不阻塞本 spec 验收。
- verdict：Converged。

## 2026-11-17 增补：真浏览器回归（规则-1 逐元素口径）+ 一处真缺陷修复

- **发现**：Node mock DOM 测试全绿但真浏览器里同意层**无样式**——根因是 SDK 运行时
  样式全部内联（floating-button/iframe-panel 均 `style.cssText = getStyle()`），
  `styles.css` 是源码侧产物、IIFE 构建不携带 CSS，consent 首版只写了 class 没写内联。
- **修复**：consent.js 补 `getStyle(part)` 内联样式（含 ≤480px 移动端变体的 JS 分支），
  与既有组件同模式；重建后 unit 114/114 + boundary 62/62 复绿。
- **真浏览器回归（ego-browser，逐元素显式操作，S1–S5）**：
  - S1 点浮标 → 同意层渲染（内联样式断言 position:fixed/border-radius 双过）+ iframe=0；
  - S2 点「同意并继续」→ localStorage 落 t1 + iframe 才创建 + 层移除；
  - S3 重开 → 同版本免重问、iframe 直接出现；
  - S4 清测试页自身 storage → 重问；
  - S5 点「暂不使用」→ 无 iframe、无存储、窗关闭。
  截图证据：/tmp/mcw-v2-s1.png（样式完好的同意层）、/tmp/mcw-s2-agreed.png、
  /tmp/mcw-v2-s5.png。修正过的测试自身错误：config 须先于 IIFE（README §4 既有口径）、
  同意层在「打开动作」时渲染（设计行为，非加载即弹）。
- **附带发现（测试页配置顺序）**：`window.MarketingChatWidgetConfig` 若在 IIFE 之后设置，
  parseConfig 已用默认值执行完毕 ⇒ 配置静默失效。README §4 已写先设 config，SDK 侧
  不做迟到配置补偿（保持薄壳）；此行为记录在案，避免部署方踩坑时误判 SDK 失灵。
- verdict：Converged（维持）。
