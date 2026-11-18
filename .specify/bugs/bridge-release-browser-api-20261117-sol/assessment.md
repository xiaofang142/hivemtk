# 扩展交付浏览器接口评估

Verdict: valid / high。

## 根因

`scripts/lib/ego-browser.mjs` 的重载脚本使用ESM运行时不可用的`require`、未加`ref=`的数字选择器、整页首个版本号和`keep: 'all'`。它还循环打开四个渠道页，违反逐页明确操作纪律。缺失版本仍可能返回成功，无法证明重载了目标扩展。

## 论证与决策

仅重载经搜索、扩展名称和唯一卡片确认的扩展，按快照逐个填写搜索框、点击一个重载按钮、读取目标卡片的版本。缺失/歧义/版本不一致均失败。使用ESM文件导入与独立结果文件；不复用或接管已交给用户登录的Space，不自动恢复渠道页，不卸载或清除存储。完成后的Space清理单独执行，错误不绕过。

## Files likely to change

- `user-web/bridge/scripts/lib/ego-browser.mjs`：仅`reloadExtension`及必要私有辅助函数。
- 新`user-web/bridge/test/release-browser-api.test.js`。
- 本目录fix/test与主进度。

## 验证

只mock浏览器/子进程边界，实际执行生成的Node脚本验证选择器、唯一卡片、严格版本与清理时序；不进行真实重载/发送。随后完整扩展测试及零告警检查。真实管理端登录交接不受此修复影响。
