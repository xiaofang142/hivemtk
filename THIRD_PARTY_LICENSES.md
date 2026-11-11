# 第三方依赖清单 / Third-Party Dependencies

> 本文件由 `scripts/gen-third-party-notice.sh` 自动生成。
> 列出 HiveMtk 使用的所有第三方依赖及其许可证。
> 最后更新：2026-08-15

## 概览

| 类别 | 数量 | 许可证 |
|------|------|--------|
| Go 模块 | 117 | 主要是 MIT / BSD / Apache-2.0 |
| npm 包 | 185 | 主要是 MIT / Apache-2.0 |

## 1. Go 后端依赖（user-server）

### 1.1 依赖清单

| 模块 | 版本 | 许可证 |
|------|------|--------|
| github.com/Baozisoftware/qrcode-terminal-go | v0.0.0-20170407111555-c0650d8dff0f | 待确认 |
| github.com/KyleBanks/depth | v1.2.1 | 待确认 |
| github.com/PuerkitoBio/purell | v1.1.1 | 待确认 |
| github.com/PuerkitoBio/urlesc | v0.0.0-20170810143723-de5bf2ad4578 | 待确认 |
| github.com/Rhymen/go-whatsapp | v0.1.1 | 待确认 |
| github.com/Rhymen/go-whatsapp/examples/echo | v0.0.0-20190325075644-cc2581bbf24d | 待确认 |
| github.com/Rhymen/go-whatsapp/examples/restoreSession | v0.0.0-20190325075644-cc2581bbf24d | 待确认 |
| github.com/Rhymen/go-whatsapp/examples/sendImage | v0.0.0-20190325075644-cc2581bbf24d | 待确认 |
| github.com/Rhymen/go-whatsapp/examples/sendTextMessages | v0.0.0-20190325075644-cc2581bbf24d | 待确认 |
| github.com/bsm/ginkgo/v2 | v2.12.0 | 待确认 |
| github.com/bsm/gomega | v1.27.10 | 待确认 |
| github.com/bytedance/sonic | v1.9.1 | Apache-2.0 |
| github.com/cespare/xxhash/v2 | v2.2.0 | MIT |
| github.com/chenzhuoyu/base64x | v0.0.0-20221115062448-fe3a3abad311 | Apache-2.0 |
| github.com/coreos/go-systemd/v22 | v22.5.0 | 待确认 |
| github.com/cpuguy83/go-md2man/v2 | v2.0.0-20190314233015-f79a8a8ca69d | 待确认 |
| github.com/creack/pty | v1.1.9 | 待确认 |
| github.com/davecgh/go-spew | v1.1.1 | ISC |
| github.com/dgryski/go-rendezvous | v0.0.0-20200823014737-9f7001d12a5f | MIT |
| github.com/fvbock/endless | v0.0.0-20170109170031-447134032cb6 | MIT |
| github.com/gabriel-vasile/mimetype | v1.4.2 | MIT |
| github.com/gin-contrib/gzip | v0.0.6 | MIT |
| github.com/gin-contrib/sse | v0.1.0 | MIT |
| github.com/gin-gonic/gin | v1.9.1 | MIT |
| github.com/go-ole/go-ole | v1.2.6 | MIT |
| github.com/go-openapi/jsonpointer | v0.19.5 | Apache-2.0 |
| github.com/go-openapi/jsonreference | v0.19.6 | Apache-2.0 |
| github.com/go-openapi/spec | v0.20.4 | Apache-2.0 |
| github.com/go-openapi/swag | v0.19.15 | Apache-2.0 |
| github.com/go-playground/assert/v2 | v2.2.0 | MIT |
| github.com/go-playground/locales | v0.14.1 | MIT |
| github.com/go-playground/universal-translator | v0.18.1 | MIT |
| github.com/go-playground/validator/v10 | v10.14.0 | MIT |
| github.com/go-telegram-bot-api/telegram-bot-api/v5 | v5.5.1 | MIT |
| github.com/goccy/go-json | v0.10.2 | MIT |
| github.com/godbus/dbus/v5 | v5.0.4 | 待确认 |
| github.com/golang-jwt/jwt/v5 | v5.2.0 | 待确认 |
| github.com/golang/protobuf | v1.5.3 | BSD |
| github.com/google/go-cmp | v0.6.0 | BSD |
| github.com/google/gofuzz | v1.0.0 | 待确认 |
| github.com/google/uuid | v1.6.0 | BSD |
| github.com/gorilla/websocket | v1.4.1 | BSD |
| github.com/jackc/pgpassfile | v1.0.0 | MIT |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| github.com/jackc/pgx/v5 | v5.6.0 | MIT |
| github.com/jackc/puddle/v2 | v2.2.2 | MIT |
| github.com/jinzhu/inflection | v1.0.0 | MIT |
| github.com/jinzhu/now | v1.1.5 | MIT |
| github.com/josharian/intern | v1.0.0 | MIT |
| github.com/json-iterator/go | v1.1.12 | MIT |
| github.com/klauspost/cpuid/v2 | v2.2.4 | MIT |
| github.com/kr/pretty | v0.3.1 | 待确认 |
| github.com/kr/pty | v1.1.1 | 待确认 |
| github.com/kr/text | v0.2.0 | 待确认 |
| github.com/ledongthuc/pdf | v0.0.0-20250511090121-5959a4027728 | BSD |
| github.com/leodido/go-urn | v1.2.4 | MIT |
| github.com/lib/pq | v1.12.3 | MIT |
| github.com/lufia/plan9stats | v0.0.0-20211012122336-39d0f177ccd0 | BSD-3-Clause |
| github.com/mailru/easyjson | v0.7.6 | 待确认 |
| github.com/mattn/go-colorable | v0.1.13 | MIT |
| github.com/mattn/go-isatty | v0.0.19 | MIT |
| github.com/modern-go/concurrent | v0.0.0-20180306012644-bacd9c7ef1dd | Apache-2.0 |
| github.com/modern-go/reflect2 | v1.0.2 | Apache-2.0 |
| github.com/niemeyer/pretty | v0.0.0-20200227124842-a10e7caefd8e | 待确认 |
| github.com/pelletier/go-toml/v2 | v2.2.4 | MIT |
| github.com/pkg/diff | v0.0.0-20210226163009-20ebb0f2a09e | 待确认 |
| github.com/pkg/errors | v0.9.1 | BSD |
| github.com/pmezard/go-difflib | v1.0.0 | BSD |
| github.com/power-devops/perfstat | v0.0.0-20210106213030-5aafc221ea8c | MIT |
| github.com/redis/go-redis/v9 | v9.7.0 | BSD |
| github.com/robfig/cron/v3 | v3.0.1 | 待确认 |
| github.com/rogpeppe/go-internal | v1.10.0 | BSD |
| github.com/rs/xid | v1.6.0 | 待确认 |
| github.com/rs/zerolog | v1.34.0 | MIT |
| github.com/russross/blackfriday/v2 | v2.0.1 | 待确认 |
| github.com/shirou/gopsutil/v3 | v3.24.5 | BSD |
| github.com/shoenig/go-m1cpu | v0.1.6 | MPL-2.0 |
| github.com/shoenig/test | v0.6.4 | MPL-2.0 |
| github.com/shopspring/decimal | v1.4.0 | MIT |
| github.com/shurcooL/sanitized_anchor_name | v1.0.0 | 待确认 |
| github.com/skip2/go-qrcode | v0.0.0-20200617195104-da1b6568686e | 待确认 |
| github.com/stretchr/objx | v0.5.2 | MIT |
| github.com/stretchr/testify | v1.11.1 | MIT |
| github.com/swaggo/files | v1.0.1 | MIT |
| github.com/swaggo/gin-swagger | v1.6.0 | MIT |
| github.com/swaggo/swag | v1.16.2 | MIT |
| github.com/tklauser/go-sysconf | v0.3.12 | BSD-3-Clause |
| github.com/tklauser/numcpus | v0.6.1 | Apache-2.0 |
| github.com/twitchyliquid64/golang-asm | v0.15.1 | BSD |
| github.com/ugorji/go/codec | v1.2.11 | MIT |
| github.com/urfave/cli/v2 | v2.3.0 | 待确认 |
| github.com/yuin/goldmark | v1.4.13 | 待确认 |
| github.com/yusufpapurcu/wmi | v1.2.4 | MIT |
| go.uber.org/goleak | v1.3.0 | MIT |
| golang.org/x/arch | v0.3.0 | BSD |
| golang.org/x/crypto | v0.40.0 | BSD |
| golang.org/x/mod | v0.26.0 | BSD |
| golang.org/x/net | v0.42.0 | BSD |
| golang.org/x/sync | v0.16.0 | BSD |
| golang.org/x/sys | v0.34.0 | BSD |
| golang.org/x/telemetry | v0.0.0-20250710130107-8d8967aff50b | 待确认 |
| golang.org/x/term | v0.33.0 | 待确认 |
| golang.org/x/text | v0.28.0 | BSD |
| golang.org/x/time | v0.15.0 | BSD |
| golang.org/x/tools | v0.35.0 | BSD |
| golang.org/x/xerrors | v0.0.0-20191204190536-9bdfabe68543 | 待确认 |
| google.golang.org/genproto | v0.0.0-20180831171423-11092d34479b | 待确认 |
| google.golang.org/protobuf | v1.33.0 | BSD |
| gopkg.in/alexcesaro/quotedprintable.v3 | v3.0.0-20150716171945-2caba252f4dc | MIT |
| gopkg.in/check.v1 | v1.0.0-20201130134442-10cb98267c6c | BSD |
| gopkg.in/gomail.v2 | v2.0.0-20160411212932-81ebce5c23df | MIT |
| gopkg.in/yaml.v2 | v2.4.0 | Apache-2.0 |
| gopkg.in/yaml.v3 | v3.0.1 | MIT |
| gorm.io/driver/postgres | v1.6.0 | MIT |
| gorm.io/gorm | v1.30.0 | MIT |
| rsc.io/pdf | v0.1.1 | 待确认 |
| sigs.k8s.io/yaml | v1.3.0 | 待确认 |

## 2. Bridge 扩展依赖（user-web/bridge）

### 2.1 依赖清单

| 包 | 版本 | 许可证 |
|----|------|--------|
| node_modules/@asamuzakjp/css-color | 3.2.0 | MIT |
| node_modules/@csstools/color-helpers | 5.1.0 | MIT-0 |
| node_modules/@csstools/css-calc | 2.1.4 | MIT |
| node_modules/@csstools/css-color-parser | 3.1.0 | MIT |
| node_modules/@csstools/css-parser-algorithms | 3.0.5 | MIT |
| node_modules/@csstools/css-tokenizer | 3.0.4 | MIT |
| node_modules/@esbuild/aix-ppc64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/android-arm | 0.21.5 | 待确认 |
| node_modules/@esbuild/android-arm64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/android-x64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/darwin-arm64 | 0.21.5 | MIT |
| node_modules/@esbuild/darwin-x64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/freebsd-arm64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/freebsd-x64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-arm | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-arm64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-ia32 | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-loong64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-mips64el | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-ppc64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-riscv64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-s390x | 0.21.5 | 待确认 |
| node_modules/@esbuild/linux-x64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/netbsd-x64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/openbsd-x64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/sunos-x64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/win32-arm64 | 0.21.5 | 待确认 |
| node_modules/@esbuild/win32-ia32 | 0.21.5 | 待确认 |
| node_modules/@esbuild/win32-x64 | 0.21.5 | 待确认 |
| node_modules/@jest/schemas | 29.6.3 | MIT |
| node_modules/@jridgewell/sourcemap-codec | 1.5.5 | MIT |
| node_modules/@rollup/rollup-android-arm-eabi | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-android-arm64 | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-darwin-arm64 | 4.62.3 | MIT |
| node_modules/@rollup/rollup-darwin-x64 | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-freebsd-arm64 | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-freebsd-x64 | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-arm-gnueabihf | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-arm-musleabihf | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-arm64-gnu | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-arm64-musl | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-loong64-gnu | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-loong64-musl | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-ppc64-gnu | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-ppc64-musl | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-riscv64-gnu | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-riscv64-musl | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-s390x-gnu | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-x64-gnu | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-linux-x64-musl | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-openbsd-x64 | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-openharmony-arm64 | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-win32-arm64-msvc | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-win32-ia32-msvc | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-win32-x64-gnu | 4.62.3 | 待确认 |
| node_modules/@rollup/rollup-win32-x64-msvc | 4.62.3 | 待确认 |
| node_modules/@sinclair/typebox | 0.27.12 | MIT |
| node_modules/@types/estree | 1.0.9 | MIT |
| node_modules/@types/node | 20.19.43 | MIT |
| node_modules/@vitest/expect | 1.6.1 | MIT |
| node_modules/@vitest/runner | 1.6.1 | MIT |
| node_modules/@vitest/snapshot | 1.6.1 | MIT |
| node_modules/@vitest/spy | 1.6.1 | MIT |
| node_modules/@vitest/utils | 1.6.1 | MIT |
| node_modules/acorn | 8.17.0 | MIT |
| node_modules/acorn-walk | 8.3.5 | MIT |
| node_modules/agent-base | 7.1.4 | MIT |
| node_modules/ansi-styles | 5.2.0 | MIT |
| node_modules/assertion-error | 1.1.0 | MIT |
| node_modules/asynckit | 0.4.0 | MIT |
| node_modules/cac | 6.7.14 | MIT |
| node_modules/call-bind-apply-helpers | 1.0.2 | MIT |
| node_modules/chai | 4.5.0 | MIT |
| node_modules/check-error | 1.0.3 | MIT |
| node_modules/combined-stream | 1.0.8 | MIT |
| node_modules/confbox | 0.1.8 | MIT |
| node_modules/cross-spawn | 7.0.6 | MIT |
| node_modules/cssstyle | 4.6.0 | MIT |
| node_modules/cssstyle/node_modules/rrweb-cssom | 0.8.0 | MIT |
| node_modules/data-urls | 5.0.0 | MIT |
| node_modules/debug | 4.4.3 | MIT |
| node_modules/decimal.js | 10.6.0 | MIT |
| node_modules/deep-eql | 4.1.4 | MIT |
| node_modules/delayed-stream | 1.0.0 | MIT |
| node_modules/diff-sequences | 29.6.3 | MIT |
| node_modules/dunder-proto | 1.0.1 | MIT |
| node_modules/entities | 6.0.1 | BSD-2-Clause |
| node_modules/es-define-property | 1.0.1 | MIT |
| node_modules/es-errors | 1.3.0 | MIT |
| node_modules/es-object-atoms | 1.1.2 | MIT |
| node_modules/es-set-tostringtag | 2.1.0 | MIT |
| node_modules/esbuild | 0.21.5 | MIT |
| node_modules/estree-walker | 3.0.3 | MIT |
| node_modules/execa | 8.0.1 | MIT |
| node_modules/form-data | 4.0.6 | MIT |
| node_modules/fsevents | 2.3.3 | MIT |
| node_modules/function-bind | 1.1.2 | MIT |
| node_modules/get-func-name | 2.0.2 | MIT |
| node_modules/get-intrinsic | 1.3.0 | MIT |
| node_modules/get-proto | 1.0.1 | MIT |
| node_modules/get-stream | 8.0.1 | MIT |
| node_modules/gopd | 1.2.0 | MIT |
| node_modules/has-symbols | 1.1.0 | MIT |
| node_modules/has-tostringtag | 1.0.2 | MIT |
| node_modules/hasown | 2.0.4 | MIT |
| node_modules/html-encoding-sniffer | 4.0.0 | MIT |
| node_modules/http-proxy-agent | 7.0.2 | MIT |
| node_modules/https-proxy-agent | 7.0.6 | MIT |
| node_modules/human-signals | 5.0.0 | Apache-2.0 |
| node_modules/iconv-lite | 0.6.3 | MIT |
| node_modules/is-potential-custom-element-name | 1.0.1 | MIT |
| node_modules/is-stream | 3.0.0 | MIT |
| node_modules/isexe | 2.0.0 | ISC |
| node_modules/js-tokens | 9.0.1 | MIT |
| node_modules/jsdom | 24.1.3 | MIT |
| node_modules/local-pkg | 0.5.1 | MIT |
| node_modules/loupe | 2.3.7 | MIT |
| node_modules/lru-cache | 10.4.3 | ISC |
| node_modules/magic-string | 0.30.21 | MIT |
| node_modules/math-intrinsics | 1.1.0 | MIT |
| node_modules/merge-stream | 2.0.0 | MIT |
| node_modules/mime-db | 1.52.0 | MIT |
| node_modules/mime-types | 2.1.35 | MIT |
| node_modules/mimic-fn | 4.0.0 | MIT |
| node_modules/mlly | 1.8.2 | MIT |
| node_modules/mlly/node_modules/pathe | 2.0.3 | MIT |
| node_modules/ms | 2.1.3 | MIT |
| node_modules/nanoid | 3.3.16 | MIT |
| node_modules/npm-run-path | 5.3.0 | MIT |
| node_modules/npm-run-path/node_modules/path-key | 4.0.0 | MIT |
| node_modules/nwsapi | 2.2.24 | MIT |
| node_modules/onetime | 6.0.0 | MIT |
| node_modules/p-limit | 5.0.0 | MIT |
| node_modules/parse5 | 7.3.0 | MIT |
| node_modules/path-key | 3.1.1 | MIT |
| node_modules/pathe | 1.1.2 | MIT |
| node_modules/pathval | 1.1.1 | MIT |
| node_modules/picocolors | 1.1.1 | ISC |
| node_modules/pkg-types | 1.3.1 | MIT |
| node_modules/pkg-types/node_modules/pathe | 2.0.3 | MIT |
| node_modules/postcss | 8.5.23 | MIT |
| node_modules/pretty-format | 29.7.0 | MIT |
| node_modules/psl | 1.15.0 | MIT |
| node_modules/punycode | 2.3.1 | MIT |
| node_modules/querystringify | 2.2.0 | MIT |
| node_modules/react-is | 18.3.1 | MIT |
| node_modules/requires-port | 1.0.0 | MIT |
| node_modules/rollup | 4.62.3 | MIT |
| node_modules/rrweb-cssom | 0.7.1 | MIT |
| node_modules/safer-buffer | 2.1.2 | MIT |
| node_modules/saxes | 6.0.0 | ISC |
| node_modules/shebang-command | 2.0.0 | MIT |
| node_modules/shebang-regex | 3.0.0 | MIT |
| node_modules/siginfo | 2.0.0 | ISC |
| node_modules/signal-exit | 4.1.0 | ISC |
| node_modules/source-map-js | 1.2.1 | BSD-3-Clause |
| node_modules/stackback | 0.0.2 | MIT |
| node_modules/std-env | 3.10.0 | MIT |
| node_modules/strip-final-newline | 3.0.0 | MIT |
| node_modules/strip-literal | 2.1.1 | MIT |
| node_modules/symbol-tree | 3.2.4 | MIT |
| node_modules/tinybench | 2.9.0 | MIT |
| node_modules/tinypool | 0.8.4 | MIT |
| node_modules/tinyspy | 2.2.1 | MIT |
| node_modules/tough-cookie | 4.1.4 | BSD-3-Clause |
| node_modules/tr46 | 5.1.1 | MIT |
| node_modules/type-detect | 4.1.0 | MIT |
| node_modules/ufo | 1.6.4 | MIT |
| node_modules/undici-types | 6.21.0 | MIT |
| node_modules/universalify | 0.2.0 | MIT |
| node_modules/url-parse | 1.5.10 | MIT |
| node_modules/vite | 5.4.21 | MIT |
| node_modules/vite-node | 1.6.1 | MIT |
| node_modules/vitest | 1.6.1 | MIT |
| node_modules/w3c-xmlserializer | 5.0.0 | MIT |
| node_modules/webidl-conversions | 7.0.0 | BSD-2-Clause |
| node_modules/whatwg-encoding | 3.1.1 | MIT |
| node_modules/whatwg-mimetype | 4.0.0 | MIT |
| node_modules/whatwg-url | 14.2.0 | MIT |
| node_modules/which | 2.0.2 | ISC |
| node_modules/why-is-node-running | 2.3.0 | MIT |
| node_modules/ws | 8.21.1 | MIT |
| node_modules/xml-name-validator | 5.0.0 | Apache-2.0 |
| node_modules/xmlchars | 2.2.0 | MIT |
| node_modules/yocto-queue | 1.2.2 | MIT |

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

