# 模型 API 与向量模型调研（2026-11-28）

> 目的：核对生产 SenseNova 错误模型名，确认可用的文本/向量模型 ID，并找一个可免费注册使用的替代方案。
> 范围：仅调研与留档；**本轮不修改生产 provider/embedding 配置，不写入任何 API Key**。
> 证据分级：**官方**=厂商一手文档；**交叉验证**=社区指南/公开仓库，需注册后实测；**工程推论**=基于现状的配置建议。

## 1. 生产现状证据（本地/线上日志与 DB 实测）

- `https://token.sensenova.cn/v1` 配 `model=deepseek-v4-flash` 持续返回 400：`matched user group has no available model instance`，触发 `low_cost`/`sop_reply` 等场景全 provider 失败，并导致线索二次 LLM 判定失败。
- `platform-dashscope` 与生产 embedding 全局覆盖返回 `Arrearage`（账号欠费）。
- 生产平台 `llm_provider_bindings` 仅 2 条：`platform-sensenova`（`deepseek-v4-flash`）、`platform-dashscope`（`qwen-plus`）；用户端 `default`/`deepseek-flash-b` 也指向同一 SenseNova 端点。
- 结论：当前失败不是智能体上下文代码回归，而是 provider 模型实例未授权/账号欠费；代码侧已把失败逐消息标 `skipped`，不留永久 pending。

## 2. SenseNova（商汤）官方事实

- 官方模型/能力查询入口：[查询模型列表](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Models/GetModelList)、[查询模型详情](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Models/GetModelDetail)、[接口列表](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/chat/GetStarted/APIList)、[帮助中心](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova)。**厂商/模型 ID 以账号实际可读的模型列表为准**，不要在代码里硬编码未授权的模型名。
- OpenAI 兼容端点 `https://token.sensenova.cn/v1`，模型列表端点 `https://token.sensenova.cn/v1/models`；无 Key 时返回 `Authorization Not Found`，注册后必须用它核验当前账号真实可用模型 ID。
- 文本向量化官方入口：[文本向量化 Embeddings](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/chat/Embeddings/Embedding)；官方知识库默认 embedding 模型为 `nova-embedding-stable`；向量输入存在最大 512 tokens 与 RPS 限制（LlamaIndex_SenseNova 集成说明交叉印证）。
- 商汤开源中文 embedding 模型：[piccolo-base-zh](https://huggingface.co/sensenova/piccolo-base-zh)、[piccolo-large-zh](https://huggingface.co/sensenova/piccolo-large-zh)，参考 E5/GTE 路线，可自托管。

## 3. 当前候选模型 ID（交叉验证，注册后必须实测）

| 用途 | 候选 ID | 证据等级 | 备注 |
|---|---|---|---|
| 文本对话 | `sensenova-6.8-flash-lite` | 交叉验证（Token Plan 社区指南） | 免费/低价档候选，先用注册 Key 调 `/v1/models` 确认已授权 |
| 文本对话 | `sensenova-u1-fast` | 交叉验证（Token Plan 社区指南） | 新一代 fast 档候选 |
| 文本对话（旧档） | `sensenova-6.7-flash-lite` | 交叉验证 | 旧模型 ID，仅作兼容参考 |
| 文本向量化 | `nova-embedding-stable` | 官方 | SenseNova 官方知识库默认 embedding |
| 开源向量化 | `piccolo-base-zh` / `piccolo-large-zh` | 官方开源仓库 | 自托管中文通用 embedding |

> 社区资料称 SenseNova Token Plan 免费档 ¥0/月、最多 20 把 Key，注册入口 `platform.sensenova.cn` → API Keys 创建 `sk-` Key。**该信息为交叉验证，不是本次抓到的一手官方页面**；最终以控制台实际套餐与 `/v1/models` 返回为准。

## 4. 免费替代方案：智谱 BigModel（一手官方事实）

- [模型概览](https://docs.bigmodel.cn/cn/guide/start/model-overview.md)：免费文本模型 `GLM-4.7-Flash`（200K 上下文 / 128K 输出）、`GLM-4.5-Flash`（128K / 96K）、`GLM-4-Flash-250414`（128K / 16K）；免费视觉模型 `GLM-4.6V-Flash`、`GLM-4.1V-Thinking-Flash`。
- 向量模型：`Embedding-3`（8K）、`Embedding-2`（8K）；另有 Rerank（4K）。[批量处理文档](https://docs.bigmodel.cn/cn/guide/tools/batch)确认 GLM-4-Flash 免费、Embedding-2/3 支持 batch 且请求数 ≤10000。
- [平台介绍](https://bigmodel.cn/dev/activities/free/glm-4-flash)：OpenAI SDK 兼容，注册即可获取 API Key。
- 工程推论：若需要“免费注册即可用 + 同时覆盖文本与向量”，优先组合 `GLM-4.7-Flash` + `embedding-3`；base_url 以注册后控制台给出的 OpenAI 兼容地址为准。

## 5. 落地建议（不自动执行）

1. **继续用 SenseNova**：先注册/登录并创建 Key，调用 `https://token.sensenova.cn/v1/models` 确认实际可用 ID；用 `sensenova-6.8-flash-lite` 或列表中的可用 ID 替换生产 `deepseek-v4-flash`；embedding 用 `nova-embedding-stable`。
2. **换免费替代**：注册智谱 BigModel，文本用 `GLM-4.7-Flash`，向量用 `embedding-3`；按控制台 OpenAI 兼容 base_url 配置。
3. **自托管向量**：若数据零出域优先，可用 `piccolo-base-zh`/`piccolo-large-zh` 或现有本地 bge-m3；但这需要本地 embedding 服务可用（生产 8208 当前未启动）。
4. **无论选哪个方案**：先在小流量/测试环境验证授权、模型 ID、输出上限、cached tokens 与成本，再更新生产 binding 与路由；禁止把未授权模型名直接推到生产。

## Sources

- [SenseNova 查询模型列表](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Models/GetModelList)
- [SenseNova 查询模型详情](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Models/GetModelDetail)
- [SenseNova 接口列表](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/chat/GetStarted/APIList)
- [SenseNova 帮助中心](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova)
- [SenseNova 文本向量化 Embeddings](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/chat/Embeddings/Embedding)
- [SenseNova Token Plan 模型列表端点](https://token.sensenova.cn/v1/models)
- [sensenova/piccolo-base-zh](https://huggingface.co/sensenova/piccolo-base-zh)
- [sensenova/piccolo-large-zh](https://huggingface.co/sensenova/piccolo-large-zh)
- [智谱 BigModel 模型概览](https://docs.bigmodel.cn/cn/guide/start/model-overview.md)
- [智谱 GLM-4-Flash 免费活动/平台介绍](https://bigmodel.cn/dev/activities/free/glm-4-flash)
- [智谱批量处理文档](https://docs.bigmodel.cn/cn/guide/tools/batch)
