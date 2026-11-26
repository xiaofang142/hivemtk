<template>
  <div class="account-matrix">
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="mb12"
    >
      <template #title>
        账号级配置是真实生效入口：这里改关键词、语义词、两类智能体和开关会立即影响命中；「全局默认」只在账号/渠道都没有规则时兜底。
      </template>
    </el-alert>

    <div class="matrix-head">
      <span class="hint">规则优先级：账号 &gt; 渠道 &gt; 全局（命中第一条即停止）</span>
      <el-button
        size="small"
        :loading="loading"
        @click="loadAll"
      >
        刷新
      </el-button>
    </div>

    <el-table
      v-loading="loading"
      :data="rows"
      border
      stripe
      empty-text="暂无渠道账号，请先在「渠道 → 渠道账号」创建"
    >
      <el-table-column
        label="渠道"
        width="120"
      >
        <template #default="{ row }">
          {{ channelLabel(row.channel_id) }}
        </template>
      </el-table-column>
      <el-table-column
        label="账号 / 识别码"
        min-width="220"
      >
        <template #default="{ row }">
          <div>{{ row.name || `#${row.id}` }}</div>
          <div class="muted">{{ row.identify_code || '-' }}</div>
        </template>
      </el-table-column>
      <el-table-column
        label="自动回复智能体"
        min-width="160"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          {{ replyLabel(row.auto_reply_agent_id) }}
        </template>
      </el-table-column>
      <el-table-column
        label="线索发现智能体"
        min-width="180"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          {{ leadLabel(row.lead_mining_agent_id) }}
        </template>
      </el-table-column>
      <el-table-column
        label="生效关键词 / 来源"
        min-width="300"
      >
        <template #default="{ row }">
          <div>{{ join(activeRuleFor(row)?.keywords) || '—' }}</div>
          <div class="muted">语义：{{ join(activeRuleFor(row)?.semantic_keywords) || '—' }}</div>
          <el-tag
            size="small"
            :type="ruleTagType(row)"
            class="mt4"
          >
            {{ ruleScopeLabel(row) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="账号状态"
        width="100"
      >
        <template #default="{ row }">
          <el-tag
            size="small"
            :type="Number(row.status) === 1 ? 'success' : 'info'"
          >
            {{ Number(row.status) === 1 ? '启用' : '停用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="90"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            @click="openEdit(row)"
          >
            编辑
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="dialogVisible"
      title="编辑账号级线索发掘配置"
      width="640px"
    >
      <el-form
        label-width="130px"
        @submit.prevent
      >
        <el-form-item label="自动回复智能体">
          <el-select
            v-model="form.auto_reply_agent_id"
            filterable
            clearable
            placeholder="选择销售 / 客服智能体"
            style="width: 100%"
          >
            <el-option
              v-for="a in replyAgents"
              :key="a.id"
              :label="a.name"
              :value="a.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="线索发现智能体">
          <el-select
            v-model="form.lead_mining_agent_id"
            filterable
            placeholder="必选，独立线索发现智能体"
            style="width: 100%"
          >
            <el-option
              v-for="a in leadAgents"
              :key="a.id"
              :label="a.name"
              :value="a.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="关键词">
          <el-input
            v-model="form.keywords"
            type="textarea"
            :rows="2"
            placeholder="逗号分隔，如：约,成都,报价"
          />
        </el-form-item>
        <el-form-item label="语义关键词">
          <el-input
            v-model="form.semantic_keywords"
            type="textarea"
            :rows="2"
            placeholder="逗号分隔，如：成都本地客户,想了解价格"
          />
        </el-form-item>
        <el-form-item label="规则描述">
          <el-input
            v-model="form.system_description"
            type="textarea"
            :rows="2"
            placeholder="自然语言描述该账号什么算线索"
          />
        </el-form-item>
        <el-form-item label="账号启用">
          <el-switch
            v-model="form.status"
            :active-value="1"
            :inactive-value="0"
          />
        </el-form-item>
        <el-form-item label="发掘规则启用">
          <el-switch v-model="form.rule_enabled" />
        </el-form-item>
      </el-form>
      <el-alert
        type="warning"
        :closable="false"
        show-icon
      >
        <template #title>
          保存会同时更新账号的两类智能体绑定与该账号的 account 级发掘规则；不会改动其他账号。
        </template>
      </el-alert>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="save"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { channelAccountApi } from '@/api/channelAccount'
import { leadsApi } from '@/api/leads'
import { listAgents } from '@/api/aiAgent'

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const channels = ref([])
const leadAgents = ref([])
const replyAgents = ref([])
const rules = ref([])
const dialogVisible = ref(false)
const current = ref(null)

const form = reactive({
  auto_reply_agent_id: 0,
  lead_mining_agent_id: 0,
  keywords: '',
  semantic_keywords: '',
  system_description: '',
  status: 1,
  rule_enabled: true,
  rule_id: 0
})

function asList(value) {
  if (Array.isArray(value)) return value
  if (Array.isArray(value?.list)) return value.list
  return []
}

function join(list) {
  return Array.isArray(list) ? list.join('、') : ''
}

function split(text) {
  return String(text || '').split(/[,，、\n]/).map((s) => s.trim()).filter(Boolean)
}

function channelLabel(id) {
  const ch = channels.value.find((c) => Number(c.id) === Number(id))
  return ch?.name || ch?.code || (id ? `#${id}` : '-')
}

function replyLabel(id) {
  if (!id) return '未绑定'
  return replyAgents.value.find((a) => Number(a.id) === Number(id))?.name || `#${id}`
}

function leadLabel(id) {
  if (!id) return '未绑定'
  return leadAgents.value.find((a) => Number(a.id) === Number(id))?.name || `#${id}`
}

function accountRule(accountID) {
  return rules.value.find((r) => r.scope === 'account' && Number(r.scope_id) === Number(accountID))
}

function channelRule(channelID) {
  return rules.value.find((r) => r.scope === 'channel' && Number(r.scope_id) === Number(channelID))
}

function globalRule() {
  return rules.value.find((r) => r.scope === 'global')
}

function activeRuleFor(row) {
  return accountRule(row.id) || channelRule(row.channel_id) || globalRule()
}

function ruleScopeLabel(row) {
  const rule = activeRuleFor(row)
  if (!rule) return '无规则'
  const name = { account: '账号级', channel: '渠道级', global: '全局兜底' }[rule.scope] || rule.scope
  return rule.enabled ? name : `${name}（已停用）`
}

function ruleTagType(row) {
  const rule = activeRuleFor(row)
  if (!rule || !rule.enabled) return 'info'
  return { account: 'success', channel: 'warning', global: '' }[rule.scope] || ''
}

async function loadAll() {
  loading.value = true
  try {
    const [accRes, chRes, leadRes, agentRes, ruleRes] = await Promise.all([
      channelAccountApi.listAccounts(),
      channelAccountApi.listChannels(),
      channelAccountApi.listLeadMiningAgents(),
      listAgents(),
      leadsApi.listRules()
    ])
    rows.value = asList(accRes)
    channels.value = asList(chRes)
    leadAgents.value = asList(leadRes)
    replyAgents.value = asList(agentRes).filter((a) =>
      ['sales', 'hybrid', 'customer_service'].includes(String(a.agent_type || '').toLowerCase())
    )
    rules.value = asList(ruleRes)
  } catch (e) {
    ElMessage.error(e?.message || '加载账号配置失败')
  } finally {
    loading.value = false
  }
}

async function openEdit(row) {
  current.value = row
  const rule = accountRule(row.id)
  Object.assign(form, {
    auto_reply_agent_id: row.auto_reply_agent_id || 0,
    lead_mining_agent_id: row.lead_mining_agent_id || 0,
    keywords: join(rule?.keywords),
    semantic_keywords: join(rule?.semantic_keywords),
    system_description: rule?.system_description || '',
    status: Number(row.status ?? 1),
    rule_enabled: rule ? !!rule.enabled : true,
    rule_id: rule?.id || 0
  })
  dialogVisible.value = true
}

async function save() {
  if (!form.lead_mining_agent_id) {
    ElMessage.warning('请选择线索发现智能体')
    return
  }
  saving.value = true
  try {
    // 详情接口带出 owner_user_ids / data_scope / remark 等全字段，
    // 否则 PUT 会把这些字段清空（Update 会覆盖 owner_user_ids）。
    const detail = await channelAccountApi.getAccount(current.value.id)
    await channelAccountApi.updateAccount(current.value.id, {
      channel_id: detail.channel_id,
      name: detail.name,
      remark: detail.remark || '',
      self_identify_name: detail.self_identify_name || '',
      type: detail.type || 'dm',
      owner_user_ids: detail.owner_user_ids || [],
      data_scope: detail.data_scope || '{}',
      auto_reply_agent_id: form.auto_reply_agent_id || undefined,
      lead_mining_agent_id: Number(form.lead_mining_agent_id),
      status: Number(form.status)
    })
    await leadsApi.saveRule({
      id: form.rule_id,
      scope: 'account',
      scope_id: current.value.id,
      keywords: split(form.keywords),
      semantic_keywords: split(form.semantic_keywords),
      system_description: form.system_description,
      enabled: !!form.rule_enabled
    })
    ElMessage.success('已保存账号绑定与账号级发掘规则')
    dialogVisible.value = false
    await loadAll()
  } catch (e) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(loadAll)
</script>

<style scoped>
.account-matrix { padding: 4px 0; }
.mb12 { margin-bottom: 12px; }
.mt4 { margin-top: 4px; }
.matrix-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.hint { color: #909399; font-size: 12px; }
.muted { color: #909399; font-size: 12px; }
</style>
