# =============================================================================
# HiveMtk 用户端 - Makefile（2026-08-17 宿主机部署重构版）
# =============================================================================
# 架构：Docker 仅提供数据层（PG + Redis）；user-server / 推理栈 / 前端均跑宿主机。
# LLM 配置通过后台「LLM 路由」页面写入数据库表 llm_providers，不落配置文件。

.PHONY: help install init up down restart logs ps build user-build web-build
.PHONY: db-up db-down db-logs db-ps db-backup db-restore
.PHONY: inference-host-install inference-host-models inference-host-up inference-host-down
.PHONY: inference-host-warmup inference-host-logs inference-host-ps inference-host-test inference-host-status
.PHONY: dev dev-install dev-stop dev-all dev-down dev-clean dev-help

# 默认目标
help:
	@echo "==================================="
	@echo "HiveMtk 用户端 - 命令清单（2026-08-17 宿主机部署）"
	@echo "==================================="
	@echo ""
	@echo "【首次部署】"
	@echo "  make install              - 一键安装：生成 .env + 拉起数据层 + 下载模型 + 启动推理栈"
	@echo ""
	@echo "【数据层（Docker：仅 PG + Redis）】"
	@echo "  make db-up                - 启动 PG + Redis 容器"
	@echo "  make db-down              - 停止 PG + Redis 容器"
	@echo "  make db-ps                - 查看 PG + Redis 容器状态"
	@echo "  make db-logs              - 查看 PG + Redis 容器日志"
	@echo "  make db-backup            - 备份 PG"
	@echo "  make db-restore FILE=...  - 恢复 PG（指定 .sql 文件）"
	@echo ""
	@echo "【宿主机推理栈（llama.cpp / MLX）】"
	@echo "  make inference-host-install  - 安装 llama.cpp 二进制（首次）"
	@echo "  make inference-host-models   - 下载 dev 档模型（首次）"
	@echo "  make inference-host-up       - 启动 LLM + Embedding + Rerank 三个推理服务"
	@echo "  make inference-host-down     - 停止三服务"
	@echo "  make inference-host-warmup   - 预热三端点（避免首请求慢）"
	@echo "  make inference-host-test     - 端到端 smoke test"
	@echo "  make inference-host-status   - 统一查看数据层+推理栈+user-server 状态"
	@echo "  make inference-host-logs     - tail 三个推理服务日志"
	@echo "  make inference-host-ps       - ps aux | grep llama-server"
	@echo "  make inference-host-models-prod  - 下载 prod 档模型（16G+ 内存机器）"
	@echo ""
	@echo "【user-server（宿主机 Go 服务）】"
	@echo "  make user-build           - 编译 user-server 二进制到 user-server/bin/"
	@echo "  make dev                  - 启动 user-server 热更新（air，开发用，无需手动重编）"
	@echo "  make dev-install          - 一次性安装 air（已装则跳过）"
	@echo "  make dev-stop             - 停止 air 进程"
	@echo "  make dev-clean            - 清理 air 临时二进制 + 日志"
	@echo "  make dev-help             - 打印热重载工作流速查"
	@echo ""
	@echo "【一键全栈】"
	@echo "  make dev-all              - 拉起数据层 + 推理栈（再手动 make dev）"
	@echo "  make dev-down             - 停止数据层 + 推理栈 + air"
	@echo ""
	@echo "【前端构建】"
	@echo "  make web-build            - 构建 user-web 前端"
	@echo "  make sdk-build            - 构建 embed-sdk"
	@echo ""
	@echo "【代码质量】"
	@echo "  make lint                 - golangci-lint 架构护栏"
	@echo "  make vet                  - go vet"
	@echo "  make test-go              - go test ./..."

# =============================================================================
# 首次安装
# =============================================================================
install:
	@if [ ! -f .env ]; then \
		cp .env-example .env; \
		echo "✅ 已生成 .env，请编辑敏感字段（POSTGRES_PASSWORD / JWT_SECRET 等）"; \
		echo "🔑 生成密钥: openssl rand -hex 32"; \
	else \
		echo "⚠️  .env 已存在，跳过生成"; \
	fi
	@make web-build
	@make sdk-build
	@make inference-host-install
	@make inference-host-models
	@make db-up
	@sleep 5
	@make inference-host-up
	@make inference-host-warmup
	@echo ""
	@echo "=========================================="
	@echo "✅ 首次安装完成！"
	@echo "  PG         : 127.0.0.1:8202"
	@echo "  Redis      : 127.0.0.1:8203"
	@echo "  LLM        : 127.0.0.1:8207/v1"
	@echo "  Embedding  : 127.0.0.1:8208/v1"
	@echo "  Rerank     : 127.0.0.1:8209"
	@echo "  user-server: 127.0.0.1:8204（make dev 或 make user-build 后启动）"
	@echo "  user-web   : 127.0.0.1:5173（cd user-web && npm run dev）"
	@echo "=========================================="
	@echo "下一步："
	@echo "  make dev           # 启动 user-server 热更新"
	@echo "  cd user-web && npm run dev  # 启动前端"

# =============================================================================
# 数据层（PG + Redis，Docker）
# =============================================================================
db-up:
	docker compose up -d

db-down:
	docker compose down

db-logs:
	docker compose logs -f

db-ps:
	docker compose ps

db-backup:
	docker compose exec -T mtk-postgres \
		pg_dump -U $${POSTGRES_USER:-admin} $${USER_DB_NAME:-user_db} > backup_$$(date +%Y%m%d_%H%M%S).sql
	@echo "✅ 备份完成"

db-restore:
	@if [ -z "$(FILE)" ]; then \
		echo "用法: make db-restore FILE=backup_20260101_120000.sql"; \
		exit 1; \
	fi
	docker compose exec -T mtk-postgres \
		psql -U $${POSTGRES_USER:-admin} -d $${USER_DB_NAME:-user_db} < $(FILE)
	@echo "✅ 恢复完成"

# =============================================================================
# 宿主机推理栈（llama.cpp 三件套）
# =============================================================================
inference-host-install:
	bash scripts/inference-host/install-llama-cpp.sh

inference-host-models:
	bash scripts/inference-host/download-models.sh

inference-host-models-prod:
	HIVEMTK_PROFILE=prod bash scripts/inference-host/download-models.sh

inference-host-up:
	bash scripts/inference-host/start-all.sh

inference-host-down:
	bash scripts/inference-host/stop-all.sh

inference-host-warmup:
	bash scripts/inference-host/warmup.sh

inference-host-test:
	bash scripts/inference-host/smoke-test.sh

inference-host-logs:
	@tail -F $${HIVEMTK_RUNTIME_DIR:-$$HOME/.hivemtk/runtime}/llm.log \
		$${HIVEMTK_RUNTIME_DIR:-$$HOME/.hivemtk/runtime}/embedding.log \
		$${HIVEMTK_RUNTIME_DIR:-$$HOME/.hivemtk/runtime}/rerank.log

inference-host-ps:
	@ps aux | grep -E "llama-server" | grep -v grep

inference-host-status: db-ps inference-host-ps
	@echo ""
	@echo "=== 端点连通性 ==="
	@for p in 8207 8208 8209; do \
		code=$$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 http://127.0.0.1:$$p/health || echo 000); \
		if [ "$$code" = "200" ]; then \
			echo "  ✅ 127.0.0.1:$$p (200)"; \
		else \
			echo "  ❌ 127.0.0.1:$$p ($$code)"; \
		fi; \
	done
	@code=$$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 http://127.0.0.1:8204/health || echo 000); \
	if [ "$$code" = "200" ]; then \
		echo "  ✅ 127.0.0.1:8204 user-server (200)"; \
	else \
		echo "  ❌ 127.0.0.1:8204 user-server ($$code)"; \
	fi

# =============================================================================
# user-server 宿主机二进制构建
# =============================================================================
user-build:
	@echo "🔨 编译 user-server 二进制..."
	@mkdir -p user-server/bin
	cd user-server && CGO_ENABLED=0 go build -o bin/user-server ./cmd/api
	@echo "✅ user-server 二进制已构建到 user-server/bin/user-server"
	@echo "运行：cd user-server && ./bin/user-server"

# =============================================================================
# 前端构建
# =============================================================================
web-build:
	cd user-web && npm install && npm run build && cd ..
	@echo "✅ user-web 已构建"

sdk-build:
	cd embed-sdk && npm install && npm run build && cd ..
	@echo "✅ embed-sdk 已构建"

# =============================================================================
# 本地开发 - air 热更新（2026-08-18 约束化）
# -----------------------------------------------------------------------------
# 工作流：
#   1) cp .env-example .env && 编辑敏感字段（首次或换机）
#   2) make dev-install   一次性安装 air（已装则跳过）
#   3) make dev           air 监听 .go / .yaml / .html / ../.env → 自动重编+重启
#
# 改 .go 文件 → 1~2s 后浏览器刷新即生效
# 改 ../.env / config.yaml → 1~2s 后自动重启（配置热重载，无需手动）
# 改 .html 模板 / .yaml / .json → 同样触发重编
#
# 不需要：go build / go run / docker compose restart user-server
# =============================================================================
USER_SERVER_DIR = user-server
# air-verse/air 是 cosmtrek/air 2024 后的新家（github.com/cosmtrek/air 仍能 install，
# 但新版本已合并到 air-verse/air；这里用 air-verse 路径以避免被废弃警告）
AIR_PKG = github.com/air-verse/air@latest

dev-install:
	@if ! command -v air >/dev/null 2>&1; then \
		echo "📦 正在安装 air 热更新工具..."; \
		go install $(AIR_PKG); \
		echo "✅ air 安装完成（位于 \$$HOME/go/bin/）"; \
		echo "💡 如未在 PATH，请执行:  export PATH=\$$HOME/go/bin:\$$PATH"; \
	else \
		echo "✅ air 已安装：$$(air -v 2>&1)"; \
	fi

# 守护式 dev 入口：
#   - 自动 install air
#   - 自动 source ../.env（air.cmd 内部已 source；本 target 仅打印可读的启动提示）
#   - 启动 air，监听 user-server/ 工作目录
dev: dev-install
	@echo "=========================================="
	@echo "🚀 user-server 热更新模式（air）"
	@echo "=========================================="
	@echo "  工作目录  : $$(pwd)/$(USER_SERVER_DIR)"
	@echo "  监听文件  : *.go *.yaml *.html *.json ../.env"
	@echo "  触发动作  : 重新编译 ./cmd/api → 杀掉旧进程 → 拉起新进程"
	@echo "  性能      : 首次冷编 ~6s，增量热编 ~1.5s（Mac M1 16GB 经验值）"
	@echo "  停止      : Ctrl+C 或另起终端 make dev-stop"
	@echo "=========================================="
	@echo "💡 第一次跑请先："
	@echo "     cp .env-example .env && 编辑敏感字段"
	@echo "     make db-up                       # 启动 PG + Redis（数据层）"
	@echo "     make inference-host-up           # 启动 llama.cpp 推理栈（可选）"
	@echo ""
	@if [ ! -f .env ]; then \
		echo "⚠️  未发现 .env，将使用 config.yaml 中的默认值启动（DB/Redis/LLM 可能连不上）"; \
		echo "   建议先: cp .env-example .env"; \
		echo ""; \
	fi
	@cd $(USER_SERVER_DIR) && air

dev-stop:
	@pkill -f "air -c" 2>/dev/null || true
	@pkill -f "air$" 2>/dev/null || true
	@pkill -f "$(USER_SERVER_DIR)/tmp/main" 2>/dev/null || true
	@pkill -f "tmp/main" 2>/dev/null || true
	@echo "✅ air 进程已停止"

# 清理 air 临时产物（重新冷启动时建议先跑）
dev-clean:
	@rm -rf $(USER_SERVER_DIR)/tmp
	@echo "✅ 已清理 $(USER_SERVER_DIR)/tmp/（air 临时二进制 + 日志）"

# 打印热重载工作流自检清单
dev-help:
	@echo "=========================================="
	@echo "user-server 热重载工作流速查"
	@echo "=========================================="
	@echo ""
	@echo "  1. make dev-install    一次性安装 air（已装跳过）"
	@echo "  2. cp .env-example .env   配置敏感字段（首次）"
	@echo "  3. make db-up          启动 PG + Redis"
	@echo "  4. make inference-host-up  启动 LLM/Embedding/Rerank（可选）"
	@echo "  5. make dev            启动 user-server + 热重载"
	@echo ""
	@echo "日常开发只需要 step 5：保存 .go → 1~2s 自动重启 → 浏览器刷新"
	@echo ""
	@echo "故障排查："
	@echo "  - air 不重启    : tail -f user-server/tmp/air.log"
	@echo "  - 编译失败      : air 停止在错误状态，修复后保存文件即继续"
	@echo "  - 端口被占用    : lsof -i :8204 ; make dev-stop"
	@echo "  - 完全卡死      : make dev-clean && make dev"
	@echo "  - 不监听 .env   : 确认 user-server/.air.toml 中 include_file 含 ../.env"

# =============================================================================
# 一键全栈（开发模式）
# =============================================================================
dev-all:
	@echo "=========================================="
	@echo "🚀 一键启动开发全栈"
	@echo "=========================================="
	@make db-up
	@sleep 3
	@make inference-host-up
	@make inference-host-warmup
	@echo ""
	@echo "=========================================="
	@echo "✅ 数据层 + 推理栈已启动"
	@echo ""
	@echo "现在请在另一个终端执行："
	@echo "  make dev         # user-server 热更新"
	@echo "  cd user-web && npm run dev   # 前端"
	@echo "=========================================="

dev-down:
	@make inference-host-down || true
	@make db-down || true
	@make dev-stop || true
	@echo "✅ 全栈已停止"

# =============================================================================
# 代码质量护栏（P0-1：架构依赖规则见 user-server/.golangci.yml depguard）
# =============================================================================
.PHONY: lint lint-install vet test-go fmt fmt-check test-db-prune audit

lint-install:
	@which golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6

# 架构护栏：分层依赖方向 + depguard 规则（提交前必跑）
lint: lint-install
	cd user-server && golangci-lint run ./...

vet:
	cd user-server && go vet ./...

# 自动格式化（gofmt 语义等价，可放心批量执行）
fmt:
	cd user-server && gofmt -w $$(gofmt -l . | grep -v '^vendor/')

# 格式门禁：gofmt -l 非空即失败，与 CI 同口径。
# 为什么必须独立于 golangci-lint：user-server/.golangci.yml 只启用 govet+depguard，
# 且 run.tests=false —— gofmt 未被覆盖，测试文件更是完全不被 lint。
# 历史上因此积累 34 个未格式化文件（见 TASKS_AUDIT_2026-09-16.md · FMT-01）。
fmt-check:
	@cd user-server && out=$$(gofmt -l . | grep -v '^vendor/'); \
	if [ -n "$$out" ]; then \
		echo "❌ 以下文件未通过 gofmt（执行 make fmt 修复）："; \
		echo "$$out"; \
		exit 1; \
	fi; \
	echo "✅ gofmt 检查通过"

test-go:
	cd user-server && go test ./... -count=1

# 清理 testutil 遗留的进程级测试库（user_db_test_<pid> / user_db_test_bench_<pid>）。
#
# 背景：testutil 每个测试进程建一个独立库，且**从不 DROP**（Go 无进程退出钩子），
# 实测累积 1466 个孤儿库 / 19 GB。详见 docs/architecture/TASKS_AUDIT_2026-09-16.md · RISK-07。
#
# 默认只列出（安全）；确认无误后加 APPLY=1 才真正 DROP。
#   make test-db-prune            # 只列
#   make test-db-prune APPLY=1    # 真删
test-db-prune:
	@cd user-server && set -a && . ../.env && set +a; \
	PW="$$POSTGRES_PASSWORD"; \
	if [ "$$APPLY" = "1" ]; then \
		echo "⚠️  APPLY=1：即将 DROP 全部 user_db_test_* 库"; \
		docker exec -e PGPASSWORD="$$PW" mtk-postgres psql -U admin -p 8202 -d postgres -tAc \
			"SELECT 'DROP DATABASE IF EXISTS \"'||datname||'\";' FROM pg_database WHERE datname LIKE 'user_db_test_%';" \
			| docker exec -i -e PGPASSWORD="$$PW" mtk-postgres psql -U admin -p 8202 -d postgres; \
		echo "✅ 清理完成"; \
	else \
		echo "（只列不删。确认后执行 make test-db-prune APPLY=1）"; \
		docker exec -e PGPASSWORD="$$PW" mtk-postgres psql -U admin -p 8202 -d postgres -tAc \
			"SELECT count(*)||' 个孤儿测试库，共 '||pg_size_pretty(sum(pg_database_size(datname))) FROM pg_database WHERE datname LIKE 'user_db_test_%';"; \
	fi

# =============================================================================
# 静态审计护栏（本地与 CI 同口径）
#
# 2026-09-16 审计（TOOL-05）：audit_api_contract.py 与 audit-cross-package-ports.sh
# **此前均未接入任何 workflow** —— 等于"写了检查但从不执行"。其中前者是
# 「功能连贯性」维度唯一的自动化检查：它不在 CI 里，前端调用一个不存在的
# 后端路由就能一路合入而无人拦截。
#
# 现已接入 .github/workflows/api-contract.yml；本目标用于本地同口径预检。
#
# 注：check-architecture.sh 不在本目标内 ——
#   ① 它已由 user-server-ci.yml 单独执行（无需重复）；
#   ② 它是全部静态检查里结构上最重的：对 internal/ 下 715 个 *_test.go 逐文件 spawn 3 个 grep、
#      再对 internal/service 下 634 个 .go 逐文件 spawn 2 个 grep（合计约 3400 次进程创建），
#      故不适合放进"改一行前端就顺手跑一下"的目标。
#   需要时直接 `bash scripts/check-architecture.sh`。
# =============================================================================
audit:
	@echo "── 前后端 API 契约（UNMATCHED 必须为 0，且不得出现通配 key）──"
	@python3 scripts/audit_api_contract.py --strict
	@echo "── 跨包端口单一源 ──"
	@bash scripts/audit-cross-package-ports.sh
	@echo "── 组件类型声明不得指向已删除的组件 ──"
	@python3 scripts/check_component_types.py
	@echo "── 有写入路径的模型必须登记 AutoMigrate ──"
	@python3 scripts/check_model_migration.py
	@echo "✅ 静态审计通过"
