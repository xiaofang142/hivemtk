<template>
  <div class="leads-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          线索通知路由
        </h2>
        <span class="page-sub">优先级：账号维护人 → 渠道维护人 → 租户默认；送达记录按平台业务码判定</span>
      </div>
      <el-button
        type="primary"
        @click="openCreate"
      >
        新增路由
      </el-button>
    </div>

    <el-card shadow="never">
      <el-table
        v-loading="loading"
        :data="rows"
        border
        stripe
        empty-text="暂无通知路由"
      >
        <el-table-column
          prop="route_id"
          label="RouteID"
          min-width="150"
        />
        <el-table-column
          prop="scope"
          label="层级"
          width="90"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ scopeLabel(row.scope) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="channel_account_id"
          label="账号ID"
          width="90"
        />
        <el-table-column
          prop="channel"
          label="渠道"
          width="110"
        />
        <el-table-column
          label="通知人"
          min-width="140"
        >
          <template #default="{ row }">
            {{ join(row.notify_user_ids) }}
          </template>
        </el-table-column>
        <el-table-column
          label="通道"
          min-width="140"
        >
          <template #default="{ row }">
            {{ join(row.notify_types) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="target"
          label="外部目标"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          label="操作"
          width="90"
        >
          <template #default="{ row }">
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
      title="新增通知路由"
      width="560px"
    >
      <el-form
        label-width="110px"
        @submit.prevent
      >
        <el-form-item label="层级">
          <el-select v-model="form.scope">
            <el-option
              label="账号维护人"
              value="account"
            />
            <el-option
              label="渠道维护人"
              value="channel"
            />
            <el-option
              label="租户默认"
              value="tenant"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="渠道账号ID">
          <el-input
            v-model.number="form.channel_account_id"
            placeholder="account 层级必填"
          />
        </el-form-item>
        <el-form-item label="渠道">
          <el-input
            v-model="form.channel"
            placeholder="channel 层级必填"
          />
        </el-form-item>
        <el-form-item label="通知人ID">
          <el-input
            v-model="form.notify_user_ids"
            placeholder="多个用逗号分隔"
          />
        </el-form-item>
        <el-form-item label="通知通道">
          <el-checkbox-group v-model="form.notify_types">
            <el-checkbox label="in_app">
              站内
            </el-checkbox>
            <el-checkbox label="telegram">
              TG
            </el-checkbox>
            <el-checkbox label="feishu">
              飞书
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="外部目标">
          <el-input
            v-model="form.target"
            placeholder="TG/飞书：accountID:chatID[:receiveIDType]"
          />
        </el-form-item>
        <el-form-item label="优先级">
          <el-input
            v-model.number="form.priority"
            placeholder="数字越小越优先"
          />
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

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const dialogVisible = ref(false)
const form = reactive({
  scope: 'account', channel_account_id: 0, channel: '',
  notify_user_ids: '', notify_types: ['in_app'], target: '', priority: 0
})

function scopeLabel(scope) {
  return { account: '账号', channel: '渠道', tenant: '租户' }[scope] || '旧路由'
}

function join(list) {
  return Array.isArray(list) ? list.join('、') : ''
}

function split(text) {
  return String(text || '').split(/[,，]/).map((s) => s.trim()).filter(Boolean)
}

function emptyForm() {
  Object.assign(form, {
    scope: 'account', channel_account_id: 0, channel: '',
    notify_user_ids: '', notify_types: ['in_app'], target: '', priority: 0
  })
}

async function fetchList() {
  loading.value = true
  try {
    const res = await leadsApi.listNotifyRoutes()
    rows.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res) ? res : [])
  } catch (e) {
    ElMessage.error(e?.message || '加载通知路由失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  emptyForm()
  dialogVisible.value = true
}

async function save() {
  saving.value = true
  try {
    await leadsApi.createNotifyRoute({
      scope: form.scope, channel_account_id: form.channel_account_id,
      channel: form.channel, account_id: form.channel ? '' : String(form.channel_account_id || ''),
      notify_user_ids: split(form.notify_user_ids), notify_types: form.notify_types,
      target: form.target, priority: form.priority, enabled: true
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
    await ElMessageBox.confirm(`确认删除路由 ${row.route_id}？`, '提示', { type: 'warning' })
    await leadsApi.deleteNotifyRoute(row.route_id)
    ElMessage.success('已删除')
    fetchList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e?.message || '删除失败')
  }
}

onMounted(fetchList)
</script>

<style scoped>
.leads-page { padding: 16px; }
.page-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
</style>
