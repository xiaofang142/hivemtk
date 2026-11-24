# UI 全量逐页点验 · A 轮实战补充（2026-11-25）

本文件是 `ui-audit-playbook.md` 的 A 轮实战补充，记录 280 条页面路由全量逐页重测的环境、账号、脚本模板、判定口径、版本过滤回归要点、go test 失败修复与测试数据清理结果。

## 1. 本轮环境（SaaS 单形态）

- 前端：`vite.audit.saas.config.js`，端口 8321，代理后端 8204。
- 后端：`bin/user-server-audit`，端口 8204。
- 后端重建启动（误用旧二进制会导致 cookie 认证 401）：

```bash
cd user-server && go build -o bin/user-server-audit ./cmd/api
cd ../ && set -a && source .env && set +a && cd user-server
nohup ./bin/user-server-audit > /tmp/user-server-8204.log 2>&1 < /dev/null &!
```

- 前端启动（zsh 用 `&!` 脱离会话）：

```bash
cd user-web && nohup ./node_modules/.bin/vite --config vite.audit.saas.config.js > /tmp/vite-8321-new.log 2>&1 < /dev/null &!
```

- 页面全白 + console 报 `Failed to fetch dynamically imported module` + 504 deps：vite 依赖预构建缓存 hash 失配，kill 监听 8321 全部 PID（逐个 kill）+ `rm -rf node_modules/.vite` + `--force` 重启。

## 2. 测试账号（复用登录态，密码不入库）

- 用户：`uit_admin`，id=26，role=admin，tenant_id=1。
- 登录态：cookie `hmtk_token`（JWT，exp 按当天有效）。
- 会话恢复：若 SPA 跳 `/#/login`，先 `p.goto` login 页 → `p.evaluate` 写 localStorage `user_info={id:26,username:'uit_admin',email:'uit_admin@uitest.local',role:'admin',tenant_id:1,tenant_code:''}` → 再做带 cache-busting 的完整文档导航；hash-only goto 不重建 Pinia store。

## 3. ego-browser 会话与单页点验脚本模板

通过 shell 调用（不是 execute 工具内的 `tools.*`）：

```bash
ego-browser nodejs --session audit <<'EOF'
(async () => {
  const p = t.page("p1");
  const R = "/目标路由";
  await p.events();                                   // 清空事件缓冲
  await p.cdp("egoAutomated.disable");
  await p.cdp("Runtime.enable");
  await p.cdp("Network.enable");
  await p.goto("http://127.0.0.1:8321/?a="+Date.now()+"#"+R, { timeout: 20000 });
  await new Promise(r => setTimeout(r, 2300));
  const before = await p.evaluate(() => ({
    hash: location.hash,
    txt: (document.body.innerText||"").replace(/\s+/g," ").slice(0,700),
    btn: [...document.querySelectorAll("button")].filter(x=>x.getClientRects().length>0).map(x=>x.innerText.trim()).filter(Boolean).slice(0,20),
    rows: document.querySelectorAll("table tbody tr").length,
    dlg: [...document.querySelectorAll(".el-dialog,.el-drawer,[role=dialog]")].filter(x=>x.getClientRects().length>0).length,
  }));
  // —— 此处由人明确指定要点的按钮名，逐元素显式点击 ——
  const click = await p.evaluate((s)=>{
    const b=[...document.querySelectorAll("button")].find(x=>x.innerText.trim()===s && x.getClientRects().length>0);
    if(!b) return "NOT_FOUND"; b.click(); return "CLICKED";
  }, "刷新");
  await new Promise(r => setTimeout(r, 900));
  const ev = await p.events();
  const cerr = ev.filter(e=>e.method==="Runtime.exceptionThrown"||(e.method==="Runtime.consoleAPICalled"&&e.params&&e.params.type==="error"));
  const bad = ev.filter(e=>e.method==="Network.responseReceived"&&e.params.response&&e.params.response.status>=400).map(e=>e.params.response.status+" "+e.params.response.url);
  return { before, click, cerr: cerr.length, bad };
})()
EOF
```

要点：
- 只点查询/刷新/筛选/详情弹窗/取消/只读 tab，不点新建/编辑/删除/发布/发消息/外部 LLM/支付等写操作。
- 新页面引导气泡挡点击时，先点 `×` 关引导。
- 取消/关闭弹窗有动画延迟，当次 dlg 可能仍=2，需等 900ms 再确认 dlg=0。
- 原生 `querySelectorAll` 不支持 `:visible`，可见性用 `getClientRects().length>0`。
- `p.fetch()` 返回 `{ok,status,body,...}`，body 是字符串（`JSON.parse(r.body)`），无 `.json()` 方法。
- 带 `:id` 路由若 id=1 fail-closed，用 `p.fetch('/api/xxx/list')` 取真实 id（body 需 `String(r.body)`）。

## 4. 台账脚本（router-list.md 280 条）

- 生成器 `scripts/gen_router_list.py`（具名分组正则 + `_comp_desc` 修复漏路由 bug）。
- 标记 `scripts/mark.py <行号> --status x --note "..."`（只追加 note，**mark 前必须 python 按 path 动态查行号**，台账被外部开发线持续重新生成，行号会偏移）。
- 紧凑清单：`python3 -c "import re;from pathlib import Path; rows=[(int(m.group(1)),m.group(2)) for ln in Path('router-list.md').read_text().splitlines() if (m:=re.match(r'^\| (\d+) \| \[.\] \| \`(.*?)\`',ln))]; [print(f'{n}:{p}') for n,p in rows]"`
- 统计：`python3 scripts/mark.py --stats` → 280 行 [x]=278 [~]=2 [!]=0 [ ]=0。

## 5. 判定口径（避免误判为缺陷）

- 设计内 fail-closed（页面有明示，非缺陷）：审批底座 503、跟进服务 503、credits 403、扩展包 404、pilot contract 400、订单草稿 DB off、卡片 id 不存在降级文案等。
- 状态：x=通过，~=设计内拒绝，!=缺陷待修。
- 2 条 [~]：`/geo-tools/site-publish`、`/geo-tools/platform-publish`（外部发布操作，设计内拒绝）。

## 6. 版本过滤（本次任务核心）回归要点

- 开源版只支持国外渠道，定制版支持国内渠道；功能模块统一一套代码，在「渠道管理」鉴别版本并过滤。
- `internal/pkg/edition/edition.go`：版本 opensource|custom，`FeatureDomesticChannels=channels.domestic`，`IsDomesticChannel` 覆盖 douyin/xiaohongshu/kuaishou/xianyu/wecom/qq/feishu；normalize 未知值 fail-closed 为 opensource。
- `repository/channel.go` 3 处过滤（GetByID/GetByCode/List）；心跳链路完整（平台下发 version_type + 漂移检测，商户上报 Current()）。
- 前端渠道页无硬编码国内渠道（数据来自 API）；HTTP 请求参数无法覆盖版本类型。
- A 轮实测：`/channels/accounts`、`/channels/manage` 在 opensource 下 `GET /api/channels` 返回 list=[],total=0，符合预期。

## 7. go test 4 个既有失败的根因与修复（第三步，已解决）

正确跑法（必须从 user-server 目录，带 .env 密码 + PLATFORM_ENABLED=true）：

```bash
cd user-server && set -a && source ../.env && set +a && PLATFORM_ENABLED=true \
  go test ./internal/service -run '4个测试名' -count=1 -v
```

| 失败 | 根因 | 修复 |
|---|---|---|
| TestSubscriptionRefreshStaleMirrorWritesPeriod / FailureKeepsMirror | `.env` 第119行 PLATFORM_ENABLED=false 污染；夹具只在未设时才 Setenv(true)，Refresh() 首关跳过回源 | 非代码缺陷，跑测试显式 PLATFORM_ENABLED=true |
| TestMerchantCallbackInbox_ServiceBoundaryFailClosed | 测试传空 page，被 boundary check 提前拦截，走不到 repo 委托点 | merchant_callback_test.go：传有效 page（TenantID/SubscriptionID 来自 f.bind） |
| TestPipeline_QuotaSettlement（2 子测试） | OutcomeUnknown 用 sendDefinitelyRejected 反推，把可分类字符串错误也算 unknown，吞掉 settleQuota default 的 Retryable 分类 | reach_send_pipeline_steps.go：仅 `ce==nil || CategoryUnknown` 才算 unknown，可分类错误交 default 按 Retryable 处置 |

4 个测试定向验证全部 PASS。`PLATFORM_ENABLED=true` 全量回归中这 4 个均不在失败列表（无回归）。全量回归剩余失败与本次改动无关：2 个既有环境问题（TestArchServiceNoNewGormDB 架构守卫、TestProcess_LeadDetected DB closed + LLM key empty）+ 并行开发线 sagex API 认证问题 4 个。

## 8. 测试数据副作用与清理结果（第五步，已执行应用层清理）

| 副作用 | 位置 | 清理动作 | 结果 |
|---|---|---|---|
| bad case bc_1795505678279407000_1 | 行4 embed 测试消息触发 | `POST /api/bad-cases/:id/dismiss` | status=dismissed |
| bad case bc_1795515050871985000_2 | 行263 /wecomAccount/data zero_hit | 同上 | status=dismissed |
| 通知 1267/1268 | 上述 bad case + 新线索采购小王 | `POST /api/auth/notifications/:id/read` | is_read=true |

- 通知 1266/1269 来源不同会话，非本轮明确产物，未动。
- bad case 无 DELETE 端点，只能 dismiss（撤销标记）；通知无删除端点，只能 MarkRead。彻底物理删除需直连 DB（高风险，未做）。

## 9. B 轮新增页面重点二次回归（11-25，22 条）

范围客观界定：`git archive 72b885773`（11-11）的 router 目录与当前目录做 path diff，得出 11-10 J 轮以来新增的 22 条路由（不靠记忆）。每条单独一次 ego 导航观察 → 我明确指定元素点击 → obs 留证 → mark.py 追加「11-25 B轮新增页重点回归」证据。写操作（新建/编辑/删除/发布/重置/外部调用）一律不点，只点查询/刷新/筛选/详情弹窗/新增表单开合/取消/前端 tab。

22 条：/aiInsightDraft/list、/businessAccountAuth/list、/channels/{accounts,manage,identify-codes}、/commerce/operations、/crm/{list,dedupe,profile360/:id?,import-export,contacts,suppliers,service-providers,companies}、/crossAccountSearch/list、/customerImportJob/list、/leads/{rules,notify-routes,evidence,archive,scoped-list,mining-agents}。

重点结果：
- channels 系列：opensource 下填「抖音」查询仍 0 行、`/api/channels` 返回 list=[]，版本过滤持续生效；识别码页「重置」会作废旧码影响扩展端，属高风险写操作未点。
- CRM 系列：客户列表 138 条查询/重置正常；profile360 真 UUID 加载正常；实体补缺页（联系人/供应商/服务商）空态新增按钮 disabled 设计内；公司主数据编辑弹窗完整开合正常。
- leads 系列：三粒度规则、通知路由新增表单完整开合；证据回放/归档空表单前端校验零 API；scoped-list 客户/消息 tab 切换正常；mining-agents 新增表单开合正常。

### B 轮发现并修复的真实缺陷

**/crm/import-export 导出 CSV 误报网关异常**：后端 `GET /api/manage/crm/export?format=csv` 返回 200 + `text/csv`（正常文件流），但 `crmApi.exportRows` 未声明 responseType，通用 axios 拦截器把非 JSON 响应判为「收到非JSON响应，请检查网关路由」，弹错误 toast + console error。修复：`user-web/src/api/crm.js` 对 `format=csv` 声明 `{ responseType: 'blob' }`；`user-web/src/views/crm/ImportExport.vue` 下载逻辑兼容 Blob 与字符串两种形态。回归：导出 CSV/JSON 均 0 console error / 0 非2xx，JSON 显示「导出 138 条」，`go build ./internal/service ./cmd/api` 通过。
