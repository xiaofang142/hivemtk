<template>
  <div class="playground">
    
    <el-card class="header-card" shadow="never">
      <el-row :gutter="20">
        <el-col :span="6">
          <el-form-item label="资产 ID" label-width="80px">
            <el-input v-model="bundle.asset_id" placeholder="hive_sales_vape_cn_001" :disabled="isEdit" />
          </el-form-item>
        </el-col>
        <el-col :span="6">
          <el-form-item label="资产包名称" label-width="90px">
            <el-input v-model="bundle.title" placeholder="销冠话术包名称" />
          </el-form-item>
        </el-col>
        <el-col :span="4">
          <el-form-item label="版本" label-width="60px">
            <el-input v-model="bundle.version" placeholder="1.0.0" />
          </el-form-item>
        </el-col>
        <el-col :span="4">
          <el-form-item label="行业" label-width="60px">
            <el-input v-model="bundle.industry" placeholder="跨境电商" />
          </el-form-item>
        </el-col>
        <el-col :span="4">
          <div class="header-actions">
            <el-button type="primary" :loading="saving" @click="handleSave">💾 保存</el-button>
            <el-button v-if="bundle.id" type="success" @click="handlePublish">🚀 发布</el-button>
          </div>
        </el-col>
      </el-row>
    </el-card>

    <el-row :gutter="16" class="main-row">
      
      <el-col :span="14">
        <el-card class="left-panel" shadow="never">
          <template #header>
            <div class="panel-header">
              <span>📝 左栏：OpenAI 协议标准的消息链流式编排区</span>
              <el-button type="primary" size="small" @click="addMessage('user')">➕ 添加 Few-Shot 对</el-button>
            </div>
          </template>

          <div class="messages-list">
            <div
              v-for="(msg, idx) in bundle.messages"
              :key="idx"
              class="message-card"
              :class="'role-' + msg.role"
            >
              <div class="message-header">
                <el-tag :type="roleTagType(msg.role)" size="small">
                  {{ roleLabel(msg.role) }}
                </el-tag>
                <span v-if="idx === 0 && msg.role === 'system'" class="hint">（固化首项，商城的灵魂核心）</span>
                <span v-else-if="msg.role === 'user'" class="hint">（Few-Shot 用户示例）</span>
                <span v-else-if="msg.role === 'assistant'" class="hint">（Few-Shot AI 回复）</span>
                <div class="message-actions">
                  <el-button
                    v-if="idx > 0"
                    size="small"
                    link
                    @click="moveUp(idx)"
                  >↑</el-button>
                  <el-button
                    v-if="idx < bundle.messages.length - 1"
                    size="small"
                    link
                    @click="moveDown(idx)"
                  >↓</el-button>
                  <el-button
                    v-if="idx > 0"
                    size="small"
                    type="danger"
                    link
                    @click="removeMessage(idx)"
                  >删除</el-button>
                </div>
              </div>
              <el-input
                v-model="msg.content"
                type="textarea"
                :rows="idx === 0 ? 10 : 4"
                :placeholder="rolePlaceholder(msg.role)"
                class="message-textarea"
              />
            </div>
          </div>

          <div class="add-pair-row">
            <el-button @click="addMessage('user')">➕ 添加 user Few-Shot</el-button>
            <el-button @click="addMessage('assistant')">➕ 添加 assistant Few-Shot</el-button>
            <el-button @click="addMessage('system')">➕ 添加 system 指令</el-button>
          </div>
        </el-card>
      </el-col>

      
      <el-col :span="10">
        <el-card class="right-panel" shadow="never">
          <template #header>
            <div class="panel-header">
              <span>🧪 右栏：本地模型沙箱内测与上架</span>
            </div>
          </template>

          
          <div class="config-section">
            <div class="section-title">⚙️ 调试基座配置</div>
            <el-form label-width="120px" size="small">
              <el-form-item label="关联本地推理端">
                <el-input v-model="sandbox.endpoint" placeholder="http://localhost:11434/v1/chat/completions" />
              </el-form-item>
              <el-form-item label="模型名">
                <el-input v-model="sandbox.model" placeholder="qwen2.5:7b" />
              </el-form-item>
              <el-form-item label="测试推理温度">
                <el-slider v-model="sandbox.temperature" :min="0" :max="1" :step="0.1" show-input />
              </el-form-item>
              <el-form-item label="最大 tokens">
                <el-input-number v-model="sandbox.maxTokens" :min="128" :max="8192" :step="128" />
              </el-form-item>
            </el-form>
          </div>

          
          <div class="chat-section">
            <div class="section-title">🖥️ 沙箱实时多轮对话模拟</div>
            <div class="chat-history" ref="chatHistoryRef">
              <div
                v-for="(m, i) in sandbox.history"
                :key="i"
                class="chat-msg"
                :class="'chat-' + m.role"
              >
                <div class="chat-role">{{ roleLabel(m.role) }}</div>
                <pre class="chat-content">{{ m.content }}</pre>
              </div>
              <el-empty v-if="!sandbox.history.length" description="输入测试用户消息开始模拟" :image-size="60" />
            </div>
            <div class="chat-input-row">
              <el-input
                v-model="sandbox.input"
                type="textarea"
                :rows="2"
                placeholder="输入测试用户消息，例如：发货到德国包清关吗？"
                @keydown.enter.ctrl="runSandbox"
              />
              <el-button type="primary" :loading="sandbox.running" @click="runSandbox">
                ▶️ 运行
              </el-button>
            </div>
          </div>

          
          <div class="intent-section" v-if="sandbox.lastIntent">
            <div class="section-title">🔍 拦截日志 JSON 预检</div>
            <div class="intent-status">
              <el-tag :type="sandbox.lastIntent.valid ? 'success' : 'danger'">
                {{ sandbox.lastIntent.valid ? '🟢 合法格式，成功抓取' : '🔴 非法格式' }}
              </el-tag>
              <span v-if="sandbox.lastIntent.intent" class="intent-label">
                intent: <code>{{ sandbox.lastIntent.intent }}</code>
              </span>
              <span v-if="sandbox.lastIntent.durationMs != null" class="intent-label">
                耗时: {{ sandbox.lastIntent.durationMs }} ms
              </span>
            </div>
            <pre class="intent-json">{{ sandbox.lastIntent.rawJSON || '(无 JSON 块)' }}</pre>
            <div v-if="sandbox.lastIntent.capturedData && Object.keys(sandbox.lastIntent.capturedData).length" class="captured-data">
              <div class="section-subtitle">captured_data:</div>
              <pre>{{ JSON.stringify(sandbox.lastIntent.capturedData, null, 2) }}</pre>
            </div>
          </div>

          
          <div class="woven-section" v-if="sandbox.wovenMessages && sandbox.wovenMessages.length">
            <div class="section-title">🧶 织布后的最终 messages 数组（{{ sandbox.wovenMessages.length }} 条）</div>
            <pre class="woven-preview">{{ JSON.stringify(sandbox.wovenMessages, null, 2) }}</pre>
          </div>

          
          <div class="publish-section">
            <div class="section-title">💰 生态上架配置</div>
            <el-form label-width="120px" size="small">
              <el-form-item label="商业买断价">
                <el-input v-model="bundle.price" placeholder="$ 299.00" />
              </el-form-item>
              <el-form-item label="作用域">
                <el-radio-group v-model="bundle.scope">
                  <el-radio label="private">私有</el-radio>
                  <el-radio label="shared">共享</el-radio>
                  <el-radio label="official">官方</el-radio>
                </el-radio-group>
              </el-form-item>
            </el-form>
            <el-button type="primary" plain class="publish-btn" @click="handlePublish">
              🚀 审核上架到官方蜂巢商城
              <span class="publish-hint">（自动序列化为标准 messages 数组）</span>
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createBundle, updateBundle, getBundleByAssetID,
  weaveBundle, publishBundle, submitToPlatform
} from '@/api/assetBundle'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const route = useRoute()
const router = useRouter()
const chatHistoryRef = ref(null)
const saving = ref(false)

const aid = computed(() => route.params.aid || '')
const isEdit = computed(() => !!aid.value)

const bundle = reactive({
  id: null,
  asset_id: '',
  title: '',
  version: '1.0.0',
  industry: '',
  author: '',
  scope: 'private',
  status: 'draft',
  price: '',
  messages: [
    {
      role: 'system',
      content: '# 核心角色与销冠人设\n你是一名经过严格训练、结果导向的【王牌私域销售代表】。\n\n# 强制业务结算协议\n你必须在每一次回复的【最后】附带一个 ```json 块：\n```json\n{\n  "intent": "faq|lead_capture|human_transfer",\n  "captured_data": {}\n}\n```'
    }
  ]
});

const sandbox = reactive({
  endpoint: 'http://localhost:11434/v1/chat/completions',
  model: 'qwen2.5:7b',
  temperature: 0.2,
  maxTokens: 1024,
  input: '',
  history: [],
  running: false,
  lastIntent: null,
  wovenMessages: null
});

const roleLabel = (r) => ({
  system: 'system',
  user: 'user',
  assistant: 'assistant',
  tool: 'tool'
}[r] || r)

const roleTagType = (r) => ({
  system: 'danger',
  user: 'primary',
  assistant: 'success',
  tool: 'warning'
}[r] || 'info')

const rolePlaceholder = (r) => ({
  system: '系统人设 / 顶层指令 / 强制业务结算协议',
  user: '客户提问示例（Few-Shot）',
  assistant: 'AI 标准回复（含末尾 ```json 业务结算块```）',
  tool: '工具调用结果'
}[r] || '')

const addMessage = (role = 'user') => {
  bundle.messages.push({ role, content: '' })
  if (role === 'user') {
    bundle.messages.push({ role: 'assistant', content: '' });
  }
}

const removeMessage = (idx) => {
  bundle.messages.splice(idx, 1)
}

const moveUp = (idx) => {
  if (idx <= 0) return
  const arr = bundle.messages
  ;[arr[idx - 1], arr[idx]] = [arr[idx], arr[idx - 1]]
}

const moveDown = (idx) => {
  const arr = bundle.messages
  if (idx >= arr.length - 1) return
  ;[arr[idx], arr[idx + 1]] = [arr[idx + 1], arr[idx]]
}

const loadBundle = async () => {
  if (!aid.value) return
  try {
    const resp = await getBundleByAssetID(aid.value)
    const data = resp?.data || resp
    if (data) {
      bundle.id = data.id || null
      bundle.asset_id = data.asset_id || ''
      bundle.title = data.title || ''
      bundle.version = data.version || '1.0.0'
      bundle.industry = data.industry || ''
      bundle.author = data.author || ''
      bundle.scope = data.scope || 'private'
      bundle.status = data.status || 'draft'
      bundle.messages = (data.messages || []).map(m => ({ ...m }))
    }
  } catch (e) {
    ElMessage.error('加载资产包失败: ' + (e?.message || e))
  }
};

const handleSave = async () => {
  if (!bundle.asset_id) {
    ElMessage.warning('请填写资产 ID')
    return
  }
  if (!bundle.title) {
    ElMessage.warning('请填写资产包名称')
    return
  }
  if (!bundle.messages.length) {
    ElMessage.warning('请至少添加一条 message')
    return
  }
  saving.value = true
  try {
    const payload = {
      asset_id: bundle.asset_id,
      title: bundle.title,
      version: bundle.version,
      industry: bundle.industry,
      author: bundle.author || userStore.username,
      scope: bundle.scope,
      messages: bundle.messages
    }
    if (bundle.id) {
      payload.id = bundle.id
      await updateBundle(bundle.id, payload)
      ElMessage.success('更新成功')
    } else {
      const resp = await createBundle(payload)
      const data = resp?.data || resp
      if (data && data.id) {
        bundle.id = data.id
        ElMessage.success('保存成功')
        router.replace(`/asset-bundle/playground/${bundle.asset_id}`)
      } else {
        ElMessage.success('保存成功')
      }
    }
  } catch (e) {
    ElMessage.error('保存失败: ' + (e?.message || e))
  } finally {
    saving.value = false
  }
};

const handlePublish = async () => {
  if (!bundle.id) {
    ElMessage.warning('请先保存资产包')
    return
  }
  try {
    await ElMessageBox.confirm('确认发布并上架该资产包？将本地发布并提交平台审核', '确认', { type: 'warning' })
    await publishBundle(bundle.id)
    bundle.status = 'active'
    try {
      await submitToPlatform(bundle.id)
      ElMessage.success('本地已发布，并已提交平台审核上架')
    } catch (e) {
      ElMessage.warning('本地已发布；提交平台审核失败：' + (e?.message || e))
    }
  } catch (e) {
    if (e !== 'cancel') {
      ElMessage.error('发布失败: ' + (e?.message || e))
    }
  }
};

const JSON_BLOCK_RE = /```(?:json|JSON)?\s*(\{[\s\S]*?\})\s*```\s*$/;
const BARE_JSON_RE = /(\{"intent"\s*:[^{}]*(?:\{[^{}]*\}[^{}]*)*\})\s*$/

const extractIntent = (reply) => {
  const result = { valid: false, intent: '', capturedData: {}, rawJSON: '' }
  const blockRE = /```(?:json|JSON)?\s*(\{[\s\S]*?\})\s*```\s*$/;
  let m = reply.match(blockRE)
  if (!m) {
    const bareRE = /(\{"intent"\s*:[\s\S]*?\})\s*$/;
    m = reply.match(bareRE)
  }
  if (!m) return { ...result, reply }
  const rawJSON = m[1]
  result.rawJSON = rawJSON
  try {
    const parsed = JSON.parse(rawJSON)
    result.valid = true
    result.intent = parsed.intent || ''
    result.capturedData = parsed.captured_data || {}
    const cleaned = reply.slice(0, reply.indexOf(m[0])).replace(/\n+\s*$/, '');
    return { ...result, reply: cleaned }
  } catch (e) {
    return { ...result, reply }
  }
};

const runSandbox = async () => {
  if (!sandbox.input.trim()) {
    ElMessage.warning('请输入测试用户消息')
    return
  }
  if (!bundle.messages.length) {
    ElMessage.warning('请先编排 messages 数组')
    return
  }
  sandbox.running = true
  sandbox.lastIntent = null
  sandbox.wovenMessages = null
  const userQuery = sandbox.input
  sandbox.history.push({ role: 'user', content: userQuery })
  sandbox.input = ''

  let weaveOk = false;
  try {
    const weaveResp = await weaveBundle({
      asset_id: bundle.asset_id,
      user_query: userQuery,
      sandbox: true,
      chat_history: sandbox.history.filter(m => m.role !== 'system').map(m => ({ role: m.role, content: m.content })),
      options: {
        rag_position: 'after_fewshots',
        max_history_messages: 10,
        strip_few_shot_json: false,
        include_merchant_vars: false
      }
    })
    weaveOk = true
    const weaveData = weaveResp?.data || weaveResp
    const wovenMessages = weaveData?.messages || []
    sandbox.wovenMessages = wovenMessages

    const llmResp = await fetch(sandbox.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        model: sandbox.model,
        messages: wovenMessages,
        temperature: sandbox.temperature,
        max_tokens: sandbox.maxTokens,
        stream: false
      })
    });
    if (!llmResp.ok) {
      const errText = await llmResp.text()
      throw new Error(`LLM 返回 ${llmResp.status}: ${errText}`)
    }
    const llmData = await llmResp.json()
    const reply = llmData?.choices?.[0]?.message?.content || ''
    const startTs = Date.now()
    const extracted = extractIntent(reply);
    sandbox.history.push({ role: 'assistant', content: extracted.reply })
    sandbox.lastIntent = {
      valid: extracted.valid,
      intent: extracted.intent,
      capturedData: extracted.capturedData,
      rawJSON: extracted.rawJSON,
      durationMs: Date.now() - startTs
    }
  } catch (e) {
    if (!weaveOk) {
      const msg = (e && e.message) ? e.message : '织布失败';
      ElMessage.error('织布失败：' + msg)
      sandbox.history.push({
        role: 'assistant',
        content: `[织布失败] ${msg}\n\n提示：\n1. 请先「保存」资产包（织布需按 asset_id 读取已保存的 messages）\n2. 确认 asset_id 正确且未被删除`
      })
    } else {
      sandbox.history.push({
        role: 'assistant',
        content: `[LLM 调用失败] ${e?.message || e}\n\n提示：\n1. 请确认本地已启动 Ollama / vLLM\n2. 推理端地址正确（默认 http://localhost:11434/v1/chat/completions）\n3. CORS 已允许（Ollama 设置 OLLAMA_ORIGINS=*）`
      })
      ElMessage.error('LLM 调用失败')
    }
  } finally {
    sandbox.running = false
    await nextTick()
    if (chatHistoryRef.value) {
      chatHistoryRef.value.scrollTop = chatHistoryRef.value.scrollHeight
    }
  }
};

onMounted(() => {
  if (aid.value) {
    bundle.asset_id = aid.value
    loadBundle()
  }
})
</script>

<style scoped>
.playground {
  padding: 12px;
}
.header-card {
  margin-bottom: 12px;
}
.header-actions {
  display: flex;
  gap: 8px;
  padding-top: 4px;
}
.main-row {
  height: calc(100vh - 200px);
}
.left-panel, .right-panel {
  height: 100%;
  overflow: auto;
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}
.messages-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.message-card {
  border-left: 4px solid #409eff;
  padding: 8px 12px;
  background: #fafafa;
  border-radius: 4px;
}
.role-system {
  border-left-color: #f56c6c;
}
.role-user {
  border-left-color: #409eff;
}
.role-assistant {
  border-left-color: #67c23a;
}
.message-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.hint {
  font-size: 12px;
  color: #909399;
}
.message-actions {
  margin-left: auto;
}
.message-textarea :deep(.el-textarea__inner) {
  font-family: 'Courier New', monospace;
  font-size: 13px;
}
.add-pair-row {
  margin-top: 12px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.config-section, .chat-section, .intent-section, .woven-section, .publish-section {
  margin-bottom: 16px;
  padding: 12px;
  background: #fafafa;
  border-radius: 4px;
}
.section-title {
  font-weight: 600;
  margin-bottom: 10px;
  padding-bottom: 6px;
  border-bottom: 1px dashed #dcdfe6;
}
.section-subtitle {
  font-size: 12px;
  color: #606266;
  margin-top: 8px;
  margin-bottom: 4px;
}
.chat-history {
  max-height: 240px;
  overflow-y: auto;
  background: #fff;
  padding: 8px;
  border-radius: 4px;
  border: 1px solid #ebeef5;
  margin-bottom: 8px;
}
.chat-msg {
  margin-bottom: 8px;
  padding: 6px 8px;
  border-radius: 4px;
}
.chat-user {
  background: #ecf5ff;
  border-left: 3px solid #409eff;
}
.chat-assistant {
  background: #f0f9eb;
  border-left: 3px solid #67c23a;
}
.chat-system {
  background: #fef0f0;
  border-left: 3px solid #f56c6c;
}
.chat-role {
  font-size: 11px;
  color: #909399;
  margin-bottom: 2px;
}
.chat-content {
  font-size: 13px;
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0;
  font-family: inherit;
}
.chat-input-row {
  display: flex;
  gap: 8px;
  align-items: flex-end;
}
.intent-status {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 6px;
}
.intent-label {
  font-size: 13px;
}
.intent-label code {
  background: #f5f7fa;
  padding: 2px 6px;
  border-radius: 3px;
  font-family: monospace;
}
.intent-json {
  background: #2d2d2d;
  color: #f8f8f2;
  padding: 8px;
  border-radius: 4px;
  font-size: 12px;
  font-family: 'Courier New', monospace;
  max-height: 160px;
  overflow: auto;
  margin: 0;
}
.captured-data pre {
  background: #f5f7fa;
  padding: 6px;
  border-radius: 4px;
  font-size: 12px;
  margin: 4px 0 0;
}
.woven-preview {
  background: #f5f7fa;
  padding: 8px;
  border-radius: 4px;
  font-size: 11px;
  max-height: 200px;
  overflow: auto;
  margin: 0;
}
.publish-btn {
  width: 100%;
}
.publish-hint {
  font-size: 11px;
  color: #909399;
  margin-left: 4px;
}
</style>
