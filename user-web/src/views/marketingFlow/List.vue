<template>
  <div class="marketing-flow-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('营销流程') }}</h2>
        <p class="subtitle">
          {{ $t('可视化编排营销自动化流程') }}
        </p>
      </div>
      <el-button
        type="primary"
        @click="showCreateDialog"
      >
        <el-icon><Plus /></el-icon>
        {{ $t('新建流程') }}
      </el-button>
    </el-card>

    <el-card>
      <el-table
        v-loading="loading"
        :data="flows"
        stripe
      >
        <el-table-column
          prop="name"
          :label="$t('流程名称')"
          min-width="150"
        />
        <el-table-column
          label="触发条件"
          min-width="150"
        >
          <template #default="{ row }">
            {{ triggerLabel(row.trigger_type) }}
          </template>
        </el-table-column>
        <el-table-column
          label="步骤数"
          width="90"
        >
          <template #default="{ row }">
            {{ stepCountOf(row) }}
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="statusTagType(row.status)"
              size="small"
            >
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="版本"
          width="80"
        >
          <template #default="{ row }">
            v{{ row.version || 1 }}
          </template>
        </el-table-column>
        <el-table-column
          label="更新时间"
          width="180"
        >
          <template #default="{ row }">
            {{ fmtTime(row.updated_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="280"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="editFlow(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="primary"
              @click="toggleFlow(row)"
            >
              {{ row.status === 'active' ? '暂停' : '启用' }}
            </el-button>
            <el-button
              link
              type="primary"
              @click="runFlow(row)"
            >
              执行
            </el-button>
            <el-button
              link
              type="primary"
              @click="viewLogs(row)"
            >
              日志
            </el-button>
            <el-button
              link
              type="danger"
              @click="deleteFlow(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无数据" />
        </template>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="900px"
      :close-on-click-modal="false"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-width="100px"
      >
        <el-form-item
          label="流程名称"
          prop="name"
        >
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="form.description"
            placeholder="选填，说明这条流程做什么"
          />
        </el-form-item>
        <el-form-item
          label="触发条件"
          prop="trigger_type"
        >
          <el-select
            v-model="form.trigger_type"
            style="width: 100%"
          >
            <el-option
              v-for="opt in TRIGGER_OPTIONS"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="流程步骤">
          <div class="flow-builder">
            <div
              v-for="(step, idx) in form.steps"
              :key="idx"
              class="flow-step"
            >
              <div class="step-header">
                <el-tag>步骤 {{ idx + 1 }}</el-tag>
                <el-button
                  v-if="form.steps.length > 1"
                  link
                  type="danger"
                  @click="removeStep(idx)"
                >
                  删除
                </el-button>
              </div>
              <el-form-item
                label="动作"
                :label-width="'80px'"
              >
                <el-select
                  v-model="step.action"
                  style="width: 100%"
                >
                  <el-option
                    v-for="opt in ACTION_OPTIONS"
                    :key="opt.value"
                    :label="opt.label"
                    :value="opt.value"
                  />
                </el-select>
              </el-form-item>
              <el-form-item
                label="延迟(小时)"
                :label-width="'80px'"
              >
                <el-input-number
                  v-model="step.delay_hours"
                  :min="0"
                  :max="720"
                />
              </el-form-item>
              <el-form-item
                label="参数"
                :label-width="'80px'"
              >
                <el-input
                  v-model="step.params"
                  type="textarea"
                  :rows="2"
                  placeholder="{&quot;template&quot;: &quot;welcome&quot;}"
                />
              </el-form-item>
            </div>
            <el-button
              type="primary"
              link
              @click="addStep"
            >
              添加步骤
            </el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitForm"
        >
          确定
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="logDialogVisible"
      title="执行日志"
      width="900px"
    >
      <div class="log-header">
        <span>流程：{{ currentFlowName }}</span>
        <span>共 {{ executionLogs.length }} 条日志</span>
      </div>
      <el-table
        :data="executionLogs"
        stripe
      >
        <el-table-column
          prop="started_at"
          label="执行时间"
          width="180"
        >
          <template #default="{ row }">
            {{ fmtTime(row.started_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="logStatusType(row.status)">
              {{ logStatusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="current_node"
          label="步骤"
          min-width="150"
        />
        <el-table-column
          label="耗时"
          width="120"
        >
          <template #default="{ row }">
            {{ calcDuration(row.started_at, row.completed_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="error_message"
          label="错误信息"
          min-width="200"
        >
          <template #default="{ row }">
            <span :style="{ color: row.error_message ? '#EF4444' : '' }">{{ row.error_message }}</span>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  getFlows,
  createFlow,
  updateFlow,
  deleteFlow as deleteFlowApi,
  runFlow as runFlowApi,
  toggleFlow as toggleFlowApi,
  getFlowLogs
} from '@/api/marketingFlow.js'
import { toList } from '@/utils/list.js'
import { fmtTime } from '@/utils/time'
import { isUserReportedError } from '@/utils/errorReporting'

const loading = ref(false)
const flows = ref([])
const dialogVisible = ref(false)
const dialogTitle = ref('新建流程')
const logDialogVisible = ref(false)
const currentFlowName = ref('')
const executionLogs = ref([])
const formRef = ref()
// 后端枚举（internal/content/model/marketing_flow.go）：前端必须用这些值，否则必填校验/节点校验直接 400
const TRIGGER_OPTIONS = [
  { value: 'user_follow', label: '用户关注' },
  { value: 'message_receive', label: '收到消息' },
  { value: 'lead_create', label: '线索创建' },
  { value: 'lead_status_change', label: '线索状态变更' },
  { value: 'timer', label: '定时触发' },
  { value: 'tag_change', label: '标签变更' },
  { value: 'order_create', label: '订单创建' },
  { value: 'order_pay', label: '订单支付' }
]
const ACTION_OPTIONS = [
  { value: 'send_email', label: '发送邮件' },
  { value: 'send_sms', label: '发送短信' },
  { value: 'send_message', label: '发送消息' },
  { value: 'add_tag', label: '添加标签' },
  { value: 'remove_tag', label: '移除标签' },
  { value: 'assign_agent', label: '分配坐席' },
  { value: 'create_task', label: '创建任务' },
  { value: 'update_lead', label: '更新线索' },
  { value: 'create_order', label: '创建订单' },
  { value: 'webhook', label: '调用 Webhook' }
]
const STATUS_META = {
  draft: { label: '草稿', type: 'info' },
  active: { label: '启用', type: 'success' },
  paused: { label: '暂停', type: 'warning' },
  inactive: { label: '停用', type: 'info' }
}

const triggerLabel = (v) => TRIGGER_OPTIONS.find((o) => o.value === v)?.label || v || '-'
const statusLabel = (v) => STATUS_META[v]?.label || v || '-'
const statusTagType = (v) => STATUS_META[v]?.type || 'info'

// flow_data 是 JSON 字符串（{nodes:[...]}），列表只展示节点数（含触发器节点）
const parseNodes = (raw) => {
  if (!raw) return []
  try {
    const def = typeof raw === 'string' ? JSON.parse(raw) : raw
    return Array.isArray(def?.nodes) ? def.nodes : []
  } catch {
    return []
  }
}
const stepCountOf = (row) => parseNodes(row.flow_data).filter((n) => n.type !== 'trigger').length || '-'

const emptyStep = () => ({ action: 'send_email', delay_hours: 0, params: '' })
const form = ref({
  id: 0,
  name: '',
  description: '',
  trigger_type: 'lead_create',
  steps: [emptyStep()]
})

// 把表单拼成后端 CreateFlowRequest/UpdateFlowRequest 的结构：
// flow_data 必须至少有一个 type=trigger 的节点，动作节点用 next_nodes 串成链
const buildPayload = () => {
  // 执行器（internal/content/service/marketing_flow.go executeNode）识别的节点类型只有
  // trigger / action / condition / delay；动作类型读的是 config.action_type，
  // 延迟是独立的 delay 节点、config.duration 以秒为单位。
  const steps = form.value.steps || []
  const nodes = [{
    id: 'trigger',
    type: 'trigger',
    name: '触发器',
    config: { trigger_type: form.value.trigger_type },
    next_nodes: []
  }]
  const chain = []
  steps.forEach((s, idx) => {
    const delayHours = Number(s.delay_hours || 0)
    if (delayHours > 0) {
      const delayId = `delay-${idx + 1}`
      nodes.push({
        id: delayId,
        type: 'delay',
        name: `等待 ${delayHours} 小时`,
        config: { duration: delayHours * 3600 },
        next_nodes: []
      })
      chain.push(delayId)
    }
    const actionId = `action-${idx + 1}`
    nodes.push({
      id: actionId,
      type: 'action',
      name: `动作 ${idx + 1}`,
      config: { action_type: s.action, delay_hours: delayHours, params: s.params || '' },
      next_nodes: []
    })
    chain.push(actionId)
  })
  // 顺序串联：trigger → (delay → action) → ...
  const order = ['trigger', ...chain]
  order.forEach((nodeId, i) => {
    const node = nodes.find((n) => n.id === nodeId)
    if (node) node.next_nodes = i < order.length - 1 ? [order[i + 1]] : []
  })
  return {
    name: form.value.name,
    description: form.value.description || '',
    trigger_type: form.value.trigger_type,
    trigger_config: {},
    flow_data: { nodes }
  }
}

const refreshData = async () => {
  loading.value = true
  try {
    const res = await getFlows()
    flows.value = toList(res)
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('获取流程列表失败'))
    flows.value = []
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  form.value = { id: 0, name: '', description: '', trigger_type: 'lead_create', steps: [emptyStep()] }
  dialogTitle.value = '新建流程'
  dialogVisible.value = true
}

const editFlow = (row) => {
  // 列表行是后端模型（flow_data 为字符串），回显时要还原成步骤数组
  const nodes = parseNodes(row.flow_data).filter((n) => n.type === 'action')
  form.value = {
    id: row.id,
    name: row.name || '',
    description: row.description || '',
    trigger_type: row.trigger_type || 'lead_create',
    steps: nodes.length
      ? nodes.map((n) => ({
        action: n.config?.action_type || n.config?.action || 'send_email',
        delay_hours: Number(n.config?.delay_hours || 0),
        params: n.config?.params || ''
      }))
      : [emptyStep()]
  }
  dialogTitle.value = '编辑流程'
  dialogVisible.value = true
}

const addStep = () => {
  form.value.steps.push(emptyStep())
}

const removeStep = (idx) => {
  form.value.steps.splice(idx, 1)
}

const submitForm = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    try {
      const payload = buildPayload()
      if (form.value.id) {
        await updateFlow(form.value.id, payload)
      } else {
        await createFlow(payload)
      }
      ElMessage.success(i18n.global.t('保存成功'))
      dialogVisible.value = false
      refreshData()
    } catch (error) {
      if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('保存失败'))
    }
  })
}

const runFlow = async (row) => {
  try {
    await runFlowApi(row.id)
    ElMessage.success(i18n.global.t('流程已执行'))
    refreshData()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('执行失败'))
  }
}

const toggleFlow = async (row) => {
  const next = row.status !== 'active'
  try {
    await toggleFlowApi(row.id, next)
    ElMessage.success(next ? '流程已启用' : '流程已暂停')
    refreshData()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('状态更新失败'))
  }
}

const viewLogs = async (row) => {
  try {
    const res = await getFlowLogs(row.id)
    executionLogs.value = res.list || []
    currentFlowName.value = row.name
    logDialogVisible.value = true
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('获取日志失败'))
  }
}

const deleteFlow = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除流程 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteFlowApi(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    refreshData()
  } catch (e) {
    if (e === 'cancel' || e === 'close' || isUserReportedError(e)) return
    ElMessage.error(i18n.global.t('删除失败'))
  }
}

const logStatusType = (status) => {
  const map = { running: 'warning', completed: 'success', failed: 'danger', cancelled: 'info' }
  return map[status]}
const logStatusText = (status) => {
  const map = { running: '运行中', completed: '已完成', failed: '失败', cancelled: '已取消' }
  return map[status] || status
}
const calcDuration = (start, end) => {
  if (!start || !end) return '-'
  const diff = new Date(end).getTime() - new Date(start).getTime()
  if (diff < 1000) return `${diff}ms`
  if (diff < 60000) return `${(diff / 1000).toFixed(1)}s`
  return `${Math.floor(diff / 60000)}m ${Math.floor((diff % 60000) / 1000)}s`
}

onMounted(() => refreshData())
</script>

<style scoped lang="scss">
.marketing-flow-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.flow-builder {
  width: 100%;
  .flow-step {
    background: #f5f7fa;
    padding: 15px;
    border-radius: 4px;
    margin-bottom: 10px;
    .step-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 10px;
    }
  }
}
.log-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 0;
  margin-bottom: 10px;
  color: #606266;
  font-size: 14px;
}
</style>
