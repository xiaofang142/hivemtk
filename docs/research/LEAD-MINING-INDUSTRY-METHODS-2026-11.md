# 线索发掘行业方法论调研（HiveMtk 完整线索发掘模块）

> 调研日期：2026-11-24。调研员：行业调研子任务。
> 目标：回答用户原始问题——「行业大量关键词、敏感词如何分析线索；上游需要渠道还是下游客户渠道；有人买东西、有人卖东西、有人有资源需要对接、有人有需求需要资源，挣中间商差价或者直接销售产品」。
> 方法：以一手/权威来源为主（厂商官方文档、官方帮助中心、官方产品页、开源项目官方文档/源码）。每条结论后附来源 URL 或仓库路径+行号。**未验证/无法访问的来源在 §7 如实列出，正文不据其下结论。**
> 范围：仅背景调研，**未修改任何产品代码**。

---

## 0. 执行摘要（给决策者）

行业对「从大量对话里找商机」这件事的成熟做法，可以归纳成一条稳定的流水线：

1. **先定义「什么算线索」**：不是关键词命中就是线索，而是「对记录（联系人/公司/商机）的画像、行为与上下文加权打分，用阈值分层」（HubSpot、Salesforce、Apollo、ZoomInfo 的一致做法）。见 §1。
2. **关键词只做「召回/预筛」，语义/LLM 做「判定/解释」**：Gong 官方明确区分「Keyword tracker＝精确短语匹配」与「AI tracker＝检测概念与意图，无论措辞」（[Gong Understanding trackers](https://help.gong.io/docs/understanding-trackers)）。LLM 判定必须带证据引用（OpenAI Cookbook 的 `evidence_refs` + 机械校验）。见 §3。
3. **会话上下文按「会话/参与人」聚合，而不是按「当前发送人」聚合**：OpenAI 官方 cookbook 用 `segment_id | speaker | 时间范围 | 文本` 的行式表示喂给模型，并要求模型引用 `segment_id`；这就是「群聊多成员上下文」和「单聊双向上下文」的一手实现范式。见 §2。
4. **角色分类用「买方委员会 + 供应链中间人」两套术语拼装**：需求方/供货方/资源方/撮合方分别对应 buyer、supplier、channel/resource partner、intermediary/broker；「挣差价 vs 直销」对应「merchant intermediary（吃货权赚差价）vs agent/broker（不取货权赚佣金）」与「wholesaler/reseller vs direct sales」。见 §4。
5. **通知是独立的生产者-路由-送达链路**：Slack/飞书/Telegram 官方文档都提供 webhook/Bot API；飞书明确给出「关键词、IP 白名单、签名」三种安全设置与限流值，可直接映射为「按账号/渠道维护人路由 + 降噪 + 审计」的设计。见 §5。

对用户原始问题的直接回答（先给结论，论证见后文）：

- **大量关键词、敏感词怎么分析线索**：关键词是「召回器」，敏感词是「风险/合规旁路信号」，都不是线索本体。行业做法是「属性+事件加权打分 → 阈值分层 → 证据引用 → 人工反馈校准」，并用时间衰减让旧信号自动降权。
- **上游需要渠道还是下游客户渠道**：两者都要，但作用不同。上游渠道解决「供给/资源从哪里来」（supply-side sourcing， wholesaler/distributor/agent），下游客户渠道解决「需求在哪里被验证」（buyer intent、 buyer committee）。行业用法是「fit 与 intent 一起打分，两者都高才优先」（ZoomInfo 原话）。
- **买/卖/资源/需求/赚差价/直销**：不是一类线索，而是**角色 + 交易模式**两个正交维度。应拆成 `lead_role`（demand/supply/resource/broker/unknown）× `deal_mode`（resale_margin/commission/direct_sale/unknown），而不是塞进一个「线索类型」字段。

---

## 1. B2B 线索挖掘/商机识别的主流产品与方法

### 1.1 HubSpot：评分 = 画像（fit）+ 行为（engagement）+ 时间衰减 + 阈值分层

官方定义与做法（逐条引用）：

- 「To prioritize the contacts, companies, and deals in your CRM, you can build custom lead scores based on record actions or properties.」「Scores assign values to records through score properties, which you can use to evaluate which leads are likely to become customers.」
  来源：<https://knowledge.hubspot.com/scoring/understand-the-lead-scoring-tool>
- 「Scores are calculated based on the criteria you set for event and property rules in score groups.」「Each score must have at least one score group…」「You can set a limit for the minimum and maximum total score…」
  同来源。
- **可减分**：「Points can either be added to or subtracted from the score.」「Scores will be negative if more points are subtracted than added.」
  同来源。这与本项目「敏感词/排除词」直接对应——排除词应当做**减分项**，而不是简单丢弃整条消息。
- **阈值/分层**：「For each score, you can set up score thresholds to categorize records based on their score values.」并生成带颜色标签的阈值属性（fit/engagement 用 High/Medium/Low，combined 用 A1…C3）。
  同来源。
- **时间衰减（防抖/防陈旧的关键一手做法）**：官方说明可「automatically reduce a score based on how long ago a scored event occurred」，示例「50% score decay every three months」；且「Decay is not supported if filtering events by frequency or timeframe」。
  来源：<https://knowledge.hubspot.com/scoring/build-lead-scores>
- **行为信号权重分级**：官方列举「email opens, email clicks, page views, and ad interactions」为一般信号，「demo requests, sales form submissions, and booked meetings」为高意向信号；并按次数（frequency）与上限（Limit to a point cap）计分。
  同来源。
- **AI 生成评分规则**：「Use Breeze Assistant to create or edit contact, company, or deal lead scores」，「event rules are set up based on high-impact events and your account's conversion data」；AI 评估「can take up to one hour」。
  来源：<https://knowledge.hubspot.com/scoring/build-lead-scores-with-ai>

**结论（可落地）**：HiveMtk 的 `min_intent_score` 只是单一阈值，行业成熟做法是「**多分数族（画像分 + 行为分 + 角色分）+ 可减分 + 分层阈值 + 时间衰减**」。建议把敏感词/排除词做成减分规则，把「询价/报价/留联系方式」做成高分事件。

### 1.2 HubSpot Intent Signals：把「会话内容」本身变成信号源

- 「Use intent signals to gain insights into high-value actions and news updates of companies you're tracking」「helping you prioritize accounts, identify engagement opportunities, and personalize outreach.」
- 信号来源包括 **CRM 会话内容**：「Budget mention」「Deal timing」「Churn risk」（来自「conversations in HubSpot (emails, transcripts, notes, etc.)」）；以及访客意图、研究级别变化、Bombora Company Surge、UserGems 换工作、新闻事件等。
- 信号「listed as events on the activity timelines」，「can be used to trigger workflows, create lists/audiences, and contribute to lead scoring」。
- 明确节流/冷却：「CRM updates have a 2-day cooling-off period.」
  以上全部来源：<https://knowledge.hubspot.com/reports/use-intent-signals>

**结论**：这与 HiveMtk「从聊天消息里发现预算/交期/求购信号」高度同构。行业做法是把每条命中写成**带时间与类型的事件**，进活动流/时间线，再参与评分与工作流，而不是只更新一个总和分数。

### 1.3 Salesforce Einstein Lead Scoring / Behavior Scoring

官方帮助中心页面存在但为 JS 渲染，抓取只得到「Salesforce Help / Loading / Sorry to interrupt / CSS Error / Refresh」，**正文未能验证，不据此下结论**。已确认存在的官方页面标题（仅作为导航证据）：

- Einstein Lead Scoring：<https://help.salesforce.com/s/articleView?id=sf.einstein_lead_scoring.htm>
- Understand How Einstein Scores Your Leads：<https://help.salesforce.com/s/articleView?id=sf.einstein_lead_scoring_understand.htm>
- Einstein Behavior Scoring：<https://help.salesforce.com/s/articleView?id=sf.behavior_scoring.htm>
- How Behavior Scoring Works：<https://help.salesforce.com/s/articleView?id=sf.behavior_scoring_how.htm>

可核实的一手信息：Salesforce 官方搜索结果显示其产品线确实区分「Einstein Lead Scoring（静态画像/成交可能性）」与「Einstein Behavior Scoring（行为变化）」两类（URL 标题如上）。**具体算法细节未验证，故本报告不引用其数值或公式。**

### 1.4 Apollo Buying Intent：公司级研究信号 + 明确分段阈值

- 「Buying intent helps you identify companies that are actively researching topics related to your products or services.」「Apollo uses company-level research signals to help you find high-intent companies」「Buying intent is measured at the company level.」
- 规模与来源：「Apollo supports more than 15,000 buying intent topics」「Bombora provides company-level buying intent scores.」「Apollo refreshes this data weekly」「Apollo buying intent is powered by Bombora.」
- **明确分段**：「High intent includes scores from 75–100, and medium intent includes scores from 61–74.」「Scores below 61 are not shown as intent.」「A company's intent history shows a 12-week trend」
  来源：<https://r.jina.ai/https://knowledge.apollo.io/hc/en-us/articles/8047704465933-Buying-Intent-Overview>（Apollo 官方 KB 原文经 Jina Reader 转译可读；直接 URL `https://knowledge.apollo.io/hc/en-us/articles/8047704465933-Buying-Intent-Overview` 返回 403 Cloudflare，见 §7）
- 用法：「Apply intent filters」「Review a company's intent topics and intent history」「Combine intent with filters like company size, industry, location, or technologies」「Save or alert on intent-based searches to monitor newly matching companies」。
  同来源。

### 1.5 ZoomInfo：fit 与 intent 联合打分，且信号快速衰减

- 定义：「Intent data is behavioral data that shows when a company, or a person at that company, is actively researching a topic tied to a business problem, product or solution.」
- 一方 vs 三方：「First-party…comes from channels you own or pay for, including your website, emails, events, product and ads.」；三方「is collected across external websites and content networks, then licensed…」「shows research activity before an account ever visits your site.」
- 信号样例：「Visits to pricing, product and comparison pages, plus repeat visits from the same account.」「Demo requests, contact forms and chat conversations.」「a past buyer or power user joins a new company」（champion move）
- **直接回答「上游渠道还是下游渠道」的疑问**：「Score every account on **fit and intent together**, then work the accounts that are high on both first.」
- **时效性**：「Intent decays quickly,」最佳触达窗口是「the week a spike appears」；可「move surging accounts into paid campaigns」「Flag a churn risk when a customer researching your competitors」「workflows that fire when an account crosses an intent threshold」。
  来源：<https://pipeline.zoominfo.com/sales/what-is-intent-data-and-how-to-use-it>

### 1.6 Apollo 官方对「意图数据如何采集」的说明（对应关键词/语义混合）

- 「An intent signal is any trackable online action that suggests buying interest.」样例：「downloading a competitor's case study」「commenting on a relevant LinkedIn post」「visiting a pricing page」「searching for specific keywords」。
- **加权**：「These signals are often scored, so a demo request would be weighted more heavily than a simple blog view.」
- 采集渠道：「Data gathered from website traffic」「Data based on online advertising engagement」「Data based on content consumption」「Data based on relevant online research across search engines, forums, blogs, social channels, etc.」「Data from software review sites」，并可叠加「new job hirings, press mentions, funding rounds, referral sources」等上下文。
- **衰减警示**：「A signal from today is far more valuable than one from six months ago.」「Like all data, buying intent insights decay over time.」「The data is accurate, but you need to contextualize it and act on it quickly.」
- 合规：「ask providers about data collection practices and privacy policies.」
  来源：<https://apollo.io/magazine/how-is-intent-data-collected>

---

## 2. 会话智能：一对一单聊 vs 群聊的上下文处理

### 2.1 Gong 官方产品定义

- 「AI-powered technology that automatically captures, transcribes, and analyzes business conversations」
- 把「unstructured communication into structured, actionable data」；对象包括「calls, meetings, emails, and more」
- 分析「key topics, objections, competitor mentions, and buying signals」「keywords, sentiment, and engagement signals」，输出「flagging deal risks, coaching opportunities, and next steps」
  来源：<https://gong.io/conversation-intelligence>

**要点**：行业先驱把「会话智能」定义为**全量捕获 + 结构化抽取 + 信号（含 buying signal）标注**，而不是「关键词搜索」。

### 2.2 关键词 tracker vs AI tracker：一手对照（这是 §3 混合策略的权威依据）

来源：<https://help.gong.io/docs/understanding-trackers> 与 <https://help.gong.io/docs/understanding-ai-tracker>

| 维度 | Keyword tracker | AI tracker / Smart tracker |
|---|---|---|
| 匹配方式 | 「detect specific words or phrases when they appear in a conversation」「You define the exact terms」 | 「detect concepts and intent rather than exact words」「regardless of the exact wording used」 |
| 提问方式 | 枚举术语/竞品名/产品名/禁用语 | 「Did the rep discuss pricing?」这样的自然语言问题 |
| 成本 | 「do not consume Gong credits」 | 自定义 tracker 按分析量消耗 credits；Gong 预置 tracker 不消耗 |
| 适用 | 「exact phrase matching is reliable」 | 主题「can be expressed in many different ways」 |
| 证据呈现 | 「Points of interest」面板显示每次触发及「timestamps linked to the exact moment each match occurred」，点击时间戳跳到录音对应时刻（<https://help.gong.io/docs/find-and-review-tracker-results>） | 同上（同面板统一展示结果） |

**结论**：这正是「不能只靠关键词」的行业共识表述——关键词负责**低成本精确召回**，AI tracker 负责**语义与意图泛化**，两者结果统一到「带时间戳的证据点」，供人工复核。

### 2.3 示例驱动的 AI tracker 训练（可用于本项目词库冷启动）

来源：<https://help.gong.io/docs/create-and-manage-example-based-smart-trackers>

- 「AI trackers are AI models that enable you to find when concepts are mentioned in conversations, even when they are said in diverse ways.」「AI trackers take into account how people naturally speak.」
- 输入方式：提供「at least 5 examples of real sentences from real calls that fit this concept」；「When copying from transcripts, copy full sentences and edit as necessary.」「Make sure each example is a single sentence only.」
- 训练循环：展示「sets of sentences from your existing calls」，加粗句为候选，上下文是「The sentences that are not in bold, before and after the bolded sentences, give you context.」；人工标 YES/NO/NOT SURE。
- **量化验收（极有参考价值）**：「at least 500 recorded calls」，理想「at least 1500 calls」；目标概念需「appear in at least 50 calls」；「4 rounds」×「25 sentences」；「When 15 of the 20 sentences in the Performance overview match your concept, it's considered a good level of accuracy.」；并定义「Precision means the percentage of AI tracker detections that are correct.」「Hit rate means the percentage of correct snippets that the AI tracker detects.」
- 官方自谦：「Performance is not an exact measure, but a fair and straightforward approximation.」

**结论**：本项目词库不应只列词，应**每个角色/意图配 5+ 条真实语料正例**，并以 precision/hit rate 做离线验收。

### 2.4 OpenAI 官方 Cookbook：多说话人（≈群聊）上下文的一手实现范式

来源：<https://developers.openai.com/cookbook/examples/audio/speaker_aware_meeting_intelligence/speaker_aware_meeting_intelligence>

- 用 `gpt-4o-transcribe-diarize` + `response_format="diarized_json"`；「Diarization answers 'which voice spoke each segment?'」
- **身份不跨会话记忆**：「it does not create a permanent identity profile」；无参考音时是「generic labels such as `speaker_0` and `speaker_1`」；「Labels do not carry across recordings automatically.」→ 本项目必须**自己维护 sender_id/one_id**，不要指望模型记住人。
- 归一化数据结构：`Segment(segment_id, speaker, start, end, text)`；「Each segment gets a stable `segment_id` such as `seg_005`。」
- **模型输入格式**：`segment_id | speaker | 时间戳范围 | 文本`，即「`segment_id | speaker | timestamp range | text.`」
- **强制证据**：「the model must cite those IDs in `evidence_refs`」；「Every extracted item must include evidence_refs with segment_id values copied from the transcript」；「Use only the transcript as evidence.」
- **不确定性显式化**：「Use null for unknown roles, owners, due dates or triggers, directed-to speakers, or decision owners.」「Use empty arrays instead of unsupported items.」
- **责任归属消歧**：「Put single-speaker commitments in action_items, not decisions.」
- **机械护栏**：guardrail 校验「each cited quote appears in the referenced segment.」
- 输出物含 `transcript_segments.json`、`speaker_labeled_transcript.md`、`meeting_intelligence.json`、`guardrail_report.json`。

**对单聊/群聊的直接映射**：

- 单聊＝两个 speaker 的 segment 序列，按时间排序，保留双向（inbound/outbound）消息；这样能识别「询价→报价→议价→留联系方式」链路。
- 群聊＝N 个 speaker 的 segment 序列，保留 `speaker` + `segment_id` + 时间；模型才能回答「谁在求购、谁在供货、谁在介绍资源」。
- 一律要求输出 `evidence_refs`（对应本项目的 `evidence_message_ids`），并在落库前做机械校验。

### 2.5 Chorus（ZoomInfo）

已确认官方产品页标题为「Chorus.ai Product | Connected Conversation Intelligence」（<https://www.chorus.ai/product/connected-ci>）与「Chorus.ai Product | Deal Visibility / Revenue Intelligence」（<https://www.chorus.ai/product/deal-visibility>）。抓取正文仅返回导航链接，**具体功能描述未验证，不据此下结论**。可验证的官方文档标题显示其提供「Talk Track Analytics」「Indepth Analytics Page」「Recordings」等能力（<https://docs.chorus.ai/hc/en-us/articles/360009686194-Talk-Track-Analytics-Unlock-what-sets-your-top-reps-apart> 等），说明行业惯例是「按话术/主题维度聚合分析多通会话」。

---

## 3. 关键词/敏感词 + 语义/LLM 混合筛选的行业共识

### 3.1 为什么不能只靠关键词

1. **措辞多样性**：Gong 官方原话「detect concepts and intent rather than exact words」「regardless of the exact wording used」（§2.2）。中文场景尤甚：「有货/现货/可发/手里有/工厂直供」都指向 supply，关键词表永远追不完。
2. **加权与语境**：Apollo 指出信号需要加权「a demo request would be weighted more heavily than a simple blog view」并「need to contextualize it」（§1.6）。关键词命中是二值，无法表达权重与语境。
3. **误报与陈旧**：HubSpot 的减分（§1.1）与时间衰减（§1.1）说明「命中」不等于「有效」；ZoomInfo「Intent decays quickly」（§1.5）。
4. **可解释性要求**：OpenAI Cookbook 要求每条抽取都带 `evidence_refs` 且机械校验引用文本真实存在（§2.4）；Gong 的 tracker 结果带时间戳跳跃到原话（§2.2）。关键词只能告诉你「命中了词」，不能告诉你「为什么这条是线索」。

### 3.2 预筛（低成本召回）怎么做

- **双层 tracker 模型**：Gong 明确「Keyword tracker 不消耗 credits」「AI tracker 消耗 credits」，所以先用精确匹配过滤、只对候选跑语义（§2.2）。这就是本项目的「规则预筛 → LLM 判定」。
- **高置信直入 + 低置信入 LLM**：本项目方案已有此设计；行业依据是 HubSpot 的阈值分层与 Apollo 的「低于 61 不显示为 intent」分段（§1.1、§1.4）。
- **频控/防抖**：飞书 webhook 官方限流「单租户单机器人 100 次/分钟，5 次/秒」（§5.3）说明高频事件必须聚合；HubSpot 的「2-day cooling-off period」（§1.2）是官方的冷却窗做法。

### 3.3 去重、幂等、防抖的一手依据

- **幂等键**：Stripe 官方「The API supports idempotency for safely retrying requests without accidentally performing the same operation twice.」并建议「V4 UUIDs, or another random string with enough entropy to avoid collisions」「Avoid using sensitive data (for example, email addresses or personal identifiers) as idempotency keys」。API v1 键「at least 24 hours old」后可清理，且「compares incoming parameters to those of the original request and errors if they're not the same」。
  来源：<https://docs.stripe.com/api/idempotent_requests>
  → **直接指导**：本项目的 `lastJudge[debounceKey]` 应替换为「基于 message_id/hash 的幂等键」，不要用联系人身份做键（Stripe 明文反对）。
- **限流/令牌桶**：AWS API Gateway 官方「throttles requests to your API using the token bucket algorithm」并在超限时返回「429 Too Many Requests」，可「enable usage plans to set throttles on client request submissions based on specified requests rates and quotas」。
  来源：<https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-request-throttling.html>
  → 用于保护 LLM 调用与外部通知发送。
- **准确率量化**：Gong 示例 tracker 给出 precision / hit rate 定义与「15/20 算良好」的经验线（§2.3）。

### 3.4 置信度（confidence）与可解释证据的一手依据

- **置信度即模型分数**：OpenAI Moderation 官方「category_scores: Each score represents the model's confidence」且「between 0 and 1」「higher values denote higher confidence」；官方建议「use `flagged` for a first-pass decision, then inspect categories/scores for logging, routing, audits, or review queues; treat scores as signals, not automatic blocking decisions.」
  来源：<https://platform.openai.com/docs/guides/moderation>（经 <https://r.jina.ai/https://platform.openai.com/docs/guides/moderation> 读取）
  → **直接指导**：置信度用于**路由/审核队列**，不是自动拦截；这与本项目「min_intent_score 入库阈值 + 人工审核」一致。
- **实体置信度可过滤**：AWS Comprehend 官方「Each entity also has a score that indicates the level of confidence…You can filter out the entities with lower scores to reduce the risk of using incorrect detections.」，示例响应含 `Score: 0.97`、`BeginOffset/EndOffset`。
  来源：<https://docs.aws.amazon.com/comprehend/latest/dg/how-entities.html>
  → **直接指导**：证据片段应带 offset/msg_id，便于高亮与复核。
- **LLM 判定必须校准**：LLM-as-a-Judge 官方指南「Compare its decisions with trusted labels, test consistency across repeated judgments, and inspect false positives and false negatives.」「Validate the judge on held-out examples that were not used to refine the prompt.」「measure precision and recall for failure detection, and set acceptance criteria based on the consequences of those errors.」并警告「position bias」「correlated errors and self-preference」。
  来源：<https://arize.com/blog/llm-as-a-judge/>
  → **直接指导**：误报反馈回流 + 留出集评估，不是凭感觉调 prompt。
- **结构化输出保证字段可用**：OpenAI Structured Outputs「constrains completed, non-refusal model output to your supplied JSON Schema」，支持 `strict: true`，但「Applications must still handle refusals, incomplete responses, and API errors」。
  来源：<https://developers.openai.com/api/docs/guides/structured-outputs>
  LangChain 结构化输出「return data in a specific, predictable format」「structured_response」并支持验证失败自动重试（「provides error feedback … and prompts the model to retry」）。
  来源：<https://docs.langchain.com/oss/python/langchain/structured-output>
  → **直接指导**：`LeadJudgement` 应用 strict JSON Schema，字段含 `lead_role/confidence/reason/evidence_message_ids`，并对 schema 违例自动重试。

---

## 4. 角色分类：需求方/供应方/资源方/撮合方、上下游、中间商 vs 直销

### 4.1 行业术语对照（一手定义）

| 本项目角色 | 行业术语 | 权威定义/来源 |
|---|---|---|
| 需求方（下游买家） | buyer / customer；buying committee | HubSpot intent signals 把「Budget mention」「Deal timing」作为会话中可检测信号（§1.2）；ZoomInfo 强调「fit and intent together」（§1.5） |
| 供货方（上游卖家） | supplier / wholesaler / distributor | Wikipedia Wholesaling：「Wholesaling or distributing is the sale of goods or merchandise to retailers」「mainly sells to organizations that will resell or use goods in operations (business-to-business)」「Merchant wholesalers: These firms take ownership of inventory and earn a margin by buying and reselling.」<https://en.wikipedia.org/wiki/Wholesaling> |
| 资源方（渠道/物流/达人等） | channel partner / agent / intermediary | Wikipedia Intermediary：「In trade, an intermediary, middleman, or commercial agent acts as a conduit for goods or services offered by a supplier to a consumer.」「Examples/types named: wholesalers, resellers, brokers」「Electronic intermediaries roles: information aggregating, providing trust, facilitating and matching.」<https://en.wikipedia.org/wiki/Intermediary> |
| 撮合方（赚差价/佣金） | merchant intermediary vs agent/broker | 同上 Wholesaling：「Agents/brokers: These firms typically do not take title to goods.」；Intermediary：「Intermediation refers to a process matching two sides of a market」 |
| 直销 | direct sale / manufacturer direct | Wholesaling 对比「retail: mainly sells to households/consumers」与「manufacturer → wholesaler → retailer → end consumer」链路，<https://en.wikipedia.org/wiki/Wholesaling> |

**上游/下游术语**：Wikipedia Downstream (manufacturing)：「Downstream, in manufacturing, refers to processes which occur later on in a production sequence or production line.」「any process occurring after another is considered to be 'downstream'.」<https://en.wikipedia.org/wiki/Downstream_(manufacturing)>。Value chain 定义：「A value chain is a sequence of activities that an organization performs to design, produce, market, deliver, and support goods or services for customers.」<https://en.wikipedia.org/wiki/Value_chain>。
→ **判定框架**：相对「本租户/我方」定位——向链上游（原材料/工厂/批发/货源）为 supply，向链下游（零售商/终端客户/需求）为 demand；「渠道」既可位于上游（货源渠道）也可位于下游（客户渠道），需用 `side=upstream|downstream` 与 `lead_role` 两个字段分开表达。

### 4.2 直接回答「上游需要渠道还是下游客户渠道」

行业证据表明**两者是不同信号源，不能二选一**：

- 下游客户渠道 → buyer intent / fit+intent 联合评分（ZoomInfo §1.5），HubSpot 的访客意图与 CRM 会话信号（§1.2）。
- 上游渠道 → wholesaler/distributor/agent 是供给侧中间人（Wikipedia Wholesaling/Intermediary §4.1）；Apollo 的 intent topics 也可用于「供应商研究」类主题（§1.4 的 topics 选择）。
- 结论：**应配置两条独立词条/规则族**：`upstream_signals`（货源、工厂、现货、批发、OEM、代理政策）与 `downstream_signals`（求购、询价、采购、MOQ、交期、找供应商），分别产 supply/demand 角色分，再与 fit/intent 合并。这与本项目方案 §5.4 的字段设计一致，且得到行业术语支撑。

### 4.3 可直接用于本项目的字段与信号词建议

> 说明：字段命名沿用本项目已有 `lead_role`（`user-server/internal/model/clue.go:32`：`demand/supply/resource/broker/unknown`）；下表信号词为基于上述行业定义的**工程化建议**，非逐字引用。

**正交维度 1：`lead_role`（角色）**

| 值 | 判定含义 | 中文信号词（建议，需真实语料校准） | 英文对应 |
|---|---|---|---|
| `demand` | 主动想买/找资源 | 求购、采购、询价、多少钱、报价、MOQ、交期、找供应商、有谁做、谁能供、急找 | buyer / in-market account |
| `supply` | 有货/产能/服务可供 | 现货、有货、工厂、厂家直供、批发、OEM/ODM、可发、能供、报价单、代理政策、库存 | supplier / wholesaler |
| `resource` | 有可对接的渠道/能力 | 有客户资源、有渠道、达人、流量、物流、清关、仓库、门店、名单、能对接 | channel partner / agent |
| `broker` | 明确想撮合并获利 | 介绍费、佣金、返点、差价、撮合、对接、合作分成、牵线、中间人 | intermediary / broker |
| `unknown` | 信息不足 | 寒暄、模糊、无关 | — |

**正交维度 2：`deal_mode`（交易模式，建议新增）**

| 值 | 判定含义 | 信号词建议 | 行业依据 |
|---|---|---|---|
| `resale_margin` | 吃货权、赚转售差价 | 买断、拿货、压货、批发价、加价出、吃差价 | merchant intermediary「earn a margin by buying and reselling」（§4.1） |
| `commission` | 不取货权、赚佣金 | 佣金、介绍费、返点、分成、牵线费 | agents/brokers「typically do not take title to goods」（§4.1） |
| `direct_sale` | 直接卖给终端 | 零售、终端、直营、一件代发、面向消费者 | wholesaling 对比 retail（§4.1） |
| `unknown` | 信息不足 | — | — |

**正交维度 3：`side`（供需侧，建议新增）**

- `upstream` / `downstream` / `unknown`，依据 §4.1 的 downstream 定义。

**判定优先级建议**：`deal_mode` 与 `lead_role` 独立判定；一条消息可同时 `lead_role=broker` 且 `deal_mode=commission`。群聊中同一会话可产出多条不同 `lead_role` 的线索（有人买、有人卖），应**按 (conversation, sender, role) 建独立线索**，而不是合并成一条。

---

## 5. 线索通知/路由（按账号/渠道维护人）的行业做法

### 5.1 Slack Incoming Webhooks（官方）

- 「Incoming webhooks are a way to post messages from apps into Slack.」创建后得到「a unique URL to which you send a JSON payload with the message text and some options.」
- URL「specific to a single user and a single channel」；支持「all the usual formatting and layout blocks」。
- 安全：「Keep it secret, keep it safe」「Don't share it online, including via public version control repositories.」
- 限制：「You cannot override the default channel, username, or icon」「Incoming webhooks do not allow you to delete a message after it's been posted.」→ 复杂场景用 `chat.postMessage`。
- 线程回复可用 `ts` 作为 `thread_ts`，但 webhook 响应不返回 `ts`，需从 Events API / `conversations.history` 获取。
  来源：<https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks>

**映射**：本项目每个「账号/渠道维护人」对应一个 webhook URL（或 Bot 的 chat.postMessage），通知内容用 Block Kit 结构化卡片。

### 5.2 Slack Block Kit（官方）

- 「The Block Kit UI framework is built with blocks, block elements and composition objects.」「Blocks are visual components that can be arranged to create app layouts.」
- 消息可「up to 50 blocks」。
  来源：<https://docs.slack.dev/block-kit/>

### 5.3 飞书自定义机器人 webhook（官方，最详尽的可用一手资料）

来源：<https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot>

- 「自定义机器人是一种只能在当前群聊中使用的机器人。」「请妥善保存好此 webhook 地址」。
- 请求：`POST`、`Content-Type: application/json`，示例 `{"msg_type":"text","content":{"text":"request example"}}`；成功 `"code": 0`，格式错误 `"code": 9499`。
- **限流**：「请求体的数据大小不能超过 20 KB。」「单租户单机器人 100 次/分钟，5 次/秒。」「建议发送消息尽量避开诸如 10:00、17:30 等整点及半点时间」。
- **三种安全设置**（可直接映射为通知网关的安全策略）：
  1. 自定义关键词：「只有包含至少一个关键词的消息，可以成功发送。」最多 10 个；「自定义关键词只对 text、title 这类文本参数值生效」。失败 `"code": 19024`。
  2. IP 白名单：「只有在白名单内的 IP 地址，可以成功请求 webhook 发送消息。」最多 10 个；失败 `"code": 19022`。
  3. 签名校验：HmacSHA256，timestamp「距当前时间不超过 1 小时（3600 秒）」；失败 `"code": 19021`。
- **消息类型**：`text`（支持 `<at user_id="ou_xxx">名字</at>` 与 `<at user_id="all">所有人</at>`）、`post` 富文本（text/a/at/img）、`share_chat`、`image`、`interactive` 卡片（`schema: 2.0`，「只支持通过按钮、文字链方式跳转 URL」）。

**映射**：

- 「按账号/渠道维护人路由」可用「一个群一个机器人」或「一个机器人 + @user_id」两种实现；后者更适合一个租户一个机器人。
- **通知降噪**：飞书官方的关键词校验正好可做成「通知必须含『线索』前缀」的二次闸门，避免误发。
- **@ 维护人**：用 `text`/`post` 的 `<at user_id=...>` 标签直接点名账号维护人。
- **卡片 + 跳转**：用 `interactive` 卡片展示角色、意向分、摘要、证据，按钮跳回系统内页（线索详情 / inbox 会话）。

### 5.4 Telegram Bot API

官方 Bot API 首页可验证的一手说明（<https://core.telegram.org/bots/api>）：

- 「All methods in the Bot API are case-insensitive.」「All queries must be made using UTF-8.」
- 「All queries to the Telegram Bot API must be served over HTTPS and need to be presented in this form: `https://api.telegram.org/bot<token>/METHOD_NAME`.」
- 「The response contains a JSON object, which always has a Boolean field 'ok' and may have an optional String field 'description'…」

**未验证**：`sendMessage` 的具体参数表（chat_id、text、parse_mode、reply_markup 等）因页面过长被抓取截断，本报告不引用其参数细节，实施时以官方页面为准。

### 5.5 通知不阻塞主链路

本项目方案 §3 与已落地代码均要求通知失败不回滚线索：`user-server/internal/service/lead_mining_notify_external.go:129` 注释明确「It must never block/panic…according to lead_mining_notify_routes」。这与 Stripe 幂等（§3.3）和飞书限流（§5.3）共同支撑「异步、可重试、有送达记录」的设计。

---

## 6. 对 HiveMtk 的 5 条具体建议

> 以下建议基于上述行业证据 + 本项目现状代码核对（路径均为产品仓内相对路径，根为 `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user`）。**不改产品代码，仅给实施建议。**

### 建议 1：把「关键词预筛 + LLM 判定」升级为「可减分的多分数族 + 阈值分层 + 时间衰减」——对齐 HubSpot/Salesforce/Apollo 共识

- 现状：`user-server/internal/service/lead_mining.go:321-353` 用 `leadMiningDebounceKey` + 60s 窗口；`min_intent_score` 单阈值；`lead_judge_context.go:123-126` 输出 `confidence/reason`。
- 落地：新增 `fit_score`（画像/行业匹配）、`intent_score`（求购/询价/报价等行为事件加权）、`risk_penalty`（敏感词/排除词为**减分**而非丢弃，依据 §1.1「Points can either be added to or subtracted」）；阈值分层为 high/medium/low 而非单阈值；对命中事件加「50% 每 N 月衰减」类机制（§1.1）。敏感词命中写 `lead_mining_hits.type=sensitive` 作为旁路证据（表见 `migrations/094_lead_mining.sql:25-36`），不直接否决线索。

### 建议 2：上下文聚合器按「会话 + 参与人」组织，输出统一 `segment_id | sender | 时间 | 文本`，并强制 `evidence_refs` 机械校验

- 现状：`migrations/094_lead_mining.sql:61-76` 已有 `conversation_contexts(member_roles, messages, summary)`；但项目方案 §4.2 指出旧 `fetchHistoryDB` 只取当前 sender inbound。
- 落地：单聊取同 conversation 双向 N 条；群聊取同 group 多 sender 消息，保留 sender_id/name、时间、方向；喂给 LLM 前转成 OpenAI Cookbook 的行式格式（§2.4），要求每条结论带 `evidence_message_ids`（已有列 `migrations/094_lead_mining.sql:31,44`），落库前校验引用文本确实出现在上下文中（§2.4 的 guardrail）。**不要依赖模型记忆身份**：OpenAI 明确「does not create a permanent identity profile」「Labels do not carry across recordings automatically」（§2.4），身份必须由本项目 OneID/sender_id 维护。

### 建议 3：角色从单字段升为「lead_role × deal_mode × side」三正交维度，并用真实语料正例校准

- 现状：`user-server/internal/model/clue.go:32` 已有 `lead_role`（demand/supply/resource/broker/unknown）；`lead_judge_context.go:123` 已提示模型枚举。
- 落地：新增 `deal_mode`（resale_margin/commission/direct_sale/unknown）与 `side`（upstream/downstream/unknown），判定框架与信号词见 §4.3（行业术语依据 §4.1）。词库每个角色/意图至少配 5 条真实语句正例（Gong §2.3 的一手做法），离线以 precision / hit rate 验收，目标可参照 Gong「15/20 命中算良好」（§2.3）。群聊同一会话按 `(conversation, sender, role)` 建多条独立线索，分别给证据。

### 建议 4：用幂等键替代纯时间窗防抖，并把置信度定位为「路由/审核信号」而非自动判定

- 现状：`lead_mining.go:294,321` 使用内存 `lastJudge` + 60s debounce；`lead_mining_judgements` 已有 `evidence_complete/degraded`（`migrations/094_lead_mining.sql:41-48`）。
- 落地：以 `message_id` / 内容 hash 生成 UUID v4 幂等键落 `lead_mining_judgements`，避免重复判定（Stripe §3.3 明确「不要用邮箱/个人身份做幂等键」）；内存 debounce 保留为第一层频控，但以持久化幂等为准。置信度按 OpenAI Moderation 建议用于「logging, routing, audits, or review queues」，低置信进人工队列而非直接入库/丢弃（§3.4）；高置信且证据完整的线索才自动通知。用 LLM-as-a-Judge 的留出集 + precision/recall 做 промпт 回归（§3.4）。

### 建议 5：通知网关按「账号维护人 → 渠道维护人 → 租户默认」三级路由，结构化卡片 + @ 人 + 送达记录，并做频控与脱敏

- 现状：`migrations/094_lead_mining.sql:52-59` 已有 `lead_mining_notify_routes(scope, priority, notify_user_ids, notify_types, delivery_records)`；`lead_mining_notify_external.go:129` 已有非阻塞要求。
- 落地：
  - 路由：按 `scope=account > channel > tenant` + `priority` 解析（与现有字段一致），每个维护人可绑定 Telegram chat 或飞书 `ou_xxx`。
  - 卡片：飞书用 `interactive` + `text/post` 的 `<at user_id>`（§5.3），Slack 用 Block Kit（≤50 blocks，§5.2），Telegram 用 Bot API（§5.4）；卡片内容含角色、意向分、渠道/账号、群/单聊、摘要、证据、跳转链接。
  - 降噪：合并同一 conversation 在窗口内的多次命中；遵守飞书限流「100 次/分钟，5 次/秒」「20 KB」（§5.3），用令牌桶/429 退避（AWS §3.3）。
  - 安全：飞书机器人 webhook 配签名校验或关键词（§5.3）；Slack webhook URL 当密钥保管（§5.1）；外发默认摘要、不带完整敏感聊天记录（项目方案 §9），详情跳回系统内页。
  - 送达：写 `delivery_records`，失败重试走幂等（§3.3），失败不影响线索落库。

---

## 7. 无法访问 / 未验证的来源（如实标注）

| 来源 | 状态 | 处置 |
|---|---|---|
| Salesforce Help `einstein_lead_scoring*.htm` 等 | HTTP 200 但正文为 JS 渲染：`Loading / Sorry to interrupt / CSS Error / Refresh`，未取到正文 | 仅列出 URL 作存在性证据，**未引用其算法细节** |
| Apollo Knowledge Base `knowledge.apollo.io/...8047704465933` | 直接 WebFetch 返回 403 Cloudflare | 经 Jina Reader 镜像 `<https://r.jina.ai/https://knowledge.apollo.io/hc/en-us/articles/8047704465933-Buying-Intent-Overview>` 取得全文并引用；已在 §1.4 标注 |
| Telegram Bot API `sendMessage` 参数表 | 页面过长，`WebFetch` 抓取在 sendMessage 前被截断 | 仅引用首页通用说明（§5.4），**未引用具体参数** |
| Chorus `connected-ci` / Salesforce news「what is a buying committee」/ HubSpot blog「buying committee」/ Gartner 文档页 | 403、404 或仅返回导航/首页 | **未引用**，角色术语改用 Wikipedia Intermediary / Wholesaling / Downstream 等可读页面 |
| Investopedia middleman | 403 | 未引用 |
| GitHub 开源会话智能项目（`k-sahi/conversation-intelligence` 等） | 搜索到仓库存在，但**未逐文件核对源码行号** | 本报告 §2.4 改用 OpenAI 官方 Cookbook 源码替代；不声称已验证这些仓库 |
| Stripe / AWS / Slack / 飞书 / OpenAI 文档 | 已成功读取并引用原文 | 见各节 URL |
| `docs.aws.amazon.com/moderate/...text-moderation-concepts.html` | 404 | 未引用；改用 OpenAI Moderation 文档 |

---

## 8. 来源索引（按章节）

1. HubSpot 评分概览：<https://knowledge.hubspot.com/scoring/understand-the-lead-scoring-tool>
2. HubSpot 构建评分（含衰减）：<https://knowledge.hubspot.com/scoring/build-lead-scores>
3. HubSpot AI 评分：<https://knowledge.hubspot.com/scoring/build-lead-scores-with-ai>
4. HubSpot Intent Signals：<https://knowledge.hubspot.com/reports/use-intent-signals>
5. Salesforce Einstein Lead Scoring（存在性，正文未验证）：<https://help.salesforce.com/s/articleView?id=sf.einstein_lead_scoring.htm>
6. Apollo Buying Intent（Jina 镜像可读）：<https://r.jina.ai/https://knowledge.apollo.io/hc/en-us/articles/8047704465933-Buying-Intent-Overview>
7. Apollo 意图数据采集：<https://apollo.io/magazine/how-is-intent-data-collected>
8. ZoomInfo Intent Data：<https://pipeline.zoominfo.com/sales/what-is-intent-data-and-how-to-use-it>
9. Gong Conversation Intelligence：<https://gong.io/conversation-intelligence>
10. Gong Trackers（关键词 vs AI）：<https://help.gong.io/docs/understanding-trackers>
11. Gong AI Tracker：<https://help.gong.io/docs/understanding-ai-tracker>
12. Gong 结果复核（时间戳证据）：<https://help.gong.io/docs/find-and-review-tracker-results>
13. Gong 示例驱动 tracker 训练：<https://help.gong.io/docs/create-and-manage-example-based-smart-trackers>
14. OpenAI Cookbook 多说话人会议智能：<https://developers.openai.com/cookbook/examples/audio/speaker_aware_meeting_intelligence/speaker_aware_meeting_intelligence>
15. OpenAI Moderation：<https://platform.openai.com/docs/guides/moderation>
16. OpenAI Structured Outputs：<https://developers.openai.com/api/docs/guides/structured-outputs>
17. LangChain Structured Output：<https://docs.langchain.com/oss/python/langchain/structured-output>
18. Arize LLM-as-a-Judge：<https://arize.com/blog/llm-as-a-judge/>
19. AWS Comprehend Entities：<https://docs.aws.amazon.com/comprehend/latest/dg/how-entities.html>
20. Stripe Idempotent requests：<https://docs.stripe.com/api/idempotent_requests>
21. AWS API Gateway throttling：<https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-request-throttling.html>
22. Wikipedia Intermediary：<https://en.wikipedia.org/wiki/Intermediary>
23. Wikipedia Wholesaling：<https://en.wikipedia.org/wiki/Wholesaling>
24. Wikipedia Downstream：<https://en.wikipedia.org/wiki/Downstream_(manufacturing)>
25. Wikipedia Value chain：<https://en.wikipedia.org/wiki/Value_chain>
26. Slack Incoming Webhooks：<https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks>
27. Slack Block Kit：<https://docs.slack.dev/block-kit/>
28. 飞书自定义机器人：<https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot>
29. Telegram Bot API：<https://core.telegram.org/bots/api>
30. 本仓现状文件：`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/migrations/094_lead_mining.sql`、`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server/internal/service/lead_mining.go`、`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server/internal/service/lead_judge_context.go`、`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server/internal/model/clue.go`、`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server/internal/service/lead_mining_notify_external.go`

---

**声明**：本报告为行业方法论背景调研，所有引用均来自上列一手/权威来源；对未能读取正文的页面已在 §7 标注且未据其编造结论。建议部分为工程化映射，需结合 HiveMtk 实际代码与业务语料进一步验证。
