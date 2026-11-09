# UI 逐页点验 Playbook（可复用）

每轮 UI 审计按本清单执行，避免“只截图不看接口”“只看 SaaS 不看独立部署”“修完不回归”。

## 0. 环境

| 形态 | 启动 | 校验 |
|---|---|---|
| 独立部署 | `npx vite --config vite.audit.standalone.config.js`（8320→8314） | `/api/system/init-status` 返回 `edition=opensource` |
| SaaS | `npx vite --config vite.audit.saas.config.js`（8321→8315） | 同上返回 `edition=saas`，页面有订阅横幅 |
| 后端 | `zsh /tmp/restart-hivemtk-dev.sh`（重建 `/tmp/hivemtk-user-server-ui-audit`，8314 off / 8315 on） | 两端口 `/api/health` ok |

> 前端是 **hash 路由**：必须访问 `http://127.0.0.1:8320/#/path`，不能只写 `/path`。

## 1. 路由清单

```bash
python3 scripts/gen_router_list.py   # 只生成索引，保留既有勾选
grep '^| [0-9]' router-list.md | grep '\[ \]'
```

**注意**：生成器会重排部分备注列（历史备注可能丢失），跑前先 `cp router-list.md /tmp/router-list.bak`；只信 `[x]/[~]` 状态。

## 2. 单页点验协议（每页必做）

1. 打开页面（等 3–4s；`transition mode=out-in` 下新页是在旧页离场后才挂载，2s 会读到上一页 DOM）
2. 采集 `console.error/warn`、`window.onerror`、`unhandledrejection`
3. 采集 Network 非 2xx（fetch + XHR 都要包）
4. 点该页**所有**可点元素：筛选/搜索/重置/分页、主操作/新增（先空提交看校验，再填合法值提交）、行内编辑/详情/删除（确认框文案要读出来）、Tab/抽屉/图表刷新
5. 有写操作就清理测试数据（记下 id，删完回查列表）
6. **双端各做一遍**；SaaS 的租户/商务元素在独立部署不得出现
7. 更新 `router-list.md`：`[x]` 或 `[~]`（设计内拒绝必须写清原因），备注写“验了什么 + 修了什么”

## 3. collector 片段（ego-browser 注入）

```js
const COLLECTOR = `(() => {
  if (window.__AUDIT) return;
  const A = window.__AUDIT = { logs: [], errors: [], net: [] };
  const str = a => { try { return typeof a === 'string' ? a : JSON.stringify(a) } catch { return String(a) } };
  ['error','warn'].forEach(k => { const o = console[k].bind(console); console[k] = (...a) => { A.logs.push({ type:k, msg:a.map(str).join(' ') }); o(...a) } });
  addEventListener('error', e => A.errors.push({ msg:e.message, src:e.filename, line:e.lineno }));
  addEventListener('unhandledrejection', e => A.errors.push({ msg:'unhandledrejection: ' + (e.reason?.message || e.reason) }));
  const rec = (m,u,s,b) => { if (s >= 400) A.net.push({ method:m, url:String(u), status:s, body:String(b||'').slice(0,300) }) };
  const of = window.fetch;
  if (of) window.fetch = async function(...a) {
    const r = await of.apply(this, a);
    try { const u = typeof a[0] === 'string' ? a[0] : a[0]?.url; const c = r.clone();
      c.text().then(t => rec(a[1]?.method || 'GET', u, r.status, t)).catch(()=>rec(a[1]?.method || 'GET', u, r.status, ''));
    } catch {}
    return r;
  };
  const oo = XMLHttpRequest.prototype.open, os = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function(m,u,...r){ this.__m=m; this.__u=u; return oo.call(this,m,u,...r) };
  XMLHttpRequest.prototype.send = function(...a){ this.addEventListener('loadend', ()=>{ try{ rec(this.__m,this.__u,this.status,this.responseText) }catch{} }); return os.apply(this,a) };
})();`
await page.cdp('Page.enable')  // 必须先 enable，否则 addScript 静默不生效
await page.cdp('Page.addScriptToEvaluateOnNewDocument', { source: COLLECTOR })
```

- 每次导航后 `window.__AUDIT = { logs:[],errors:[],net:[] }` 清零，否则会带上一页的旧响应
- 关键约定：`net` 只记 ≥400；页面里“设计内拒绝”要看 response body 确认原因，不要只看状态码

## 4. 判定

| 结果 | 条件 |
|---|---|
| `[x]` | 控制台无 error、无非预期非 2xx、主操作闭环、测试数据已清理 |
| `[~]` | 页面/文案正确，失败来自未装配/未配凭证/模拟探针，且页面已给出可执行说明 |
| `[!]` | 有缺陷 → 修复 → 双端回归 → 才允许改 `[x]` |

## 5. 常见坑

- 弹窗内按钮文案不一定是“确定”：可能是“保存/创建/确认删除”，按文案点，别猜
- 行内删除确认框按钮可能是“确认删除”，不是“确定”
- 剪贴板 API 在无头/无权限环境会失败：失败提示存在即可，不算缺陷
- `window.open` 在自动化环境被拦截；用 `task.tabs()` 或直接验证 API 200 + 报告内容
- 前端构建必须过：`npx vite build --mode production`（HMR 能跑不代表能打包）
- Go 测试要用专用快测库：
  ```bash
  password=$(docker exec mtk-postgres-test printenv POSTGRES_PASSWORD)
  user=$(docker exec mtk-postgres-test printenv POSTGRES_USER)
  export POSTGRES_TEST_DSN= POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 \
         POSTGRES_TEST_USER="$user" POSTGRES_TEST_PASSWORD="$password" \
         POSTGRES_TEST_DBNAME=user_db_test EMBEDDING_ALLOW_FALLBACK=true
  go test ./internal/geo/service ./internal/service ./internal/controller -count=1
  ```
  （首次先跑 `go test ./internal/pkg/testutil -run '^TestTestDBAuthenticationPreflight$'` 建库）
