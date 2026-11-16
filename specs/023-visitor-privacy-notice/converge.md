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
