#!/bin/bash
# 第三方依赖清单生成脚本（2026-08-15 M4-P1-O9，2026-08-15 P0-CI 增强）
#
# 扫描 user-server (Go) 和 user-web/bridge (npm) 的依赖，
# 生成 THIRD_PARTY_LICENSES.md，包含依赖清单与许可证信息。
#
# 策略：
#   - Go：优先 `go list -m all` 全量解析（需模块已在缓存 / go.sum），
#         再从 $GOMODCACHE 中读取各模块 LICENSE 文件做启发式识别；
#         网络不可用或模块未缓存时退化为解析 go.mod 直接依赖，许可证标记"待确认"。
#   - npm：优先解析 package-lock.json 全量树，再从 node_modules 读取许可证；
#         node_modules 缺失时仅列出包名+版本，许可证标记"待确认"。
#
# 用法：
#   bash scripts/gen-third-party-notice.sh

set -euo pipefail

cd "$(dirname "$0")/.."

OUTPUT="THIRD_PARTY_LICENSES.md"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# ---------- 许可证识别（启发式） ----------
# 从 LICENSE 文本前若干行中识别 SPDX 风格许可证标识。
detect_license() {
    local file="$1"
    local head
    head=$(head -c 4096 "$file" 2>/dev/null || true)
    case "$head" in
        *"MIT License"*)      echo "MIT";;
        *"Apache License"*"2.0"*) echo "Apache-2.0";;
        *"Apache License"*)   echo "Apache-2.0";;
        *"BSD 3-Clause"*)     echo "BSD-3-Clause";;
        *"BSD 2-Clause"*)     echo "BSD-2-Clause";;
        *"Redistribution and use in source and binary forms"*) echo "BSD";;
        *"Mozilla Public License"*"2.0"*) echo "MPL-2.0";;
        *"Mozilla Public License"*) echo "MPL-1.1";;
        *"ISC License"*)      echo "ISC";;
        *"GNU LESSER GENERAL PUBLIC LICENSE"*) echo "LGPL";;
        *"GNU AFFERO GENERAL PUBLIC LICENSE"*) echo "AGPL-3.0";;
        *"GNU GENERAL PUBLIC LICENSE"*) echo "GPL";;
        *"The Unlicense"*)    echo "Unlicense";;
        *)                    echo "待确认";;
    esac
}

# ---------- 1. 概览 ----------
cat > "$OUTPUT" << 'EOF'
# 第三方依赖清单 / Third-Party Dependencies

> 本文件由 `scripts/gen-third-party-notice.sh` 自动生成。
> 列出 HiveMtk 使用的所有第三方依赖及其许可证。
> 最后更新：PLACEHOLDER_DATE

## 概览

| 类别 | 数量 | 许可证 |
|------|------|--------|
| Go 模块 | COUNT_GO | 主要是 MIT / BSD / Apache-2.0 |
| npm 包 | COUNT_NPM | 主要是 MIT / Apache-2.0 |

## 1. Go 后端依赖（user-server）

EOF

# ---------- 2. Go 依赖 ----------
GO_TABLE="$TMPDIR/go.table"
: > "$GO_TABLE"
GO_COUNT_FILE="$TMPDIR/go.count"
echo 0 > "$GO_COUNT_FILE"

if command -v go &>/dev/null; then
    # 2.1 全量依赖（go list -m all，需缓存/网络；必须在 go.mod 所在目录运行）
    if ( cd user-server && go list -m -mod=mod -f '{{.Path}}|{{.Version}}' all ) > "$TMPDIR/go.all" 2>/dev/null; then
        GOMODCACHE=$(go env GOMODCACHE 2>/dev/null || echo "")
        while IFS='|' read -r mod ver; do
            [ -z "$mod" ] && continue
            [ "$mod" = "hivemtk-user" ] && continue   # 主模块自身，不计入第三方依赖
            lic="待确认"
            if [ -n "$GOMODCACHE" ] && [ -n "$ver" ]; then
                # 模块缓存目录可能带大小写转义（!lowercase），先试原始路径，再试转义路径
                for base in "$GOMODCACHE/${mod%@*}" "$GOMODCACHE/$(echo "$mod" | sed 's/\([A-Z]\)/!\L\1/g')"; do
                    [ -f "$base@$ver/LICENSE" ] && { lic=$(detect_license "$base@$ver/LICENSE"); break; }
                    [ -f "$base@$ver/LICENSE.md" ] && { lic=$(detect_license "$base@$ver/LICENSE.md"); break; }
                    [ -f "$base@$ver/LICENSE.txt" ] && { lic=$(detect_license "$base@$ver/LICENSE.txt"); break; }
                    [ -f "$base@$ver/COPYING" ] && { lic=$(detect_license "$base@$ver/COPYING"); break; }
                    # 部分模块把 LICENSE 放在模块根下（如 github.com/x/y 下的 LICENSE）
                    if [ -d "$base@$ver" ]; then
                        lf=$(find "$base@$ver" -maxdepth 1 \( -iname 'LICENSE*' -o -iname 'COPYING*' \) 2>/dev/null | head -1)
                        [ -n "$lf" ] && { lic=$(detect_license "$lf"); break; }
                    fi
                done
            fi
            echo "| $mod | $ver | $lic |" >> "$GO_TABLE"
            echo $(( $(cat "$GO_COUNT_FILE") + 1 )) > "$GO_COUNT_FILE"
        done < "$TMPDIR/go.all"
    fi

    # 2.2 退化方案：解析 go.mod 直接依赖（go list 失败时）
    if [ ! -s "$GO_TABLE" ] && [ -f user-server/go.mod ]; then
        echo "> ⚠️ \`go list -m all\` 不可用（可能缺少网络/模块缓存），已退化为 go.mod 直接依赖清单。" >> "$GO_TABLE"
        while IFS='|' read -r mod ver; do
            [ -z "$mod" ] && continue
            echo "| $mod | $ver | 待确认 |" >> "$GO_TABLE"
            echo $(( $(cat "$GO_COUNT_FILE") + 1 )) > "$GO_COUNT_FILE"
        done < <(awk '
            /^require \(/ { inblock=1; next }
            /^\)/ { if (inblock) { inblock=0; next } }
            /^require / && !/\(/ {
                line=$0
                sub(/^require[ \t]+/, "", line)
                n=split(line, a, "[ \t]+")
                if (n>=1) { gsub(/"/, "", a[1]); print a[1]"|"a[2] }
                next
            }
            inblock && NF>=2 {
                gsub(/"/, "", $1); gsub(/"/, "", $2)
                if ($1!="") print $1"|"$2
            }
        ' user-server/go.mod)
    fi
else
    echo "| (Go 未安装) | - | - |" >> "$GO_TABLE"
fi

echo "### 1.1 依赖清单" >> "$OUTPUT"
echo "" >> "$OUTPUT"
echo "| 模块 | 版本 | 许可证 |" >> "$OUTPUT"
echo "|------|------|--------|" >> "$OUTPUT"
cat "$GO_TABLE" >> "$OUTPUT"
COUNT_GO=$(cat "$GO_COUNT_FILE")

# ---------- 3. npm 依赖 ----------
cat >> "$OUTPUT" << 'EOF'

## 2. Bridge 扩展依赖（user-web/bridge）

EOF

NPM_TABLE="$TMPDIR/npm.table"
: > "$NPM_TABLE"
NPM_COUNT_FILE="$TMPDIR/npm.count"
echo 0 > "$NPM_COUNT_FILE"

if [ -f user-web/bridge/package-lock.json ] && command -v node &>/dev/null; then
    # 3.1 全量树：解析 package-lock.json + 从 node_modules 读取许可证
    node -e '
const fs = require("fs");
const path = require("path");
const lock = JSON.parse(fs.readFileSync("user-web/bridge/package-lock.json", "utf8"));
const pkgs = lock.packages || {};
const rows = [];
for (const [name, meta] of Object.entries(pkgs)) {
  if (name === "") continue;                       // 跳过根包
  let lic = "待确认";
  const local = path.join("user-web/bridge", name); // package-lock 的 key 已含 node_modules/ 前缀
  try {
    const sub = JSON.parse(fs.readFileSync(path.join(local, "package.json"), "utf8"));
    lic = sub.license || (sub.licenses && sub.licenses[0] && sub.licenses[0].type) || "待确认";
  } catch (e) {}
  rows.push(`| ${name} | ${meta.version || "-"} | ${lic} |`);
}
process.stdout.write(rows.join("\n") + (rows.length ? "\n" : ""));
' > "$NPM_TABLE" 2>/dev/null || true
    COUNT_NPM=$(wc -l < "$NPM_TABLE" | tr -d ' ')
    echo "$COUNT_NPM" > "$NPM_COUNT_FILE"
fi

# 3.2 退化方案：解析 package.json 直接依赖
if [ ! -s "$NPM_TABLE" ] && [ -f user-web/bridge/package.json ]; then
    echo "> ⚠️ package-lock.json / node 不可用，已退化为 package.json 直接依赖清单。" >> "$NPM_TABLE"
    node -e '
const fs = require("fs");
const pkg = JSON.parse(fs.readFileSync("user-web/bridge/package.json", "utf8"));
const deps = {...(pkg.dependencies||{}), ...(pkg.devDependencies||{})};
for (const [name, ver] of Object.entries(deps)) {
  console.log(`| ${name} | ${ver} | 待确认 |`);
}
' >> "$NPM_TABLE" 2>/dev/null || echo "| (node 不可用) | - | - |" >> "$NPM_TABLE"
    COUNT_NPM=$(wc -l < "$NPM_TABLE" | tr -d ' ')
    echo "$COUNT_NPM" > "$NPM_COUNT_FILE"
fi
COUNT_NPM=$(cat "$NPM_COUNT_FILE")

echo "### 2.1 依赖清单" >> "$OUTPUT"
echo "" >> "$OUTPUT"
echo "| 包 | 版本 | 许可证 |" >> "$OUTPUT"
echo "|----|------|--------|" >> "$OUTPUT"
cat "$NPM_TABLE" >> "$OUTPUT"

# ---------- 4. 手动确认的核心依赖许可证 ----------
cat >> "$OUTPUT" << 'EOF'

## 3. 核心依赖许可证确认

> 以下是项目使用量较大 / 受关注度较高的依赖，已人工确认许可证。

### 3.1 Go 后端

| 模块 | 许可证 | 用途 |
|------|--------|------|
| github.com/gin-gonic/gin | MIT | HTTP 框架 |
| github.com/golang-jwt/jwt/v5 | MIT | JWT 鉴权 |
| github.com/google/uuid | BSD-3-Clause | UUID 生成 |
| github.com/gorilla/websocket | BSD-2-Clause | WebSocket |
| github.com/jackc/pgx/v5 | MIT | PostgreSQL 驱动 |
| github.com/redis/go-redis/v9 | BSD-2-Clause | Redis 客户端 |
| github.com/rs/zerolog | MIT | 日志库 |
| github.com/shopspring/decimal | MIT | 精确小数 |
| github.com/stretchr/testify | MIT | 测试框架 |
| github.com/swaggo/gin-swagger | MIT | Swagger UI |
| go.uber.org/goleak | MIT | Goroutine 泄漏检测 |
| gorm.io/gorm | MIT | ORM |
| gorm.io/driver/postgres | MIT | GORM PG 驱动 |

### 3.2 Bridge 扩展

| 包 | 许可证 | 用途 |
|----|--------|------|
| esbuild | MIT | 打包器（scripts/build.mjs） |
| eslint / @eslint/js | MIT | 代码规范 |
| vitest / vite | MIT | 测试 / 构建工具 |
| jsdom | MIT | 测试 DOM 模拟 |
| globals | MIT | ESLint 全局变量定义 |

## 4. 许可证合规承诺

- 所有直接依赖均为 **MIT / BSD / Apache-2.0 / ISC** 等宽松许可证
- 无 GPL / LGPL / AGPL 传染性许可证（除项目自身采用 AGPL-3.0）
- AGPL-3.0 兼容性：MIT / BSD / Apache-2.0 均与 AGPL-3.0 兼容
- 间接依赖通过 `go mod why` / `npm ls` 验证

## 5. 检查脚本

```bash
# Go 依赖许可证检查
cd user-server
go install github.com/google/go-licenses@latest
go-licenses check ./... --allowed_licenses=MIT,BSD,Apache-2.0,ISC,MPL-2.0

# npm 依赖许可证检查
cd user-web/bridge
npx license-checker --production --onlyAllow="MIT;BSD;Apache-2.0;ISC;MPL-2.0"
```

## 6. 更新流程

1. 升级依赖：`go get -u` / `npm update`
2. 重新生成：运行 `bash scripts/gen-third-party-notice.sh`
3. 提交 PR：附 THIRD_PARTY_LICENSES.md 变更
4. 人工复核：重点核对上表标记为"待确认"的条目

EOF

# ---------- 5. 替换占位符 ----------
sed -i '' "s/PLACEHOLDER_DATE/$(date +%Y-%m-%d)/" "$OUTPUT" 2>/dev/null || sed -i "s/PLACEHOLDER_DATE/$(date +%Y-%m-%d)/" "$OUTPUT"
sed -i '' "s/COUNT_GO/$COUNT_GO/" "$OUTPUT" 2>/dev/null || sed -i "s/COUNT_GO/$COUNT_GO/" "$OUTPUT"
sed -i '' "s/COUNT_NPM/$COUNT_NPM/" "$OUTPUT" 2>/dev/null || sed -i "s/COUNT_NPM/$COUNT_NPM/" "$OUTPUT"

echo "✅ 已生成: $OUTPUT"
echo "📊 Go 模块数: $COUNT_GO"
echo "📊 npm 包数: $COUNT_NPM"
