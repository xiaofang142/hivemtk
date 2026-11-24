<template>
  <div class="channel-account-manage">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>渠道账号管理</span>
          <el-button
            type="primary"
            size="small"
            @click="openCreate"
          >
            <el-icon><Plus /></el-icon>新增账号
          </el-button>
        </div>
      </template>

      <el-form
        :inline="true"
        :model="filter"
        size="small"
      >
        <el-form-item label="渠道">
          <el-select
            v-model="filter.channel_id"
            clearable
            placeholder="全部渠道"
            style="width: 180px"
          >
            <el-option
              v-for="c in channels"
              :key="c.id"
              :label="c.name"
              :value="c.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="关键字">
          <el-input
            v-model="filter.keyword"
            placeholder="账号名称/识别码"
            clearable
            @keyup.enter="loadList"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="loadList"
          >
            查询
          </el-button>
          <el-button @click="resetFilter">
            重置
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="accounts"
        stripe
        size="small"
        border
      >
        <el-table-column
          prop="id"
          label="ID"
          width="70"
          align="center"
        />
        <el-table-column
          prop="name"
          label="账号名称"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          label="渠道"
          width="130"
        >
          <template #default="{ row }">
            {{ channelName(row.channel_id) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="identify_code"
          label="识别码"
          min-width="150"
          show-overflow-tooltip
        />
        <el-table-column
          prop="type"
          label="类型"
          width="90"
        />
        <el-table-column
          label="自动回复"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.auto_reply_agent_id ? 'success' : 'info'"
              size="small"
            >
              {{ row.auto_reply_agent_id ? '启用' : '关闭' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="线索发现"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.lead_mining_agent_id ? 'success' : 'info'"
              size="small"
            >
              {{ row.lead_mining_agent_id ? '启用' : '关闭' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="80"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.status === 1 ? 'success' : 'info'"
              size="small"
            >
              {{ row.status === 1 ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="200"
          align="center"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="openEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="primary"
              size="small"
              @click="openCrossRef(row)"
            >
              交叉引用
            </el-button>
            <el-button
              link
              type="danger"
              size="small"
              @click="onDelete(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 账号编辑：两类智能体绑定统一在此配置（FR-024/025） -->
    <el-dialog
      v-model="dialogVisible"
      :title="editingId ? '编辑渠道账号' : '新增渠道账号'"
      width="680px"
      top="5vh"
    >
      <el-form
        :model="form"
        label-width="130px"
        size="small"
      >
        <el-form-item
          label="所属渠道"
          required
        >
          <el-select
            v-model="form.channel_id"
            placeholder="请选择渠道"
            style="width: 100%"
          >
            <el-option
              v-for="c in channels"
              :key="c.id"
              :label="c.name"
              :value="c.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="账号名称"
          required
        >
          <el-input
            v-model="form.name"
            placeholder="请输入账号名称"
          />
        </el-form-item>
        <el-form-item label="自他识别名称">
          <el-input
            v-model="form.self_identify_name"
            placeholder="用于单聊自他识别"
          />
        </el-form-item>
        <el-form-item label="会话类型">
          <el-select
            v-model="form.type"
            style="width: 100%"
          >
            <el-option
              label="单聊"
              value="dm"
            />
            <el-option
              label="群聊"
              value="group"
            />
            <el-option
              label="客服"
              value="cs"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.remark"
            type="textarea"
            :rows="2"
          />
        </el-form-item>
        <el-form-item label="负责人">
          <el-input
            v-model="form.ownerUserText"
            placeholder="多个用户 ID 用英文逗号分隔"
          />
        </el-form-item>
        <el-form-item label="数据授权">
          <el-input
            v-model="form.dataScope"
            type="textarea"
            :rows="2"
            placeholder='JSON 对象，如 {"visible_industries":[]}'
          />
        </el-form-item>
        <el-form-item label="账号状态">
          <el-switch
            v-model="form.statusOn"
            active-text="启用"
            inactive-text="停用"
          />
        </el-form-item>

        <el-divider content-position="left">
          自动回复智能体
        </el-divider>
        <el-form-item label="启用自动回复">
          <el-switch
            v-model="form.autoReplyOn"
            @change="onAutoReplyToggle"
          />
          <span class="hint">关闭后该账号不触发意图识别/拟人工回复，不影响线索发现</span>
        </el-form-item>
        <el-form-item
          v-if="form.autoReplyOn"
          label="绑定智能体"
        >
          <el-select
            v-model="form.auto_reply_agent_id"
            clearable
            placeholder="选择自动回复智能体"
            style="width: 100%"
          >
            <el-option
              v-for="a in autoReplyAgents"
              :key="a.id"
              :label="a.name"
              :value="a.id"
            />
          </el-select>
        </el-form-item>

        <el-divider content-position="left">
          线索发现智能体
        </el-divider>
        <el-form-item label="启用线索发现">
          <el-switch
            v-model="form.leadMiningOn"
            @change="onLeadMiningToggle"
          />
          <span class="hint">关闭后该账号不识别/归档商机，且不触发自动回复</span>
        </el-form-item>
        <el-form-item
          v-if="form.leadMiningOn"
          label="绑定智能体"
        >
          <el-select
            v-model="form.lead_mining_agent_id"
            clearable
            placeholder="选择线索发现智能体"
            style="width: 100%"
          >
            <el-option
              v-for="a in leadMiningAgents"
              :key="a.id"
              :label="a.name"
              :value="a.id"
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
          @click="onSave"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    <!-- T054：两类智能体交叉引用展示 -->
    <el-dialog
      v-model="crossRefVisible"
      title="共享上下文与交叉引用"
      width="760px"
      top="5vh"
    >
      <el-form
        :inline="true"
        size="small"
      >
        <el-form-item label="会话ID">
          <el-input
            v-model="crossRefConvId"
            placeholder="external_conversation_id"
            style="width: 300px"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="loadCrossRef"
          >
            查看
          </el-button>
        </el-form-item>
      </el-form>
      <el-descriptions
        v-if="crossRef"
        :column="2"
        border
        size="small"
      >
        <el-descriptions-item label="账号ID">
          {{ crossRef.channel_account_id }}
        </el-descriptions-item>
        <el-descriptions-item label="会话ID">
          {{ crossRef.conversation_id }}
        </el-descriptions-item>
        <el-descriptions-item
          label="共享标签"
          :span="2"
        >
          <el-tag
            v-for="(t, i) in crossRef.tags"
            :key="i"
            size="small"
            style="margin-right: 6px"
          >
            {{ t }}
          </el-tag>
          <span v-if="!crossRef.tags || !crossRef.tags.length">-</span>
        </el-descriptions-item>
        <el-descriptions-item
          label="摘要"
          :span="2"
        >
          {{ crossRef.summary || '-' }}
        </el-descriptions-item>
      </el-descriptions>
      <el-divider content-position="left">
        自动回复侧备注
      </el-divider>
      <div class="note-list">
        <el-tag
          v-for="(n, i) in crossRef?.auto_reply_notes || []"
          :key="i"
          type="warning"
          size="small"
        >
          {{ n }}
        </el-tag>
        <span v-if="!crossRef?.auto_reply_notes?.length">暂无</span>
      </div>
      <el-divider content-position="left">
        线索发现侧备注
      </el-divider>
      <div class="note-list">
        <el-tag
          v-for="(n, i) in crossRef?.lead_mining_notes || []"
          :key="i"
          type="success"
          size="small"
        >
          {{ n }}
        </el-tag>
        <span v-if="!crossRef?.lead_mining_notes?.length">暂无</span>
      </div>
      <el-divider content-position="left">
        群成员角色（多成员聚合）
      </el-divider>
      <div class="note-list">
        <el-tag
          v-for="(role, sid, i) in crossRef?.member_roles || {}"
          :key="i"
          size="small"
        >
          {{ sid }}: {{ role }}
        </el-tag>
        <span v-if="!crossRef || !Object.keys(crossRef.member_roles || {}).length">暂无</span>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import channelAccountApi from '@/api/channelAccount.js'
import { listAgents } from '@/api/aiAgent.js'

const loading = ref(false)
const saving = ref(false)
const accounts = ref([])
const channels = ref([])
const autoReplyAgents = ref([])
const leadMiningAgents = ref([])
const filter = reactive({ channel_id: null, keyword: '' })

const dialogVisible = ref(false)
const editingId = ref(null)
const form = reactive({
  channel_id: null,
  name: '',
  remark: '',
  self_identify_name: '',
  type: 'dm',
  statusOn: true,
  autoReplyOn: false,
  leadMiningOn: false,
  auto_reply_agent_id: null,
  lead_mining_agent_id: null,
  ownerUserText: '',
  dataScope: ''
})

const crossRefVisible = ref(false)
const crossRefAccount = ref(null)
const crossRefConvId = ref('')
const crossRef = ref(null)

function channelName(id) {
  const c = channels.value.find((x) => x.id === id)
  return c ? c.name : '-'
}

async function loadList() {
  loading.value = true
  try {
    const res = await channelAccountApi.listAccounts({ channel_id: filter.channel_id || undefined, keyword: filter.keyword })
    accounts.value = res?.list || res || []
  } catch (e) {
    ElMessage.error('加载渠道账号失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

function resetFilter() {
  filter.channel_id = null
  filter.keyword = ''
  loadList()
}

function resetForm() {
  Object.assign(form, {
    channel_id: null, name: '', remark: '', self_identify_name: '', type: 'dm',
    statusOn: true, autoReplyOn: false, leadMiningOn: false,
    auto_reply_agent_id: null, lead_mining_agent_id: null,
    ownerUserText: '', dataScope: ''
  })
}

function openCreate() {
  editingId.value = null
  resetForm()
  dialogVisible.value = true
}

async function openEdit(row) {
  editingId.value = row.id
  Object.assign(form, {
    channel_id: row.channel_id,
    name: row.name,
    remark: row.remark || '',
    self_identify_name: row.self_identify_name || '',
    type: row.type || 'dm',
    statusOn: row.status === 1,
    autoReplyOn: !!row.auto_reply_agent_id,
    leadMiningOn: !!row.lead_mining_agent_id,
    auto_reply_agent_id: row.auto_reply_agent_id || null,
    lead_mining_agent_id: row.lead_mining_agent_id || null,
    ownerUserText: '', dataScope: ''
  })
  dialogVisible.value = true
  // 列表来自 ListOptions，不含负责人/数据授权；用 GetDetail 回填全字段（T056）。
  try {
    const detail = await channelAccountApi.getAccount(row.id)
    if (detail) {
      form.ownerUserText = (detail.owner_user_ids || []).join(',')
      form.dataScope = detail.data_scope || ''
    }
  } catch (e) {
    ElMessage.warning('加载账号完整信息失败：' + (e.message || '未知错误'))
  }
}

function onAutoReplyToggle(on) {
  if (!on) form.auto_reply_agent_id = null
}

function onLeadMiningToggle(on) {
  if (!on) form.lead_mining_agent_id = null
}

async function onSave() {
  if (!form.channel_id || !form.name) {
    ElMessage.warning('渠道与账号名称必填')
    return
  }
  saving.value = true
  const payload = {
    channel_id: form.channel_id,
    name: form.name,
    remark: form.remark,
    self_identify_name: form.self_identify_name,
    type: form.type,
    status: form.statusOn ? 1 : 0,
    auto_reply_agent_id: form.autoReplyOn ? form.auto_reply_agent_id : null,
    lead_mining_agent_id: form.leadMiningOn ? form.lead_mining_agent_id : null,
    owner_user_ids: form.ownerUserText.split(',').map((s) => s.trim()).filter(Boolean),
    data_scope: form.dataScope || ''
  }
  try {
    if (editingId.value) {
      await channelAccountApi.updateAccount(editingId.value, payload)
    } else {
      await channelAccountApi.createAccount(payload)
    }
    ElMessage.success('保存成功')
    dialogVisible.value = false
    loadList()
  } catch (e) {
    ElMessage.error('保存失败：' + (e.message || '未知错误'))
  } finally {
    saving.value = false
  }
}

async function onDelete(row) {
  try {
    await ElMessageBox.confirm(`确认删除渠道账号「${row.name}」？`, '提示', { type: 'warning' })
    await channelAccountApi.deleteAccount(row.id)
    ElMessage.success('删除成功')
    loadList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败：' + (e.message || '未知错误'))
  }
}

function openCrossRef(row) {
  crossRefAccount.value = row
  crossRefConvId.value = ''
  crossRef.value = null
  crossRefVisible.value = true
}

async function loadCrossRef() {
  if (!crossRefConvId.value) {
    ElMessage.warning('请输入会话ID')
    return
  }
  try {
    const res = await channelAccountApi.conversationContext({
      channel_account_id: crossRefAccount.value?.id,
      conversation_id: crossRefConvId.value
    })
    crossRef.value = res?.data || res || null
  } catch (e) {
    ElMessage.error('加载交叉引用失败：' + (e.message || '未知错误'))
  }
}

onMounted(async () => {
  loadList()
  try {
    const c = await channelAccountApi.listChannels()
    channels.value = c?.list || c || []
    const agents = await listAgents({})
    autoReplyAgents.value = (agents?.list || agents || []).filter((a) => a.agent_type !== 'lead_mining')
    const lm = await channelAccountApi.listLeadMiningAgents()
    leadMiningAgents.value = lm?.list || lm || []
  } catch {
    // 配置下拉加载失败不阻断账号列表，逐项 warn 由具体接口处理。
  }
})
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.hint { margin-left: 12px; color: #909399; font-size: 12px; }
.note-list { display: flex; flex-wrap: wrap; gap: 6px; min-height: 24px; }
</style>
