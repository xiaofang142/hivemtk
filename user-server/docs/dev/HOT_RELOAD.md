# user-server 热重载（Hot Reload）开发手册

> **规则级别**: ⭐⭐ 项目级开发文档
> **生效日期**: 2026-08-18
> **关联文档**:
> - 启动说明: [./DEVELOPMENT.md §2](./DEVELOPMENT.md)
> - 仓库 Makefile: [`../../Makefile`](../../../Makefile)
> - 工具配置: [`../../.air.toml`](../../.air.toml)
> - 父级 README: [`../README.md`](../../README.md)

本手册解释 **user-server 在本地开发态如何实现「保存即生效」**，无需每次手动 `go build` / `go run` / `docker compose restart`。面向所有需要高频修改 .go / .yaml / .html 的开发者。

---

## 一、TL;DR — 30 秒上手

```bash
# 首次（一次性）
cp .env-example .env                # 准备敏感字段
make dev-install                    # 装 air（已装跳过）
make db-up                          # 启动 PG + Redis
make inference-host-up              # 启动 LLM/Embedding/Rerank（可选）

# 日常（每次开机/重启 shell 后）
make dev                            # 启动热重载，Ctrl+C 停止
```

之后：
- 改任意 `.go` 文件 → 1~2s 后 air 自动重编并重启 user-server
- 改 `../.env` 或 `config.yaml` → 1~2s 后自动重启
- 改前端 `user-web/**` → 不会触发 user-server 重启（前端有自己的 vite 热重载）

> **后续不再需要 `go build`、`go run`、`docker compose restart user-server`** —— 保存即热重载。

---

## 二、为什么选 air（不是 fresh / CompileDaemon / nodemon / reflex）

### 2.1 主流 Go 热重载方案对比

| 工具 | 仓库 | 维护状态 | 编译控制 | 配置灵活度 | 性能 | 推荐场景 |
|---|---|---|---|---|---|---|
| **air** (cosmtrek → air-verse) | [air-verse/air](https://github.com/air-verse/air) | ⭐ 活跃（v1.67.4，2026-08） | ✅ 自定义 build cmd | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | **Go Web / API 通用首选** |
| CompileDaemon | [github.com/CodeSkyBlue/CompileDaemon](https://github.com/CodeSkyBlue/CompileDaemon) | 维护停滞（最近版本 2019） | 简单 | ⭐⭐ | ⭐⭐⭐ | 极简项目 |
| fresh | [github.com/gravityblast/fresh](https://github.com/gravityblast/fresh) | ❌ 2018 起停更 | 简单 | ⭐⭐ | ⭐⭐ | 弃用 |
| realize | [github.com/oxequa/realize](https://github.com/oxequa/realize) | 维护放缓 | 复杂 | ⭐⭐⭐ | ⭐⭐ | 大型 monorepo（过重） |
| wgo | [github.com/c9s/wgo](https://github.com/c9s/wgo) | 维护停滞 | 简单 | ⭐⭐ | ⭐⭐⭐ | 老项目回退 |
| nodemon | Node 生态 | 活跃 | 通用 | ⭐⭐⭐ | ⭐⭐ | 非 Go 项目的通用选择 |
| delve (dlv) | [github.com/go-delve/delve](https://github.com/go-delve/delve) | 活跃 | 调试器 | ⭐⭐⭐⭐⭐ | ⭐⭐ | 断点调试态 |
| fvbock/endless | [github.com/fvbock/endless](https://github.com/fvbock/endless) | 维护放缓 | ❌ 不编译 | 无 | ⭐⭐⭐⭐ | **生产环境 SIGHUP 热重启**（user-server 已用） |

### 2.2 选 air 的 4 个核心理由

1. **Go 生态默认选择**：23k+ star，2026 年仍在 v1.67 活跃迭代；几乎所有 Go Web 项目的 `.air.toml` 模板都开箱可用。
2. **配置灵活**：支持任意 build cmd，可以 `sh -c` 套娃（user-server 需要先 source .env 再编译，air 一行配置搞定）。
3. **watcher 准**：基于 fsnotify，macOS / Linux 原生支持；可配置 `exclude_dir` / `exclude_regex` / `include_file`（精确控制监听）。
4. **生产对照**：`fvbock/endless` 已在 `main.go` 用于生产 SIGHUP 平滑重启（不中断长连接），与开发态 `air` 配合形成「dev=air，prod=endless」的双轨制，不打架。

### 2.3 air 关键能力清单

- ✅ 监听 `.go` / `.yaml` / `.html` / `.json` 变更 → 自动重编 + 重启
- ✅ `include_file` 支持监听项目根 `.env`（配置热重载）
- ✅ `exclude_regex` 屏蔽 `_test.go` / `.out` / `coverage.html` 等噪音文件
- ✅ `stop_on_error = true` 编译失败时**不启动残缺二进制**（防 500）
- ✅ `clean_on_exit = true` 退出时自动清理 `tmp/` 临时二进制 + 日志
- ✅ `clear_on_rebuild = true` 每次重建清屏，日志更易读

---

## 三、配置文件 `.air.toml` 全解

文件位置：`hivemtk/user-server/.air.toml`（**已入仓**，所有开发者共享同一份；本地覆盖请用 `.air.local.toml`）。

```toml
root = "."
tmp_dir = "tmp"                                        # 临时二进制 + air.log 存放目录

[build]
# 关键设计：编译 + 运行都 source ../.env
# 1) cmd：编译前 `set -a; . ../.env; set +a` 让 .env 全部 auto-export
#    - 工作目录是 user-server/，所以 ../.env 指向 hivemtk/.env
#    - [ -f ../.env ] && . ../.env：仅当 .env 存在时 source（不报错）
#    - CGO_ENABLED=0：user-server 纯 Go 编译
#    - -buildvcs=false：跳过 VCS 信息注入（避免 git 状态变化导致二进制指纹抖）
cmd = "sh -c 'set -a; [ -f ../.env ] && . ../.env; set +a; CGO_ENABLED=0 go build -buildvcs=false -o ./tmp/main ./cmd/api'"
bin = "tmp/main"
# 2) full_bin：运行前再次 source .env（核心！不 source 的话 env 只在 build 进程，spawn 出的 tmp/main 看不到）
#    - exec ./tmp/main：exec 替换 shell 进程，让 tmp/main 收到完整 env
#    - 当 full_bin 设置时，air 跳过 bin+args_bin，直接用 full_bin 拉起子进程
full_bin = "sh -c 'set -a; [ -f ../.env ] && . ../.env; set +a; exec ./tmp/main'"
# include_ext：监听这些扩展名的文件变更
include_ext = ["go", "tpl", "tmpl", "html", "yaml", "json"]
# include_file：额外监听具体文件（air 不会 watch .env，必须显式声明）
include_file = ["../.env"]
# exclude_dir：完全不监听这些目录（提速 + 屏蔽产物）
exclude_dir = ["tmp", "dist", "node_modules", "logs", "bin", "backups", "uploads", "data", "vendor", ".git"]
# exclude_regex：路径匹配正则的文件也不监听
exclude_regex = ["_test\\.go$", "flycheck_", "\\.out$", "\\.prof$", "\\.bak$", "coverage\\.html$"]
# exclude_unchanged：仅监听实际变更的文件（默认 true，省 CPU）
exclude_unchanged = true
# delay：检测到变更后等待多久触发 build（防 IDE 短时间多次保存）
delay = 1000
# stop_on_error：编译失败时**不**启动残缺二进制
stop_on_error = true
log = "air.log"                                         # 编译日志位置

[env]
# air [env] 会用 os.Setenv 注入到 build 进程；只放构建期参数，业务 env 已在 cmd / full_bin 里 source ../.env
CGO_ENABLED = "0"

[color]                                                 # 日志着色
build = "yellow"
main = "magenta"
runner = "green"
watcher = "cyan"

[log]
time = false                                            # 不打印时间戳（air 自己会标）

[misc]
clean_on_exit = true                                    # 退出时清理 tmp/

[screen]
clear_on_rebuild = true                                 # 每次重建清屏
```

---

## 四、Makefile 入口与工作流

| 命令 | 作用 | 何时用 |
|---|---|---|
| `make dev-install` | 一次性安装 air（已装则跳过） | 首次 / 换机 |
| `make dev` | 启动 user-server + 热重载 | 日常开发 |
| `make dev-stop` | 停止 air 进程 | 切换分支 / 端口冲突 |
| `make dev-clean` | 清理 `user-server/tmp/`（air 临时二进制 + air.log） | 重置冷启动 |
| `make dev-help` | 打印热重载工作流速查 | 忘记流程时 |
| `make dev-all` | 拉起数据层 + 推理栈（提示再 `make dev`） | 全新环境 |
| `make dev-down` | 停止数据层 + 推理栈 + air | 下班 |
| `make user-build` | **生产态**编译二进制到 `user-server/bin/` | 部署 / CI |

### 4.1 `make dev` 内部做了什么

```bash
1. dev-install   # air 不存在则 go install github.com/air-verse/air@latest
2. 打印工作流提示（监听文件 / 触发动作 / 性能 / 停止方式）
3. 检查 .env 是否存在（不存在则警告，配置走 config.yaml 默认值）
4. cd user-server && air
   # air 内部：
   #   1. 监听 .air.toml 中的 include_ext + include_file
   #   2. 变更触发 sh -c cmd → set -a; . ../.env; set +a; go build ...
   #   3. 编译成功 → 杀掉旧 tmp/main → 拉起新 tmp/main
   #   4. 编译失败 → 保留旧 binary 在跑（stop_on_error=true，状态红屏）
```

### 4.2 全栈热重载工作流（首推）

```bash
# === 第一次（在新机器上）===
cd hivemtk
cp .env-example .env
vim .env                          # 改密码 / 密钥
make dev-install
make install                      # 拉数据层 + 推理栈（一次性）

# === 每天开机 ===
cd hivemtk
make dev-all                      # 拉数据层 + 推理栈
# 再开一个终端：
make dev                          # user-server 热重载
# 再开一个终端（前端开发）：
cd user-web && npm run dev        # user-web Vite HMR

# === 改动 .go 时的体感 ===
# IDE 保存 .go → 0.5s 后 air 提示 "file xxx.go has changed" → 1~1.5s 编译 → 杀掉旧进程 → 拉起新进程 → 浏览器刷新即生效
# 整个过程 < 3s
```

---

## 五、不需要重编的边界（约束文档）

> ⚠️ **本节是项目级硬约束**：以下变更**不会**也不会**应该**需要重编 user-server。违反 = 工作流回退到石器时代。

### 5.1 ✅ 无需任何操作即生效

| 变更类型 | 是否自动 | 体感延迟 | 备注 |
|---|---|---|---|
| 修改任意 `internal/**/*.go` | ✅ | 1~2s | air 自动重编 + 重启 |
| 修改 `cmd/api/*.go` | ✅ | 1~2s | 同上 |
| 修改 `config.yaml` | ✅ | 1~2s | air 监听 `.yaml` |
| 修改 `../.env` | ✅ | 1~2s | air `include_file = ["../.env"]` |
| 修改 `internal/template/*.html` | ✅ | 1~2s | air 监听 `.html` |
| 修改 `migrations/*.sql` | ⚠️ 部分 | 重启后生效 | air 重启时 main.go 自动跑 `migration.NewMigrationService` |
| 修改 `go.mod` / `go.sum` | ✅ | 2~5s | air 重新 `go build`（会跑 `go mod download`） |

### 5.2 ❌ 需要手动操作

| 变更类型 | 原因 | 操作 |
|---|---|---|
| 修改前端 `user-web/src/**` | 前端 Vite 单独 HMR | `cd user-web && npm run dev`；**无需**重编 user-server |
| 新增 go 依赖 `go get xxx` | air 不会自动 `go get` | 先 `go get xxx`，再让 air 重启 |
| 修改根 `docker-compose.yml`（数据层） | 容器不随 air 重建 | `docker compose up -d mtk-postgres mtk-redis`（服务本体无容器，`make user-build` 只出二进制） |
| 跨平台原生依赖（如 CGO） | 编译工具链 | `make user-build` 验证 |

### 5.3 ⛔ 千万别做的事

- ❌ `go build -o bin/user-server ./cmd/api && ./bin/user-server` —— 完全多余，会跟 air 抢端口 8204
- ❌ `docker compose restart user-server` —— docker dev 模式专属，宿主机 air 模式无意义
- ❌ `pkill -9 user-server` —— air 失去对子进程的跟踪，下次重启会有僵尸进程
- ❌ `kill -9 air` —— 留孤儿 `tmp/main` 在 8204 端口；正确做法 `make dev-stop`
- ❌ 在 air 跑着的时候 `go test` —— air 监听到 `_test.go` 变更不会触发重编（已 exclude），但 `go test` 本身的产物 `*.out` / `coverage.html` 已被 exclude，不会造成 hot reload 风暴

---

## 六、性能基线（Mac M1 16GB 实测）

| 场景 | 耗时 | 备注 |
|---|---|---|
| 首次冷编译 | ~6s | air 启动后第一次 build |
| 增量编译（改 1 个 .go） | ~1.2s | air 跑 build cmd 全程 |
| 改 .env / config.yaml | ~1.0s | 同上，但 build 缓存命中率高 |
| 改 1 个 .html 模板 | ~1.0s | 同上 |
| 重启 user-server 二进制（kill + spawn） | <0.1s | endless 走优雅关闭（带 drain 逻辑） |
| **总体感延迟**（改 .go → 浏览器生效） | **< 3s** | 等于 IDE 保存 + 编译 + 重启 + 浏览器刷新 |

对比：
- `go build` 手动 + `./bin/user-server` 手动：~7s（外加手抖杀错进程风险）
- `go run ./cmd/api`（无 air）：改完不会自动重启，必须 Ctrl+C 再 `go run`

---

## 七、常见问题排查

### 7.1 air 启动后端口 8204 被占用

```bash
# 症状：air 报错 "address already in use"
# 原因：上次 air 退出时留了 tmp/main 进程，或之前手动 go build 的二进制还在跑
lsof -i :8204
# 找到 PID 后：
kill <PID>            # 优雅停止
# 或一键：
make dev-stop
make dev-clean
make dev
```

### 7.2 改了 .go 但 air 没反应

```bash
# 1. 看 air 实际监听了什么
tail -f user-server/tmp/air.log
# 2. 确认 .go 文件被 exclude_regex 命中（不应该）
grep -c "_test\.go$" <your-file>      # 命中 _test.go 不监听
# 3. 强制 air 重启
Ctrl+C 停止当前 air
make dev-clean && make dev
```

### 7.3 改了 .env 但应用没读到新值

```bash
# 1. 确认 .air.toml 的 include_file 含 ../.env
grep "include_file" user-server/.air.toml
# 2. 确认 .env 路径正确（air 跑在 user-server/ 下，../.env 才是项目根）
ls -la .env ../.env
# 3. 改 .env 后看 air.log 应有 "file has changed" + 一次 build
tail -f user-server/tmp/air.log
```

### 7.4 air 编译失败但旧 binary 还在跑

预期行为：air 不会启动残缺二进制（`stop_on_error = true`），旧 binary 继续服务。

```bash
# 1. 看 build 错误
tail -f user-server/tmp/air.log
# 2. 修复代码 → 保存 → air 自动重试
# 3. 若 air 卡死：Ctrl+C → make dev-clean → make dev
```

### 7.5 想要纯编译不启动（不重启服务）

```bash
# 单独跑一次 build（air 不会跑）：
cd user-server && go build -o ./tmp/main ./cmd/api
# 注意：air 不会自动 pick up 这个改动，需要 touch 一个 .go 文件触发 air 重启
```

### 7.6 Docker dev 模式 vs 宿主机 air 模式混用

⚠️ **严禁混用**。两种模式都监听 8204 端口。

| 模式 | user-server 在哪 | 启动方式 | 是否需要重编 |
|---|---|---|---|
| **宿主机 air 模式**（默认 dev） | 宿主机 tmp/main | `make dev` | air 自动 |
| **Docker dev 模式** | 容器内 mtk-user-server | `docker compose up mtk-user-server` | 修改源码后 `docker compose restart mtk-user-server` 或挂载 volume |

切换时先 `make dev-stop` + `docker compose down mtk-user-server`，再启另一种。

---

## 八、CI / 生产环境为何不用 air

| 维度 | dev（air） | CI（air 不参与） | prod（endless） |
|---|---|---|---|
| 触发方式 | 文件 watch | `go build` + `go test` | 进程级 SIGHUP |
| 监听 | fsnotify | 无 | 信号监听 |
| 启动 | `air -c .air.toml` | `make lint / test / build` | `./bin/user-server` |
| 优雅重启 | 不需要（dev） | 不适用 | `endless.ListenAndServe`（已在 main.go） |
| 配置热重载 | 监听 ../.env | 不适用 | `config.yaml` 改后需 `kill -HUP <pid>` |

CI 流程参见 `.github/workflows/user-server-ci.yml`：lint → vet → build → test，均不依赖 air。
生产重启流程参见 [`docs/operations/DR_RECOVERY.md`](../../../docs/operations/DR_RECOVERY.md)：用 SIGHUP 触发 endless 优雅重启。

---

## 九、变更历史

| 日期 | 变更 | 作者 |
|---|---|---|
| 2026-08-18 | 首版入仓：选定 air，`.air.toml` 入仓，Makefile `dev` / `dev-install` / `dev-stop` / `dev-clean` / `dev-help` 目标全部约束化 | 工程化基线 |
| 2026-08-05 | 上一版 `.air.toml` 仅监听 .go；本次新增 `.env` / `.yaml` / `.html` 监听 + source .env | 增量加固 |

---

最近更新日期: 2026-08-18
