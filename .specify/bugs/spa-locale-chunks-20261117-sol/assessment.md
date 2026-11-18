# 语言产物与重复动态导入评估

Verdict: valid / medium。

## 根因

隔离构建已确认语言模块约2392KB，超过现有1600KB单块提示阈值。九种语言在i18n入口静态导入，同时locale加载函数又动态导入相同JSON，后者无法产生懒加载收益。术语页面已静态引用API模块，又为详情动态导入同模块，同样产生无效动态导入提示。

## 论证与决策

不提高阈值、不删语言、不屏蔽提示，也不引入异步启动或运行时消息编译。九语言保持同步初始化、中文回退及现有离线预缓存；将静态消息集中为目录模块，兼容加载函数读取同一目录；按语言分配独立构建块。分块不意味着首屏总下载量减少。术语详情使用现有模块的命名导入，保留错误回退。不改变PWA策略、依赖或后端冻结源码。

## Files likely to change

- `user-web/src/i18n/index.js`、`locale.js`、新`catalog.js`
- `user-web/vite.config.js`
- `user-web/src/views/glossary/List.vue`：仅详情API导入方式
- 新`user-web/tests/unit/locale_catalog_contract.test.js`、`locale_chunk_contract.test.js`
- 本目录fix/test与主进度

## 验证

先验证九语言同步启动、中文回退、RTL、既有加载返回契约与逐语言分块的失败测试；修复后完整SPA/ESLint及隔离生产构建检查真实块大小和重复导入提示。不运行历史UI跑批，不用编译耗时推断线上收益。
