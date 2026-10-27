# ego-runner 安装器（macOS）

> 设计文档 R2 / specs/015 C7。Windows 包按 specs/014 改进清单 C11 另轮排期。

```bash
# 一键安装（编译 + conf + LaunchAgent）
./install.sh --server-url ws://127.0.0.1:8204 --token eg_<userID>_<rand>
# 多 ego lite 实例并存时指定连接目标（官方 --ego-server-name，调用机制调研 E3）
./install.sh --token eg_... --ego-server-name work-a
# 已有编译产物时免编译
./install.sh --bin /path/to/ego-runner --token eg_...
# 环境自检（只读）
./install.sh --check
# 卸载（conf 与日志保留）
./uninstall.sh
```

## conf 键说明（~/.hivemtk/ego_runner.conf）

| 键 | 必填 | 说明 |
|---|---|---|
| `server_url` | ✅ | user-server WS 地址，`ws(s)://host:8204`；loopback 地址时安装器写入形态甲提示 |
| `token` | ✅ | `eg_<userID>_<rand>`，user-web「ego Runner」页签发（明文只显示一次） |
| `device_id` | ✅ | 首次生成持久化（形态乙补偿控制，调研 §6.5） |
| `loopback_only` | — | 形态甲标记（服务端回环门以服务端配置为准，此处仅自检提示） |
| `ego_server_name` | — | 多 ego 实例并存时按名连接（`--ego-server-name`，官方唯一跨实例选择手段） |
| `workspace_dir` | — | learnings 资产目录（默认 `~/.local/share/ego/ego-skills`，勿走 env 重定向——F13 实测 env 不穿透） |
| `ego_bin` | — | ego-browser 可执行文件路径（默认 PATH 上的 `ego-browser`） |

## 安装器做的事

1. `go build ./cmd/ego-runner` → `~/.hivemtk/bin/ego-runner`（或 `--bin` 复制现成产物）；
2. conf 模板落盘（**已存在的 conf 不覆盖**——轮换 token 请手改文件或走服务端撤销重签）；
3. LaunchAgent `cn.hivemtk.ego-runner`（RunAtLoad + KeepAlive，崩溃自动拉起）；
4. 日志：`~/.hivemtk/ego-runner.log`。

## 升级 ego lite

能力升级归 ego 官方：管理端 `POST /api/egolite/admin/runner/upgrade` 向在线 runner 广播
升级指令，runner 执行官方 `ego-browser upgrade` 并回传结果事件；升级后 API 手册随二进制
更新（`ego-browser skill` 动态打印，无需任何本地文档同步）。
