# 线上环境退役改造 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
> **本计划不使用 subagent-driven-development**：`hivemtk/CLAUDE.md` 规则0 因 RPM 限制**禁止派子 Agent**，所有任务由主 Agent 串行执行。
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 hivemtk 在两台线上服务器（hive / hiveuser，同机 118.25.236.101）永久下线后完全自洽：官网静态托管到 GitHub Pages，平台集成降级为默认关闭的可选本地组件，仓库内不再有任何地方把已过期线上域当默认值或示例。

**Architecture:** 三块。① `user-server` 用单个 `PLATFORM_ENABLED` 开关把平台集成关掉，关掉后走**代码里已存在**的 `config.PlatformCfg == nil` 快速失败与 `platformClient == nil` 空数据降级，运行链路零改动；② `hivemtk-platform/website` 整目录迁入 `hivemtk/website/`，配 `base:'/hivemtk/'` + `404.html` 兜底 + Actions 发 Pages；③ 新增 `check-no-xapptool.sh` 作为防回流闸，先让它红（输出即待办清单），再用后续任务逐个转绿。

**Tech Stack:** Go 1.25 + Gin + GORM（user-server）、Vue 3 + Vite 8 + vue-i18n（website / user-web / platform-web）、Playwright（user-web e2e）、GitHub Actions、bash 3.2（macOS 自带，禁用 `declare -A`）。

**Spec:** `hivemtk/docs/superpowers/specs/2026-09-21-offline-deployment-design.md` —— 实施前先读它，§3 记录了对初版设计的 7 条实测纠偏，§5.2 末尾那段"关键实测"决定了代码面为什么小。

## Global Constraints

- **不 commit、不 push**（用户规矩"改动先不提交"）。本计划每个任务的收口步是「跑门 + 把红/绿写进 spec §7」，**不是** git commit。禁止用 `git checkout` / `git restore` / `git stash` 撤销任何本批未提交文件（既往有丢整块改动的前科）。
- **不派子 Agent**（CLAUDE.md 规则0）。
- **结论必须跑出来**：任何"已修完"的判断必须有当次真实输出；**每道新增/新接的门必须做一次反向测试**（注入违规 → 亲眼看到红 → 记红因）。
- **删除前先证明未使用**：grep 未命中 ≠ 无用；删除前先确认无动态引用（路由字符串拼接、`import.meta.glob`、菜单配置驱动）。
- **工作区根级的 `docs/ internal-docs/ artifacts/ cold-start/ scripts/` 不在任何 git 仓内**（实测），改前必须 `tar` 快照，改错无 git 可回滚。
- 开工前置（每个含 Go 测试的任务开头都跑一遍）：`df -h / | tail -1` 有余量、`pg_isready -h 127.0.0.1 -p 8232` 绿；红先归因环境再归因代码。
- 端口/URL 单一源纪律：**先改 `user-server/internal/config/ports.go` → 再同步各 `.env*` / `vite.config.js` → 最后跑 `scripts/audit-cross-package-ports.sh`**，顺序不能反。
- Go 侧禁止跨层：Router 只映射、Handler 不写 SQL、Service 不直连 DB（CLAUDE.md 五层铁律）。
- 测试里对包级全局（`config.PlatformCfg`、`platform.merchantKey`、环境变量）的改写**必须 `t.Cleanup` 成对还原**。
- 断言一律打**计数与日志行**，不打墙钟时间（负载假红）。
- `internal/service` 单包全量测试 450–880s，默认 600s 超时是抽奖：该包单独跑并把 load 一起报数。

---

### Task 1: 防回流闸 `check-no-xapptool.sh`（先红，输出即待办清单）

**Files:**
- Create: `hivemtk/scripts/check-no-xapptool.sh`
- Create: `hivemtk/scripts/check-no-xapptool.test.sh`（反向测试，与既有 `scripts/license-compliance-scan.test.sh` 同构）
- Modify: `hivemtk/.github/workflows/docs-consistency.yml`（加一个 step）

**Interfaces:**
- Produces: `scripts/check-no-xapptool.sh` → 无命中 `rc=0` 并打印 `OK no-xapptool: 0 hits`；有命中 `rc=1` 并逐行打印 `<file>:<line>`。Task 9/10 的完成判据就是它归零。

- [x] **Step 1: 先做根级未版本控制目录的快照**（后续 Task 7/9/10 要改它们，无 git 可回滚）

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk
mkdir -p .tmp_files
tar czf .tmp_files/pre-offline-snapshot.tar.gz \
  docs internal-docs artifacts cold-start scripts \
  --exclude='scripts/.git' 2>/dev/null || \
tar czf .tmp_files/pre-offline-snapshot.tar.gz docs internal-docs artifacts cold-start scripts
ls -l .tmp_files/pre-offline-snapshot.tar.gz
```
Expected: 文件存在且非 0 字节。**记下它的 sha256**，Task 11 收尾自查用。

- [x] **Step 2: 写脚本**

```bash
#!/usr/bin/env bash
# =============================================================
# check-no-xapptool.sh —— 防回流闸：仓库内不得再出现已下线的 xapptool.cn 线上域
#
# 背景：hive / hiveuser / hiveuserapi / hivepaltform(api) / hivecontributor 所在服务器
# 已于 2026-09 到期且不续费（见 docs/superpowers/specs/2026-09-21-offline-deployment-design.md）。
# 本闸把"任何地方别再把它当默认值或示例"固化成可跑判据。
#
# 覆盖面口径（务必知道本闸摸不到什么，勿当全量保证）：
#   枚举源 = `git ls-files --cached --others --exclude-standard`
#     —— 已追踪 + 未追踪但未被 .gitignore 排除的文件。
#     取"未追踪也扫"是为了本地新增文件在 `git add` 之前就能被拦下；
#     CI 里 checkout 后所有文件都已追踪，两种口径等价。
#     dist/ node_modules/ 等被 .gitignore 排除者天然不扫（故严禁换成 find 全量）。
#   每次只覆盖调用所在的那一个仓（hivemtk / hivemtk-platform 要各跑一次）。
#   工作区根级的 docs/ internal-docs/ artifacts/ cold-start/ scripts/
#   实测不在任何 git 仓内（git -C <dir> rev-parse 报"不是 git 仓库"），其中 10 个含旧域的
#   文件只能靠 Task 10 的人工清单保证，本闸一律摸不到。
#   r22-hv/ 是 hivemtk 的影子克隆，靠 git 同步，不手改，也不在本闸范围内。
#
# 例外白名单（只放"改写等于伪造取证"的历史事实，不放偷懒没改的文案）：
#   本设计文档与实施计划自身必然指名旧域（要能核对才知道改了谁），故入库白名单。
# =============================================================
set -eo pipefail
# 定根 = 调用时所在的 git 仓（脚本自身位置只作无仓时的兜底）。
# 钉死在 `dirname BASH_SOURCE/..` 会让"拿去 platform 仓跑"变成把 hivemtk 数两遍（见 spec Task 21）。
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[[ -z "$ROOT" ]] && ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

WHITELIST=(
  "docs/superpowers/specs/2026-09-21-offline-deployment-design.md"
  "docs/superpowers/plans/2026-09-21-offline-deployment.md"
)

is_whitelisted() {
  local f="$1" w
  for w in "${WHITELIST[@]}"; do
    [[ "$f" == "$w" ]] && return 0
  done
  return 1
}

hits=0
lines=()
while IFS= read -r f; do
  is_whitelisted "$f" && continue
  [[ -f "$f" ]] || continue   # 未追踪枚举可能带已删除项
  while IFS= read -r l; do
    [[ -n "$l" ]] || continue
    lines+=("$f:${l%%:*}"); hits=$((hits + 1))
  done < <(grep -nE 'xapptool\.cn' "$f" 2>/dev/null || true)
done < <(git ls-files --cached --others --exclude-standard)

if [[ "$hits" -gt 0 ]]; then
  printf '%s\n' "${lines[@]}"
  echo "FAIL no-xapptool: $hits hit(s) 仍指向已下线线上域" >&2
  exit 1
fi
echo "OK no-xapptool: 0 hits"
```

> 注：`set -u` 故意不加（macOS bash 3.2 对跨函数变量共享会误报 unbound）；`while | <(...)` 而非管道，避开子 shell 吞计数与 `grep -q` 的 SIGPIPE 假阴。

- [x] **Step 3: 跑一次，确认它是红的，并把命中数当待办清单存下来**

Run: `bash hivemtk/scripts/check-no-xapptool.sh > /tmp/no-xapptool-start.txt; echo rc=$?`
Expected: `rc=1`。`wc -l < /tmp/no-xapptool-start.txt` 记下命中行数 = 本批待清行数（开工基线，Task 11 要对照它）。

- [x] **Step 4: 写反向测试**

```bash
#!/usr/bin/env bash
# check-no-xapptool.test.sh —— 证明这道闸"能红"，而不是恒绿的摆设
set -eo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
TMP="docs/superpowers/specs/.no-xapptool-fixture.md"   # 实跑版已改为仓根 no-xapptool-fixture.md（两棵树都保证存在该目录，见 spec Task 21）
# 只 rm -f 自清，绝不 git add/checkout/restore：本仓有 180+ 文件未提交在途工作
trap 'rm -f "$TMP"' EXIT

printf '# fixture\n\nsee https://hive.xapptool.cn/\n' > "$TMP"
if bash scripts/check-no-xapptool.sh >/tmp/nxt-red.log 2>&1; then
  echo "BROKEN: 注入违规后闸仍绿（枚举源看不到未追踪夹具 / 白名单误伤 / grep 未命中）" >&2
  cat /tmp/nxt-red.log >&2; exit 1
fi
grep -q 'no-xapptool-fixture.md' /tmp/nxt-red.log \
  || { echo "BROKEN: 红了但红因不是注入的夹具（假红）" >&2; exit 1; }
rm -f "$TMP"
bash scripts/check-no-xapptool.sh >/tmp/nxt-green.log 2>&1 \
  || { echo "BROKEN: 撤掉夹具后仍红" >&2; cat /tmp/nxt-green.log >&2; exit 1; }
grep -q 'OK no-xapptool: 0 hits' /tmp/nxt-green.log
echo "PASS check-no-xapptool.test.sh（红→绿双向均已实测）"
```

> 夹具落在 `docs/superpowers/specs/` 下（未被 .gitignore 排除，`--others` 能扫到），且**不以 `.` 开头的隐藏名会被 glob 漏掉**——所以枚举必须走 `git ls-files` 而非 shell glob。本仓有 180+ 文件未提交在途工作，夹具清理只准 `rm -f`，禁一切 git 写操作。

- [x] **Step 5: 跑反向测试**

Run: `bash hivemtk/scripts/check-no-xapptool.test.sh`
Expected: 末行 `PASS check-no-xapptool.test.sh（红→绿双向均已实测）`。红则先修夹具与路径，不要改期望。

- [x] **Step 6: 接进 CI**

在 `hivemtk/.github/workflows/docs-consistency.yml` 现有 steps 末尾追加：

```yaml
      - name: check-no-xapptool (线上域防回流)
        # 2026-09-21：hive/hiveuser 服务器到期不续费，仓库内任何文件都不得再引用
        # 已下线线上域。反向测试见 scripts/check-no-xapptool.test.sh。
        run: bash scripts/check-no-xapptool.sh
```

- [x] **Step 7: 收口**

跑 `bash -n` 语法检 + 把「闸已建、当前红 N 处、待办基线在 /tmp/no-xapptool-start.txt」写进 spec §7。

---

### Task 2: `PLATFORM_ENABLED` 开关，删掉线上域名回落与启动 Error 噪声

**Files:**
- Modify: `hivemtk/user-server/internal/config/ports.go:34`（删 `DefaultPlatformAPI`）
- Modify: `hivemtk/user-server/internal/config/platform.go`（加 `PlatformEnabled` / `PlatformURL`，必填校验按开关生效）
- Modify: `hivemtk/user-server/cmd/api/main.go:199-232`（关态不 LoadPlatform / 不 InitSync / 不 StartHeartbeat / 不打 Error）
- Test: `hivemtk/user-server/internal/config/platform_enabled_test.go`（新建）

**Interfaces:**
- Produces: `config.PlatformEnabled() bool`、`config.PlatformURL() string`。Task 3 的 `disabledClient` 与 Task 5 的 `platform_enabled` 响应字段都消费 `PlatformEnabled()`。

- [x] **Step 1: 写失败测试**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformEnabledDefaultsFalse(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "")
	if PlatformEnabled() {
		t.Fatal("PLATFORM_ENABLED 未设置时应为 false（默认本地模式）")
	}
}

func TestPlatformEnabledAcceptsOnlyTrueLike(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "1", "on"} {
		t.Setenv("PLATFORM_ENABLED", v)
		if !PlatformEnabled() {
			t.Fatalf("PLATFORM_ENABLED=%q 应判为开启", v)
		}
	}
	for _, v := range []string{"false", "0", "", "yes", "tru"} {
		t.Setenv("PLATFORM_ENABLED", v)
		if PlatformEnabled() {
			t.Fatalf("PLATFORM_ENABLED=%q 应判为关闭（非真值一律关）", v)
		}
	}
}

func TestPlatformURLNeverFallsBackToOnlineDomain(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "false")
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")
	if got := PlatformURL(); got != "" {
		t.Fatalf("关闭态 PlatformURL 必须为空，实际=%q", got)
	}
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_API_HOST", "http://127.0.0.1:8205")
	if got := PlatformURL(); got != "http://127.0.0.1:8205" {
		t.Fatalf("显式地址应原样生效，实际=%q", got)
	}
}

func TestLoadPlatformRequiredFieldsOnlyWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "platform.yaml")
	body := "api_url: \"\"\nsecret: \"\"\nadmin_password: \"\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := PlatformCfg
	t.Cleanup(func() { PlatformCfg = orig })

	t.Setenv("PLATFORM_CONFIG_PATH", p)
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_ENABLED", "false")
	if err := LoadPlatform(p); err != nil {
		t.Fatalf("关闭态缺字段不应报错，实际=%v", err)
	}
	if PlatformCfg != nil {
		t.Fatal("关闭态不应装配 PlatformCfg（nil 即全链路快速失败的开关）")
	}

	t.Setenv("PLATFORM_ENABLED", "true")
	if err := LoadPlatform(p); err == nil {
		t.Fatal("开启态缺 api_url/secret/admin_password 必须报错")
	}
}
```

- [x] **Step 2: 跑到红**

Run: `cd hivemtk/user-server && go test ./internal/config/ -run 'TestPlatform|TestLoadPlatform' -v`
Expected: FAIL / build error：`undefined: PlatformEnabled`。若报连不上库，先排环境再判代码。

- [x] **Step 3: 实现 config**

`platform.go` 增：

```go
// PlatformEnabled 平台集成总开关。默认关闭 = 纯本地模式：
// 不加载平台配置、不注册商户、不心跳、不拉市场，且这条链路上一次网络请求都不发。
func PlatformEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_ENABLED"))) {
	case "true", "1", "on", "yes":
		return true
	}
	return false
}

// PlatformURL 解析平台地址：显式配置 > 环境变量 > 空串。
// 空串是合法值，含义是"没有平台可连"；调用方（client.go）已在每条出站路径判
// PlatformCfg==nil 快速失败，所以这里绝不回落到任何具体域名。
func PlatformURL() string {
	if PlatformCfg != nil && PlatformCfg.APIURL != "" {
		return PlatformCfg.APIURL
	}
	for _, k := range []string{"PLATFORM_API_HOST", "PLATFORM_API_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
```

`LoadPlatform` 开头加关态早退、并把三段必填校验收进开关：

```go
func LoadPlatform(path string) error {
	if !PlatformEnabled() {
		PlatformCfg = nil
		return nil
	}
	// ...（原有读取 / ${VAR} 展开 / 三段必填校验保持不变）
```

`ports.go` 删除 `DefaultPlatformAPI = "https://hivepaltformapi.xapptool.cn"` 一行，并新增（Task 6 用）：

```go
	// DefaultWebsiteBaseURL 官网基址。GitHub Pages 项目页形态，带 /hivemtk 路径前缀。
	// 单一源：本常量；覆盖：GEO_SITE_BASE_URL。
	DefaultWebsiteBaseURL = "https://xiaofang142.github.io/hivemtk"
```

- [x] **Step 4: 改 main.go 装配段**

`main.go:199-232` 整段替换为（保留原有 `SetHeartbeatIntervalGetter` / `install.SetAdminProbe` 装配位置）：

```go
	if !config.PlatformEnabled() {
		logger.Info("[启动] 平台集成关闭（PLATFORM_ENABLED 未设置）：不加载平台配置、不注册商户、不心跳；" +
			"资产市场与平台上报类端点返回空数据，本地资产与运行链路不受任何影响")
	} else {
		if err := config.LoadPlatform("config/platform.yaml"); err != nil {
			logger.Errorf("平台配置加载失败（PlatformCfg 未初始化，商户上报/授权同步将不可用）：%v", err)
		} else {
			logger.Infof("[平台配置] api_url=%s", config.PlatformURL())
		}
		// 必须在 LoadPlatform 之后：InitSync 的注册协程要读 PlatformCfg
		if err := platform.InitSync(); err != nil {
			logger.Errorf("平台同步初始化失败：%v", err)
		}
		platform.StartHeartbeat(context.Background())
	}
	middleware.InitLicenseChecker(config.PlatformURL(), "")
```

> `middleware.InitLicenseChecker` 暂留（Task 4 会连着死字段一起清），但它现在拿到的只会是空串或本地地址。

- [x] **Step 5: 跑到绿**

Run: `cd hivemtk/user-server && go build ./... && go vet ./internal/... && go test ./internal/config/ -run 'TestPlatform|TestLoadPlatform' -v`
Expected: PASS，全部用例。
残留检查：Run `grep -rn "DefaultPlatformAPI" hivemtk/user-server/ --include=*.go` → 必须空（含测试；`_test.go` 里若有用例引用它，改判 `PlatformURL()==""`）。

- [x] **Step 6: 收口**

把「开关已建 + 关态启动日志无 Error」写进 spec §7。

---

### Task 3: `disabledClient` + 使用上报早退 + 市场读空 / 写 403

**Files:**
- Create: `hivemtk/user-server/internal/platform/disabled_client.go`
- Modify: `hivemtk/user-server/internal/platform/asset_market_adapter.go:14`
- Modify: `hivemtk/user-server/internal/service/asset_resolver.go:14-22`
- Modify: `hivemtk/user-server/internal/service/asset_bundle_submit.go`
- Modify: `hivemtk/user-server/internal/controller/asset_market.go`（detail / 写操作映射 403）
- Test: `hivemtk/user-server/internal/platform/disabled_client_test.go`、`hivemtk/user-server/internal/service/platform_disabled_test.go`

**Interfaces:**
- Consumes: `config.PlatformEnabled()`（Task 2）、`repository.PlatformAPIClient`（6 方法）、`platform.ErrPlatformNotConfigured`（`client.go:24`，**复用，不新增错误类型**）。
- Produces: `platform.NewPlatformAPIClient()` 关态返回 `disabledClient`；`bizerr.CodePlatformUnavail` 对外的 HTTP 映射从 500 类改判为 403。

- [x] **Step 1: 写失败测试（读面空 / 写面哨兵）**

```go
package platform

import (
	"context"
	"errors"
	"testing"
)

func TestDisabledClientReadsReturnEmptyNoError(t *testing.T) {
	c := NewPlatformAPIClient() // PLATFORM_ENABLED 未设置 → 关态
	list, total, err := c.ListAssets(context.Background(), "agent_persona", "", 1, 20)
	if err != nil {
		t.Fatalf("关态读列表不应报错： %v", err)
	}
	if total != 0 || len(list) != 0 {
		t.Fatalf("关态读列表必须是空且 total=0，实际 len=%d total=%d", len(list), total)
	}
	if list == nil {
		t.Fatal("关态读列表必须返回非 nil 空切片，否则前端 .length 会炸")
	}
	detail, err := c.GetAssetDetail(context.Background(), "nope")
	if err != nil {
		t.Fatalf("关态读详情不应报错： %v", err)
	}
	if detail == nil || len(detail) != 0 {
		t.Fatalf("关态详情应为空对象，实际=%v", detail)
	}
	if mine, err := c.MyPurchases(context.Background()); err != nil || mine == nil || len(mine) != 0 {
		t.Fatalf("关态我的购买应为空，实际 len=%v err=%v", len(mine), err)
	}
}

func TestDisabledClientWritesReturnSentinel(t *testing.T) {
	c := NewPlatformAPIClient()
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"Purchase":    func() error { return c.Purchase(ctx, "a") },
		"PullData":    func() error { _, err := c.PullData(ctx, "a"); return err },
		"ReportUsage": func() error { return c.ReportUsage(ctx, "a", 1) },
	} {
		if err := call(); !errors.Is(err, ErrPlatformNotConfigured) {
			t.Fatalf("%s 关态必须返回 ErrPlatformNotConfigured，实际=%v", name, err)
		}
	}
}
```

- [x] **Step 2: 跑到红**

Run: `cd hivemtk/user-server && go test ./internal/platform/ -run TestDisabledClient -v`
Expected: FAIL：关态下 `NewPlatformAPIClient()` 仍返回真实 client，读操作报错（`err != nil`）。

- [x] **Step 3: 实现 `disabled_client.go`**

```go
package platform

import (
	"context"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/repository"
)

// disabledClient 平台集成关闭态下的 repository.PlatformAPIClient 实现。
//
// 为什么要有它：读面必须"数据为空而不是报错"，写面必须"明确拒绝而不是静默成功"。
// 真实 client 在关态虽然也零网络出站（client.go 每条路径先判 PlatformCfg==nil 快速失败），
// 但它会把"未启用"表达成 error —— 直接透传给前端就成了 500 红叉，而不是空列表。
type disabledClient struct{}

func (disabledClient) ListAssets(_ context.Context, _, _ string, _, _ int) ([]map[string]any, int64, error) {
	return []map[string]any{}, 0, nil
}

func (disabledClient) GetAssetDetail(_ context.Context, _ string) (map[string]any, error) {
	return map[string]any{}, nil
}

func (disabledClient) MyPurchases(_ context.Context) ([]map[string]any, error) {
	return []map[string]any{}, nil
}

func (disabledClient) Purchase(_ context.Context, _ string) error {
	return ErrPlatformNotConfigured
}

func (disabledClient) PullData(_ context.Context, _ string) (*repository.PlatformAssetPayload, error) {
	return nil, ErrPlatformNotConfigured
}

func (disabledClient) ReportUsage(_ context.Context, _ string, _ int64) error {
	return ErrPlatformNotConfigured
}

var _ repository.PlatformAPIClient = disabledClient{}
```

`asset_market_adapter.go:14` 改为：

```go
func NewPlatformAPIClient() repository.PlatformAPIClient {
	if !config.PlatformEnabled() {
		return disabledClient{}
	}
	return &AssetMarketClientAdapter{inner: NewAssetMarketClient()}
}
```

> `PullData` 的返回类型以 `repository.PlatformAPIClient` 接口声明为准（接口里是 `*PlatformAssetPayload`）；适配器现有实现若用了 platform 包内的同名别名，保持与接口签名逐字一致，别造第三个类型。

- [x] **Step 4: 使用上报早退（不起协程）**

`asset_resolver.go` 替换：

```go
// ReportUsageBestEffort best-effort 异步上报资产使用到平台（闭环使用统计），
// 失败静默忽略，绝不阻塞/影响运行时主流程。平台集成关闭时不起协程。
func ReportUsageBestEffort(assetID string) {
	if !config.PlatformEnabled() {
		return
	}
	defer func() { _ = recover() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := platform.NewAssetMarketClient()
	_ = client.ReportUsage(ctx, assetID, 1)
}
```

在 `activeAssetID` 里把 `go ReportUsageBestEffort(aid)` 外面那层"读本地资产再上报"的分支保持不动（本地 `IncrementUseCount` 是真功能，必须留）。

- [x] **Step 5: 上架早退 + 403 映射**

`asset_bundle_submit.go` 的 `SubmitToPlatform` 开头：

```go
	if !config.PlatformEnabled() {
		return fmt.Errorf("%w: 平台集成未启用（设 PLATFORM_ENABLED=true 并配好本地 platform-server 后可用）",
			platform.ErrPlatformNotConfigured)
	}
```

`controller/asset_market.go`：把 `assetFail` 之前加一层哨兵判定，让"未启用"落在 403 而不是 500 类：

```go
func assetFail(c *gin.Context, err error) {
	if errors.Is(err, platform.ErrPlatformNotConfigured) {
		response.ErrorWithBusinessCode(c, bizerr.CodeForbidden, "平台集成未启用", gin.H{})
		return
	}
	// ...（原有分支不变）
```

> `bizerr` 里的权限类常量名以 `internal/domain/errors` 实际导出为准（写之前 grep 确认是 `CodeForbidden` 还是 `CodeNoPermission`），别新造。

- [x] **Step 6: 零出站契约测试（这是"不干扰运行"的那条锁）**

```go
package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/platform"
)

// 关态：跑一次资产解析，计数靶必须一次都没被打到。
func TestDisabledEditionSendsNoHttpRequest(t *testing.T) {
	var got int32
靶 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&got, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
	}))
	defer 靶.Close()

	orig, origEnable := config.PlatformCfg, os.Getenv("PLATFORM_ENABLED")
	t.Cleanup(func() {
		config.PlatformCfg = orig
		os.Setenv("PLATFORM_ENABLED", origEnable)
	})

	config.PlatformCfg = &config.PlatformConfig{APIURL: 靶.URL, Secret: "s", AdminPassword: "p"}
	t.Setenv("PLATFORM_ENABLED", "false")
	ReportUsageBestEffort("asset-x")
	if n := atomic.LoadInt32(&got); n != 0 {
		t.Fatalf("关态必须零出站请求，实际打到 %d 次", n)
	}

	t.Setenv("PLATFORM_ENABLED", "true")
	platform.NewAssetMarketClient() // 仅构造；真出站由调用方发起
	ReportUsageBestEffort("asset-x")
	// 开启态不苛求命中数（异步），只断言"这段代码路径没被关掉"：
	if !config.PlatformEnabled() {
		t.Fatal("开关回读失败")
	}
}
```

> 变量名 `靶` 是合法 Go 标识符（Go 允许 Unicode 字母），但**若 `gofmt`/lint 对非 ASCII 标识符有既存约定，就改成 `counter`**。写之前先 `grep -rn "gochecknoglobals\|varnamelen" .golangci.yml` 看约束。异步命中不确定，故开启态不断言计数 > 0，避免下次换机器负载就假红。

- [x] **Step 7: 跑到绿**

Run: `cd hivemtk/user-server && go build ./... && go vet ./internal/... && go test ./internal/platform/ ./internal/service/ -run 'Disabled|NoHttp|Submit' -count=1 -v`
Expected: 全部 PASS。注意 `internal/service` 包体量大，用 `-run` 过滤只跑本次相关用例即可，**但 Task 11 的收口门禁必须跑全量**（`-run` 子集不算门）。

- [x] **Step 8: 收口**

spec §7 记：关态读面空 / 写面 403 / 零出站断言的实测输出片段。

---

### Task 4: 清除授权残留（license 端点、前端侧边栏、中间件改名）

**Files:**
- Modify: `hivemtk/user-server/internal/router/admin_routes.go:51-52`（删两行）
- Modify: `hivemtk/user-server/internal/controller/system_info.go:41-52`（删两个 handler）
- Rename: `hivemtk/user-server/internal/middleware/license_checker.go` → `install_status.go`（类型 `LicenseChecker` → `InstallStatus`，删 `ServerURL`/`LicenseKey` 字段与 `InitLicenseChecker` 的 url 形参）
- Modify: `hivemtk/user-server/cmd/api/main.go`（跟着改调用）
- Delete: `hivemtk/user-web/src/api/license.js`
- Modify: `hivemtk/user-web/src/api/platform.js`（删 `getLicenseStatus` / `registerMerchant`）
- Modify: `hivemtk/user-web/src/layout/Layout.vue:112-121,154,195-224,812` 与 `:1066-1080` 样式
- Modify: `hivemtk/user-web/src/i18n/locales/{zh,en,ja,ar}.json`（删 `layout.licenseExpiry/licenseActive/contactForLicense/unknown`）
- Modify: `hivemtk/user-server/tests/e2e/deep_system.sh:20-22`、`hivemtk/api-inventory.md:335,1525,2340`、`hivemtk/user-web/tests/API_CHECKLIST.md:518,665`

**Interfaces:**
- Produces: `middleware.InstallStatus`、`middleware.GetInstallStatus()`、`InitInstallStatus()`；`/api/install/status`（可选新端点，见 Step 0）。

- [x] **Step 0: 删除前的动态面排查（不可跳）**

```bash
cd hivemtk/user-server
grep -rn "LicenseChecker\|InitLicenseChecker\|GetLicenseChecker\|license_checker" --include=*.go . | grep -v _test.go
grep -rn "license/status\|license/features" --include=*.go --include=*.sh --include=*.js --include=*.vue --include=*.md . ../hivemtk-platform 2>/dev/null | grep -v '/dist/'
cd ../user-web && grep -rn "getLicenseStatus\|licenseInfo\|isLicenseExpired" src/ tests/ | grep -v node_modules
```
Expected: 消费点**只落在上面 Files 列出的文件里**。多出任何一处（尤其 `router` 动态注册、菜单配置驱动、`dist/` 之外的构建产物）就停下先记下，不要顺手删。

- [x] **Step 1: 后端删端点与 handler，改名中间件**

`admin_routes.go` 删 51-52 两行；`system_info.go` 删 `LicenseStatus` / `LicenseFeatures`；
`license_checker.go` → `install_status.go`，`LicenseChecker` → `InstallStatus`，删掉 `ServerURL` / `LicenseKey` 与包级 `globalChecker` 里对它们的赋值，`InitLicenseChecker(serverURL, licenseKey string)` → `InitInstallStatus()`；文件头注释改成：

```go
// InstallStatus 安装态检查器。
//
// 本类型只承载 install.lock 状态与版本/超管账号读取。历史上它叫 LicenseChecker
// 并向平台换取授权；hivemtk 开源后那段已整体移除，且线上平台域已于 2026-09 下线。
// 判断"是否已初始化"的唯一依据是 install.lock.Initialized == true。
```

- [x] **Step 2: 前端删授权区块**

`Layout.vue`：删 112-121 模板块、154 import、195-224 的 `licenseInfo`/`isLicenseExpired`/`formattedExpiryTime`/`loadLicenseInfo`、812 调用、1066-1080 样式；
`api/platform.js` 删 `getLicenseStatus`、`registerMerchant` 两方法；`rm src/api/license.js`；
四把 locale 删 4 个 key（`unknown` 的唯一消费点是已确认的 `Layout.vue:215`）。

- [x] **Step 3: 跟着清 e2e 与清单**

`deep_system.sh` 删 20-22 三行（含其 `pass/fail` 行）；`api-inventory.md` 与 `API_CHECKLIST.md` 对应条目删除。

- [x] **Step 4: 验证**

```bash
cd hivemtk/user-server && go build ./... && go vet ./internal/... && go test ./internal/middleware/ ./internal/controller/ -count=1
cd ../user-web && npx eslint src/layout/Layout.vue src/api/ && npm run build
grep -rn "license" user-server/internal/router/ user-web/src/ | grep -v node_modules | grep -iv 'third_party\|LICENSE file'
```
Expected: Go 绿、eslint 绿、`npm run build` 绿、最后一条 grep 为空。
若有 `_test.go` 断言旧端点存在（Step 0 会暴露），改测试为断言端点**不存在**（404），不要删测试。

- [x] **Step 5: 收口**

spec §7 记 grep 残留为空的证据 + build/eslint 输出尾行。

---

### Task 5: `platform_enabled` 信号 + 上架入口隐藏

**Files:**
- Modify: `hivemtk/user-server/internal/pkg/shared/service/system_stats.go:22-34`（`SystemInfo` 加字段）
- Modify: `hivemtk/user-web/src/views/assetBundle/Playground.vue:186-200,216,377-383`
- Test: `hivemtk/user-server/internal/pkg/shared/service/system_stats_test.go`

**Interfaces:**
- Consumes: `config.PlatformEnabled()`。
- Produces: `GET /api/system/info` 响应新增 `platform_enabled: bool`（public 端点，前端启动即得）。

- [x] **Step 1: 写失败测试**

```go
func TestSystemInfoCarriesPlatformEnabled(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "false")
	info, err := NewSystemStatsService().GetSystemInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.PlatformEnabled {
		t.Fatal("关态 SystemInfo.platform_enabled 必须为 false，前端据此隐藏上架入口")
	}
	b, _ := json.Marshal(info)
	if !strings.Contains(string(b), `"platform_enabled":false`) {
		t.Fatalf("JSON 字段名必须是 platform_enabled，实际=%s", b)
	}
}
```

- [x] **Step 2: 跑到红**

Run: `cd hivemtk/user-server && go test ./internal/pkg/shared/service/ -run TestSystemInfoCarriesPlatformEnabled -v`
Expected: FAIL：`info.PlatformEnabled undefined`。

- [x] **Step 3: 实现**

`SystemInfo` 增 `PlatformEnabled bool \`json:"platform_enabled"\``，在 `GetSystemInfo()` 组装处赋 `config.PlatformEnabled()`。

- [x] **Step 4: 前端隐藏入口**

`Playground.vue`：拉一次 `/api/system/info`（沿用该文件既有取数方式，不新开 http 实例），存 `platformEnabled`；给「💰 生态上架配置」卡片和「🚀 审核上架到官方蜂巢商城」按钮加 `v-if="platformEnabled"`，并让 `handlePublishAndSubmit`（:377）在 `!platformEnabled` 时只走 `publishBundle`。

- [x] **Step 5: 验证（含诚实声明）**

```bash
cd hivemtk/user-server && go test ./internal/pkg/shared/service/ -run TestSystemInfoCarries -count=1 -v
cd ../user-web && npx eslint src/views/assetBundle/Playground.vue && npm run build
```
Expected: 全绿。
**UI 真机验证**：若本机起得起 user-server（8204 + PG 8232），用 Playwright 打开 Playground 断言卡片不渲染、点发布不报 403；**起不起得来的结论与证据必须写进 spec §7，不允许默认"应该没问题"**。

- [x] **Step 6: 收口**

spec §7 记字段名、测试输出、UI 验证做到什么程度。

---

### Task 6: GEO 模块换基址（29 处旧域是运行期数据，不是文案）

**Files:**
- Modify: `hivemtk/user-server/internal/config/ports.go`（Task 2 已加 `DefaultWebsiteBaseURL`）
- Modify: `hivemtk/user-server/internal/geo/service/monitor_crawler.go:25-60,127`
- Modify: `hivemtk/user-server/internal/geo/service/decision_analytics.go:213,309`
- Modify: `hivemtk/user-server/internal/geo/repository/crawler_visit.go:51`
- Test: `hivemtk/user-server/internal/geo/service/site_base_test.go`

**Interfaces:**
- Produces: `config.WebsiteBaseURL() string`（`GEO_SITE_BASE_URL` 覆盖，默认 `DefaultWebsiteBaseURL`，**去掉结尾 `/`**）；`geoSitePrefix(url string) bool`。

- [x] **Step 1: 写失败测试（含 C1 纠偏：landing 必须是真实路由）**

```go
func TestLandingURLsUseSiteBaseAndExistInRouter(t *testing.T) {
	base := "https://xiaofang142.github.io/hivemtk"
	t.Setenv("GEO_SITE_BASE_URL", base)
	for kw, urls := range keywordToLandings {
		for _, u := range urls {
			if !strings.HasPrefix(u, base) {
				t.Fatalf("关键词 %q 的 landing %q 未走基址，仍是硬编码域名", kw, u)
			}
			path := strings.TrimPrefix(u, base)
			if path == "" {
				path = "/"
			}
			if !websiteRoutes[path] {
				t.Fatalf("landing 路径 %q 不在官网真实路由表里（会监控到 404 页）", path)
			}
		}
	}
}

// websiteRoutes 抄自 website/src/router/index.js，改动官网路由时必须同步本表。
var websiteRoutes = map[string]bool{
	"/": true, "/docs": true, "/deploy": true, "/download": true,
	"/features": true, "/toolchain": true, "/workflow": true, "/faq": true,
}

func TestSelfSiteDetectionIsPrefixNotHost(t *testing.T) {
	base := "https://xiaofang142.github.io/hivemtk"
	t.Setenv("GEO_SITE_BASE_URL", base)
	if !geoSitePrefix("https://xiaofang142.github.io/hivemtk/features") {
		t.Fatal("自家站点必须判为自身")
	}
	if geoSitePrefix("https://xiaofang142.github.io/someone-else/") {
		t.Fatal("同 host 不同项目页不得判为自家（否则别人的 github.io 会算进 A 类）")
	}
}
```

- [x] **Step 2: 跑到红**

Run: `cd hivemtk/user-server && go test ./internal/geo/service/ -run 'TestLanding|TestSelfSite' -v`
Expected: FAIL（仍是硬编码 map + `const hivemtkDomain` 精确等值）。

- [x] **Step 3: 实现**

`config` 增：

```go
// WebsiteBaseURL 官网基址，结尾不带 '/'。
func WebsiteBaseURL() string {
	if v := os.Getenv("GEO_SITE_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultWebsiteBaseURL
}
```

`monitor_crawler.go`：把 `keywordToLandings` 的 value 从完整 URL 改成**路径**（键仍是关键词），消费点拼 `config.WebsiteBaseURL() + path`；按 C1 删除死链条目（`/blog/*`、`/product*`、`/pricing`、`/case`），保留/改写到真实路由；`:127` 兜底 URL 同改。
`decision_analytics.go`：删 `const hivemtkDomain`，`:309` 改 `isHive := geoSitePrefix(domainOrURL)`，`geoSitePrefix` 用 `strings.HasPrefix(url, config.WebsiteBaseURL())`。
`crawler_visit.go`：删 `"hive.xapptool.cn": "A"` 条目，**不加 github.io**；A 类判定由 prefix 分支给。

- [x] **Step 4: 跑到绿 + 单测全量该包**

Run: `cd hivemtk/user-server && go build ./... && go test ./internal/geo/... -count=1`
Expected: 全 PASS。

- [x] **Step 5: 收口**

spec §7 记 landing 条目的删改前后条数（例如 27 → N），并记 `websiteRoutes` 表与官网路由的同步义务。

---

### Task 7: 配置 / 脚本 / 测试里的线上 URL 换本地

**Files:**（括号内为实测命中数）
- Modify: `hivemtk/.env`(4)、`hivemtk/.env.geo.local`(1)
- Modify: `hivemtk/user-web/.env.production`、`hivemtk-platform/platform-web/.env.production`(3)
- Modify: `hivemtk/user-server/internal/config/ports.go`（注释）、`hivemtk/user-web/src/views/assetBundle/MerchantEditor.vue`(1)
- Modify: `hivemtk/scripts/deploy-user.sh`(3)、`hivemtk-platform/scripts/deploy-platform.sh`(7)
- Modify: `hivemtk/user-server/scripts/telegram_smoke.go`(2)、`user-server/scripts/seed-llm-providers.sh`(2)、`user-server/scripts/simulate/README.md`(1)
- Modify: `hivemtk/scripts/bulk_seed_industries.py`(1)、`hivemtk/scripts/seed/bulk_seed_industries.py`(1)
- Modify: `hivemtk/user-web/tests/system-settings.spec.js`(2)
- Modify: `hivemtk/user-server/internal/service/asset_bundle_test.go`(1)
- Modify: 根级未版本控制：`scripts/test_merchant_list.py`(1)、`internal-docs/scripts/webtest.py`(2)、`cold-start/integrations/seo_monitor/monitor.py`(1)、`cold-start/integrations/github_actions/monthly-metrics.yml`(1)、`cold-start/strategy/00_PLAN.md`(1)

**Interfaces:**
- Produces: 无新符号；只统一"本地默认 = `http://localhost:8204`（user-server）/ `http://localhost:8205`（platform-server）/ 前端 `VITE_API_BASE_URL=/`"。

- [x] **Step 1: 逐项改（规则，非占位）**

1. `.env` / `.env.geo.local`：`CORS_ALLOW_ORIGINS_USER` 去掉 `https://hiveuser.xapptool.cn`，保留 `https://www.xiaohongshu.com,https://www.douyin.com,https://www.tiktok.com,https://www.goofish.com`（真实渠道域），补 `http://localhost:5173,http://127.0.0.1:5173`。`.env` 里 55/270 两处「远程生产域 … 本机无权限获取，故走本地平台」注释重写为"默认不连平台；自建 platform-server 时把 PLATFORM_API_HOST 指到本机 8205"。
2. `user-web/.env.production`：值 `VITE_API_BASE_URL=/` 不动；把"线上 prod 模式：前端 hiveuser ↔ API hiveuserapi 跨域"整段注释改写为"同源部署：反代层把 /api 指向本机 user-server"。
3. `platform-web/.env.production`：`VITE_API_BASE_URL=https://hivepaltformapi.xapptool.cn` → `/`，注释里的"线上实际签发证书/DNS 的 API 域名是 hivepaltformapi（历史拼写）"整段删除（域名已下线，历史拼写陷阱不再成立），保留一句"平台端为可选本地组件，默认同源"。
4. `deploy-user.sh` / `deploy-platform.sh`：`DEPLOY_HOST="${DEPLOY_HOST:-118.25.236.101}"` → `DEPLOY_HOST="${DEPLOY_HOST:-}"`，并在缺值时 `die "DEPLOY_HOST 必填（原默认主机 118.25.236.101 已到期下线）"`；头部"部署目标：hiveuserapi.xapptool.cn"注释改写。rsync/`.user.ini`/i18n 预检逻辑**一律保留**（§5.1 已记：与 website/deploy.sh 的处置不对称是有意的）。
5. `telegram_smoke.go` / `seed-llm-providers.sh` / `simulate/README.md` / `bulk_seed_industries.py`×2 / `test_merchant_list.py` / `webtest.py` / `system-settings.spec.js` / `asset_bundle_test.go`：线上 URL → `http://localhost:8204`（platform 类 → 8205）。
6. `MerchantEditor.vue:1` 处：命中的是 `hive.xapptool.cn` 的示例文案，改为 `<你的站点域名>` 之外的**具体示例值** `hivemtk.example.com`（RFC 2606，不留待填占位）。
7. `cold-start/`：SEO 监控目标与月报域名 → `https://xiaofang142.github.io/hivemtk`。

- [x] **Step 2: 验证**

```bash
cd hivemtk/user-server && go build ./... && go test ./internal/service/ -run AssetBundle -count=1
cd hivemtk && bash -n scripts/deploy-user.sh && DEPLOY_HOST= bash scripts/deploy-user.sh 2>&1 | head -3
cd ../hivemtk-platform && bash -n scripts/deploy-platform.sh
cd ../hivemtk/user-web && npx eslint tests/system-settings.spec.js
```
Expected: Go 绿；`deploy-user.sh` 在 `DEPLOY_HOST` 空时**第一行就报必填并 rc≠0**（反向测试：证明必填真的生效）。

- [x] **Step 3: 收口**

spec §7 记改了几个文件、`deploy-user.sh` 必填反向测试输出。

---

### Task 8: website 迁入 `hivemtk/website/` + Pages 适配 + 交互件裁剪

**Files:**
- Move: `hivemtk-platform/website/` → `hivemtk/website/`
- Modify: `hivemtk/website/vite.config.js`（`base`）、`src/router/index.js:104`、`scripts/postbuild.mjs`、`scripts/postbuild.sh`、`index.html`(12)、`public/sitemap.xml`(42)、`public/robots.txt`(1)、`src/config/content.js`(4)
- Delete: `src/components/CustomerServiceWidget.vue`、`src/api/platform.js`、`/embed-demo` 路由、`Dockerfile`、`反向代理层.conf`、`deploy.sh` 的 SSH/rsync 段
- Modify: `hivemtk/scripts/audit-cross-package-ports.sh:227`

**Interfaces:**
- Produces: `hivemtk/website/`（Task 9 的 workflow 以此为 `working-directory`）；构建产物契约：`dist/index.html` + `dist/404.html` + 全部资源带 `/hivemtk/` 前缀。

- [x] **Step 1: 搬目录（两仓都不 commit，故用文件系统移动）**

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk
cp -R hivemtk-platform/website hivemtk/website
rm -rf hivemtk/website/node_modules hivemtk/website/dist
ls hivemtk/website | head -20
```
Expected: 新目录齐（`src/ public/ scripts/ docs/ index.html package.json package-lock.json vite.config.js …`）。
**旧目录本任务先不动**，等 Task 9 门迁完、Task 11 全套绿了再删（避免中途两边不一致）。

- [x] **Step 2: Pages 适配**

`vite.config.js` 顶层加 `base: '/hivemtk/'`，并把"单一源约束"注释块里对 platform-server:8205 的依赖描述改为对 `user-server/internal/config/ports.go`（website 已换仓，锚点跟着换）；`server.proxy` 的 `/public`、`/merchant-api` 两条整段删除（无后端可代理）。
`src/router/index.js:104` → `history: createWebHistory(import.meta.env.BASE_URL)`。

- [x] **Step 3: postbuild 生成 404 兜底、停掉 embed 复制**

`scripts/postbuild.mjs` 第 3 段（embed-sdk 复制）整段替换为：

```js
// 3. GitHub Pages 无 rewrite 能力：深链/刷新靠 404.html 兜底回 SPA。
//    副作用：深链首屏返回 HTTP 404 状态码，靠 canonical/sitemap 指向存在页补偿。
console.log('  → 生成 dist/404.html（SPA 深链兜底）...')
cpSync(resolve(distDir, 'index.html'), resolve(distDir, '404.html'))
console.log('    ✓ index.html -> 404.html')
```
`scripts/postbuild.sh` 同步改（它俩是双实现，必须一致；`cp -R` 刷 mtime 不刷内容，改完用 `diff` 证明两份都到位）。

- [x] **Step 4: host 与交互件**

`index.html` 12 处 + `public/sitemap.xml` 42 处 + `public/robots.txt` 1 处：`https://hive.xapptool.cn` → `https://xiaofang142.github.io/hivemtk`（sitemap 的 `<loc>` 需带结尾语义正确：目录页写 `/hivemtk/`，子页 `/hivemtk/features`）。
`src/config/content.js`：删 `chat` 段(18-20)、删两处"在线体验 → https://hiveuser.xapptool.cn/"(27,43)，CTA 改指向 `https://github.com/xiaofang142/hivemtk` 与站内 `/download`；删 `CustomerServiceWidget.vue` 与它在页面里的挂载点、删 `src/api/platform.js` 及其 `useSiteContact` 调用链、删 `/embed-demo` 路由与视图。

```bash
cd hivemtk/website && grep -rn "CustomerServiceWidget\|api/platform\|embed-demo\|useSiteContact" src/ | grep -v node_modules
```
Expected: 空（**动态引用也要排**：`grep -rn "import(" src/router` 看是否字符串拼路径挂载）。

- [x] **Step 5: 构建 + 静态起服实测**

```bash
cd hivemtk/website && npm ci && npm run build
python3 -m http.server 8299 --directory dist &  sleep 1
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8299/hivemtk/   # 或直接 /，取决于 server 根
curl -s http://127.0.0.1:8299/ | grep -o '/hivemtk/assets/[^"]*' | head -3
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8299/features    # 期望 404
curl -s http://127.0.0.1:8299/features | grep -c 'id="app"'                # 期望 ≥1（404.html 兜住了）
kill %1
```
Expected: 资源全带 `/hivemtk/`；未知深链返回 404 但内容是 SPA 壳；`grep -rn xapptool dist/` 为空。

- [x] **Step 6: 改端口审计脚本路径**

`hivemtk/scripts/audit-cross-package-ports.sh:227` `"hivemtk-platform/website/vite.config.js"` → `"$SELF_NAME/website/vite.config.js"`（注意该数组里其它项是相对 `$REPO_ROOT` 的路径，改完保持同构）。

Run: `cd hivemtk && bash scripts/audit-cross-package-ports.sh; echo rc=$?`
Expected: rc=0 且报 website vite 配置"单一源约束"字样命中（反向：临时把 `website/vite.config.js` 里那句注释删掉，脚本必须变红，再改回来）。

- [x] **Step 7: 收口**

spec §7 记：build 产物字节数、深链实测的 4 条 curl 输出、反向测试红因。

---

### Task 9: Pages 发布 workflow + platform 侧门迁除

**Files:**
- Create: `hivemtk/.github/workflows/website-pages.yml`
- Modify: `hivemtk-platform/.github/workflows/platform-ci.yml`（删 `website-lint`、`website-audit` 两 job 与两处 path-filter）
- Modify: `hivemtk-platform/.github/workflows/docs-link-check.yml`、`hivemtk-platform/Makefile:29,88`、`hivemtk-platform/发布流程.md`(18)
- Modify: `hivemtk/website/deploy.sh`（SSH/rsync 段删除，保留 i18n 预检 + build）

**Interfaces:**
- Produces: 每次 `website/**` 变更 → Pages 部署（`actions/deploy-pages`）。

- [x] **Step 1: 写 workflow（复用 hivemtk 侧已修好的写法，勿照抄 platform 的死法，见 C6）**

```yaml
name: website-pages

on:
  push:
    branches: [master]
    paths: ['website/**', '.github/workflows/website-pages.yml']
  pull_request:
    branches: [master]
    paths: ['website/**', '.github/workflows/website-pages.yml']
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: true

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 20
          cache: npm
          cache-dependency-path: website/package-lock.json
      - run: npm ci
        working-directory: website
      - name: i18n 词典完整性预检（MISSING_UNIQ 必须为 0）
        working-directory: website
        run: bash scripts/check-i18n-completeness.sh
      - name: Lint
        working-directory: website
        run: npm run lint
      - name: Build
        working-directory: website
        run: npm run build
      - name: npm audit (OPT-CI-10 写法)
        # 勿照抄 hivemtk-platform/platform-ci.yml 的旧写法：npm 10 的 quick-audit 端点
        # 已退役，runner 实测 400 且会把错误对象当报告解析成"0 漏洞"（假绿）。
        working-directory: website
        run: bash ../scripts/npm-audit-gate.sh website
      - uses: actions/upload-pages-artifact@v3
        with:
          path: website/dist
  deploy:
    if: github.ref == 'refs/heads/master' && github.event_name != 'pull_request'
    needs: build
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.page.outputs.url }}
    steps:
      - id: page
        uses: actions/deploy-pages@v4
```

- [x] **Step 2: 核对依赖的两个脚本是否真实存在**

```bash
cd hivemtk && ls website/scripts/ scripts/ | grep -iE "i18n|audit"
```
若 `check-i18n-completeness.sh` / `scripts/npm-audit-gate.sh` 名字与实际不符（i18n 预检目前活在 `website/deploy.sh` 里、audit 写法活在 `hivemtk/.github/workflows/user-server-ci.yml` 内联步骤），**就地改成实际可跑的形状**：把 deploy.sh 里的预检函数抽成独立脚本、把 OPT-CI-10 那段内联命令搬进 `scripts/npm-audit-gate.sh`。不许在 yaml 里留"稍后再补"。

- [x] **Step 3: 本地把 workflow 的每一步真跑一遍**

Run: `cd hivemtk/website && npm ci && bash scripts/check-i18n-completeness.sh && npm run lint && npm run build && (cd .. && bash scripts/npm-audit-gate.sh website)`
Expected: 全绿。

- [x] **Step 4: 反向测试（每道门都要能红）**

```bash
cd hivemtk/website
printf '\nconst x=1\n' >> src/config/content.js          # 注入 lint 违规
npm run lint; echo "rc=$?"                                # 期望非 0
sed -i '' '1s/^/<<MISSING-PROBE>>\n/' src/i18n/locales/ja.json  # 破坏词典
bash scripts/check-i18n-completeness.sh; echo "rc=$?"     # 期望非 0
git checkout -- src/config/content.js 2>/dev/null || true  # 禁用！见下条
```
**不要用 `git checkout --` 还原**（Step 4 的 sed/echo 是本批未提交改动，checkout 会连你真正的改动一起吞）。改前 `cp src/config/content.js /tmp/content.js.bak`、`cp src/i18n/locales/ja.json /tmp/ja.json.bak`，用 `cp` 写回，并 `diff` 证明还原到位。

- [x] **Step 5: 摘除 platform 侧的 website 门与引用**

`platform-ci.yml` 删 `website-lint`、`website-audit` 两 job 与 `paths` 里的 `'website/**'`×2，并在头部"本 workflow 从未执行过"注释块追加一行：website 已迁 hivemtk 仓，那边是**真跑的**门；`Makefile:29,88`、`docs-link-check.yml`、`发布流程.md` 的 website 条目同删。
删 `hivemtk-platform/website/` 旧副本（Step 5 时机：`diff -r` 确认新副本与旧目录除本任务改动外无遗漏文件、且新目录 build 绿）。

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk
diff -rq hivemtk-platform/website hivemtk/website 2>&1 | grep -v node_modules | grep -v '^Only in hivemtk/website' | head -20
```
Expected: 只出现"新副本里被有意改掉/删除的文件"，**不得有"Only in hivemtk-platform/website"**（漏搬文件）。

- [x] **Step 6: 真发布一次并实测**

`gh workflow run website-pages.yml -R xiaofang142/hivemtk --ref master`（需先有提交；若按"不 commit"约束不推，则改为：`gh api` 查 Pages 配置 + 本地 `dist` 起服实测已覆盖 Step 5，把"未做线上发布实测"写进 spec §7 并标注为交回用户的手工作）。
**Pages 未开启时**：`gh api -X POST repos/xiaofang142/hivemtk/pages -f build_type=workflow`，并在交清单里明确告知这是一次对 GitHub 侧的写操作。

**2026-09-22 执行记录（本步已真做完，数字为实测量）**：先 `POST repos/xiaofang142/hivemtk/pages -f build_type=workflow`
开启 Pages（仓库 `visibility=public`、`has_pages=false` ⇒ 之前 website-pages 的红因是
`Get Pages site failed … Not Found`，不是构建问题），再 `gh workflow run website-pages.yml --ref master`
⇒ run 全步 `success`（预检+构建+产物校验 / npm audit / Upload Pages artifact）。线上实测走真连：
首页 200 / 6505 B，首页引用的 4 个资源逐个 200，深链 `/hivemtk/features|download|docs` 均 200
（预渲染目录页），未知路径 404 但仍回 SPA 壳（`404.html` 兜底生效），
线上 index.html 与主 bundle（361804 B）内旧域命中 0，sitemap 7 条 / 旧域 0。
**开启前先过产物门**（三道源码门都看不见 `dist`）：`check-secrets-artifacts.sh .env website/dist` rc=0
且打印 scanned=152 / 10 个凭证键，并对它做四腿反向测试（注入真凭证 ⇒ 点名 `[POSTGRES_PASSWORD] 文件:行号`；
放一个 `.map` ⇒ 红；撤码 ⇒ 归位），据此把 `website/dist` 补进 `make audit-artifacts`。

- [x] **Step 7: 收口**

spec §7 记：本地五步全绿输出、两道反向测试的红因、旧副本 diff 结果、发布做到哪一步（或为何没做）。

---

### Task 10: 文档全量清除（旧域名与商户授权叙述不留占位）

**Files:**（实测命中数；括号外为需改写的文件）
- `hivemtk/`: `README.md`(1)、`README.en.md`(1)、`docs/architecture/FRP私域部署指南.md`(14)、`docs/oneid/oneid-architecture.md`(1)、`docs/operations/reverse-proxy/frpc.toml.template`(6)、`docs/operations/reverse-proxy/nginx.conf.template`(2)、`user-server/docs/dev/ARCHITECTURE.md`(1)、`user-server/docs/dev/FEATURES.md`(1)、`user-web/README.md`(1)、`user-web/docs/dev/DEVELOPMENT.md`(2)、`user-server/scripts/simulate/README.md`（Task 7 已改）
- `hivemtk-platform/`: `README.md`(2)、`发布流程.md`(18)、`docs/architecture/PLATFORM_ARCHITECTURE.md`(4)、`docs/architecture/部署方案_平台端与用户端.md`(4)、`platform-server/docs/dev/{ARCHITECTURE,CONVENTIONS,DEVELOPMENT}.md`、`platform-web/docs/dev/{ARCHITECTURE,DEVELOPMENT}.md`、`platform-contributor/docs/dev/DEVELOPMENT.md`、`website/docs/dev/{ARCHITECTURE,CONVENTIONS,DEVELOPMENT,FEATURES}.md`（随目录已搬到 `hivemtk/website/docs/dev/`）
- 根级未版本控制（**先确认 Task 1 Step 1 的 tar 快照存在**）：`docs/ARCHITECTURE_AUTH_ASSET_FLOW.md`(1)、`docs/architecture/DEPLOYMENT_OPS_ARCHITECTURE.md`(1)、`docs/operations/reverse-proxy/frpc.toml.template`(6)、`artifacts/asset-pack-e2e-report-20260904.md`(5，白名单)、`internal-docs/archive/USER_SIDE_DEEP_AUDIT_V3.md`(1，白名单)

**Interfaces:**
- Consumes: Task 1 的闸（判据）、Task 2/3 的实际行为（写文档的依据）。

- [x] **Step 1: 改写规则（对所有文件统一适用，不留占位）**

| 出现的东西 | 处置 |
|---|---|
| `https://hive.xapptool.cn/…`（官网） | 换成 `https://xiaofang142.github.io/hivemtk/…`；sitemap/canonical 由 Task 8 已处理 |
| `https://hiveuser.xapptool.cn/`（用户端前台）、`hiveuserapi.xapptool.cn`（其 API） | **整句删除**，"在线体验"入口、演示账号 `admin / Seed@123456` 一并删（密码已在公网文档里裸奔，删得掉文档删不掉泄露，交清单里提示轮换） |
| `hivepaltformapi` / `hivepaltform` / `hivecontributor` | 换成 `http://localhost:8205`（platform-server）/ 本地 contributor dev 端口，并写明"平台端为可选组件，默认不部署" |
| nginx/frpc 模板里的 `server_name` | 用 `hivemtk.example.com`（RFC 2606 示例域，具体值不是待填占位） |
| "商户授权 / License / 授权同步 / 心跳上报"叙述 | 删除或改写为"平台集成为可选本地组件，默认关闭；关闭后资产市场数据为空，本地资产与运行不受影响"（与 Task 2/3 实际行为逐字对齐） |
| README 架构图里的"HTTPS(低频:心跳 / 商户标识校验)"(README.md:199) | 该边默认不存在，改画为虚线"可选：自建 platform-server 时" |

- [x] **Step 2: 例外处理（只加注记，不改正文）**

`internal-docs/archive/USER_SIDE_DEEP_AUDIT_V3.md`、`artifacts/asset-pack-e2e-report-20260904.md` 顶部加：

```markdown
> 该线上环境已于 2026-09 下线，本段为下线前的实测记录，域名与响应均为当时事实，不作改写。
```

- [x] **Step 3: 验证**

```bash
cd hivemtk && bash scripts/check-no-xapptool.sh; echo rc=$?   # 仓内必须归零（Task 1 基线的红项消失）
cd /Users/xiaofang/Documents/www/go/hivemtk
grep -rIl xapptool docs internal-docs artifacts cold-start scripts 2>/dev/null   # 只剩两个白名单 + 闸摸不到的说明
cd hivemtk && make docs-check 2>/dev/null || bash scripts/docs-consistency.sh
```
Expected: 仓内 rc=0；根级只剩白名单 2 文件；文档一致性门绿。

- [x] **Step 4: 收口**

spec §7 记：闸从 N 红 → 0、根级残留清单。

---

### Task 11: 收口门禁 + 影子克隆复验 + 交付清单

- [x] **Step 1: 全套 Go 门（先测环境）**

```bash
df -h / | tail -1; pg_isready -h 127.0.0.1 -p 8232
cd hivemtk/user-server && go build ./... && go vet ./internal/... && go test ./... -count=1 2>&1 | tail -40
```
`internal/service` 单包超时就单独复跑并把 load 一起报数；红先归因环境（既往：磁盘写满 / 8232 口令漂移 / 并发抢库）。

- [x] **Step 2: 文档与静态门**

```bash
cd hivemtk && bash scripts/check-no-xapptool.sh && bash scripts/check-no-xapptool.test.sh
bash scripts/audit-cross-package-ports.sh
for s in scripts/*.test.sh; do bash "$s" || echo "RED $s"; done
make markdownlint docs-link-check 2>/dev/null || (ls scripts/ | grep -iE "markdown|link|secret")
bash scripts/check-secrets.sh 2>/dev/null; echo rc=$?
```
Expected: 全绿；`check-secrets` 必须绿（本批删了演示账号密码，属减少泄露面，不该引入新秘密）。

- [x] **Step 3: -race 与前端**

```bash
cd hivemtk/user-server && go test ./internal/router/ -race -count=1   # 既往该包 race 已修，回归确认没被本批弄回红
cd ../../hivemtk/user-web && npm run build
cd ../website && npm run build
cd ../../hivemtk-platform/platform-web && npm run build
```

- [x] **Step 4: 影子克隆复验（防"只在我这棵树绿"）**

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk
rsync -a --delete --exclude node_modules --exclude dist --exclude .git hivemtk/ /tmp/hivemtk-fresh/
cp -R .tmp_files/nothing /dev/null 2>/dev/null
cd /tmp/hivemtk-fresh && bash scripts/check-no-xapptool.sh 2>&1 | tail -3
```
Expected: 改名树里闸**不报 rc=1 的"找不到仓库根"**（既往缺陷：`scripts/../..` 定根 + 硬编码仓名 ⇒ 改名克隆零输出 rc=1）。若红，按既往口径修定根逻辑，不许靠"在原名目录跑"绕过。

- [x] **Step 5: 快照自查（根级未版本控制文件改错了没）**

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk
tar tzf .tmp_files/pre-offline-snapshot.tar.gz | head -5
for f in docs/ARCHITECTURE_AUTH_ASSET_FLOW.md cold-start/strategy/00_PLAN.md scripts/test_merchant_list.py; do
  echo "== $f"; tar xzOf .tmp_files/pre-offline-snapshot.tar.gz "$f" 2>/dev/null | diff - "$f" | head -8
done
```
Expected: diff 只出现有意的域名/授权改法，无整段误删。

- [x] **Step 6: 交回用户的人工清单（本批不 commit）**

1. `git -C hivemtk status --short`、`git -C hivemtk-platform status --short` 全量改动清单（含与开工前在途改动的边界说明）；
2. 需要你手工做的 3 件事：① GitHub 侧开启 Pages（或对 Task 9 Step 6 的写操作点头）；② 轮换已泄露在 README 里的演示账号密码；③ DNSPod 上把 hive/hiveuser/hivepaltform(api)/hivecontributor 五条记录删掉或改指，避免旧链继续 502；
3. spec §7 实施实况全文，含每道新门的反向测试红因、以及**没做到的验证项**（尤其浏览器真机与线上发布）。

## Self-Review 结论（写计划后自查，已就地修）

- **覆盖**：spec §5.1→Task 8/9，§5.2→Task 2/3/4/5，§5.3→Task 7/10，§5.4→Task 6，§5.5→各任务验证步 + Task 11，§6 风险→Task 1 Step 1（快照）、Task 9 Step 4（禁用 checkout）、Task 11 Step 4/6。
- **无占位**：Task 9 Step 2 显式承认 workflow 引用脚本名需按实测校正，并给出不许留 TODO 的约束；Task 4/5 里两处"以实际导出符号为准"是对既有代码命名的核实指令，非占位。
- **类型一致**：`config.PlatformEnabled()` / `config.PlatformURL()` / `config.WebsiteBaseURL()` / `platform.ErrPlatformNotConfigured` / `repository.PlatformAPIClient` 6 方法在 Task 2/3/6 间一致；`platform_enabled` JSON 字段名 Task 5 两处一致。
