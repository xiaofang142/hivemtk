<template>
  <div class="approval-task">
    <el-alert
      v-if="unavailable"
      class="mb-12"
      type="warning"
      show-icon
      :closable="false"
      title="待办底座不可用"
      description="后端回的是 503（待办竖未装配或缺 DB 句柄）。下面的空列表不是「人工清完了」，是一次都没读到。"
    />
    <el-alert
      v-if="!me"
      class="mb-12"
      type="info"
      show-icon
      :closable="false"
      title="登录态里没有 user id"
      description="动作类端点全部要求 operator，所以这一页此刻只能看不能点。重新登录通常就能拿回身份。"
    />

    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>待办中心</span>
          <el-button
            :loading="loading"
            @click="refresh"
          >
            刷新
          </el-button>
        </div>
      </template>

      <div class="counts">
        <span
          v-for="row in counts.by_kind"
          :key="row.kind"
          class="count-chip"
        >
          {{ kindLabel(row.kind) }} 开放 {{ row.open }} ／<b :class="{ overdue: row.overdue > 0 }">逾期 {{ row.overdue }}</b>
        </span>
        <span
          v-if="counts.unknown_kind_open"
          class="count-chip"
        >
          未知类型开放 {{ counts.unknown_kind_open }}（不进上面任何一档的逾期读数）
        </span>
        <span
          v-if="counts.at"
          class="counts-at"
        >读数时刻 {{ formatTime(counts.at) }}</span>
      </div>

      <el-form
        inline
        class="mb-12"
      >
        <el-form-item label="类型">
          <el-select
            v-model="filter.kind"
            class="filter-select"
            multiple
            clearable
            collapse-tags
            placeholder="全部类型"
            @change="reload"
          >
            <el-option
              v-for="k in KINDS"
              :key="k"
              :label="kindLabel(k)"
              :value="k"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select
            v-model="filter.status"
            class="filter-select"
            multiple
            clearable
            collapse-tags
            placeholder="开放态（待处理＋已认领）"
            @change="reload"
          >
            <el-option
              v-for="s in STATUSES"
              :key="s"
              :label="statusLabel(s)"
              :value="s"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="归属">
          <el-select
            v-model="filter.assignee"
            class="filter-select"
            @change="reload"
          >
            <el-option
              label="全部待办"
              value="all"
            />
            <el-option
              label="挂在我名下"
              value="me"
            />
          </el-select>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="list"
        stripe
      >
        <el-table-column
          label="类型"
          width="110"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="kindTagType(row.kind)"
            >
              {{ kindLabel(row.kind) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="标题"
          min-width="240"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openDetail(row)"
            >
              {{ row.title || row.subject_id }}
            </el-button>
            <div
              v-if="row.reason"
              class="reason"
            >
              {{ row.reason }}
            </div>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            {{ statusLabel(row.status) }}
          </template>
        </el-table-column>
        <el-table-column
          label="认领人"
          width="100"
        >
          <template #default="{ row }">
            {{ row.assignee_user_id || '—' }}
          </template>
        </el-table-column>
        <el-table-column
          label="截止"
          width="170"
        >
          <template #default="{ row }">
            <span :class="{ overdue: isOverdue(row) }">{{ slaText(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="240"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-for="act in rowActions(row, me)"
              :key="act"
              link
              :type="act === 'cancel' ? 'danger' : 'primary'"
              @click="runAction(row, act)"
            >
              {{ actionLabel(act) }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        class="mt-12"
        layout="total, prev, pager, next"
        :current-page="filter.page"
        :page-size="filter.page_size"
        :total="total"
        @current-change="onPageChange"
      />
    </el-card>

    <el-drawer
      v-model="detail.visible"
      title="待办详情"
      size="42%"
    >
      <pre class="json-preview">{{ detail.text }}</pre>
    </el-drawer>

    <el-drawer
      v-model="decide.visible"
      title="审批裁决"
      size="46%"
    >
      <el-alert
        v-if="decide.error"
        class="mb-12"
        type="error"
        show-icon
        :closable="false"
        :title="decide.error"
      />
      <el-skeleton
        v-if="decide.loading"
        :rows="5"
        animated
      />

      <template v-else>
        <el-descriptions
          :column="1"
          border
          class="mb-12"
        >
          <el-descriptions-item label="待办">
            {{ decide.task?.title || '—' }}
          </el-descriptions-item>
          <el-descriptions-item label="审批 id">
            {{ decide.approvalId }}
          </el-descriptions-item>
          <el-descriptions-item label="策略">
            {{ decide.approval?.policy_key || '—' }}
          </el-descriptions-item>
          <el-descriptions-item label="被审对象">
            {{ subjectText(decide.approval) }}
          </el-descriptions-item>
          <el-descriptions-item label="审批状态">
            {{ decide.approval?.status || '—' }}
          </el-descriptions-item>
          <el-descriptions-item label="截止">
            {{ formatTime(decide.approval?.expires_at) }}
          </el-descriptions-item>
          <el-descriptions-item
            v-if="decide.approval?.decided_by"
            label="裁决者"
          >
            {{ decide.approval.decided_by }}
          </el-descriptions-item>
          <el-descriptions-item
            v-if="decide.approval?.decision_note"
            label="裁决说明"
          >
            {{ decide.approval.decision_note }}
          </el-descriptions-item>
        </el-descriptions>

        <el-form
          v-if="verdicts.length"
          label-position="top"
        >
          <el-form-item label="裁决说明">
            <el-input
              v-model="decide.note"
              type="textarea"
              :rows="3"
              maxlength="2000"
              show-word-limit
              placeholder="批给谁、凭什么批；驳回的话必须写清为什么（驳回没有理由是复盘时最难补的一句）"
            />
          </el-form-item>
        </el-form>
        <p
          v-else-if="decide.approval"
          class="hint"
        >
          这条审批已经不需要人来裁决了（已落定或已由清扫器按 TTL 收口）。
        </p>

        <div class="decide-actions">
          <el-button
            v-for="v in verdicts"
            :key="v"
            :type="v === 'approved' ? 'success' : 'danger'"
            :loading="decide.submitting"
            :disabled="v === 'rejected' && !hasRejectReason(decide.note)"
            @click="submitDecide(v)"
          >
            {{ verdictLabel(v) }}
          </el-button>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { humanTaskApi } from '@/api/humanTask'
import { approvalApi } from '@/api/approval'
import { toList } from '@/utils/list'
import { useUserStore } from '@/stores/user'
import {
  KIND_APPROVAL,
  KIND_COLLECTION,
  KIND_HANDOFF,
  hasRejectReason,
  rowActions,
  slaField,
  verdictButtons
} from './actions'

// 待办中心（N-9）。三类待办一张表：会话转人工 / 待审批 / 催收升级。
//
// 与坐席收件箱（views/inbox）的关系是**分离视图、不分离数据出口**之外的另一半：
// 收件箱读的是会话（/api/inbox/conversations），这一页读的是"等人办的事"
// （/api/human-tasks），两边谁也不转调谁 —— 隔离由 tests/unit/api_humanTask.test.js 双向钉住。
//
// 通知不在这里发：全站通知出口只有一个（views/Notifications.vue），待办页再挂一个铃铛
// 就会有两套已读状态。这一页要的是"打开时看到的是此刻的数"，所以未读数走轮询而不是推送。

const KINDS = [KIND_HANDOFF, KIND_APPROVAL, KIND_COLLECTION]
const STATUSES = ['pending', 'claimed', 'done', 'cancelled']
const OPEN_STATUSES = ['pending', 'claimed']

const KIND_LABELS = {
  [KIND_HANDOFF]: '会话转人工',
  [KIND_APPROVAL]: '待审批',
  [KIND_COLLECTION]: '催收升级'
}
const KIND_TAG_TYPES = {
  [KIND_HANDOFF]: 'warning',
  [KIND_APPROVAL]: 'danger',
  [KIND_COLLECTION]: 'info'
}
const STATUS_LABELS = {
  pending: '待处理',
  claimed: '已认领',
  done: '已完成',
  cancelled: '已撤销'
}
const ACTION_LABELS = {
  claim: '认领',
  release: '释放',
  complete: '完成',
  cancel: '撤销',
  decide: '裁决'
}
const VERDICT_LABELS = { approved: '批准', rejected: '驳回' }

// COUNTS_POLL_MS 未读数轮询间隔。30s 是"值班换班时看得见新账"的量级；
// 更短只是把读接口打成压力源，而这一页的正确性不靠推送（推送归 Notifications.vue）。
const COUNTS_POLL_MS = 30000

const userStore = useUserStore()
// 后端的 operator / assignee_user_id 存的是 user id 的十进制字符串；这里统一成字符串，
// 否则 userInfo.id 是数字时 rowActions 的"是不是我认领的"会永远判否。
const me = computed(() => String(userStore.userInfo?.id ?? ''))

const loading = ref(false)
const unavailable = ref(false)
const list = ref([])
const total = ref(0)
const counts = reactive({ at: '', total_open: 0, by_kind: [], unknown_kind_open: 0 })
const filter = reactive({ kind: [], status: [...OPEN_STATUSES], assignee: 'all', page: 1, page_size: 20 })

const detail = reactive({ visible: false, text: '' })
const decide = reactive({
  visible: false,
  loading: false,
  submitting: false,
  error: '',
  task: null,
  approvalId: '',
  approval: null,
  note: ''
})

const kindLabel = (k) => KIND_LABELS[k] || k || '未知类型'
const kindTagType = (k) => KIND_TAG_TYPES[k] || 'info'
const statusLabel = (s) => STATUS_LABELS[s] || s
const actionLabel = (a) => ACTION_LABELS[a] || a
const verdictLabel = (v) => VERDICT_LABELS[v] || v
const subjectText = (a) => (a ? `${a.subject_type}/${a.subject_id}` : '—')

const verdicts = computed(() => verdictButtons(decide.approval))

function formatTime(value) {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString()
}

// isOverdue 只判"这一行自己那一档的截止"过没过。
// 判据时刻用后端 counts.at 而不是本地 Date.now()：值班机器的钟快十分钟，
// 满屏就都是逾期，而那是个看不出来的错。拿不到 at 时退回本地钟。
function isOverdue(row) {
  const due = dueAt(row)
  if (!due) return false
  const now = counts.at ? new Date(counts.at).getTime() : Date.now()
  return due < now
}

function dueAt(row) {
  const field = slaField(row?.kind)
  const raw = field ? row[field] : ''
  if (!raw) return 0
  const t = new Date(raw).getTime()
  return Number.isNaN(t) ? 0 : t
}

function slaText(row) {
  const field = slaField(row.kind)
  if (!field) return '—'
  const due = dueAt(row)
  if (!due) return '未设截止'
  if (row.status === 'done' || row.status === 'cancelled') return formatTime(row[field])
  return `${formatTime(row[field])} 前`
}

function queryParams() {
  const q = { page: filter.page, page_size: filter.page_size }
  if (filter.kind.length) q.kind = filter.kind
  if (filter.status.length) q.status = filter.status
  // 'all' 不进查询串：后端把空 assignee 读成"不过滤"，把 'all' 读成"查一个叫 all 的人"。
  if (filter.assignee === 'me') q.assignee = 'me'
  return q
}

async function fetchList() {
  loading.value = true
  try {
    const data = await humanTaskApi.list(queryParams())
    list.value = toList(data)
    total.value = Number(data?.total) || 0
    unavailable.value = false
  } catch (err) {
    // 503 与"这次查询失败"要分开：前者是配置状态，页面要长期挂着说一句话；
    // 后者刷新就好。空列表两者都不是答案，所以这里绝不清成 []假装读到了。
    unavailable.value = err?.status === 503
  } finally {
    loading.value = false
  }
}

async function fetchCounts() {
  try {
    const data = await humanTaskApi.counts()
    if (!data) return
    counts.at = data.at || ''
    counts.total_open = Number(data.total_open) || 0
    counts.by_kind = Array.isArray(data.by_kind) ? data.by_kind : []
    counts.unknown_kind_open = Number(data.unknown_kind_open) || 0
    unavailable.value = false
  } catch (err) {
    // 静默失败：上一轮读数留着。轮询每 30s 一次，弹一句"服务异常"只会变成噪音。
    if (err?.status === 503) unavailable.value = true
  }
}

function reload() {
  filter.page = 1
  fetchList()
}

function refresh() {
  fetchList()
  fetchCounts()
}

function onPageChange(page) {
  filter.page = page
  fetchList()
}

function openDetail(row) {
  detail.text = JSON.stringify(row, null, 2)
  detail.visible = true
}

async function openDecide(row) {
  decide.visible = true
  decide.task = row
  decide.approvalId = row.subject_id || ''
  decide.approval = null
  decide.note = ''
  decide.error = ''
  if (!decide.approvalId) {
    decide.error = '这条待办没有指向任何审批记录（subject_id 为空）'
    return
  }
  decide.loading = true
  try {
    decide.approval = await approvalApi.get(decide.approvalId)
  } catch (err) {
    decide.error = err?.message || '审批详情读取失败'
  } finally {
    decide.loading = false
  }
}

async function submitDecide(verdict) {
  decide.submitting = true
  try {
    decide.approval = await approvalApi.decide(decide.approvalId, verdict, decide.note)
    ElMessage.success(verdict === 'approved' ? '已批准，等待中的流程会被叫醒' : '已驳回')
    decide.visible = false
    refresh()
  } catch (err) {
    if (err?.status === 409) {
      // 两个人同时开着这条详情：后端在 409 的响应体里回的就是当前那一行，
      // 直接把它显示出来 —— 这比"刷新一下看看"准，因为它不会把别人的结论冲成新读取。
      decide.approval = err.response?.data?.data || decide.approval
      ElMessage.warning(err.message || '这条审批已被别人裁决，界面显示的是当前状态')
      refresh()
    }
  } finally {
    decide.submitting = false
  }
}

// 认领/释放/完成/撤销的调用点。这里不逐条写成功提示之外的分支：
// 409/403 的文案服务端已经给得很准（"这条刚被别人完成了"），
// 前端再包一句同义改写只会让两边对不上。
const ACTION_FN = {
  claim: (id) => humanTaskApi.claim(id),
  release: (id) => humanTaskApi.release(id),
  complete: (id) => humanTaskApi.complete(id)
}

async function runAction(row, action) {
  if (action === 'decide') {
    await openDecide(row)
    return
  }
  if (action === 'cancel') {
    await cancelTask(row)
    return
  }
  try {
    await ACTION_FN[action](row.id)
    ElMessage.success(`${actionLabel(action)}成功`)
    refresh()
  } catch (err) {
    if (err?.status === 409 || err?.status === 403) refresh()
  }
}

async function cancelTask(row) {
  let value
  try {
    ({ value } = await ElMessageBox.prompt('撤销这条待办，必须给理由', '撤销待办', {
      inputType: 'textarea',
      inputPlaceholder: '例如：客户已自行解决 / 这次裁决已在别处完成',
      inputValidator: (v) => (hasRejectReason(v) ? true : '撤销必须写理由'),
      confirmButtonText: '撤销',
      cancelButtonText: '返回'
    }))
  } catch {
    return // 点了返回
  }
  try {
    await humanTaskApi.cancel(row.id, value)
    ElMessage.success('已撤销')
    refresh()
  } catch (err) {
    if (err?.status === 409 || err?.status === 403) refresh()
  }
}

let pollTimer = null

onMounted(() => {
  refresh()
  pollTimer = setInterval(fetchCounts, COUNTS_POLL_MS)
})

onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = null
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.mb-12 {
  margin-bottom: 12px;
}
.mt-12 {
  margin-top: 12px;
}
.filter-select {
  width: 220px;
}
.counts {
  margin-bottom: 12px;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  font-size: 13px;
}
.count-chip {
  white-space: nowrap;
}
.counts-at {
  color: #909399;
  font-size: 12px;
}
.overdue {
  color: #f56c6c;
}
.reason {
  color: #909399;
  font-size: 12px;
}
.hint {
  color: #909399;
  font-size: 13px;
}
.decide-actions {
  display: flex;
  gap: 8px;
}
.json-preview {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 6px;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
