# ESM/Sass构建兼容修复

依据[评估](assessment.md)仅迁移配置和导入。

- 配置目录来自模块URL；Git短版本读取使用ESM `execFileSync`并明确cwd，保留无Git回退。
- 别名不依赖`__dirname`；SCSS全局变量改`@use … as *`，入口reset改`@use`，两注释同步。
- 真实Sass编译覆盖全部变量及完整入口CSS，字节与旧导入一致；没有改主题、PWA、依赖版本或抑制弃用提示。

修正测试的原全局变量和实际版本插件前置后，三项真实RED→三项GREEN（619毫秒）。初入口缺变量的夹具错误不计有效行为RED。
