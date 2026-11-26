<template>
  <div class="lead-mining-agents-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          线索发现智能体
        </h2>
        <span class="page-sub">独立管理线索发现智能体，与自动回复智能体剥离（FR-026）</span>
      </div>
      <el-button
        type="primary"
        @click="openCreate"
      >
        新增智能体
      </el-button>
    </div>

    <el-card shadow="never">
      <el-form
        inline
        label-width="60px"
        @submit.prevent
      >
        <el-form-item label="关键词">
          <el-input
            v-model="keyword"
            placeholder="名称/说明"
            clearable
            style="width: 240px"
            @keyup.enter="fetchList"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            @click="fetchList"
          >
            查询
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="rows"
        border
        stripe
        empty-text="暂无线索发现智能体"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="90"
        />
        <el-table-column
          prop="name"
          label="名称"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          prop="description"
          label="说明"
          min-width="200"
          show-overflow-tooltip
        />
        <el-table-column
          prop="llm_model"
          label="模型"
          min-width="120"
        />
        <el-table-column
          label="绑定账号"
          min-width="180"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ boundAccountLabel(row.id) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            {{ Number(row.status) === 1 ? '启用' : '停用' }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="180"
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
      :title="editing ? '编辑线索发现智能体' : '新增线索发现智能体'"
      width="640px"
    >
      <el-form
        :model="form"
        label-width="100px"
      >
        <el-form-item label="名称">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.description" />
        </el-form-item>
        <el-form-item label="模式">
          <el-input v-model="form.agent_mode" />
        </el-form-item>
        <el-form-item label="人设">
          <el-input
            v-model="form.persona"
            type="textarea"
          />
        </el-form-item>
        <el-form-item label="系统提示词">
          <el-input
            v-model="form.system_prompt"
            type="textarea"
            :rows="4"
          />
        </el-form-item>
        <el-form-item label="模型">
          <el-input v-model="form.llm_model" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch
            v-model="form.status"
            :active-value="1"
            :inactive-value="0"
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
import { leadMiningAgentApi } from '@/api/leadMiningAgent'
import { channelAccountApi } from '@/api/channelAccount'

const boundAccounts = ref([])
const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const keyword = ref('')
const dialogVisible = ref(false)
const editing = ref(false)
const currentId = ref(0)
const form = reactive({
  name: '', description: '', agent_mode: '', persona: '', system_prompt: '', llm_model: '', status: 1,
})

function emptyForm() {
  Object.assign(form, { name: '', description: '', agent_mode: '', persona: '', system_prompt: '', llm_model: '', status: 1 })
}

function boundAccountLabel(agentID) {
  const names = boundAccounts.value
    .filter((a) => Number(a.lead_mining_agent_id) === Number(agentID))
    .map((a) => a.name)
  return names.length ? names.join('、') : '未绑定'
}

async function loadBoundAccounts() {
  try {
    const res = await channelAccountApi.listAccounts()
    boundAccounts.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res) ? res : [])
  } catch {
    boundAccounts.value = []
  }
}

async function fetchList() {
  loading.value = true
  try {
    const res = await leadMiningAgentApi.listAgents({ keyword: keyword.value || undefined })
    rows.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res) ? res : [])
    await loadBoundAccounts()
  } catch (e) {
    ElMessage.error(e?.message || '加载智能体失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = false
  currentId.value = 0
  emptyForm()
  dialogVisible.value = true
}

function openEdit(row) {
  editing.value = true
  currentId.value = row.id
  Object.assign(form, {
    name: row.name || '', description: row.description || '', agent_mode: row.agent_mode || '',
    persona: row.persona || '', system_prompt: row.system_prompt || '', llm_model: row.llm_model || '',
    status: Number(row.status ?? 1),
  })
  dialogVisible.value = true
}

async function save() {
  saving.value = true
  try {
    if (editing.value) {
      await leadMiningAgentApi.updateAgent(currentId.value, form)
    } else {
      await leadMiningAgentApi.createAgent(form)
    }
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
    await ElMessageBox.confirm('确认删除该线索发现智能体？', '操作确认', { type: 'warning' })
    await leadMiningAgentApi.deleteAgent(row.id)
    ElMessage.success('删除成功')
    fetchList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e?.message || '删除失败')
  }
}

onMounted(fetchList)
</script>

<style scoped>
.lead-mining-agents-page { padding: 16px; }
.page-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
</style>
