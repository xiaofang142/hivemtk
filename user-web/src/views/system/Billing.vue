<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>计费管理</span>
        </div>
      </template>

      <el-tabs v-model="tab">
        <!-- 计费方案 CRUD -->
        <el-tab-pane label="计费方案" name="plans">
          <div style="margin-bottom: 12px">
            <el-button type="primary" size="small" @click="openPlanDialog()">新建方案</el-button>
          </div>
          <el-table v-loading="plansLoading" :data="plans" border size="small">
            <el-table-column prop="code" label="编码" width="110" />
            <el-table-column prop="name" label="名称" width="120" />
            <el-table-column label="月付" width="100">
              <template #default="{ row }">¥{{ (row.price_cents / 100).toFixed(2) }}</template>
            </el-table-column>
            <el-table-column label="年付" width="110">
              <template #default="{ row }">¥{{ (row.yearly_price_cents / 100).toFixed(2) }}</template>
            </el-table-column>
            <el-table-column label="成员上限" width="90">
              <template #default="{ row }">{{ row.max_users || '不限' }}</template>
            </el-table-column>
            <el-table-column label="月消息量" width="100">
              <template #default="{ row }">{{ row.max_messages_monthly || '不限' }}</template>
            </el-table-column>
            <el-table-column label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
                  {{ row.status === 'active' ? '在售' : '下架' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="remark" label="备注" min-width="160" show-overflow-tooltip />
            <el-table-column label="操作" width="150" fixed="right">
              <template #default="{ row }">
                <el-button size="small" @click="openPlanDialog(row)">编辑</el-button>
                <el-button size="small" type="danger" @click="removePlan(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- 团队订阅 -->
        <el-tab-pane label="团队订阅" name="subs">
          <el-table v-loading="subsLoading" :data="subs" border size="small">
            <el-table-column label="团队" width="140">
              <template #default="{ row }">{{ teamName(row.team_id) }}</template>
            </el-table-column>
            <el-table-column prop="plan_code" label="方案" width="110" />
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
                  {{ row.status === 'active' ? '生效中' : '已停用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="起" width="160">
              <template #default="{ row }">{{ fmtTime(row.starts_at) }}</template>
            </el-table-column>
            <el-table-column label="止" width="160">
              <template #default="{ row }">{{ fmtTime(row.ends_at) }}</template>
            </el-table-column>
            <el-table-column label="自动续费" width="90">
              <template #default="{ row }">{{ row.auto_renew ? '是' : '否' }}</template>
            </el-table-column>
            <el-table-column label="操作" width="220" fixed="right">
              <template #default="{ row }">
                <el-button size="small" @click="openAssign(row)">续期/换方案</el-button>
                <el-button v-if="row.status === 'active'" size="small" type="warning" plain @click="cancelSub(row)">停用</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div style="margin-top: 12px">
            <el-button type="primary" size="small" @click="openAssign(null)">为团队指派方案</el-button>
          </div>
        </el-tab-pane>

        <!-- 账单记录 -->
        <el-tab-pane label="账单记录" name="records">
          <div style="margin-bottom: 12px; display: flex; gap: 8px; align-items: center">
            <el-select v-model="recordTeamId" placeholder="全部团队" clearable size="small" style="width: 180px" @change="loadRecords">
              <el-option v-for="t in teams" :key="t.id" :label="t.name" :value="t.id" />
            </el-select>
            <el-select v-model="recordStatus" placeholder="全部状态" clearable size="small" style="width: 140px" @change="loadRecords">
              <el-option label="应收" value="pending" />
              <el-option label="已收" value="paid" />
              <el-option label="作废" value="void" />
            </el-select>
            <el-button type="primary" size="small" @click="openRecordDialog">新建应收账单</el-button>
          </div>
          <el-table v-loading="recordsLoading" :data="records" border size="small">
            <el-table-column label="团队" width="140">
              <template #default="{ row }">{{ teamName(row.team_id) }}</template>
            </el-table-column>
            <el-table-column prop="plan_code" label="方案" width="110" />
            <el-table-column label="金额" width="110">
              <template #default="{ row }">¥{{ (row.amount_cents / 100).toFixed(2) }}</template>
            </el-table-column>
            <el-table-column prop="months" label="月数" width="70" />
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <el-tag :type="row.status === 'paid' ? 'success' : row.status === 'pending' ? 'warning' : 'info'" size="small">
                  {{ { pending: '应收', paid: '已收', void: '作废' }[row.status] }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="支付时间" width="160">
              <template #default="{ row }">{{ fmtTime(row.paid_at) }}</template>
            </el-table-column>
            <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
            <el-table-column label="操作" width="170" fixed="right">
              <template #default="{ row }">
                <el-button v-if="row.status === 'pending'" size="small" type="success" plain @click="markPaid(row)">标记支付</el-button>
                <el-button v-if="row.status === 'pending'" size="small" type="warning" plain @click="voidRecord(row)">作废</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="recordPage"
            :page-size="recordSize"
            :total="recordTotal"
            layout="total, prev, pager, next"
            small
            style="margin-top: 12px; justify-content: flex-end"
            @current-change="loadRecords"
          />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 方案弹窗 -->
    <el-dialog v-model="planDialogVisible" :title="planEditing ? '编辑方案' : '新建方案'" width="480px">
      <el-form label-width="110px">
        <el-form-item label="编码">
          <el-input v-model="planForm.code" :disabled="planEditing" placeholder="free/pro/..." maxlength="32" />
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="planForm.name" maxlength="60" />
        </el-form-item>
        <el-form-item label="月付（元）">
          <el-input-number v-model="planForm.priceYuan" :min="0" :precision="2" style="width: 160px" />
        </el-form-item>
        <el-form-item label="年付（元）">
          <el-input-number v-model="planForm.yearlyYuan" :min="0" :precision="2" style="width: 160px" />
        </el-form-item>
        <el-form-item label="成员上限">
          <el-input-number v-model="planForm.max_users" :min="0" style="width: 160px" />
          <span class="muted" style="margin-left: 8px">0 = 不限</span>
        </el-form-item>
        <el-form-item label="月消息量">
          <el-input-number v-model="planForm.max_messages_monthly" :min="0" style="width: 160px" />
          <span class="muted" style="margin-left: 8px">0 = 不限</span>
        </el-form-item>
        <el-form-item v-if="planEditing" label="状态">
          <el-radio-group v-model="planForm.status">
            <el-radio value="active">在售</el-radio>
            <el-radio value="retired">下架</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="planForm.remark" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="planDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="savePlan">保存</el-button>
      </template>
    </el-dialog>

    <!-- 指派/续期弹窗 -->
    <el-dialog v-model="assignVisible" title="指派/续期订阅" width="440px" append-to-body>
      <el-form label-width="110px">
        <el-form-item label="团队">
          <el-select v-model="assignForm.team_id" placeholder="选择团队" style="width: 220px" :disabled="!!assignEditingTeam">
            <el-option v-for="t in teams" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="方案">
          <el-select v-model="assignForm.plan_id" style="width: 220px">
            <el-option v-for="p in activePlans" :key="p.id" :label="`${p.name}（¥${(p.price_cents / 100).toFixed(2)}/月）`" :value="p.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="月数">
          <el-input-number v-model="assignForm.months" :min="1" :max="36" style="width: 160px" />
        </el-form-item>
        <el-form-item label="自动续费">
          <el-switch v-model="assignForm.auto_renew" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="assignVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveAssign">确认</el-button>
      </template>
    </el-dialog>

    <!-- 应收账单弹窗 -->
    <el-dialog v-model="recordDialogVisible" title="新建应收账单" width="440px" append-to-body>
      <el-form label-width="110px">
        <el-form-item label="团队">
          <el-select v-model="recordForm.team_id" style="width: 220px">
            <el-option v-for="t in teams" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="方案">
          <el-select v-model="recordForm.plan_id" style="width: 220px">
            <el-option v-for="p in plans" :key="p.id" :label="p.name" :value="p.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="月数">
          <el-input-number v-model="recordForm.months" :min="1" :max="36" style="width: 160px" />
        </el-form-item>
        <el-form-item label="金额（元）">
          <el-input-number v-model="recordForm.amountYuan" :min="0" :precision="2" style="width: 160px" />
          <span class="muted" style="margin-left: 8px">0 = 按方案月价×月数</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="recordForm.remark" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="recordDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveRecord">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/request'
import { teamAdminApi } from '@/api/teamAdmin'

// 计费 API（独立成段：与团队管理面同源但路径不同）
const billingApi = {
  listPlans: () => http.get('/api/manage/billing/plans'),
  createPlan: (d) => http.post('/api/manage/billing/plans', d),
  updatePlan: (id, d) => http.put(`/api/manage/billing/plans/${id}`, d),
  removePlan: (id) => http.delete(`/api/manage/billing/plans/${id}`),
  listSubs: () => http.get('/api/manage/billing/subscriptions'),
  assign: (teamId, d) => http.put(`/api/manage/billing/subscriptions/${teamId}`, d),
  cancel: (teamId) => http.post(`/api/manage/billing/subscriptions/${teamId}/cancel`),
  listRecords: (params) => http.get('/api/manage/billing/records', { params }),
  createRecord: (d) => http.post('/api/manage/billing/records', d),
  payRecord: (id) => http.put(`/api/manage/billing/records/${id}/pay`),
  voidRecord: (id) => http.put(`/api/manage/billing/records/${id}/void`)
}

const tab = ref('plans')
const teams = ref([])

// 方案
const plans = ref([])
const plansLoading = ref(false)
const planDialogVisible = ref(false)
const planEditing = ref(false)
const planEditingId = ref(0)
const saving = ref(false)
const planForm = ref({})

// 订阅
const subs = ref([])
const subsLoading = ref(false)
const assignVisible = ref(false)
const assignEditingTeam = ref(null)
const assignForm = ref({ team_id: null, plan_id: null, months: 1, auto_renew: false })

// 账单
const records = ref([])
const recordsLoading = ref(false)
const recordTotal = ref(0)
const recordPage = ref(1)
const recordSize = 20
const recordTeamId = ref(null)
const recordStatus = ref('')
const recordDialogVisible = ref(false)
const recordForm = ref({})

const activePlans = computed(() => plans.value.filter((p) => p.status === 'active'))
const teamName = (id) => teams.value.find((t) => t.id === id)?.name || `团队 #${id}`
const fmtTime = (v) => (v ? String(v).replace('T', ' ').slice(0, 19) : '—')

const loadPlans = async () => {
  plansLoading.value = true
  try {
    const res = await billingApi.listPlans()
    plans.value = (res?.data ?? res ?? {}).list ?? []
  } finally {
    plansLoading.value = false
  }
}

const loadSubs = async () => {
  subsLoading.value = true
  try {
    const res = await billingApi.listSubs()
    subs.value = (res?.data ?? res ?? {}).list ?? []
  } finally {
    subsLoading.value = false
  }
}

const loadRecords = async () => {
  recordsLoading.value = true
  try {
    const params = { page: recordPage.value, size: recordSize }
    if (recordTeamId.value) params.team_id = recordTeamId.value
    if (recordStatus.value) params.status = recordStatus.value
    const res = await billingApi.listRecords(params)
    const data = res?.data ?? res ?? {}
    records.value = data.list ?? []
    recordTotal.value = data.total ?? 0
  } finally {
    recordsLoading.value = false
  }
}

const loadTeams = async () => {
  const res = await teamAdminApi.list({ page: 1, size: 200 })
  teams.value = (res?.data ?? res ?? {}).list ?? []
}

const openPlanDialog = (row) => {
  planEditing.value = !!row
  planEditingId.value = row?.id || 0
  planForm.value = row
    ? { code: row.code, name: row.name, priceYuan: row.price_cents / 100, yearlyYuan: row.yearly_price_cents / 100, max_users: row.max_users, max_messages_monthly: row.max_messages_monthly, status: row.status, remark: row.remark || '' }
    : { code: '', name: '', priceYuan: 0, yearlyYuan: 0, max_users: 0, max_messages_monthly: 0, status: 'active', remark: '' }
  planDialogVisible.value = true
}

const savePlan = async () => {
  saving.value = true
  try {
    const payload = {
      code: planForm.value.code,
      name: planForm.value.name,
      price_cents: Math.round((planForm.value.priceYuan || 0) * 100),
      yearly_price_cents: Math.round((planForm.value.yearlyYuan || 0) * 100),
      max_users: planForm.value.max_users || 0,
      max_messages_monthly: planForm.value.max_messages_monthly || 0,
      remark: planForm.value.remark
    }
    if (planEditing.value) {
      payload.status = planForm.value.status
      await billingApi.updatePlan(planEditingId.value, payload)
    } else {
      await billingApi.createPlan(payload)
    }
    ElMessage.success('已保存')
    planDialogVisible.value = false
    await loadPlans()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

const removePlan = async (row) => {
  await ElMessageBox.confirm(`确认删除方案「${row.name}」？被订阅引用的方案需先下架。`, '删除确认', { type: 'warning' })
  try {
    await billingApi.removePlan(row.id)
    ElMessage.success('已删除')
    await loadPlans()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '删除失败')
  }
}

const openAssign = (row) => {
  assignEditingTeam.value = row?.team_id || null
  assignForm.value = {
    team_id: row?.team_id || null,
    plan_id: row?.plan_id || null,
    months: 1,
    auto_renew: row?.auto_renew || false
  }
  assignVisible.value = true
}

const saveAssign = async () => {
  if (!assignForm.value.team_id || !assignForm.value.plan_id) {
    ElMessage.warning('请选择团队与方案')
    return
  }
  saving.value = true
  try {
    await billingApi.assign(assignForm.value.team_id, {
      plan_id: assignForm.value.plan_id,
      months: assignForm.value.months,
      auto_renew: assignForm.value.auto_renew
    })
    ElMessage.success('已指派')
    assignVisible.value = false
    await loadSubs()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '指派失败')
  } finally {
    saving.value = false
  }
}

const cancelSub = async (row) => {
  await ElMessageBox.confirm(`确认停用 ${teamName(row.team_id)} 的订阅？`, '确认', { type: 'warning' })
  await billingApi.cancel(row.team_id)
  ElMessage.success('已停用')
  await loadSubs()
}

const openRecordDialog = () => {
  recordForm.value = { team_id: recordTeamId.value || null, plan_id: null, months: 1, amountYuan: 0, remark: '' }
  recordDialogVisible.value = true
}

const saveRecord = async () => {
  if (!recordForm.value.team_id || !recordForm.value.plan_id) {
    ElMessage.warning('请选择团队与方案')
    return
  }
  saving.value = true
  try {
    await billingApi.createRecord({
      team_id: recordForm.value.team_id,
      plan_id: recordForm.value.plan_id,
      months: recordForm.value.months,
      amount_cents: Math.round((recordForm.value.amountYuan || 0) * 100),
      remark: recordForm.value.remark
    })
    ElMessage.success('已创建应收账单')
    recordDialogVisible.value = false
    await loadRecords()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '创建失败')
  } finally {
    saving.value = false
  }
}

const markPaid = async (row) => {
  await billingApi.payRecord(row.id)
  ElMessage.success('已标记支付')
  await loadRecords()
}

const voidRecord = async (row) => {
  await ElMessageBox.confirm('确认作废该账单？', '确认', { type: 'warning' })
  await billingApi.voidRecord(row.id)
  ElMessage.success('已作废')
  await loadRecords()
}

onMounted(() => {
  loadPlans()
  loadSubs()
  loadRecords()
  loadTeams()
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
