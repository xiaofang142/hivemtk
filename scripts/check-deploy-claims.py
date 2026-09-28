#!/usr/bin/env python3
"""对外口径可核性门：写进种子文案/文档里的部署命令与资源名，必须和仓库真值同源。

背景（2026-09-23 · 第五十一轮 R38）：`cmd/seed/` 里的 FAQ、知识条目与"部署引导助手"人设
是**装到客户机器上、由客服 AI 原样念给客户听**的东西。本轮实测它们与真值差了一整代：

- `make up` 在 2026-08-17 宿主机化重构（`94415060`，删掉 `user-server/Dockerfile`）后**没有规则、
  只在 `.PHONY` 里留着名字** ⇒ `make -n up` 退 **0** 且只印 `Nothing to be done for 'up'`。
  客户照做：命令"成功"、什么都没起来、没有任何报错。比"报错缺失"更坏（后者 rc=2 至少会喊）。
  同类还有 `init` / `down` / `restart` / `logs` / `ps` / `build`；`backup` / `restore` /
  `inference-up` / `inference-down` 则连 `.PHONY` 都没留、直接 rc=2。
- `docker logs mtk-user-server` 指向一个**不存在的容器**（compose 里只有 `mtk-postgres` / `mtk-redis`）。
- `PG 8232` 写的是**开发可选覆盖档**（`.env-example:47-48`：Docker 数据层默认 8202，
  宿主机直连 Dev 才 8232），被当成默认值印在端口表里。
- `数据卷：… / mtk_user_logs / mtk_user_uploads` 两条卷根本没人声明。
- `make install 自动生成 .env 与 docker-compose.yml` —— install 只做 `cp .env-example .env`，
  `docker-compose.yml` 是仓库自带的（且只含数据层）。

同一批缺陷不止在 seed 里：把扫描面扩到 `migrations/*.sql`（031 那份 RAG 种子的端口/卷/命令
全是 docker 时代口径，且 `PLATFORM_ADMIN_PASSWORD` 被写成 admin 的默认口令）、
`scripts/seed/*.py`（`make upgrade` 查无此名）与**对外官网** `website/src/`
（`DeployPage.vue` / `DocsPage.vue` + `i18n/modules/docs*.js` 里 43 处在教客户
 `make up` / `make backup` / `docker compose logs mtk-user-server`）。官网是**客户第一眼看到的部署口径**，
比仓内文档更坏。

判据（九条，任一不成立即 rc=1；每条的真值都从仓库现读，不写死在门里）：
  1. 扫描面里每个 `make <target>` 的 target 必须在**根 Makefile 里真有规则**
     （行首 `^[A-Za-z0-9_.-]+:` 且非 `.PHONY` 行）；只在 `.PHONY` 挂名的判红——
     这正是"退 0 但什么都不做"的那一档；
  2. `mtk-*` / `mtk_*` 形态的容器/网络/卷名必须出现在 `docker-compose.yml` 的声明里
     （services / networks / volumes 的键，或 `name:` 值）——docker 时代的服务名与卷名
     在宿主机化重构后大半已不存在，而"数据卷 mtk_user_logs""docker logs mtk-user-server"
     这类话客户照着敲只会得到 "No such container" / "找不到卷"。
     例外：**读者自己起的名字**不判（`container_name: mtk-frpc`、`name = "mtk-user-chat"`
     这类声明位置）——那是在教人创建资源，不是断言我们仓库里有这个资源；
  3. 种子文案字段（Content/Description/Template/A/Question/Title）里 `PG <四位端口>` 与
     `Redis <四位端口>` 必须等于 compose 里 `${USER_POSTGRES_HOST_PORT:-N}` /
     `${REDIS_HOST_PORT:-N}` 的默认值（端口表是给客户排冲突用的，写错一档＝客户白停服务）；
  4. 不许出现"（自动）生成 … docker-compose"的表述：该文件由版本控制提供，install 不产出它；
  5. 把"默认账号 / 初始口令"归给某个 `*_PASSWORD` 环境变量时，该变量必须在
     `cmd/seed/seed_users.go` 的 `resolveSeedPassword` 键名单里（现值 SEED_PASSWORD / ADMIN_PASSWORD）。
     `PLATFORM_ADMIN_PASSWORD` 是**平台端代理**的管理员口令（只被 `config/platform.yaml` 读），
     把它写成用户端 admin 的默认口令，客户会拿它去登 8204 并一直登不进；
  6. 文案里指向源码的 `cmd|internal|pkg/...*.go` 路径必须真存在（仓库根与 `user-server/` 两个根都试）
     ——`migrations/README.md` 用一条不存在的 `internal/pkg/utils/db/migrate.go` 解释了
     "改 .sql 会在启动时自动执行"，据此改迁移的人只会等到"什么都没发生"；
  7. `user-web` / "前端工作台" / "Vite dev" 语境里的四位数端口必须等于
     `user-web/vite.config.js` 的 `server.port`（本轮现读＝8211）。仓内 6 处口径（含**根 Makefile 的
     `make install` 成功回显**）写的是 Vite 出厂默认 5173，客户按它打开只会得到空白页；
     Playwright `baseURL` 本轮已跟着抬到 8211（它本就指向 dev server），不再需要豁免；
  8. `rag_products('hivemtk-platform-cs')` 这一行注册数据有**两条产码路径**：
     `migrations/031_platform_cs_rag_seed.sql`（自带"不会被任何自动路径执行"的头注释，人工一次性灌）
     与 `user-server/cmd/seed/seed_rag_product.go`（cmd/seed 每次跑的幂等补齐）。
     两边同名字段（`RAG_PARITY` 六格：id / category / vector_table / embedding_model /
     embedding_dim / llm_model）必须逐格同值——漂移后 031 就成了"看起来会建、其实永远不建"的
     第二事实源，而 `embedding_model` + `embedding_dim` 直接决定已灌进分段表的数据能否被召回命中。
     这条与前七条不同档：前七条比的是"文案 vs 仓库真值"，这条比的是**两份产码互相**；
  9. 模型名必须等于 `.env-example` 里那三档 served name（`LLM_SERVED_NAME` /
     `EMBEDDING_SERVED_NAME` / `RERANK_SERVED_NAME`，现读）。`.env-example` 是 `make install`
     原样 `cp` 成客户机 `.env` 的清单，宿主机 llama-server 只认它声明的那几个 `--alias`
     ⇒ 分两条腿：
     A **落库值**只认 SQL 里真会进库的位置（建表列 `DEFAULT`、带 VALUES 的 INSERT 按列名同位置配对；
       列名单点了模型列却没有 VALUES 名单时退化成"该语句里每个引号字面量"），
       不扫语句全文——031 的 13 篇知识文档正文正常提到 14B 这类"可换的大档"，全文扫就是误伤；
     B **文案口径**判"当前默认档"语境（`TIER_CUE`）所在句点到的模型名是否在清单里，
       同句里"prod 档 14B"这种**对比档**说法放行（判据见 `_bound_to_default_tier`）。
     建门实测到的三处真缺陷：033 首批智能体行 `llm_model='gpt-4o-mini'`、
     `init-user-db.sql` 列默认 `'gpt-3.5-turbo'`、以及一批文档/种子文案仍写 1.5B 档
     ——同文件 `llm_base_url=''` 意味着走本地 8207，这些名字发出去就是"指向一个本机没跑的模型"。
     `llm_model` 字段被复用来存 **provider 名** `"default"`（`dispatcher_register.go` 以它注册默认
     provider、`sales_engine.go` 的 `case "default"` 认它）⇒ 这一档放行。

豁免：
  - 行内 `deploy-claim:historical` 标记（见 docs/architecture/部署方案_用户端.md:155 那条
    "这些 target 已废弃"的说明——它**提到**废弃命令正是它的职责，不许被门打掉）；
  - 规则 2 的"读者自己起的名字"（声明位置）与规则 6 的"教读者新建哪个文件"（创建语境）；
  - 规则 7 只剩 embed-sdk 自己的 5174 预览（另一个服务的真值）与 5432/6379（同行出现的
    PG/Redis 出厂端口，会被触发词窗口误捞）；原先那档 `playwright|E2E|baseURL` 在本轮
    baseURL 抬到真值后**一处都不再命中**，按 R20"永不生效的豁免表本身就是洞"删掉；
  - 规则 9 的对比档说法（同句 cue 与模型名之间出现 `prod|更大|可选|高配…`）。

自证：门必须打印扫描面的**命中数**（make 令牌数 / 容器名数 / 端口断言数 / 源码路径断言数 /
前端端口断言数 / 规则 8 比对格数 / 规则 9 两类命中数）。
`scanned=0` 不是"没问题"，是门瞎了——历史教训见 docs 里"门禁口径盲区"一族。
规则 9 把这句话写成了判据：两类命中数任一为 0 直接进红账（判据被改坏时不许沉默放行）。

执行入口：`make audit`（本地聚合门）。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SEED_DIR = REPO_ROOT / "user-server" / "cmd" / "seed"
COMPOSE = REPO_ROOT / "docker-compose.yml"
MAKEFILE = REPO_ROOT / "Makefile"
SEED_USERS = SEED_DIR / "seed_users.go"
USER_WEB_VITE = REPO_ROOT / "user-web" / "vite.config.js"
MIG31 = REPO_ROOT / "migrations" / "031_platform_cs_rag_seed.sql"
SEED_RAG_PRODUCT = SEED_DIR / "seed_rag_product.go"

# 扫描面＝"会被客户或运维照着敲"的那批文案。
# 刻意不含 docs/architecture/ 整体（里面大量历史叙述），只点名本轮实测在教人敲命令的那几份；
# 新增对外文案面时往这里加名字，别扩大成"全仓 md"。
DOC_FACE: list[Path] = [
    REPO_ROOT / "CONTRIBUTING.md",
    REPO_ROOT / "SECURITY.md",
    REPO_ROOT / "website" / "MENU_SPEC.md",
    REPO_ROOT / "website" / "docs" / "dev" / "FEATURES.md",
    REPO_ROOT / "docs" / "architecture" / "ARCHITECTURE_DIAGRAM.md",
    REPO_ROOT / "migrations" / "README.md",
    # 2026-09-23 扩面：本轮实测"前端 dev 端口"这一档漂移全在下列文件里（5173 vs 真值 8211）。
    # 根 Makefile 自己也入面——它的 `help` / `install` 回显是**客户第一条命令的来源**，
    # 里的 target 名同样必须真有规则。
    MAKEFILE,
    REPO_ROOT / "docs" / "PORT_REGISTRY.md",
    REPO_ROOT / "docs" / "contribute" / "CODEBASE_MAP.md",
    REPO_ROOT / "docs" / "architecture" / "HOST_INFERENCE_PLAN.md",
    REPO_ROOT / "user-server" / "docs" / "dev" / "DEVELOPMENT.md",
    REPO_ROOT / "user-web" / "docs" / "dev" / "DEVELOPMENT.md",
    REPO_ROOT / "user-server" / "scripts" / "seed-llm-providers.sh",
]

# 目录型扫描面：整个目录都算，不按文件名点名——`migrations/` 下每加一份种子/迁移都会带新的口径，
# 官网 `website/src/` 下每加一页都是对外口径，点名文件等于给"下一份漂移"留门。
GLOB_FACE: list[tuple[Path, tuple[str, ...]]] = [
    (SEED_DIR, ("*.go",)),
    (REPO_ROOT / "migrations", ("*.sql",)),
    (REPO_ROOT / "scripts" / "seed", ("*.py",)),
    (REPO_ROOT / "website" / "src", ("*.vue", "*.js")),
]

IGNORE_MARK = "deploy-claim:historical"

MAKE_TOKEN = re.compile(r"\bmake\s+([a-z][a-z0-9_-]*)")
# 容器/卷/网络名一律 mtk[-_]xxx；下划线那一档是卷（mtk_user_pg_data），
# 只认连字符会把"数据卷 mtk_user_logs"这类假名字整个放过。
MTK_NAME = re.compile(r"\bmtk[-_][A-Za-z0-9][A-Za-z0-9_-]*\b")
# 规则 2 的豁免位：读者**自己起**的名字（FRP 片段里的 `name = "mtk-user-chat"`、
# `container_name: mtk-frpc`）。这类行是在教人创建资源，不是在断言仓库里已有它。
MTK_DECLARED = re.compile(r"\b(?:container_)?name\s*[:=]\s*[\"']?mtk[-_][A-Za-z0-9_-]*")
# 端口口径：关键字后 12 字符内、且不跨分隔符的第一个四位数
# （"PG 8232"、"PostgreSQL :8202"、"8203 Redis；8207 …" 都要落在紧邻的那个数上，
#  分隔符必须进排除集，否则 Redis 会去吃后面 LLM 的 8207——实测踩过）。
PORT_CLAIM = re.compile(r"\b(PG|PostgreSQL|Redis)\b[^。；;、，,\n]{0,12}?(\d{4})")
# 规则 3 只管"会被原样念给客户听"的种子文案字段。刻意不收运维侧提示：
# `cmd/seed/main.go` 那条 FATAL 文案写的是"dev 本机 PG=8232，docker=8202"——两档都在、说的是真话，
# 按整行判就成了假红（本轮实测撞上，据此收紧）。
COPY_FIELD = re.compile(r"\b(Content|Description|Template|Question|Title)\s*:|\bA\s*:")
COMPOSE_GEN = re.compile(r"生成[^\n。]{0,20}docker-compose")
# 规则 5：口令语境绑得紧，只认"默认账号/口令"与其后 30 字内的环境变量名，
# 或反向 20 字内。整行判会把"改 3 个密钥（POSTGRES_PASSWORD…）……默认账号 admin"这种
# 一句里既有真密钥清单、又有登录说明的正常文案误伤（实测踩过）。
PW_TAIL = re.compile(r"(?:默认账号|默认口令|初始口令|默认管理员|超管初始)[^。\n]{0,30}?\b([A-Z][A-Z0-9_]*PASSWORD)\b")
PW_HEAD = re.compile(r"\b([A-Z][A-Z0-9_]*PASSWORD)\b[^。\n]{0,20}?(?:默认账号|默认口令|初始口令|即管理员|就是密码)")
# 规则 6：文案里指向源码的 `internal/.../x.go` 路径必须真存在。
# `migrations/README.md` 就栽在这——它写着"002-033 由 internal/pkg/utils/db/migrate.go 在启动时执行"，
# 而那个文件从来没有过（真路径是 user-server/internal/pkg/db/migrate.go，且它是 GORM AutoMigrate +
# Go 迁移任务，**从不读 .sql**）。读者据此会以为改动 SQL 就会自动生效。
GO_PATH = re.compile(r"(?<![\w/.-])((?:cmd|internal|pkg)/[A-Za-z0-9_./-]+\.go)")
# 路径可以带 :行号 或不带；这里只取到 .go 为止，存在性按两个根试：仓库根 / user-server。
# 规则 6 的豁免位：**教读者新建哪个文件**的标题行（`user-server/docs/dev/DEVELOPMENT.md` §4 的
# 「步骤 2：定义 DTO（`internal/dto/customer_tag.go`）」）——那是"下一步去创建它"，
# 不是在断言仓库里已有它。判据用创建语境词，不用"文件名像不像示例"。
CREATE_CUE = re.compile(r"步骤\s*\d|新建|创建")
# 规则 7：前端 dev server 端口。`user-web/vite.config.js` 的 `server.port` 是唯一真值（现读，不写死）。
# 触发词后 26 字符内的第一个四位数＝这句话在说的前端端口；窗口必须收紧且不跨句号，
# 否则"前端 user-web（Node 18+，8211）→ 后端 8204"这种一行两口的正常文案会去吃后端的数。
FE_CLAIM = re.compile(r"(?:user-web|前端工作台|Vite dev|vite)[^。\n]{0,26}?(\d{4})")
# 豁免位只留**今天真的在用**的两档：embed-sdk 自己的 vite 预览端口（5174，与 user-web 无关）、
# 以及 5432/6379（PG/Redis 出厂端口常出现在同一行配置说明里，会被触发词窗口捞走）。
# 原先还豁免 `playwright|E2E|baseURL`——那是 baseURL 停在 5173 那一代留的口子。本轮把
# `user-web/playwright.config.js:23` 抬到 8211（＝真值）后它一处都不再命中（实测：整张面上
# 只靠豁免活下来的前端断言只剩 embed-sdk 那一行）。按 R20 的教训，永不生效的豁免表本身就是洞
# ——任何一行带 "E2E" 字样的前端端口断言都能被它白放行，故删而不是留着"以防万一"。
FE_EXEMPT = re.compile(r"embed-sdk|5432|6379")
# 判据必须跑在**渲染后的句子**上：官网 `.vue` 里的文案是分片存的
# （`{{ $t('默认管理员账号') }} <code>admin</code>{{ $t('，密码为 …PLATFORM_ADMIN_PASSWORD。') }}`），
# 客户看到的是一整句，门读的却是源码——夹在中间的 `{{ $t() }}`／`<code>` 标记能把
# "默认管理员"和它后面的口令变量名推出 30 字窗口外。本轮实测：官网两处把**平台端代理口令**
# 写成用户端超管登录口令（DocsPage.vue:127 / :353），按原始行判一条都抓不到。
VUE_INTERP = re.compile(r"\{\{\s*\$t\(\s*'((?:[^'\\]|\\.)*)'\s*\)\s*\}\}")
HTML_SOFT = re.compile(r"</?(?:code|pre|strong|em|span|p|li|ul|ol|br|kbd|samp)\s*/?>")
HTML_ENT = {"&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": '"', "&nbsp;": " "}


def render(line: str) -> str:
    """把 Vue 模板还原成客户在浏览器里读到的那句话（行号不变，只换内容）。"""
    out = VUE_INTERP.sub(lambda m: m.group(1), line)
    out = HTML_SOFT.sub("", out)
    for ent, ch in HTML_ENT.items():
        out = out.replace(ent, ch)
    return out


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="ignore")


def implemented_targets() -> tuple[set[str], set[str]]:
    """(真有规则的 target, 只在 .PHONY 挂名的 target)。

    只认行首 `name:`（行首空白＝配方行）；`.PHONY:` 那一行是声明不是规则。
    两者分开返回是因为"挂名无规则"这一档在 make 里退 **0** 且只印
    `Nothing to be done for 'x'`——比查无此名（rc=2 会报错）更坏，判红时必须点出来。
    """
    body = read(MAKEFILE)
    targets: set[str] = set()
    for line in body.splitlines():
        if not line or line[0] in " \t#":
            continue
        m = re.match(r"^([A-Za-z0-9_.-]+):(?!=)", line)
        if not m:
            continue
        name = m.group(1)
        if name != ".PHONY":
            targets.add(name)
    phony: set[str] = set()
    for line in re.findall(r"^\.PHONY:.*", body, re.M):
        phony.update(line.split()[1:])
    return targets, phony - targets


def compose_names() -> set[str]:
    """compose 里真实声明过的 mtk-* / mtk_* 名字（services / networks / volumes 的键，
    以及 `name:` 显式赋值）。按"缩进两格的键 + name 值"收，不按顶层块位置切分——
    本仓 compose 的顶层顺序是 networks/volumes/services，按位置切容易在文件重排后漏收，
    而漏收会让门把真名判成假名（红得无理由）。
    """
    body = read(COMPOSE)
    names = set(re.findall(r"^\s{2}(mtk[-_][A-Za-z0-9_-]+):", body, re.M))
    names.update(re.findall(r"^\s+name:\s*(mtk[-_][A-Za-z0-9_-]+)\s*$", body, re.M))
    return names


def compose_default_port(env_key: str) -> str | None:
    m = re.search(r"\$\{" + env_key + r":-([0-9]+)\}", read(COMPOSE))
    return m.group(1) if m else None


def frontend_port() -> str | None:
    """user-web 开发服务器端口真值：只认 `user-web/vite.config.js` 里 `server:` 块的 `port:`。

    本轮实测：文档里"前端 5173"（Makefile 回显、两份 dev 文档、官网两份口径、种子脚本提示）
    写的是 **Vite 出厂默认**，而本仓 vite.config.js 把端口钉成了别的数 ⇒ 客户按文档打开 5173 必空白页。
    所以真值必须从配置文件现读，而不是从"大家一般都写 5173"里抄。
    """
    m = re.search(r"server:\s*\{[^{}]*?port:\s*(\d{4})", read(USER_WEB_VITE), re.S)
    return m.group(1) if m else None


def password_truth() -> set[str]:
    """用户端初始超管口令**真的**会从哪些环境变量取。

    不写死名单，从 `cmd/seed/seed_users.go` 现读：那条 `for _, key := range []string{...}`
    就是唯一事实源。门里写死 `SEED_PASSWORD` 的话，改代码的人换键名时门会红得说不清原因。
    """
    body = read(SEED_USERS)
    m = re.search(r"range\s*\[\]string\{([^}]*)\}", body)
    if not m:
        return set()
    return set(re.findall(r"[\"']([A-Z0-9_]+)[\"']", m.group(1)))


# 规则 8：rag_products 注册行的两条产码路径必须同值。
# 同一行注册数据有两条产码：`migrations/031_platform_cs_rag_seed.sql`（人工一次性灌入，
# 它同时带 13 篇知识文档与分段）与 `user-server/cmd/seed/seed_rag_product.go`（cmd/seed 的
# 幂等补齐）。031 按它自己的头注释「不会被任何自动路径执行」⇒ 常规装完的库里只有 Go 那一版，
# 两边一旦漂移，031 就成了"看起来会建、其实永远不建"的第二事实源；而它带的
# embedding_model / embedding_dim 直接决定已灌进分段表的数据能不能被检索命中。
RAG_PARITY: tuple[tuple[str, str], ...] = (
    ("id", "ID"),
    ("category", "Category"),
    ("vector_table", "VectorTable"),
    ("embedding_model", "EmbeddingModel"),
    ("embedding_dim", "EmbeddingDim"),
    ("llm_model", "LLMModel"),
)


def split_top_level(text: str) -> list[str]:
    """按**引号外、括号外**的逗号切分 SQL 列表。

    引号感知不是洁癖：031 的 `system_prompt` 值里有 `1)` `2)` 这种 ASCII 括号，
    按裸逗号/裸括号切会把一列劈成两列，位置配对随之整体错位——错位的门比不开门更坏。
    """
    items: list[str] = []
    buf: list[str] = []
    depth = 0
    in_str = False
    i = 0
    while i < len(text):
        ch = text[i]
        if in_str:
            if ch == "'":
                if text[i + 1 : i + 2] == "'":  # SQL 里 '' 是转义的单引号，不是字符串结束
                    buf.append("''")
                    i += 2
                    continue
                in_str = False
        elif ch == "'":
            in_str = True
        elif ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth -= 1
        elif ch == "," and depth == 0:
            items.append("".join(buf))
            buf = []
            i += 1
            continue
        buf.append(ch)
        i += 1
    if buf:
        items.append("".join(buf))
    return [x.strip() for x in items]


def group_after(text: str, anchor: str) -> str | None:
    """取 anchor 之后第一个配对组（`(...)`）内部的原文，引号内的括号不计入深度。"""
    idx = text.find(anchor)
    if idx < 0:
        return None
    start = text.find("(", idx + len(anchor))
    if start < 0:
        return None
    depth = 0
    in_str = False
    i = start
    while i < len(text):
        ch = text[i]
        if in_str:
            if ch == "'":
                if text[i + 1 : i + 2] == "'":
                    i += 2
                    continue  # '' 是值里的转义引号，字符串还没结束
                in_str = False
        elif ch == "'":
            in_str = True
        elif ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
            if depth == 0:
                return text[start + 1 : i]
        i += 1
    return None


def unquote_sql(value: str) -> str:
    if len(value) >= 2 and value[0] == "'" and value[-1] == "'":
        return value[1:-1].replace("''", "'")
    return value


def rag_product_sql_row() -> dict[str, str]:
    """031 里那条 INSERT 的 `列名 -> 值`。按位置配对，列/值数量不等即判读空。"""
    body = read(MIG31)
    anchor = "INSERT INTO rag_products"
    head = body[body.find(anchor):] if anchor in body else ""
    if not head:
        return {}
    cols_raw = group_after(head, anchor)
    vals_raw = group_after(head, "VALUES")
    if cols_raw is None or vals_raw is None:
        return {}
    cols = split_top_level(cols_raw)
    vals = split_top_level(vals_raw)
    if len(cols) != len(vals):
        return {}
    return {c: unquote_sql(v) for c, v in zip(cols, vals)}


def rag_product_go_row() -> dict[str, str]:
    """`seed_rag_product.go` 里构造的 `字段名 -> 值`。

    按行取 gofmt 后的 `Key: value,` 而不是配对括号：结构体字面量一行一个字段，
    而 `SystemPrompt` 的值里带 ASCII 括号，括号配对极易被字符串内容带偏。
    `platformCS*` 形态的常量引用回落到 const 块取字面值——否则比对的是变量名而不是值。
    """
    body = read(SEED_RAG_PRODUCT)
    anchor = "model.RagProduct{"
    idx = body.find(anchor)
    if idx < 0:
        return {}
    consts = dict(re.findall(r'^\s*(platformCS\w+)\s*=\s*"([^"]*)"', body, re.M))
    row: dict[str, str] = {}
    for line in body[idx + len(anchor):].splitlines()[1:]:
        if line.strip().startswith("}"):
            break
        m = re.match(r"^\s*([A-Z]\w*):\s*(.+?)\s*,?\s*$", line)
        if not m:
            continue
        key, val = m.group(1), m.group(2)
        val = consts.get(val, val)
        if len(val) >= 2 and val[0] == val[-1] and val[0] in "\"'":
            val = val[1:-1]
        row[key] = val
    return row


def rag_product_parity() -> tuple[list[str], int]:
    """规则 8 的判定：返回 (红项, 实际比对字段数)。"""
    sql_row, go_row = rag_product_sql_row(), rag_product_go_row()
    if not sql_row or not go_row:
        return (
            [f"rag_products 注册行对照读空：SQL 侧 {len(sql_row)} 列 / Go 侧 {len(go_row)} 字段"
             f"（{MIG31.name} / {SEED_RAG_PRODUCT.name}）⇒ 一侧解析不出字段不等于两边一致，判红"],
            0,
        )
    red: list[str] = []
    checked = 0
    for col, field in RAG_PARITY:
        if col not in sql_row or field not in go_row:
            red.append(f"rag_products 对照缺字段：SQL `{col}` / Go `{field}`"
                       f"（任一侧读不到即判红，静默少比一整格就是给下一轮漂移留门）")
            continue
        checked += 1
        if sql_row[col] != go_row[field]:
            red.append(f"rag_products `{col}` 两条产码路径不同值："
                       f"031 = {sql_row[col]!r}，cmd/seed = {go_row[field]!r}")
    return red, checked


# 规则 9：模型名必须等于 `.env-example` 里那三档 served name。
# `.env-example` 是 `make install` 原样 cp 成客户机 `.env` 的清单，宿主机 llama-server
# 只认它声明的 `LLM_SERVED_NAME` ⇒ 种子/迁移里写死的第二个模型名，发出去就是"指向一个本机
# 没跑的模型"（建门实测：033 首批智能体行 `llm_model='gpt-4o-mini'`、`init-user-db.sql`
# 的列默认 `'gpt-3.5-turbo'`，而同文件 `llm_base_url=''` 意味着走本地 8207）。
# 两条腿分工不同：
#   A 落库值——只认 SQL 里真会进库的位置（列 DEFAULT 与 INSERT 绑定值），不扫全文：
#     031 的 13 篇知识文档正文正常提到 14B 这类"可换的大档"，按全文判就是误伤。
#   B 文案口径——扫描面里"当前默认档"语境（TIER_CUE）所在句点到的模型名必须在清单里；
#     同一句里"prod 档 14B"这类**对比档**说法放行（见 _bound_to_default_tier）。
ENV_EXAMPLE = REPO_ROOT / ".env-example"
MODEL_ROLE = {
    "llm_model": "LLM_SERVED_NAME",
    "embedding_model": "EMBEDDING_SERVED_NAME",
    "rerank_model": "RERANK_SERVED_NAME",
}
# "default" 是 **provider 名**而非模型名（dispatcher_register.go 以此注册默认 provider，
# sales_engine.go 的 case "default" 认它），ai_agents.llm_model 字段被复用来存它 ⇒ 放行。
MODEL_LITERAL_OK = {"", "default"}
# 只认"看起来就是在点一个模型名"的字面量，其余（'unknown'、占位符、表达式）不判。
MODEL_NAME = re.compile(
    r"\b(?:Qwen[0-9.]+-[0-9.]+[BM][A-Za-z0-9._-]*|Qwen[0-9.]+-[0-9.]+[BM]"
    r"|bge-[A-Za-z0-9._-]+|[A-Za-z0-9]+-Embedding-[0-9.]+[BM]"
    r"|gpt-[0-9.]+[a-z]*|smollm[0-9A-Za-z._-]*)\b"
)
# 列定义里 `llm_model VARCHAR(100) DEFAULT 'x'` 带括号，排除集不能含 `)`。
DDL_MODEL_DEFAULT = re.compile(
    r"^[ \t]*(llm_model|embedding_model|rerank_model)\b[^\n,]*?DEFAULT[ \t]+'([^']*)'", re.M
)
INSERT_HEAD = re.compile(r"\bINSERT\s+INTO\s+([\w.]+)\s*\(", re.I)
VALUES_HEAD = re.compile(r"\bVALUES\b", re.I)
QUOTED_LITERAL = re.compile(r"'([^']*)'")
TIER_CUE = re.compile(
    r"当前默认|默认档|默认推理|仓库默认|内置模型|开箱即用|随包|dev\s*轻量档|dev\s*档|本地默认|默认使用"
)
# 同一句里的"另一档"标签：cue 与模型名之间出现它 ⇒ 这个名字是在说对比档，不是默认档断言。
CONTRAST_TIER = re.compile(r"prod|生产档|大档|更大|更强|升级|切到|换到|可选|高配|另一档|高档")
# 句子边界：除中文句读外，还认 Go 反引号字符串里写成的字面 `\n`（种子文案一行装一整篇 SOP）。
SENTENCE_SPLIT = re.compile(r"[。；;]|\\n")


def inference_truth() -> dict[str, str]:
    """.env-example 里的三档 served name —— 模型名唯一事实源。"""
    return dict(
        re.findall(
            r"^(LLM_SERVED_NAME|EMBEDDING_SERVED_NAME|RERANK_SERVED_NAME)=(\S+)$",
            read(ENV_EXAMPLE),
            re.M,
        )
    )


def line_of(text: str, pos: int) -> int:
    return text.count("\n", 0, pos) + 1


def sql_model_literals() -> list[tuple[Path, int, str, str]]:
    """规则 9-A：SQL 里真会落库的模型名字面量，返回 (`文件`, 行号, 列名, 值)。

    三种位置：
      - 建表列默认（`llm_model ... DEFAULT 'x'`）；
      - 带 VALUES 名单的 INSERT：按**列名→同位置值**取，绝不扫语句全文——031 的文档正文里
        正常有 14B 这类"可换的大档"说法，全文扫会把真话判红；
      - 列名单点名了模型列却没有 VALUES 名单（`INSERT … SELECT`）或配对数量不等：
        退化成"该语句里每个引号字面量"都判，列名记空串（看不见绑定值不等于没有硬编码；
        048 那类语句因此仍受检，它的字面量是 'unknown'，不是在点模型名，自然放行）。
    """
    found: list[tuple[Path, int, str, str]] = []
    for f in face_files():
        if f.suffix != ".sql":
            continue
        body = read(f)
        for col, val in DDL_MODEL_DEFAULT.findall(body):
            found.append((f, line_of(body, body.index(f"'{val}'", body.index(col))), col, val))
        for m in INSERT_HEAD.finditer(body):
            stmt = body[m.start():]
            stop = stmt.find(";")
            stmt = stmt[: stop + 1] if stop > 0 else stmt
            line = line_of(body, m.start())
            cols_raw = group_after(stmt, "INTO")
            listed = [c.strip().lower() for c in split_top_level(cols_raw)] if cols_raw else []
            if not any(c in MODEL_ROLE for c in listed):
                continue
            vm = VALUES_HEAD.search(stmt)
            vals = split_top_level(group_after(stmt[vm.end():], "") or "") if vm else []
            if vm and len(vals) == len(listed):
                for col in (c for c in listed if c in MODEL_ROLE):
                    found.append((f, line, col, unquote_sql(vals[listed.index(col)])))
                continue
            for val in QUOTED_LITERAL.findall(stmt):
                found.append((f, line, "", val))
    return found


def model_tier_claims(hits: list[tuple[Path, int, str]]) -> tuple[list[str], int]:
    """规则 9-B：文案里"当前默认档"句子点到的模型名，必须在 `.env-example` 清单里。"""
    manifest = read(ENV_EXAMPLE)
    red: list[str] = []
    checked = 0
    for path, no, raw in hits:
        if IGNORE_MARK in raw:
            continue
        line = render(raw)
        for seg in SENTENCE_SPLIT.split(line):
            if not TIER_CUE.search(seg):
                continue
            for m in MODEL_NAME.finditer(seg):
                if not _bound_to_default_tier(seg, m.start(), m.end()):
                    continue  # 同句里的对比档说法（"prod 档 14B"），不是默认档断言
                checked += 1
                if m.group(0) in manifest:
                    continue
                red.append(f"{path.relative_to(REPO_ROOT)}:{no} 在「默认档」语境里点 `{m.group(0)}`，"
                           f"而 .env-example 的三档是 {'/'.join(sorted(inference_truth().values()))}"
                           f"（客户按它配机器会装出一个跑不起默认档的环境）")
    return red, checked


def _bound_to_default_tier(seg: str, start: int, end: int) -> bool:
    """判断 seg 里 `[start,end)` 这个模型名是否**被默认档断言绑住**。

    取离它最近的那个 TIER_CUE，看两者之间有没有"另一档"标签：
    「默认档约 8GB（3B）、prod 档 16GB+（14B）」这种同句对比里，cue 与 14B 之间隔着
    "prod 档" ⇒ 放行；而「默认档 Qwen2.5-1.5B」之间没有对比标签 ⇒ 判。
    按句不切开是因为"默认档为 A、B、C"这类枚举要整体受检。
    """
    cues = list(TIER_CUE.finditer(seg))
    before = [c for c in cues if c.end() <= start]
    after = [c for c in cues if c.start() >= end]
    if before:
        span = seg[before[-1].end():start]
    elif after:
        span = seg[end:after[0].start()]
    else:
        return False
    return not CONTRAST_TIER.search(span)


def face_files() -> list[Path]:
    """扫描面的文件清单；种子 Go 只扫非测试文件（测试里的断言文案不是对外口径）。"""
    files: list[Path] = []
    for base, patterns in GLOB_FACE:
        if not base.is_dir():
            continue
        for pat in patterns:
            files += sorted(base.rglob(pat))
    files = [p for p in files if not p.name.endswith("_test.go")]
    return list(dict.fromkeys(files + [p for p in DOC_FACE if p.exists()]))


def scan_face() -> list[tuple[Path, int, str]]:
    """返回 (文件, 行号, 行内容)。"""
    hits: list[tuple[Path, int, str]] = []
    for f in face_files():
        for no, line in enumerate(read(f).splitlines(), 1):
            hits.append((f, no, line))
    return hits


def main() -> int:
    targets, phony_only = implemented_targets()
    known_names = compose_names()
    pw_keys = password_truth()
    fe_truth = frontend_port()
    rag_red, rag_checked = rag_product_parity()
    env_truth = inference_truth()
    port_truth = {
        "PG": compose_default_port("USER_POSTGRES_HOST_PORT"),
        "PostgreSQL": compose_default_port("USER_POSTGRES_HOST_PORT"),
        "Redis": compose_default_port("REDIS_HOST_PORT"),
    }

    if not targets or not known_names or not pw_keys or not fe_truth or len(env_truth) != len(MODEL_ROLE):
        print(f"❌ 真值面读空：Makefile target {len(targets)} 个、compose 资源名 {len(known_names)} 个、"
              f"口令键 {len(pw_keys)} 个、前端端口 {fe_truth}、推理档位 {len(env_truth)}/{len(MODEL_ROLE)} 个"
              f"（{MAKEFILE} / {COMPOSE} / {SEED_USERS} / {USER_WEB_VITE} / {ENV_EXAMPLE}）"
              f"⇒ 门自身前提不成立，判红而不是放行", file=sys.stderr)
        return 1

    # 规则 8 是"两条产码路径互相"的判定，不走扫描面，红项直接进总账；
    # 它读空时自己也返回红，所以不进上面那道"前提不成立即 return"的闸——
    # 并进去会让 SQL 一改版就顺带屏蔽掉 1~7 条。
    red: list[str] = list(rag_red)
    make_hits = name_hits = port_hits = pw_hits = path_hits = fe_hits = 0
    model_hits = tier_hits = 0

    # 规则 9-A：SQL 里真会落库的模型名，逐列对到 .env-example 的那一档。
    all_served = set(env_truth.values())
    for path, no, col, val in sql_model_literals():
        if not MODEL_NAME.search(val):
            continue  # 'unknown' / 占位符 / 表达式不是在点模型名
        model_hits += 1
        want = env_truth[MODEL_ROLE[col]] if col else ""
        if val in MODEL_LITERAL_OK:
            continue
        if (val == want) if want else (val in all_served):
            continue
        red.append(f"{path.relative_to(REPO_ROOT)}:{no} 把 {col or '某个模型列'} 落成 {val!r}，"
                   f"而 .env-example 的 {MODEL_ROLE.get(col) or '三档 served name'} 是"
                   f" {want or '/'.join(sorted(all_served))}"
                   f" ⇒ 这行数据指向一个本机没跑的模型，检索/回复都会静默打空")

    face = scan_face()
    for path, no, raw in face:
        if IGNORE_MARK in raw:
            continue
        line = render(raw)
        rel = path.relative_to(REPO_ROOT)
        for tok in MAKE_TOKEN.findall(line):
            make_hits += 1
            if tok not in targets:
                why = ("只在 .PHONY 挂名（`make -n` 实测退 0、只印 Nothing to be done ⇒ 客户以为起来了）"
                       if tok in phony_only else "Makefile 里查无此名（`make` 直接 rc=2 报错）")
                red.append(f"{rel}:{no} 教人敲 `make {tok}` —— {why}")
        # 规则 2：先抹掉"读者自己起的名字"，剩下的才是对仓库资源的断言。
        for nm in MTK_NAME.findall(MTK_DECLARED.sub("", line)):
            name_hits += 1
            if nm not in known_names:
                red.append(f"{rel}:{no} 引用了 compose 里没有的资源名 `{nm}`（真值 {sorted(known_names)}）")
        for kind, port in PORT_CLAIM.findall(line):
            if not COPY_FIELD.search(line):
                continue
            port_hits += 1
            truth = port_truth[kind]
            if truth and port != truth:
                red.append(f"{rel}:{no} 把 {kind} 端口写成 {port}，compose 默认是 {truth}")
        for key in set(PW_TAIL.findall(line)) | set(PW_HEAD.findall(line)):
            pw_hits += 1
            if key not in pw_keys:
                red.append(f"{rel}:{no} 把初始超管口令归给 {key}，而 seed 只读 {'/'.join(sorted(pw_keys))}"
                           f"（{key} 是平台端代理口令，只被 config/platform.yaml 读）")
        if COMPOSE_GEN.search(line):
            red.append(f"{rel}:{no} 声称会生成 docker-compose.yml，而它由版本控制提供、install 只 cp .env-example → .env")
        for gopath in set(GO_PATH.findall(line)):
            if CREATE_CUE.search(line):
                continue  # 创建语境：只数"断言既有实现"的路径
            path_hits += 1
            roots = (REPO_ROOT, SEED_DIR.parents[1])  # 仓库根 与 user-server/
            if not any((r / gopath).exists() for r in roots):
                red.append(f"{rel}:{no} 指向源码路径 `{gopath}`，仓库根与 user-server/ 下都不存在")
        if not FE_EXEMPT.search(line):
            for port in FE_CLAIM.findall(line):
                fe_hits += 1
                if fe_truth and port != fe_truth:
                    red.append(f"{rel}:{no} 把 user-web 前端端口写成 {port}，"
                               f"{USER_WEB_VITE.relative_to(REPO_ROOT)} 的 server.port 是 {fe_truth}")

    # 规则 9-B：默认档语境里的模型名口径（放在主循环后，因为它要按句而不是按行判）。
    tier_red, tier_hits = model_tier_claims(face)
    red.extend(tier_red)
    if model_hits == 0 or tier_hits == 0:
        red.append(f"规则 9 命中数 SQL 落库模型名 {model_hits} / 默认档文案 {tier_hits}"
                   f" ⇒ 判据瞎了（TIER_CUE、MODEL_NAME 或扫描面被改坏），而不是代码干净")

    print(f"扫描面：{len(face_files())} 个文件 · make 令牌 {make_hits} 处"
          f" · mtk-* 名 {name_hits} 处 · 端口断言 {port_hits} 处 · 口令断言 {pw_hits} 处"
          f" · 源码路径断言 {path_hits} 处 · 前端端口断言 {fe_hits} 处"
          f" · rag_products 同值字段 {rag_checked}/{len(RAG_PARITY)} 格"
          f" · SQL 落库模型名 {model_hits} 处 · 默认档文案断言 {tier_hits} 处")
    print(f"真值：Makefile 已实现 target {len(targets)} 个（.PHONY 挂名无规则 {len(phony_only)} 个）"
          f" · compose 资源名 {len(known_names)} 个"
          f" · PG {port_truth['PG']} / Redis {port_truth['Redis']} / 前端 {fe_truth}"
          f" · 口令键 {'/'.join(sorted(pw_keys))}"
          f" · 推理三档 {'/'.join(env_truth[k] for k in MODEL_ROLE.values())}")

    for r in red:
        print(f"   RED: {r}")
    print(f"── 对外部署口径一致性：红 {len(red)} 处 ──")
    if red:
        return 1
    print("✅ 对外部署口径一致性门通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
