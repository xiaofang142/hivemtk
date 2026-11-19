# Bug Test: 发布前质量门阻塞

- **Slug**: release-quality-gate-regressions
- **Date**: 2026-11-20
- **Status**: verified
- **Contract**: [assessment](assessment.md) / [fix](fix.md)

## Original Failures and Regression

原 lint 14 项、架构三项、配置遗漏、十处断链、死词条和隐藏检出漏扫均由实际命令发现。默认 URL 回归先证实微信客服客户端返回空地址，修复后默认地址及显式模拟上游覆盖均通过。工作流控制器的五项失败在独立缓存下复现，更新合法成功夹具后通过，另补五种非法图仍被拒绝且零持久化。隐藏上层夹具修复前真实被误判 STALE，修复后 11 枚门禁控制全通过，内部隐藏/依赖目录仍排除。

## Final Checks on Isolated Sources

| 命令 | 实际结果 |
| --- | --- |
| `make test-go` | 退出 0，1363.7 秒；专用 PostgreSQL 8217，独立 Go 缓存 |
| `go build ./... && go vet ./...` | 退出 0 |
| `make lint` | 退出 0，0 issues |
| `bash scripts/check-architecture.sh` | 退出 0，未屏蔽既有 warning |
| `bash scripts/check-tenant-ctx-writes.test.sh` | 退出 0，11 枚控制通过 |
| 修复路径扫描后 `make audit` | 退出 0，67.6 秒 |
| `npm run lint:check`（user-web） | 退出 0 |
| `npm run test -- --maxWorkers=2`（user-web） | 退出 0，98 文件 / 956 测试通过 |
| `npm run build`（user-web） | 退出 0 |
| `go test -race -count=1 -v ./internal/service ./internal/repository -run Consent` | 退出 0，119 条含子例通过 |

未关闭检查器、未刷新棘轮为绿灯背书；SignatureWindow 仅登记固定安全边界的具体理由，验签窗口和回归不变。源码隔离于其他会话的备份、投稿和配额在途改动，没有替这些变更作验收。

## Snapshot Evidence

4457 个 Go 源码/模块文件的排序 JSON 校验清单 SHA-256：`05abbd5d3514487d25887aa722d89b860e06c1054ee2e8affe15461345b13d00`。720 个前端源码、测试与共享消费文件对应清单 SHA-256：`45c75079516cc2248edcc03cba959866a6aea5cb1165f3d1b029fbc22aa9dc31`。两组在运行前后和交付前均一致；审计脚本及说明在运行期间的变化已单独复验。

完整 Go 日志 SHA-256：`c86a82bfdc748fdc3ad0b72a1a58fa01b1e6c24f48e3c4c454cc188c2ee2ac5b`；最终审计日志 SHA-256：`4a24821e391fde245291dc7dfac3daa8d237e120c4fccf477d8957428cd2a408`；前端测试日志 SHA-256：`aeed353170dc4fff81a7a329736cf32b9333e658f85ec114241a101ca1f9e683`。

## Coverage and Delivery Boundary

本地无覆盖率制品，覆盖率阈值维度为 SKIP，未声称已验；本机无运行 API，默认口令探针为 SKIP，发布执行器会在公网补验。隔离审计只包含当前主仓，未把缺席的同级平台仓扫描算作通过。构建通过不代表已上线；实际部署、资产哈希及回滚登记另见发布状态与拓扑台账。
