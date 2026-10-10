<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>Skill 注册表</span>
          <el-button type="primary" size="small" @click="openCreate">登记 Skill</el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="一个 Skill = 一份给外部 AI Agent 的能力说明（SKILL.md）+ 一把绑定凭证 + 可用工具集。登记后可审阅、可吊销；保存时工具集同步写进绑定凭证的授权范围。"
        style="margin-bottom: 12px"
      />

      <el-table v-loading="loading" :data="rows" border size="small">
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column prop="description" label="说明" min-width="200" show-overflow-tooltip />
        <el-table-column label="绑定凭证" width="140">
          <template #default="{ row }">
            <span v-if="row.credential_id">{{ '凭证 #' + row.credential_id }}</span>
            <el-tag v-else type="info" size="small">未绑定</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="工具集" min-width="180">
          <template #default="{ row }">
            <el-tag v-for="t in (row.allowed_tools || '').split(',').filter(Boolean)" :key="t" size="small" style="margin-right: 4px">
              {{ t }}
            </el-tag>
            <span v-if="!row.allowed_tools" class="muted">不限制</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" @click="toggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 登记 / 编辑弹窗 -->
    <el-dialog v-model="editorVisible" :title="editing ? '编辑 Skill' : '登记 Skill'" width="560px">
      <el-form label-width="90px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="如：hivemtk-sales" maxlength="128" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.description" type="textarea" :rows="2" placeholder="给人和 Agent 识别用途（对应 SKILL.md 的 description）" />
        </el-form-item>
        <el-form-item label="绑定凭证">
          <el-select v-model="form.credential_id" clearable placeholder="选择已签发凭证" style="width: 100%">
            <el-option v-for="c in credentials" :key="c.id" :label="c.client_id" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="工具集">
          <el-input v-model="form.allowed_tools" placeholder="逗号分隔，如：customer.read,reach.send（留空=不限制）" />
        </el-form-item>
        <el-form-item label="文档地址">
          <el-input v-model="form.doc_url" placeholder="SKILL.md 位置（仓库路径或文档地址）" />
        </el-form-item>
        <el-form-item label="红线提示">
          <el-input v-model="form.guardrails" type="textarea" :rows="2" placeholder="如：reach.* 外发前必须人工确认" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editorVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { mcpSkillApi, mcpCredentialApi } from '@/api/mcpCredential'

const rows = ref([])
const credentials = ref([])
const loading = ref(false)
const saving = ref(false)
const editorVisible = ref(false)
const editing = ref(false)
const form = ref({})

const load = async () => {
  loading.value = true
  try {
    const res = await mcpSkillApi.list()
    rows.value = res?.data?.list ?? res?.list ?? []
  } finally {
    loading.value = false
  }
}

const loadCredentials = async () => {
  try {
    const res = await mcpCredentialApi.list()
    credentials.value = res?.data ?? res ?? []
  } catch { /* 凭证列表加载失败不阻塞 Skill 页 */ }
}

const openCreate = () => {
  editing.value = false
  form.value = { name: '', description: '', credential_id: undefined, allowed_tools: '', doc_url: '', guardrails: '' }
  editorVisible.value = true
}

const openEdit = (row) => {
  editing.value = true
  form.value = { ...row, credential_id: row.credential_id ?? undefined }
  editorVisible.value = true
}

const save = async () => {
  if (!form.value.name) {
    ElMessage.warning('请填写名称')
    return
  }
  saving.value = true
  try {
    const payload = {
      name: form.value.name,
      description: form.value.description,
      credential_id: form.value.credential_id || undefined,
      allowed_tools: form.value.allowed_tools || '',
      doc_url: form.value.doc_url || '',
      guardrails: form.value.guardrails || ''
    }
    if (editing.value) {
      await mcpSkillApi.update(form.value.id, payload)
      ElMessage.success('已更新')
    } else {
      await mcpSkillApi.create(payload)
      ElMessage.success('已登记')
    }
    editorVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

const toggle = async (row) => {
  await mcpSkillApi.update(row.id, { enabled: !row.enabled })
  ElMessage.success(row.enabled ? '已停用' : '已启用')
  await load()
}

const remove = async (row) => {
  await ElMessageBox.confirm(`确认删除 Skill ${row.name}？`, '删除确认', { type: 'warning' })
  await mcpSkillApi.remove(row.id)
  ElMessage.success('已删除')
  await load()
}

onMounted(() => {
  load()
  loadCredentials()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.muted {
  color: #999;
  font-size: 12px;
}
</style>