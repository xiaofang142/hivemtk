<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          业务账号授权
        </h2>
        <span class="page-sub">管理用户对业务账号的读/写/管理授权（PR-5）</span>
      </div>
      <div class="head-actions">
        <el-input
          v-model="filters.userId"
          placeholder="按用户ID筛选"
          clearable
          style="width: 180px"
          @clear="load"
          @keyup.enter="load"
        />
        <el-button
          :loading="loading"
          @click="load"
        >
          刷新
        </el-button>
        <el-button
          type="primary"
          @click="openCreate"
        >
          新增授权
        </el-button>
      </div>
    </div>

    <el-card shadow="never">
      <el-table
        v-loading="loading"
        :data="rows"
        empty-text="暂无授权记录"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="user_id"
          label="用户ID"
          width="100"
        />
        <el-table-column
          prop="channel"
          label="渠道"
          min-width="120"
        />
        <el-table-column
          prop="account_id"
          label="账号ID"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          prop="scope"
          label="权限"
          width="120"
        >
          <template #default="{ row }">
            <el-tag :type="scopeTag(row.scope)">
              {{ scopeLabel(row.scope) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="创建时间"
          min-width="170"
        >
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="120"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="danger"
              @click="confirmRemove(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      title="新增/更新授权"
      width="520px"
      destroy-on-close
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="96px"
      >
        <el-form-item
          label="用户ID"
          prop="user_id"
        >
          <el-input-number
            v-model="form.user_id"
            :min="1"
            :controls="false"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item
          label="渠道"
          prop="channel"
        >
          <el-input
            v-model="form.channel"
            placeholder="如 wechat / douyin / whatsapp"
          />
        </el-form-item>
        <el-form-item
          label="账号ID"
          prop="account_id"
        >
          <el-input
            v-model="form.account_id"
            placeholder="业务账号ID"
          />
        </el-form-item>
        <el-form-item
          label="权限范围"
          prop="scope"
        >
          <el-select
            v-model="form.scope"
            style="width: 100%"
          >
            <el-option
              label="只读 (read)"
              value="read"
            />
            <el-option
              label="读写 (write)"
              value="write"
            />
            <el-option
              label="管理 (manage)"
              value="manage"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="submit"
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
import { businessAccountAuthApi } from '@/api/businessAccountAuth'

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const dialogVisible = ref(false)
const formRef = ref()
const filters = reactive({ userId: '' })
const form = reactive({
  user_id: undefined,
  channel: '',
  account_id: '',
  scope: 'read',
})

const rules = {
  user_id: [{ required: true, message: '请输入用户ID', trigger: 'blur' }],
  channel: [{ required: true, message: '请输入渠道', trigger: 'blur' }],
  account_id: [{ required: true, message: '请输入账号ID', trigger: 'blur' }],
  scope: [{ required: true, message: '请选择权限范围', trigger: 'change' }],
}

function fmtTime(v) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  return d.toLocaleString('zh-CN', { hour12: false })
}

function scopeLabel(scope) {
  const map = { read: '只读', write: '读写', manage: '管理' }
  return map[scope] || scope || '-'
}

function scopeTag(scope) {
  const map = { read: 'info', write: 'warning', manage: 'success' }
  return map[scope] || 'info'
}

async function load() {
  loading.value = true
  try {
    const params = {}
    if (filters.userId) params.user_id = filters.userId
    const res = await businessAccountAuthApi.list(params)
    rows.value = Array.isArray(res?.list) ? res.list : Array.isArray(res) ? res : []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, { user_id: undefined, channel: '', account_id: '', scope: 'read' })
  dialogVisible.value = true
}

async function submit() {
  await formRef.value?.validate()
  saving.value = true
  try {
    await businessAccountAuthApi.upsert({
      user_id: Number(form.user_id),
      channel: form.channel.trim(),
      account_id: form.account_id.trim(),
      scope: form.scope,
    })
    ElMessage.success('保存成功')
    dialogVisible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function confirmRemove(row) {
  await ElMessageBox.confirm(`确认删除授权 #${row.id} (${row.channel}/${row.account_id})？`, '删除确认', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  })
  await businessAccountAuthApi.remove(row.id)
  ElMessage.success('删除成功')
  await load()
}

onMounted(load)
</script>

<style scoped>
.page { padding: 16px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; gap: 12px; }
.page-title { margin: 0; font-size: 20px; font-weight: 600; }
.page-sub { color: var(--el-text-color-secondary); font-size: 13px; }
.head-actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
</style>
