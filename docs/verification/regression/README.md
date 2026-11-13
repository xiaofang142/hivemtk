# API 回归用例库（K-4 台账配套）

本目录是 `docs/verification/api-list.md` 台账逐路由验证的**用例沉淀**：每条实测过的 curl
固化为一个具名用例，供后续回归复跑。**用例库只是存储，不是批量跑批工具**——
执行仍须遵守仓库纪律（规则-1 / K-4 约束）：一次只跑一个用例，人工判读响应，
禁止 for 循环枚举、禁止无人值守批量循环。

## 目录结构

```
regression/
├── README.md            ← 本文件
├── 00-env.sh            ← 公共环境（BASE_URL / TOKEN，不落任何密钥明文）
└── <module>.sh          ← 按前端 api 模块组织的用例（文件名 = src/api/<module>.js 去后缀）
```

## 用例命名

`case_<台账行号>_<slug>`，如 `case_404_create_rollout_zero`。
每个用例头部注释写明：对应台账行、验证的缺陷编号（D-xx）、预期响应。

## 用法（逐条）

```bash
cd docs/verification/regression
source 00-env.sh                          # 读 K4_BASE / K4_TOKEN
bash featureFlags.sh case_404_create_rollout_zero   # 单个用例单发
```

- `K4_BASE` 默认 `http://127.0.0.1:8401`（TENANT_MODE=off 私域独立部署）；
  SaaS 形态改 `K4_BASE=http://127.0.0.1:8402`。
- `K4_TOKEN` 从环境变量读；未设时提示先 `POST /api/auth/login` 获取
  （账号 `admin`，口令见本地 `LOCAL-DEV-CREDENTIALS.md`，不入库）。
- 写操作用例自带 setup → 断言（echo 判据）→ cleanup 三段，可重复执行，不残留测试数据。

## 口径

- 通过判据与台账一致：HTTP 200 且 `code=0`，或**已确认是正确**的业务错误响应
  （404 NOT_FOUND_1002 / 401 UNAUTHORIZED_2001 / 400 校验类 / 403 门禁 / 502 上游失败 / 503 未配置）。
- 每次回归跑完，把结果（日期 + 用例名 + 实测响应摘要）回填台账对应行的「响应」列。
