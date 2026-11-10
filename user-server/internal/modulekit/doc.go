// Package modulekit 定义 HiveMtk 的「可插拔模块边界」：
// 模块契约（Module 接口）+ 进程内注册表（Registry）+ 两个内建示例模块。
//
// # 为什么有这个包
//
// 现状取证（2026-10）：internal/plugin/ 里只有一个 gitignored 的编译产物
// main/auth_plugin.so（Go plugin 单点），git 跟踪树里该目录**没有任何源码**，
// 全仓也没有任何 plugin.Open / plugin.Lookup 装载代码。也就是说：没有模块契约、
// 没有注册表、没有生命周期管理——「可插拔」三个字目前只存在于一份二进制里。
// 本包是 P2 项「A3 可插拔模块边界」的第一刀：把边界立起来，而不是把整个插件体系重写。
//
// # 两种实现如何挂进同一张表
//
//  1. 内建模块：编译期内联在宿主二进制里，直接 r.Register(m)。
//  2. Go plugin：宿主 plugin.Open(path) 后 plugin.Lookup(PluginSymbol) 拿到
//     interface{}，再交给 r.RegisterFromLookup(sym) 断言并登记。
//
// 两者能挂进同一张表的唯一前提是**类型同一性**：Go 的 plugin 机制要求宿主与插件
// 导入同一个包里的同一个类型。插件侧必须 import 本包并
// `var Module modulekit.Module = impl{...}`，且宿主与插件的 go.mod 模块路径、
// Go 版本、依赖版本逐字节一致，否则 Lookup 出来的符号断言不到本包的 Module。
//
// # 使用示例（装配期）
//
//	r := modulekit.NewRegistry()
//	for _, m := range modulekit.BuiltinModules() {
//	    if err := r.Register(m); err != nil { logger.Fatalf("%v", err) }
//	}
//	if err := r.EnableAll(ctx); err != nil { logger.Fatalf("%v", err) }
//
// 巡检期（并发安全，可与请求同时跑）：
//
//	if err := r.CheckHealth(ctx, "builtin.health"); err != nil { ... }
package modulekit
