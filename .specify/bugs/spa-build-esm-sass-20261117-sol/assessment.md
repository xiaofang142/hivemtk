# 构建配置与 Sass 兼容性评估

Verdict: valid / medium。

## 根因与证据

完整SPA测试已通过94文件937项，ESLint761文件零错误零告警；仍输出Vite原生ESM配置中的`__dirname`提示和Sass全局`@import`弃用提示。当前`vite.config.js`使用ESM导入但版本函数内使用`require`，别名依赖`__dirname`；所有SCSS组件被注入旧式变量导入，入口样式也使用旧式reset导入。

## 论证与决策

- 不屏蔽弃用提示、不升级依赖、不改变主题、重置样式或PWA策略。
- 使用模块URL定位配置目录，版本生成改Node原生ESM导入，保留Git短版本加时间戳及无Git时回退语义。
- 变量仅有声明、reset仅有CSS规则，可迁移为Sass模块导入。用真实Sass编译核对全部变量及完整入口CSS字节相同，并验证无导入弃用提示。

## Files likely to change

- `user-web/vite.config.js`
- `user-web/src/styles/index.scss`
- `user-web/src/views/email/Jobs.vue`、`user-web/src/views/telegram/account.vue`：仅对应全局注入说明。
- 新`user-web/tests/unit/build_esm_sass_contract.test.js`
- 本目录fix/test及总进度。

## 验证

先运行原生Node配置加载、Sass全量变量及reset样式契约失败测试；修复后运行同一测试、完整SPA/ESLint与隔离生产构建。生产数据库测试正在运行，不修改冻结后端或启动另一组PG测试。
