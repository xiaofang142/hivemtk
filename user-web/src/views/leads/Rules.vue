<template>
  <div class="leads-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          线索三粒度规则
        </h2>
        <span class="page-sub">按 account → channel 优先级匹配；列表按当前租户隔离</span>
      </div>
      <div class="page-actions">
        <el-button @click="$router.push('/leads/notify-routes')">
          通知路由
        </el-button>
        <el-button @click="$router.push('/leads/evidence')">
          判定证据
        </el-button>
        <el-button @click="$router.push('/leads/archive')">
          线索归档
        </el-button>
        <el-button @click="$router.push('/leads/scoped-list')">
          权限内列表
        </el-button>
        <el-button @click="$router.push('/leads/mining-agents')">
          线索发现智能体
        </el-button>
        <el-button
          type="primary"
          @click="openCreate"
        >
          新增规则
        </el-button>
      </div>
    </div>

    <WorkflowNavigation
      owner="/leads/rules"
      :links="workflowLinks"
    />
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="mb12"
    >
      <template #title>
        命中优先级：账号（account） &gt; 渠道（channel），取第一条启用的规则即停止；账号级 scope_id 是渠道账号 ID，渠道级是渠道 ID。两级都没有启用规则时不会发掘，不设全局兜底。
      </template>
    </el-alert>
    <el-card shadow="never">
      <el-table
        v-loading="loading"
        :data="rows"
        border
        stripe
        empty-text="暂无规则"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="scope"
          label="粒度"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="scopeTag(row.scope)"
            >
              {{ scopeLabel(row.scope) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="范围"
          width="140"
        >
          <template #default="{ row }">
            {{ scopeIdLabel(row) }}
          </template>
        </el-table-column>
        <el-table-column
          label="关键词"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ join(row.keywords) }}
          </template>
        </el-table-column>
        <el-table-column
          label="语义关键词"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ join(row.semantic_keywords) }}
          </template>
        </el-table-column>
        <el-table-column
          label="启用"
          width="90"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="row.enabled ? 'success' : 'info'"
            >
              {{ row.enabled ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="140"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              :disabled="row.scope === 'global'"
              :title="row.scope === 'global' ? '全局规则为历史数据，已停用写入' : ''"
              @click="openEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="danger"
              @click="remove(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editing ? '编辑规则' : '新增规则'"
      width="560px"
    >
      <el-form
        label-width="110px"
        @submit.prevent
      >
        <el-form-item label="粒度">
          <el-select v-model="form.scope">
            <el-option
              label="渠道"
              value="channel"
            />
            <el-option
              label="账号"
              value="account"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="范围选择">
          <ChannelSelect
            v-if="form.scope === 'channel'"
            v-model="form.scope_id"
            value-key="id"
            placeholder="请选择渠道"
          />
          <ChannelAccountSelect
            v-else
            v-model="form.scope_id"
            placeholder="请选择账号"
          />
        </el-form-item>
        <el-form-item label="关键词">
          <el-input
            v-model="form.keywords"
            type="textarea"
            :rows="2"
            placeholder="逗号分隔"
          />
        </el-form-item>
        <el-form-item label="语义关键词">
          <el-input
            v-model="form.semantic_keywords"
            type="textarea"
            :rows="2"
            placeholder="逗号分隔，如同义/相关词"
          />
        </el-form-item>
        <el-form-item label="规则描述">
          <el-input
            v-model="form.system_description"
            type="textarea"
            :rows="3"
          />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { leadsApi } from '@/api/leads'
import WorkflowNavigation from '@/components/WorkflowNavigation.vue'
import ChannelSelect from '@/components/ChannelSelect.vue'
import ChannelAccountSelect from '@/components/ChannelAccountSelect.vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const catalog = useChannelCatalogStore()

const workflowLinks = [
  { key: 'notifyRoutes', path: '/leads/notify-routes' },
  { key: 'evidence', path: '/leads/evidence' },
  { key: 'archive', path: '/leads/archive' },
  { key: 'scopedList', path: '/leads/scoped-list' }
]

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const dialogVisible = ref(false)
const editing = ref(false)
const form = reactive({
  id: 0, scope: 'account', scope_id: 0,
  keywords: '', semantic_keywords: '', system_description: '', enabled: true
})

function scopeLabel(scope) {
  // global 仅作历史数据展示，不参与解析，也不允许新建/编辑写入。
  return { global: '全局（历史）', channel: '渠道', account: '账号' }[scope] || scope
}

function scopeIdLabel(row) {
  if (row.scope === 'channel') return catalog.channelLabel(row.scope_id)
  if (row.scope === 'account') return catalog.accountLabel(row.scope_id)
  return '-'
}

function scopeTag(scope) {
  return { account: 'success', channel: 'warning' }[scope] || 'info'
}

function join(list) {
  return Array.isArray(list) ? list.join('、') : ''
}

function split(text) {
  return String(text || '').split(/[,，]/).map((s) => s.trim()).filter(Boolean)
}

function emptyForm() {
  Object.assign(form, {
    id: 0, scope: 'account', scope_id: 0,
    keywords: '', semantic_keywords: '', system_description: '', enabled: true
  })
}

async function fetchList() {
  loading.value = true
  try {
    const res = await leadsApi.listRules()
    rows.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res) ? res : [])
  } catch (e) {
    ElMessage.error(e?.message || '加载规则失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = false
  emptyForm()
  dialogVisible.value = true
}

function openEdit(row) {
  editing.value = true
  Object.assign(form, {
    id: row.id, scope: row.scope, scope_id: row.scope_id,
    keywords: join(row.keywords), semantic_keywords: join(row.semantic_keywords),
    system_description: row.system_description || '', enabled: !!row.enabled
  })
  dialogVisible.value = true
}

async function save() {
  if (!form.scope_id) {
    ElMessage.warning('channel/account 粒度必须填写范围ID')
    return
  }
  saving.value = true
  try {
    await leadsApi.saveRule({
      id: form.id, scope: form.scope, scope_id: form.scope_id,
      keywords: split(form.keywords), semantic_keywords: split(form.semantic_keywords),
      system_description: form.system_description, enabled: form.enabled
    })
    ElMessage.success('保存成功')
    dialogVisible.value = false
    fetchList()
  } catch (e) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确认删除规则 #${row.id}？`, '提示', { type: 'warning' })
    await leadsApi.deleteRule(row.id)
    ElMessage.success('已删除')
    fetchList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e?.message || '删除失败')
  }
}

onMounted(async () => {
  catalog.ensure()
  await fetchList()
})
</script>

<style scoped>
.leads-page { padding: 16px; }
.page-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.page-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
.mb12 { margin-bottom: 12px; }
</style>
