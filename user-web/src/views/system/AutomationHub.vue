<template>
  <div class="automation-hub">
    
    <el-card class="mb16">
      <template #header><b>办公时间与离开自动回复</b><el-tag size="small" style="margin-left:8px">{{ oh.enabled ? '已启用' : '未启用' }}</el-tag></template>
      <el-form label-width="120px" size="small">
        <el-form-item label="启用办公时间">
          <el-switch v-model="oh.enabled" />
        </el-form-item>
        <el-form-item label="工作时段">
          <div v-for="(r, i) in oh.daily_ranges" :key="i" style="margin-bottom:6px">
            <el-time-select v-model="r[0]" start="00:00" step="00:30" end="23:30" style="width:110px" />
            <span style="margin:0 6px">至</span>
            <el-time-select v-model="r[1]" start="00:00" step="00:30" end="23:30" style="width:110px" />
            <el-button text type="danger" size="small" @click="oh.daily_ranges.splice(i, 1)">删除</el-button>
          </div>
          <el-button size="small" @click="oh.daily_ranges.push(['09:00','18:00'])">添加时段</el-button>
        </el-form-item>
        <el-form-item label="离开回复">
          <el-input v-model="oh.away_message" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="saveOH">保存策略</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card class="mb16">
      <template #header><b>会话宏（一键多动作）</b></template>
      <div style="margin-bottom:10px;display:flex;gap:8px">
        <el-input v-model="newMacro.name" placeholder="宏名称" style="width:200px" size="small" />
        <el-select v-model="newMacro.type" size="small" style="width:160px" placeholder="动作">
          <el-option label="加内部备注" value="add_note" />
          <el-option label="加标签" value="add_tag" />
          <el-option label="设优先级" value="set_priority" />
          <el-option label="发消息" value="send_message" />
          <el-option label="关闭会话" value="close" />
        </el-select>
        <el-input v-model="newMacro.value" placeholder="动作内容" size="small" style="width:260px" />
        <el-button type="primary" size="small" @click="addMacro">创建宏</el-button>
      </div>
      <el-table :data="macros" size="small">
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column label="动作序列" min-width="300">
          <template #default="{ row }">
            <el-tag v-for="(a, i) in parseActions(row.actions)" :key="i" size="small" style="margin-right:4px">{{ a.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="delMacro(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-card class="mb16">
      <template #header><b>出站 Webhook 订阅</b><span class="hint">事件推送：message.created / session.created / session.closed / all</span></template>
      <div style="margin-bottom:10px;display:flex;gap:8px">
        <el-input v-model="newHook.url" placeholder="https://your-app.com/webhook" size="small" style="width:320px" />
        <el-input v-model="newHook.events" placeholder="all" size="small" style="width:160px" />
        <el-button type="primary" size="small" @click="addHook">创建订阅</el-button>
      </div>
      <el-table :data="hooks" size="small">
        <el-table-column prop="url" label="URL" min-width="240" show-overflow-tooltip />
        <el-table-column prop="events" label="事件" width="160" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }"><el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag></template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="delHook(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-card class="mb16">
      <template #header><b>定时邮件报表</b><el-button size="small" style="float:right" @click="sendNow">立即发送一次</el-button></template>
      <div style="margin-bottom:10px;display:flex;gap:8px">
        <el-input v-model="newSub.email" placeholder="接收邮箱" size="small" style="width:260px" />
        <el-select v-model="newSub.schedule" size="small" style="width:120px">
          <el-option label="每日" value="daily" />
          <el-option label="每周" value="weekly" />
        </el-select>
        <el-button type="primary" size="small" @click="addSub">订阅</el-button>
      </div>
      <el-table :data="subs" size="small">
        <el-table-column prop="email" label="邮箱" min-width="200" />
        <el-table-column prop="schedule" label="频率" width="100" />
        <el-table-column label="最近发送" width="170">
          <template #default="{ row }">{{ row.last_sent ? String(row.last_sent).replace('T',' ').slice(0,16) : '—' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="delSub(row)">退订</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-card>
      <template #header><b>AI 代理绩效</b>
        <el-radio-group v-model="perfDays" size="small" style="float:right" @change="loadPerf">
          <el-radio-button :value="7">7天</el-radio-button>
          <el-radio-button :value="30">30天</el-radio-button>
        </el-radio-group>
      </template>
      <el-row :gutter="12" v-if="perf">
        <el-col :span="5"><div class="stat"><div class="v">{{ perf.total_sessions }}</div><div class="l">总会话</div></div></el-col>
        <el-col :span="5"><div class="stat"><div class="v" style="color:#10b981">{{ perf.auto_rate?.toFixed(1) }}%</div><div class="l">AI 自动化率</div></div></el-col>
        <el-col :span="5"><div class="stat"><div class="v">{{ perf.llm_calls }}</div><div class="l">LLM 调用</div></div></el-col>
        <el-col :span="5"><div class="stat"><div class="v">${{ perf.llm_cost?.toFixed(4) }}</div><div class="l">LLM 成本</div></div></el-col>
        <el-col :span="4"><div class="stat"><div class="v">{{ perf.human_handled }}</div><div class="l">人工接管</div></div></el-col>
      </el-row>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/request'

const oh = ref({ enabled: false, daily_ranges: [['09:00', '18:00']], away_message: '' })
const macros = ref([])
const hooks = ref([])
const subs = ref([])
const perf = ref(null)
const perfDays = ref(7)
const newMacro = ref({ name: '', type: 'add_note', value: '' })
const newHook = ref({ url: '', events: 'all' })
const newSub = ref({ email: '', schedule: 'daily' })

const parseActions = (raw) => { try { return JSON.parse(raw || '[]') } catch { return [] } }

const loadAll = async () => {
  const [ohRes, macroRes, hookRes, subRes] = await Promise.all([
    http.get('/api/office-hours').catch(() => null),
    http.get('/api/macros').catch(() => null),
    http.get('/api/webhook-subscriptions').catch(() => null),
    http.get('/api/report-subscriptions').catch(() => null)
  ])
  // 响应拦截器已把 {code,data} 拆包：/api/office-hours 直接回配置对象，
  // 列表端点直接回 {list,total}。按信封再取 .data 只会读到 undefined。
  const ohData = ohRes?.data ?? ohRes
  if (ohData) oh.value = { ...oh.value, ...ohData }
  macros.value = macroRes?.list ?? []
  hooks.value = hookRes?.list ?? []
  subs.value = subRes?.list ?? []
  loadPerf()
}

const loadPerf = async () => {
  const res = await http.get(`/api/analytics/ai-performance?days=${perfDays.value}`).catch(() => null)
  perf.value = res?.data ?? res ?? null
}

const saveOH = async () => {
  try {
    await http.put('/api/office-hours', oh.value)
    ElMessage.success('办公时间策略已保存')
  } catch (e) { ElMessage.error(e?.message || '保存失败') }
}

const addMacro = async () => {
  try {
    await http.post('/api/macros', { name: newMacro.value.name, actions: [{ type: newMacro.value.type, value: newMacro.value.value }] })
    ElMessage.success('宏已创建')
    newMacro.value = { name: '', type: 'add_note', value: '' }
    loadAll()
  } catch (e) { ElMessage.error(e?.message || '创建失败') }
}

const delMacro = async (row) => {
  try {
    await ElMessageBox.confirm(`删除宏「${row.name}」？`, '确认', { type: 'warning' })
    await http.delete(`/api/macros/${row.id}`)
    loadAll()
  } catch (e) { if (e !== 'cancel' && e !== 'close') ElMessage.error('删除失败') }
}

const addHook = async () => {
  try {
    const res = await http.post('/api/webhook-subscriptions', newHook.value)
    ElMessageBox.alert(`订阅创建成功。签名密钥（仅显示一次，用于校验推送）：\n${res?.data?.secret || res?.secret || ''}`, '请保存 Secret')
    loadAll()
  } catch (e) { ElMessage.error(e?.message || '创建失败') }
}

const delHook = async (row) => {
  try {
    await ElMessageBox.confirm(`删除订阅 ${row.url}？`, '确认', { type: 'warning' })
    await http.delete(`/api/webhook-subscriptions/${row.id}`)
    loadAll()
  } catch (e) { if (e !== 'cancel' && e !== 'close') ElMessage.error('删除失败') }
}

const addSub = async () => {
  try {
    await http.post('/api/report-subscriptions', newSub.value)
    ElMessage.success('订阅成功（每日 08:00 发送昨日汇总）')
    newSub.value = { email: '', schedule: 'daily' }
    loadAll()
  } catch (e) { ElMessage.error(e?.message || '订阅失败') }
}

const delSub = async (row) => {
  try {
    await http.delete(`/api/report-subscriptions/${row.id}`)
    loadAll()
  } catch (e) { ElMessage.error('退订失败') }
}

const sendNow = async () => {
  try {
    const res = await http.post('/api/report-subscriptions/send-now')
    ElMessage.success(`已发送 ${res?.data?.sent ?? res?.sent ?? 0} 份（未配置 SMTP 时会失败并记录日志）`)
  } catch (e) { ElMessage.error(e?.message || '发送失败') }
}

onMounted(loadAll)
</script>

<style scoped>
.automation-hub { padding: 16px; }
.mb16 { margin-bottom: 16px; }
.hint { font-size: 12px; color: #94a3b8; margin-left: 10px; }
.stat { text-align: center; padding: 10px 0; background: #f8fafc; border-radius: 8px; }
.stat .v { font-size: 24px; font-weight: 700; }
.stat .l { font-size: 12px; color: #64748b; margin-top: 4px; }
</style>
