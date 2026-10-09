# LICENSE 合规自检（AGPL-3.0 第 13 条）

> **v3 审计结论 [P0-S2]**：项目 LICENSE 是 AGPL-3.0（含网络 copyleft 第 13 条）。
> 任何 fork 后**通过网络对外提供服务**（SaaS / 托管 / API）必须开源修改。
> 2026-10-09 复核该结论成立：`LICENSE:1` 即 `GNU AFFERO GENERAL PUBLIC LICENSE`，
> 第 13 条正文在 `LICENSE:186`（标题）/ `LICENSE:188`（义务句），
> 项目的法律补充声明在 `NOTICE:13-24`。
> **但这条结论只覆盖「网络 copyleft」一面**；依赖与模型权重的 copyleft 面
> 脚本完全不查，实测见下文「自检范围与实测缺口」。

## 自检工具

```bash
# 判定当前仓库是否违反 AGPL-3.0
./scripts/license-compliance-scan.sh

# 指定 URL 校验
./scripts/license-compliance-scan.sh --public-url https://your-domain.com

# 仅判定不探活
./scripts/license-compliance-scan.sh --dry-run

# 关颜色（非 TTY 会自动关，见 :37；脚本只认 --no-color，传 --color 走 :32 的 unknown arg 退 1）
./scripts/license-compliance-scan.sh --dry-run --no-color
```

可用参数实测在 `scripts/license-compliance-scan.sh:27-34`，只有
`--dry-run` / `--no-color` / `--public-url <URL>` 三个；其余参数命中 `:32` 的
`*) echo "unknown arg: $1"; exit 1`（复算：`--bogus` → `unknown arg: --bogus`，rc=1）。

## 判定矩阵

| 维度 | PASS | WARN | 不改变判定（只打印） |
|---|---|---|---|
| git remote | 命中官方仓库正则（`:56`） | 无 remote（`:53-54`）或非官方（`:59-60`） | — |
| PUBLIC_BASE_URL | 未配置（`:72`）或解析为私网 IP（`:87`） | 解析为公网 IP（`:90-91`） | 域名解析失败（`:85-86`） |
| HTTP 探活 | 无（该维度不参与判定，见下） | 无 | 200 / 不可达 / 其它码全部只打印（`:100-107`） |
| 整体 | 全部 PASS | 任一 WARN | **FAIL 不可达** |

2026-10-09 实测对原矩阵的两处订正：

- **探活维度根本不参与判定**：`200` 打绿字（`:102`）、`000` 打绿字（`:103-104`）、
  其它状态码打黄字（`:106`），三个分支都没有给 `VERDICT` 赋值，也没有 `WARNINGS+=`。
  即「服务已公网可达并返回 200」这一条 §13 的直接证据，不会把结论从 PASS 抬到 WARN。
- **FAIL 是死代码**：全文对 `VERDICT` 的赋值只有三处
  （`grep -n 'VERDICT=' scripts/license-compliance-scan.sh` →
  `43:VERDICT="PASS"`、`59:VERDICT="WARN"`、`90:VERDICT="WARN"`），
  因此 `:131-133` 的 FAIL 分支永不进入，末行
  `[[ "$VERDICT" == "FAIL" ]] && exit 1 || exit 0`（`:137`）恒返回 0。
  三次实跑复算：
  `--dry-run --no-color` → `VERDICT: PASS`，rc=0；
  `--public-url http://127.0.0.1:8204` → `[3/3] ... → 200 ✓` + `VERDICT: PASS`，rc=0；
  `--dry-run --public-url http://203.0.113.10` → `VERDICT: WARN`，rc=0（不是 1）。
- 附带一个探针缺陷（属脚本层，本文不改脚本）：curl 失败时 `%{http_code}` 已经打印
  `000`，`:100` 尾部的 `|| echo "000"` 又追加一次，实测
  `--public-url http://127.0.0.1:18299`（端口有效但无人监听；同一地址单独跑
  `curl -sk -o /dev/null -w '%{http_code}' --max-time 3` → `000`，rc=7）
  得到 `→ 000000`，于是 `:103` 的 `== "000"` 匹配不上，
  「不可达（仅内网部署）✓」这条 PASS 文案在真实不可达场景里不会触发。

## AGPL-3.0 第 13 条原文（核心）

> 如果你修改本程序并通过网络提供服务，使得服务对象能够通过计算机网络
> 与本程序进行交互，你必须向服务对象提供你修改后的对应源代码。

对照 `LICENSE:188` 英文正文，该转述保留了两处关键限定：**modify**（仅改动作触发）
与 **users interacting with it remotely through a computer network**（对象是远程交互用户）。

## 私域豁免

- **内部使用**（同一法律实体内的员工/团队）：§13 的触发对象是「与本程序远程交互的用户」，
  未向第三方 propagate 时不产生对外开源义务。
- **私有部署（客户内网运行）**：**不是无条件豁免**。把副本交付给客户属于 convey
  （`LICENSE:42`：「To "convey" a work means any kind of propagation that enables other
  parties to make or receive copies」），须按第 6 条随附 Corresponding Source；
  `LICENSE:42` 只把「仅网络交互、无副本转移」排除在 convey 之外。
  只有客户自行获取原版、你不交付修改副本时，才只在 §13 框架下判定。
- **对外提供网络服务**：必须开源修改（`NOTICE:17-20` 已按此口径写明）。

## 自检范围与实测缺口

`license-compliance-scan.sh` 的三个维度是 git remote、`PUBLIC_BASE_URL` 解析结果、
一次 `/health` GET 探针；脚本内不解析任何 lock 文件、不读 `LICENSE` 文本、不看模型目录
（`grep -n 'go.mod\|package-lock\|LICENSE' scripts/license-compliance-scan.sh` 只命中
注释与 `THIRD_PARTY` 无关行）。因此依赖许可证与模型权重两条 copyleft 面需要单独核对，
2026-10-09 实测结果如下（命中行按原样列出，只给行号不转录口令/密钥类值）。

### 1. 前端直接依赖里存在 GPL，且与清单承诺冲突

- `user-web/package.json:42` → `"tinymce": "7.9.3"`（dependencies 段，直接依赖）；
  `user-web/package.json:29` → `"@tinymce/tinymce-vue": "^5.1.0"`；
  `user-web/package.json:72` → overrides 里再次钉定 `"tinymce": "7.9.3"`。
- `user-web/package-lock.json:11820-11824` → `"node_modules/tinymce"` /
  `"version": "7.9.3"` / `"license": "GPL-2.0-or-later"`。
- `user-web/vite.config.js:334` 的 `optimizeDeps.include` 显式预打包
  `'tinymce'`、`'@tinymce/tinymce-vue'`；`vite.config.js:292` 的 `manualChunks` 为
  `tinymce` 单列分桶 ⇒ dev 服务启动期就会把 GPL 代码送进浏览器缓存包。
- 但 `grep -rn -i 'tinymce\|ymce' user-web/src/` **零命中**，富文本当前是仓内自研
  `user-web/src/components/SimpleEditor/index.vue:41`（`contenteditable` + DOMPurify）。
  `user-web/README.md:87` 仍写「富文本 | TinyMCE 6」。
  ⇒ 结论：`THIRD_PARTY_LICENSES.md:365`「无 GPL / LGPL / AGPL 传染性许可证（除项目自身
  采用 AGPL-3.0）」被这条直接依赖否证；处置是「删依赖 + 改 README」或「按 GPL 履行义务并
  在清单声明」二选一，两条都属代码/清单一侧改动，本文只记录不落码。
- 同一份 lock 里其它非「MIT/BSD/Apache/ISC」条目（按 `packages[*].license` 聚合）：
  `MPL-2.0` 13、`BlueOak-1.0.0` 13、`MIT-0` 2、`(MIT OR CC0-1.0)` 2、`CC0-1.0` 1、
  `(MPL-2.0 OR Apache-2.0)` 1、`CC-BY-4.0` 1（`node_modules/caniuse-lite`）、
  `GPL-2.0-or-later` 1（即上条 tinymce），另有 **67 个包 lock 里无 license 字段**
  （含 `sass`、`pinia`、`vue-router`、`@parcel/watcher*` 等平台分发包）⇒ 自动识别会落到
  「待确认」。

### 2. 清单生成器没扫主应用

- `scripts/gen-third-party-notice.sh:147` 读的是 `user-web/bridge/package-lock.json`，
  `:172` 退化路径读 `user-web/bridge/package.json`；全文没有指向 `user-web/package-lock.json`
  的路径 ⇒ 主应用 `user-web/package-lock.json`（lockfileVersion 3，实数 **958** 个
  `packages` 条目）从未进入清单。
- `THIRD_PARTY_LICENSES.md:12` 声称「npm 包 | 185」，对应的是 bridge 侧历史行数
  （现 `user-web/bridge/package-lock.json` 实数 222 条），该数字既不含主应用也已过期。

### 3. Go 清单已过期且漏了直接依赖

- `THIRD_PARTY_LICENSES.md:11` 声称「Go 模块 | 117」；`user-server/go.mod` 现为 99 条
  `require`（37 条直接 + 62 条 `// indirect`）。
- 反查「在 go.mod 里、不在清单第 1 节里」共 22 条，其中直接依赖：
  `github.com/go-redis/redis_rate/v10`（`go.mod:12`）、`gonum.org/v1/gonum`（`:39`）、
  `go.opentelemetry.io/otel`（`:30`）、`go.opentelemetry.io/otel/sdk`（`:31`）、
  `go.opentelemetry.io/otel/trace`（`:32`）、`gorm.io/datatypes`（`:42`）。
- 版本漂移样本：清单 `:43` `gin v1.9.1` vs `go.mod:10` `v1.12.0`；
  清单 `:56` `golang-jwt/jwt/v5 v5.2.0` vs `go.mod:14` `v5.3.1`；
  清单 `:89` `redis/go-redis/v9 v9.7.0` vs `go.mod:20` `v9.22.0`；
  清单 `:93` `rs/zerolog v1.34.0` vs `go.mod:22` `v1.35.1`。
- 「清单有、依赖图没有」的样本（多为旧 `go list -m all` 全图残留）：
  `THIRD_PARTY_LICENSES.md:20` `Baozisoftware/qrcode-terminal-go`、
  `:24-28` `Rhymen/go-whatsapp/examples/*`、`:29-30` `bsm/ginkgo/v2`+`bsm/gomega`、
  `:113` 与 `:348` 的 `go.uber.org/goleak`——goleak 在 `user-server/go.mod` 里不存在，
  代码里只在注释中被提及（`user-server/internal/controller/chat_channel.go:233`、
  `user-server/internal/controller/chat_channel_whatsapp_test.go:333`），不是真依赖。

### 4. 模型权重：本地无许可证文本，下载期不留痕

- 磁盘权重（`find` 实测，均在 `.gitignore` 的 `*.gguf` / `*.safetensors` 之下）：
  `models/llm/qwen2.5-7b-instruct-q4_k_m.gguf`（4.4G）、
  `models/embedding/bge-m3-Q4_K_M.gguf`（417M）、
  `models/rerank/bge-reranker-v2-m3-q4_k_m.gguf`（418M）、
  `models/laya/model.safetensors`（804M）。
- `find models -iname '*licen*'` **零命中** ⇒ 前三份 GGUF 目录里没有任何许可证文件；
  唯一有许可声明的是 `models/laya/README.md:2` → `license: apache-2.0`（随模型卡自带）。
- 来源记录只有仓库名与文件名：`.env:240-241` `LLM_REPO=Qwen/Qwen2.5-3B-Instruct-GGUF` /
  `LLM_FILE=qwen2.5-3b-instruct-q4_k_m.gguf`，`.env:246-247`
  `EMBEDDING_REPO=Xorbits/bge-m3-gguf`、`.env:253-254`
  `RERANK_REPO=puppyM/bge-reranker-v2-m3-Q4_K_M-GGUF`。embedding/rerank 两份都来自
  **第三方量化仓**而非原始模型仓，权重许可需回到上游模型卡逐一确认；且 LLM 侧配置写 3B、
  磁盘上是 7B 文件，来源与产物对不上。
- `grep -n -iE 'licen|terms|agreement|checksum|sha256' scripts/inference-host/download-models.sh`
  **零命中** ⇒ 下载脚本既不落许可证副本也不校验摘要（`:97-99` 直接按 repo/file 拉取）。
- `grep -n -iE 'qwen|bge|gguf|模型|weight|safetensors' THIRD_PARTY_LICENSES.md` **零命中**
  ⇒ 模型权重的 copyleft/商用限制在清单与本文工具里都是空白面。

### 5. CI 侧覆盖

`.github/workflows/lint.yml`：`:19` `cron: '0 3 1 * *'`、`:22` `workflow_dispatch: {}`、
`:293` `if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'`、
`:299` `run: bash scripts/license-compliance-scan.sh --dry-run --no-color`。
⇒ CI 里探活维度恒为「跳过（--dry-run）」；该 job 的 steps 只有 checkout + run，
没有注入 `.env`（`.env` 未被 git 跟踪，`git ls-files --error-unmatch .env` 报 not tracked），
`PUBLIC_BASE_URL` 在 CI 恒未配置 ⇒ 维度 2 恒 PASS，实际只剩维度 1 的 remote 正则；
叠加「FAIL 不可达」，这个 job 永远绿。
`grep -rn -iE 'go-licenses|license-checker|gen-third-party|THIRD_PARTY' .github/workflows/`
**零命中** ⇒ `THIRD_PARTY_LICENSES.md:369-379` 的 `go-licenses check` /
`license-checker --onlyAllow` 与 `:382` 的重新生成流程都是手动约定，无任何 CI 把关，
这也是第 2、3 节清单过期能长期存在的原因。

## 定期自检

`.github/workflows/lint.yml` 的 `license-compliance` job 每月 1 日
（cron `0 3 1 * *` UTC）自动跑一次 `license-compliance-scan.sh --dry-run --no-color`，
支持 `workflow_dispatch` 手动触发；WARN 不阻断（脚本恒退 0），
**FAIL 不会使 workflow 失败——脚本没有可达的 FAIL 路径**，
需要「阻断」的场合得由依赖/权重侧的独立检查提供（见「自检范围与实测缺口」）。
