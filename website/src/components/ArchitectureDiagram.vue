<script setup>
const layers = [
  {
    id: 'L1',
    name: '接入与渠道层',
    desc: '7 类适配器统一入站',
    items: [
      { t: '网页客服 · WebSocket', s: 'embed-sdk + internal/websocket' },
      { t: '闲鱼自动化', s: 'xianyu_ws.go' },
      { t: '平台机器人', s: 'browser/platform.go' },
      { t: 'Telegram', s: 'telegram_webhook_bootstrap.go' },
      { t: 'WhatsApp', s: 'whatsapp.go' },
      { t: '站内 Web 入站', s: 'POST /api/chat/ingress' },
      { t: '访客 Chat API', s: '/api/chat/public/*' },
    ],
    note: 'Webhook / WebSocket → InboxIngressService.HandleIngressMessage',
  },
  {
    id: 'L2',
    name: '智能体运行时',
    desc: 'AgentRuntime',
    items: [
      { t: '事件订阅', s: 'customer.message.received' },
      { t: '上下文加载', s: 'AgentContext（人设+SOP+记忆）' },
      { t: '消息路由', s: 'HandleCustomerMessage · AgentType' },
    ],
    note: '全局单例 agent_runtime.GetGlobalRuntime() · NewPGAgentContextLoader',
  },
  {
    id: 'L3',
    name: '智能体引擎',
    desc: 'InferenceCycle · 决策大脑',
    core: true,
    items: [
      { t: '感知', s: '情绪 + 意图' },
      { t: '对齐', s: '6 维拟人度' },
      { t: '门禁', s: '危机 → 转人工' },
      { t: '规划', s: 'Planner' },
      { t: 'Bridge', s: 'SalesEngine / SmartCS' },
    ],
    note: '⚡ ReAct：Thought → Action（react_adapter.go）· 被动应答 + 主动触达共用同一大脑与记忆',
  },
  {
    id: 'L4',
    name: '工具注册表',
    desc: '42 工具 · ToolRouter',
    items: [
      { t: 'customer.*', s: '客户' },
      { t: 'knowledge.*', s: '知识' },
      { t: 'reach.*', s: '触达' },
      { t: 'business.*', s: '订单' },
      { t: 'pm.*', s: '私信' },
    ],
    note: '装饰器链：限流 → 重试 → 审计 → 计费 · ToolRouter.Route · GetGlobalExecutor',
  },
  {
    id: 'L5',
    name: '记忆系统',
    desc: '4 层 · MemorySystem',
    items: [
      { t: 'L1 短期', s: 'PG MemoryItem' },
      { t: 'L2 长期', s: 'PG + pgvector' },
      { t: 'L3 SOP', s: 'SOPStateMemory' },
      { t: 'L4 业务', s: 'BusinessMemory' },
    ],
    note: '统一入口 MemorySystem · service/memory_system.go · Remember / Recall',
  },
  {
    id: 'L6',
    name: 'AI 算力底座',
    desc: 'Compute',
    items: [
      { t: 'Embedding', s: '向量化' },
      { t: 'PGvector', s: '向量存储' },
      { t: 'RAG 引擎', s: '混合检索' },
      { t: 'LLM 调度', s: '多厂商' },
      { t: '故障切换', s: '熔断降级' },
    ],
    note: '多 LLM 路由：DeepSeek / 通义千问 / GPT-4o / 智谱 GLM · llm.GetGlobalDispatcher() · provider_failover.go',
  },
]

const arrows = ['消息事件', '上下文就绪', '工具调用', '记忆读写', '检索/生成']
</script>

<template>
  <section class="section arch-section" id="arch">
    <div class="container">
      <!-- 编辑杂志风章节头 -->
      <header class="arch-head">
        <div class="head-meta">
          <span class="tag-line">{{ $t('系统架构') }}</span>
          <span class="head-counter">
            <span class="counter-num">{{ String(layers.length).padStart(2, '0') }}</span>
            <span class="counter-label">{{ $t('层物理拓扑') }}</span>
          </span>
        </div>
        <h2 class="arch-title">
          {{ $t('一个真智能体，') }}<span class="ink-mark">{{ $t('六层协同') }}</span>
        </h2>
        <p class="arch-sub">{{ $t('从上到下是消息入站到回复出站的真实链路。决策大脑（智能体引擎）居中调度，工具是手脚、记忆是经验、本地向量化与多 LLM 是算力底座。') }}</p>
      </header>

      <!-- 架构分层卡片 -->
      <div class="arch">
        <template v-for="(layer, i) in layers" :key="layer.id">
          <!-- 层卡片 -->
          <div class="layer" :class="{ core: layer.core }">
            <!-- 左侧标签 -->
            <div class="layer-tag">
              <span class="layer-id">{{ layer.id }}</span>
              <span class="layer-name">{{ $t(layer.name) }}</span>
              <span class="layer-desc">{{ $t(layer.desc) }}</span>
              <span v-if="layer.core" class="layer-core-flag">CORE</span>
            </div>

            <!-- 右侧内容 -->
            <div class="layer-body">
              <div class="items">
                <div v-for="item in layer.items" :key="item.t" class="item">
                  <span class="item-t">{{ $t(item.t) }}</span>
                  <span class="item-s">{{ $t(item.s) }}</span>
                </div>
              </div>
              <p class="layer-note">
                <span class="note-mark" aria-hidden="true">▎</span>
                {{ $t(layer.note) }}
              </p>
            </div>
          </div>

          <!-- 层间箭头:数据流 -->
          <div v-if="i < layers.length - 1" class="arrow">
            <svg width="14" height="24" viewBox="0 0 14 24" fill="none" aria-hidden="true">
              <line x1="7" y1="0" x2="7" y2="16" stroke="currentColor" stroke-width="1.5" stroke-dasharray="4 3" class="flow-line" />
              <path d="M3 14 L7 22 L11 14" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span class="arrow-label">{{ $t(arrows[i]) }}</span>
          </div>
        </template>
      </div>

      <!-- 底部说明 -->
      <p class="arch-cap">{{ $t('智能体引擎（决策大脑）居中调度：向下调用工具注册表执行动作、读写记忆系统积累经验、请求 AI 算力底座完成检索与生成。它不是写死的流程，而是会自己想办法把事办成的真智能体。') }}</p>
    </div>
  </section>
</template>

<style scoped>
.arch-section {
  background: var(--bg-subtle);
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}

/* —— 章节头 —— */
.arch-head {
  max-width: 880px;
  margin-bottom: 56px;
  border-top: 2px solid var(--text);
  padding-top: 28px;
}

.head-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
  flex-wrap: wrap;
  gap: 12px;
}

.head-counter {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  font-family: var(--font-mono);
  color: var(--text-muted);
  font-size: 0.78rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.counter-num {
  font-family: var(--font-display);
  font-size: 1.4rem;
  font-weight: 900;
  color: var(--primary);
  letter-spacing: -0.02em;
}

.counter-label {
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

.arch-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5vw, 3.4rem);
  font-weight: 900;
  line-height: 1.1;
  letter-spacing: -0.028em;
  margin-bottom: 20px;
  color: var(--text);
}

.arch-sub {
  font-size: 1.06rem;
  color: var(--text-muted);
  line-height: 1.75;
  max-width: 760px;
}

/* —— 架构主体 —— */
.arch {
  max-width: 980px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
}

/* 层卡片 */
.layer {
  display: grid;
  grid-template-columns: 200px 1fr;
  border-radius: var(--radius-lg);
  overflow: hidden;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  transition: border-color 0.22s ease, box-shadow 0.22s ease, transform 0.22s ease;
}

.layer:hover {
  border-color: var(--border-strong);
  box-shadow: var(--shadow-md);
}

/* 核心层(L3 智能体引擎)高亮 */
.layer.core {
  border-color: var(--primary);
  border-width: 1.5px;
  box-shadow: var(--shadow-primary);
  background: linear-gradient(180deg, var(--primary-soft) 0%, var(--bg-surface) 40%);
}

.layer.core:hover {
  box-shadow: 0 12px 32px rgba(200, 57, 47, 0.18);
}

/* 左侧标签 */
.layer-tag {
  position: relative;
  padding: 22px 20px;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  border-right: 1px solid var(--border);
  background: var(--bg-subtle);
}

.layer.core .layer-tag {
  background: var(--primary);
  border-right-color: var(--primary-deep);
}

.layer-id {
  font-family: var(--font-mono);
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.14em;
  color: var(--accent);
  padding: 3px 8px;
  border-radius: 4px;
  background: var(--accent-soft);
  border: 1px solid var(--accent-border);
}

.layer.core .layer-id {
  color: #fff;
  background: rgba(255, 255, 255, 0.18);
  border-color: rgba(255, 255, 255, 0.3);
}

.layer-name {
  font-family: var(--font-display);
  font-size: 1.08rem;
  font-weight: 800;
  letter-spacing: -0.015em;
  color: var(--text);
  line-height: 1.25;
  margin-top: 4px;
}

.layer.core .layer-name {
  color: #fff;
}

.layer-desc {
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--text-muted);
  letter-spacing: 0.02em;
}

.layer.core .layer-desc {
  color: rgba(255, 255, 255, 0.78);
}

.layer-core-flag {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  margin-top: 8px;
  border-radius: 4px;
  background: #fff;
  color: var(--primary);
  font-family: var(--font-body);
  font-size: 0.66rem;
  font-weight: 800;
  letter-spacing: 0.16em;
  align-self: flex-start;
}

/* 右侧内容 */
.layer-body {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  justify-content: center;
}

.items {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 7px 12px;
  border-radius: var(--radius-sm);
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  min-width: 110px;
  transition: border-color 0.2s ease, background 0.2s ease;
}

.item:hover {
  border-color: var(--primary-border);
  background: var(--bg-surface);
}

.layer.core .item {
  background: var(--bg-surface);
  border-color: var(--primary-border);
}

.item-t {
  font-family: var(--font-display);
  font-size: 0.84rem;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.01em;
}

.item-s {
  font-family: var(--font-mono);
  font-size: 0.7rem;
  color: var(--text-muted);
  letter-spacing: 0.01em;
}

.layer-note {
  font-size: 0.78rem;
  color: var(--text-muted);
  line-height: 1.6;
  margin: 0;
  display: flex;
  gap: 6px;
  align-items: flex-start;
}

.note-mark {
  color: var(--primary);
  font-weight: 700;
  flex-shrink: 0;
  line-height: 1.4;
}

.layer.core .note-mark {
  color: var(--primary-deep);
}

/* 层间箭头:数据流 */
.arrow {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  padding: 6px 0;
  color: var(--primary);
}

.arrow-label {
  font-family: var(--font-mono);
  font-size: 0.7rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--primary);
  background: var(--bg-subtle);
  padding: 2px 10px;
  border-radius: 999px;
  border: 1px solid var(--primary-border);
  margin-top: 2px;
}

.flow-line {
  animation: arch-dash 1.4s linear infinite;
}

@keyframes arch-dash {
  to { stroke-dashoffset: -14; }
}

@media (prefers-reduced-motion: reduce) {
  .flow-line { animation: none; }
}

/* 底部说明 */
.arch-cap {
  max-width: 880px;
  margin: 36px auto 0;
  text-align: center;
  color: var(--text-soft);
  font-size: 0.96rem;
  line-height: 1.78;
  padding: 22px 28px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  border-top: 2px solid var(--primary);
}

/* —— 响应式 —— */
@media (max-width: 768px) {
  .layer {
    grid-template-columns: 1fr;
  }
  .layer-tag {
    border-right: none;
    border-bottom: 1px solid var(--border);
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 14px 16px;
  }
  .layer-name {
    margin-top: 0;
  }
  .layer-body {
    padding: 14px 16px;
  }
  .items {
    flex-direction: column;
  }
  .item {
    min-width: 0;
  }
  .arch-cap {
    padding: 18px 20px;
    font-size: 0.9rem;
  }
}

@media (max-width: 480px) {
  .layer-tag {
    padding: 12px 14px;
    gap: 8px;
  }
  .layer-name {
    font-size: 0.98rem;
  }
  .item {
    padding: 6px 10px;
  }
  .item-t {
    font-size: 0.78rem;
  }
  .item-s {
    font-size: 0.66rem;
  }
}
</style>
