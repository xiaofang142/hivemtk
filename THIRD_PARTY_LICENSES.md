# 第三方依赖清单 / Third-Party Dependencies

> 本文件由 `scripts/gen-third-party-notice.sh` 自动生成。
> 列出 HiveMtk 使用的所有第三方依赖及其许可证。
> 最后更新：2026-11-12

## 概览

| 类别 | 数量 | 许可证 |
|------|------|--------|
| Go 模块 | 158 | 主要是 MIT / BSD / Apache-2.0 |
| npm 包 | 211 | 主要是 MIT / Apache-2.0 |

## 1. Go 后端依赖（user-server）

### 1.1 依赖清单

| 模块 | 版本 | 许可证 |
|------|------|--------|
| codeberg.org/go-fonts/liberation | v0.5.0 | BSD |
| codeberg.org/go-latex/latex | v0.1.0 | BSD |
| codeberg.org/go-pdf/fpdf | v0.10.0 | MIT |
| filippo.io/edwards25519 | v1.2.0 | BSD |
| git.sr.ht/~sbinet/gg | v0.6.0 | MIT |
| github.com/KyleBanks/depth | v1.2.1 | MIT |
| github.com/PuerkitoBio/goquery | v1.13.0 | BSD |
| github.com/PuerkitoBio/purell | v1.1.1 | BSD |
| github.com/PuerkitoBio/urlesc | v0.0.0-20170810143723-de5bf2ad4578 | BSD |
| github.com/ajstarks/svgo | v0.0.0-20211024235047-1546f124cd8b | CC-BY-4.0 |
| github.com/andybalholm/cascadia | v1.3.4 | BSD |
| github.com/bsm/ginkgo/v2 | v2.12.0 | MIT |
| github.com/bsm/gomega | v1.27.10 | MIT |
| github.com/bytedance/gopkg | v0.1.3 | Apache-2.0 |
| github.com/bytedance/sonic | v1.15.0 | Apache-2.0 |
| github.com/bytedance/sonic/loader | v0.5.0 | Apache-2.0 |
| github.com/campoy/embedmd | v1.0.0 | Apache-2.0 |
| github.com/cespare/xxhash/v2 | v2.3.0 | MIT |
| github.com/chenzhuoyu/base64x | v0.0.0-20221115062448-fe3a3abad311 | Apache-2.0 |
| github.com/cloudwego/base64x | v0.1.6 | Apache-2.0 |
| github.com/coreos/go-systemd/v22 | v22.7.0 | Apache-2.0 |
| github.com/cpuguy83/go-md2man/v2 | v2.0.0-20190314233015-f79a8a8ca69d | MIT |
| github.com/creack/pty | v1.1.9 | MIT |
| github.com/davecgh/go-spew | v1.1.1 | ISC |
| github.com/dgryski/go-rendezvous | v0.0.0-20200823014737-9f7001d12a5f | MIT |
| github.com/fvbock/endless | v0.0.0-20170109170031-447134032cb6 | MIT |
| github.com/gabriel-vasile/mimetype | v1.4.15 | MIT |
| github.com/gin-contrib/gzip | v0.0.6 | MIT |
| github.com/gin-contrib/sse | v1.1.0 | MIT |
| github.com/gin-gonic/gin | v1.12.0 | MIT |
| github.com/go-logr/logr | v1.4.4 | Apache-2.0 |
| github.com/go-logr/stdr | v1.2.2 | Apache-2.0 |
| github.com/go-ole/go-ole | v1.2.6 | MIT |
| github.com/go-openapi/jsonpointer | v0.19.5 | Apache-2.0 |
| github.com/go-openapi/jsonreference | v0.19.6 | Apache-2.0 |
| github.com/go-openapi/spec | v0.20.4 | Apache-2.0 |
| github.com/go-openapi/swag | v0.19.15 | Apache-2.0 |
| github.com/go-playground/assert/v2 | v2.2.0 | MIT |
| github.com/go-playground/locales | v0.14.1 | MIT |
| github.com/go-playground/universal-translator | v0.18.1 | MIT |
| github.com/go-playground/validator/v10 | v10.30.5 | MIT |
| github.com/go-redis/redis_rate/v10 | v10.0.1 | BSD |
| github.com/go-sql-driver/mysql | v1.8.1 | MPL-2.0 |
| github.com/go-telegram-bot-api/telegram-bot-api/v5 | v5.5.1 | MIT |
| github.com/goccmack/gocc | v1.0.2 | Apache-2.0 |
| github.com/goccy/go-json | v0.10.5 | MIT |
| github.com/goccy/go-yaml | v1.19.2 | MIT |
| github.com/golang-jwt/jwt/v5 | v5.3.1 | MIT |
| github.com/golang-sql/civil | v0.0.0-20220223132316-b832511892a9 | Apache-2.0 |
| github.com/golang-sql/sqlexp | v0.1.0 | BSD |
| github.com/golang/freetype | v0.0.0-20170609003504-e2365dfdc4a0 | FTL OR GPL-2.0+ |
| github.com/golang/protobuf | v1.5.0 | BSD |
| github.com/google/go-cmp | v0.7.0 | BSD |
| github.com/google/gofuzz | v1.0.0 | Apache-2.0 |
| github.com/google/uuid | v1.6.0 | BSD |
| github.com/gorilla/websocket | v1.5.3 | BSD |
| github.com/jackc/pgpassfile | v1.0.0 | MIT |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| github.com/jackc/pgx/v5 | v5.11.0 | MIT |
| github.com/jackc/puddle/v2 | v2.2.2 | MIT |
| github.com/jinzhu/inflection | v1.0.0 | MIT |
| github.com/jinzhu/now | v1.1.5 | MIT |
| github.com/jordanlewis/gcassert | v0.0.0-20250430164644-389ef753e22e | MIT |
| github.com/josharian/intern | v1.0.0 | MIT |
| github.com/json-iterator/go | v1.1.12 | MIT |
| github.com/klauspost/compress | v1.17.6 | Apache-2.0 |
| github.com/klauspost/cpuid/v2 | v2.4.0 | MIT |
| github.com/kr/pretty | v0.3.1 | MIT |
| github.com/kr/pty | v1.1.1 | MIT |
| github.com/kr/text | v0.2.0 | MIT |
| github.com/ledongthuc/pdf | v0.0.0-20250511090121-5959a4027728 | BSD |
| github.com/leodido/go-urn | v1.5.0 | MIT |
| github.com/lib/pq | v1.12.3 | MIT |
| github.com/lufia/plan9stats | v0.0.0-20211012122336-39d0f177ccd0 | BSD-3-Clause |
| github.com/mailru/easyjson | v0.7.6 | MIT |
| github.com/mattn/go-colorable | v0.1.14 | MIT |
| github.com/mattn/go-isatty | v0.0.20 | MIT |
| github.com/mattn/go-sqlite3 | v1.14.22 | MIT |
| github.com/microsoft/go-mssqldb | v1.7.2 | BSD |
| github.com/modern-go/concurrent | v0.0.0-20180306012644-bacd9c7ef1dd | Apache-2.0 |
| github.com/modern-go/reflect2 | v1.0.2 | Apache-2.0 |
| github.com/niemeyer/pretty | v0.0.0-20200227124842-a10e7caefd8e | MIT |
| github.com/pelletier/go-toml/v2 | v2.2.4 | MIT |
| github.com/pkg/errors | v0.9.1 | BSD |
| github.com/pmezard/go-difflib | v1.0.0 | BSD |
| github.com/power-devops/perfstat | v0.0.0-20210106213030-5aafc221ea8c | MIT |
| github.com/quic-go/qpack | v0.6.0 | MIT |
| github.com/quic-go/quic-go | v0.59.1 | MIT |
| github.com/redis/go-redis/v9 | v9.23.0 | BSD |
| github.com/robfig/cron/v3 | v3.0.1 | MIT |
| github.com/rogpeppe/go-internal | v1.14.1 | BSD |
| github.com/rs/xid | v1.6.0 | MIT |
| github.com/rs/zerolog | v1.35.1 | MIT |
| github.com/russross/blackfriday/v2 | v2.0.1 | BSD |
| github.com/shirou/gopsutil/v3 | v3.24.5 | BSD |
| github.com/shoenig/go-m1cpu | v0.1.6 | MPL-2.0 |
| github.com/shoenig/test | v0.6.4 | MPL-2.0 |
| github.com/shurcooL/sanitized_anchor_name | v1.0.0 | MIT |
| github.com/skip2/go-qrcode | v0.0.0-20200617195104-da1b6568686e | MIT |
| github.com/stretchr/objx | v0.5.3 | MIT |
| github.com/stretchr/testify | v1.12.1 | MIT |
| github.com/swaggo/files | v1.0.1 | MIT |
| github.com/swaggo/gin-swagger | v1.6.1 | MIT |
| github.com/swaggo/swag | v1.16.6 | MIT |
| github.com/tklauser/go-sysconf | v0.3.12 | BSD-3-Clause |
| github.com/tklauser/numcpus | v0.6.1 | Apache-2.0 |
| github.com/twitchyliquid64/golang-asm | v0.15.1 | BSD |
| github.com/ugorji/go/codec | v1.3.1 | MIT |
| github.com/urfave/cli/v2 | v2.3.0 | MIT |
| github.com/xdg-go/pbkdf2 | v1.0.0 | Apache-2.0 |
| github.com/xdg-go/scram | v1.2.0 | Apache-2.0 |
| github.com/xdg-go/stringprep | v1.0.4 | Apache-2.0 |
| github.com/youmark/pkcs8 | v0.0.0-20240726163527-a2c0da244d78 | MIT |
| github.com/yuin/goldmark | v1.4.13 | MIT |
| github.com/yusufpapurcu/wmi | v1.2.4 | MIT |
| github.com/zeebo/xxh3 | v1.1.0 | BSD-2-Clause |
| go.mongodb.org/mongo-driver/v2 | v2.5.0 | Apache-2.0 |
| go.opentelemetry.io/auto/sdk | v1.2.1 | Apache-2.0 |
| go.opentelemetry.io/otel | v1.47.0 | Apache-2.0 |
| go.opentelemetry.io/otel/log | v1.47.0 | Apache-2.0 |
| go.opentelemetry.io/otel/metric | v1.47.0 | Apache-2.0 |
| go.opentelemetry.io/otel/sdk | v1.47.0 | Apache-2.0 |
| go.opentelemetry.io/otel/sdk/metric | v1.47.0 | Apache-2.0 |
| go.opentelemetry.io/otel/trace | v1.47.0 | Apache-2.0 |
| go.uber.org/atomic | v1.12.0 | MIT |
| go.uber.org/goleak | v1.3.0 | MIT |
| go.uber.org/mock | v0.6.0 | Apache-2.0 |
| go.yaml.in/yaml/v3 | v3.0.5 | MIT |
| golang.org/x/arch | v0.22.0 | BSD |
| golang.org/x/crypto | v0.58.0 | BSD |
| golang.org/x/image | v0.25.0 | BSD |
| golang.org/x/mod | v0.41.0 | BSD |
| golang.org/x/net | v0.62.0 | BSD |
| golang.org/x/sync | v0.24.0 | BSD |
| golang.org/x/sys | v0.49.0 | BSD |
| golang.org/x/telemetry | v0.0.0-20260924152758-ed294f943157 | BSD |
| golang.org/x/term | v0.47.0 | BSD |
| golang.org/x/text | v0.43.0 | BSD |
| golang.org/x/time | v0.16.0 | BSD |
| golang.org/x/tools | v0.51.0 | BSD |
| golang.org/x/xerrors | v0.0.0-20191204190536-9bdfabe68543 | BSD |
| gonum.org/v1/gonum | v0.17.0 | BSD |
| gonum.org/v1/plot | v0.15.2 | BSD |
| gonum.org/v1/tools | v0.0.0-20200318103217-c168b003ce8c | BSD |
| google.golang.org/protobuf | v1.36.10 | BSD |
| gopkg.in/alexcesaro/quotedprintable.v3 | v3.0.0-20150716171945-2caba252f4dc | MIT |
| gopkg.in/check.v1 | v1.0.0-20201130134442-10cb98267c6c | BSD |
| gopkg.in/gomail.v2 | v2.0.0-20160411212932-81ebce5c23df | MIT |
| gopkg.in/yaml.v2 | v2.4.0 | Apache-2.0 |
| gopkg.in/yaml.v3 | v3.0.1 | MIT |
| gorm.io/datatypes | v1.2.7 | MIT |
| gorm.io/driver/mysql | v1.5.6 | MIT |
| gorm.io/driver/postgres | v1.6.3 | MIT |
| gorm.io/driver/sqlite | v1.6.0 | MIT |
| gorm.io/driver/sqlserver | v1.6.0 | MIT |
| gorm.io/gorm | v1.31.2 | MIT |
| rsc.io/pdf | v0.1.1 | BSD |
| sigs.k8s.io/yaml | v1.3.0 | MIT |

## 2. Bridge 扩展依赖（user-web/bridge）

### 2.1 依赖清单

| 包 | 版本 | 许可证 |
|----|------|--------|
| node_modules/@asamuzakjp/css-color | 7.1.3 | MIT |
| node_modules/@asamuzakjp/dom-selector | 9.2.4 | MIT |
| node_modules/@babel/helper-string-parser | 7.29.7 | MIT |
| node_modules/@babel/helper-validator-identifier | 7.29.7 | MIT |
| node_modules/@babel/parser | 7.29.9 | MIT |
| node_modules/@babel/types | 7.29.8 | MIT |
| node_modules/@bcoe/v8-coverage | 1.0.2 | MIT |
| node_modules/@bramus/specificity | 2.4.2 | MIT |
| node_modules/@cacheable/memory | 2.2.0 | MIT |
| node_modules/@cacheable/utils | 2.5.0 | MIT |
| node_modules/@csstools/color-helpers | 6.1.2 | MIT-0 |
| node_modules/@csstools/css-calc | 3.4.3 | MIT |
| node_modules/@csstools/css-color-parser | 4.2.6 | MIT |
| node_modules/@csstools/css-parser-algorithms | 4.0.2 | MIT |
| node_modules/@csstools/css-syntax-patches-for-csstree | 1.1.15 | MIT-0 |
| node_modules/@csstools/css-tokenizer | 4.0.2 | MIT |
| node_modules/@esbuild/aix-ppc64 | 0.28.2 | MIT |
| node_modules/@esbuild/android-arm | 0.28.2 | MIT |
| node_modules/@esbuild/android-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/android-x64 | 0.28.2 | MIT |
| node_modules/@esbuild/darwin-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/darwin-x64 | 0.28.2 | MIT |
| node_modules/@esbuild/freebsd-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/freebsd-x64 | 0.28.2 | MIT |
| node_modules/@esbuild/linux-arm | 0.28.2 | MIT |
| node_modules/@esbuild/linux-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/linux-ia32 | 0.28.2 | MIT |
| node_modules/@esbuild/linux-loong64 | 0.28.2 | MIT |
| node_modules/@esbuild/linux-mips64el | 0.28.2 | MIT |
| node_modules/@esbuild/linux-ppc64 | 0.28.2 | MIT |
| node_modules/@esbuild/linux-riscv64 | 0.28.2 | MIT |
| node_modules/@esbuild/linux-s390x | 0.28.2 | MIT |
| node_modules/@esbuild/linux-x64 | 0.28.2 | MIT |
| node_modules/@esbuild/netbsd-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/netbsd-x64 | 0.28.2 | MIT |
| node_modules/@esbuild/openbsd-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/openbsd-x64 | 0.28.2 | MIT |
| node_modules/@esbuild/openharmony-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/sunos-x64 | 0.28.2 | MIT |
| node_modules/@esbuild/win32-arm64 | 0.28.2 | MIT |
| node_modules/@esbuild/win32-ia32 | 0.28.2 | MIT |
| node_modules/@esbuild/win32-x64 | 0.28.2 | MIT |
| node_modules/@eslint-community/eslint-utils | 4.10.1 | MIT |
| node_modules/@eslint-community/eslint-utils/node_modules/eslint-visitor-keys | 3.4.3 | Apache-2.0 |
| node_modules/@eslint-community/regexpp | 4.12.2 | MIT |
| node_modules/@eslint/config-array | 0.23.5 | Apache-2.0 |
| node_modules/@eslint/config-helpers | 0.7.0 | Apache-2.0 |
| node_modules/@eslint/core | 1.2.1 | Apache-2.0 |
| node_modules/@eslint/js | 9.39.5 | MIT |
| node_modules/@eslint/object-schema | 3.0.5 | Apache-2.0 |
| node_modules/@eslint/plugin-kit | 0.7.3 | Apache-2.0 |
| node_modules/@exodus/bytes | 1.16.0 | MIT |
| node_modules/@humanfs/core | 0.19.2 | Apache-2.0 |
| node_modules/@humanfs/node | 0.16.8 | Apache-2.0 |
| node_modules/@humanfs/types | 0.15.0 | Apache-2.0 |
| node_modules/@humanwhocodes/module-importer | 1.0.1 | Apache-2.0 |
| node_modules/@humanwhocodes/retry | 0.4.3 | Apache-2.0 |
| node_modules/@jridgewell/resolve-uri | 3.1.2 | MIT |
| node_modules/@jridgewell/sourcemap-codec | 1.6.0 | MIT |
| node_modules/@jridgewell/trace-mapping | 0.3.31 | MIT |
| node_modules/@keyv/bigmap | 1.3.1 | MIT |
| node_modules/@keyv/serialize | 1.1.1 | MIT |
| node_modules/@oxc-project/types | 0.152.0 | MIT |
| node_modules/@rolldown/binding-android-arm-eabi | 1.2.12 | MIT |
| node_modules/@rolldown/binding-android-arm64 | 1.2.12 | MIT |
| node_modules/@rolldown/binding-darwin-arm64 | 1.2.12 | MIT |
| node_modules/@rolldown/binding-darwin-x64 | 1.2.12 | MIT |
| node_modules/@rolldown/binding-freebsd-x64 | 1.2.12 | MIT |
| node_modules/@rolldown/binding-linux-arm-gnueabihf | 1.2.12 | MIT |
| node_modules/@rolldown/binding-linux-arm64-gnu | 1.2.12 | MIT |
| node_modules/@rolldown/binding-linux-arm64-musl | 1.2.12 | MIT |
| node_modules/@rolldown/binding-linux-ppc64-gnu | 1.2.12 | MIT |
| node_modules/@rolldown/binding-linux-s390x-gnu | 1.2.12 | MIT |
| node_modules/@rolldown/binding-linux-x64-gnu | 1.2.12 | MIT |
| node_modules/@rolldown/binding-linux-x64-musl | 1.2.12 | MIT |
| node_modules/@rolldown/binding-openharmony-arm64 | 1.2.12 | MIT |
| node_modules/@rolldown/binding-win32-arm64-msvc | 1.2.12 | MIT |
| node_modules/@rolldown/binding-win32-x64-msvc | 1.2.12 | MIT |
| node_modules/@rolldown/pluginutils | 1.0.1 | MIT |
| node_modules/@types/chai | 5.2.3 | MIT |
| node_modules/@types/deep-eql | 4.0.2 | MIT |
| node_modules/@types/esrecurse | 4.3.1 | MIT |
| node_modules/@types/estree | 1.0.9 | MIT |
| node_modules/@types/json-schema | 7.0.15 | MIT |
| node_modules/@vitest/coverage-v8 | 5.0.3 | MIT |
| node_modules/@vitest/istanbul-lib-coverage | 1.0.2 | MIT |
| node_modules/@vitest/istanbul-lib-report | 1.0.2 | MIT |
| node_modules/@vitest/mocker | 5.0.3 | MIT |
| node_modules/@vitest/spy | 5.0.3 | MIT |
| node_modules/acorn | 8.18.0 | MIT |
| node_modules/acorn-jsx | 5.3.2 | MIT |
| node_modules/ajv | 6.15.0 | MIT |
| node_modules/assertion-error | 2.0.1 | MIT |
| node_modules/ast-v8-to-istanbul | 1.0.6 | MIT |
| node_modules/balanced-match | 4.0.4 | MIT |
| node_modules/bidi-js | 1.1.0 | MIT |
| node_modules/brace-expansion | 5.0.12 | MIT |
| node_modules/cacheable | 2.5.0 | MIT |
| node_modules/chai | 6.2.2 | MIT |
| node_modules/cross-spawn | 7.0.6 | MIT |
| node_modules/css-tree | 3.2.1 | MIT |
| node_modules/data-urls | 8.0.0 | MIT |
| node_modules/debug | 4.4.3 | MIT |
| node_modules/decimal.js | 10.6.0 | MIT |
| node_modules/deep-is | 0.1.4 | MIT |
| node_modules/detect-libc | 2.1.2 | Apache-2.0 |
| node_modules/entities | 8.1.0 | BSD-2-Clause |
| node_modules/es-module-lexer | 2.3.2 | MIT |
| node_modules/esbuild | 0.28.2 | MIT |
| node_modules/escape-string-regexp | 4.0.0 | MIT |
| node_modules/eslint | 10.12.0 | MIT |
| node_modules/eslint-scope | 9.1.2 | BSD-2-Clause |
| node_modules/eslint-visitor-keys | 5.0.1 | Apache-2.0 |
| node_modules/espree | 11.2.0 | BSD-2-Clause |
| node_modules/esquery | 1.7.0 | BSD-3-Clause |
| node_modules/esrecurse | 4.3.0 | BSD-2-Clause |
| node_modules/estraverse | 5.3.0 | BSD-2-Clause |
| node_modules/estree-walker | 3.0.3 | MIT |
| node_modules/esutils | 2.0.3 | BSD-2-Clause |
| node_modules/expect-type | 1.4.0 | Apache-2.0 |
| node_modules/fast-deep-equal | 3.1.3 | MIT |
| node_modules/fast-json-stable-stringify | 2.1.0 | MIT |
| node_modules/fast-levenshtein | 2.0.6 | MIT |
| node_modules/fdir | 6.5.0 | MIT |
| node_modules/file-entry-cache | 11.1.5 | MIT |
| node_modules/find-up | 5.0.0 | MIT |
| node_modules/flat-cache | 6.1.23 | MIT |
| node_modules/flatted | 3.4.4 | ISC |
| node_modules/fsevents | 2.3.3 | MIT |
| node_modules/glob-parent | 6.0.2 | ISC |
| node_modules/globals | 17.13.0 | MIT |
| node_modules/hashery | 1.5.1 | MIT |
| node_modules/hookified | 1.15.1 | MIT |
| node_modules/html-encoding-sniffer | 7.0.0 | MIT |
| node_modules/ignore | 5.3.2 | MIT |
| node_modules/imurmurhash | 0.1.4 | MIT |
| node_modules/is-extglob | 2.1.1 | MIT |
| node_modules/is-glob | 4.0.3 | MIT |
| node_modules/is-potential-custom-element-name | 1.0.1 | MIT |
| node_modules/isexe | 2.0.0 | ISC |
| node_modules/js-tokens | 10.0.0 | MIT |
| node_modules/jsdom | 30.1.2 | MIT |
| node_modules/json-schema-traverse | 0.4.1 | MIT |
| node_modules/json-stable-stringify-without-jsonify | 1.0.1 | MIT |
| node_modules/keyv | 5.6.0 | MIT |
| node_modules/levn | 0.4.1 | MIT |
| node_modules/lightningcss | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-android-arm64 | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-darwin-arm64 | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-darwin-x64 | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-freebsd-x64 | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-linux-arm-gnueabihf | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-linux-arm64-gnu | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-linux-arm64-musl | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-linux-x64-gnu | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-linux-x64-musl | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-win32-arm64-msvc | 1.33.0 | MPL-2.0 |
| node_modules/lightningcss-win32-x64-msvc | 1.33.0 | MPL-2.0 |
| node_modules/locate-path | 6.0.0 | MIT |
| node_modules/lru-cache | 11.5.3 | BlueOak-1.0.0 |
| node_modules/magic-string | 1.4.3 | MIT |
| node_modules/magicast | 0.5.5 | MIT |
| node_modules/mdn-data | 2.27.1 | CC0-1.0 |
| node_modules/minimatch | 10.2.6 | BlueOak-1.0.0 |
| node_modules/ms | 2.1.3 | MIT |
| node_modules/nanoid | 3.3.20 | MIT |
| node_modules/natural-compare | 1.4.0 | MIT |
| node_modules/obug | 2.2.1 | MIT |
| node_modules/optionator | 0.9.4 | MIT |
| node_modules/p-limit | 3.1.0 | MIT |
| node_modules/p-locate | 5.0.0 | MIT |
| node_modules/parse5 | 8.0.1 | MIT |
| node_modules/path-exists | 4.0.0 | MIT |
| node_modules/path-key | 3.1.1 | MIT |
| node_modules/picocolors | 1.1.1 | ISC |
| node_modules/picomatch | 4.0.7 | MIT |
| node_modules/postcss | 8.5.29 | MIT |
| node_modules/prelude-ls | 1.2.1 | MIT |
| node_modules/punycode | 2.3.1 | MIT |
| node_modules/qified | 0.10.1 | MIT |
| node_modules/qified/node_modules/hookified | 2.2.0 | MIT |
| node_modules/require-from-string | 2.0.2 | MIT |
| node_modules/rolldown | 1.2.12 | MIT |
| node_modules/saxes | 6.0.0 | ISC |
| node_modules/shebang-command | 2.0.0 | MIT |
| node_modules/shebang-regex | 3.0.0 | MIT |
| node_modules/source-map-js | 1.2.2 | BSD-3-Clause |
| node_modules/std-env | 4.2.0 | MIT |
| node_modules/tinybench | 6.1.4 | MIT |
| node_modules/tinyexec | 1.3.0 | MIT |
| node_modules/tinyglobby | 0.2.17 | MIT |
| node_modules/tinyrainbow | 3.2.0 | MIT |
| node_modules/tldts | 7.4.16 | MIT |
| node_modules/tldts-core | 7.4.16 | MIT |
| node_modules/tough-cookie | 6.0.2 | BSD-3-Clause |
| node_modules/tr46 | 7.0.0 | MIT |
| node_modules/type-check | 0.4.0 | MIT |
| node_modules/undici | 8.11.2 | MIT |
| node_modules/uri-js | 4.4.1 | BSD-2-Clause |
| node_modules/vite | 8.3.4 | MIT |
| node_modules/vitest | 5.0.3 | MIT |
| node_modules/w3c-xmlserializer | 6.0.0 | MIT |
| node_modules/webidl-conversions | 8.0.1 | BSD-2-Clause |
| node_modules/whatwg-mimetype | 5.0.0 | MIT |
| node_modules/whatwg-url | 17.2.0 | MIT |
| node_modules/which | 2.0.2 | ISC |
| node_modules/why-is-node-running | 3.2.1 | MIT |
| node_modules/word-wrap | 1.2.5 | MIT |
| node_modules/xml-name-validator | 5.0.0 | Apache-2.0 |
| node_modules/xmlchars | 2.2.0 | MIT |
| node_modules/yocto-queue | 0.1.0 | MIT |

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

