<template>
  <div class="followup-today">
    <div class="page-head">
      <div class="head-left">
        <h2 class="page-title">
          今日跟进
        </h2>
        <span class="page-sub">逾期、今日日历与全部待办，完成或取消即推进旅程</span>
      </div>
      <div class="head-right">
        <el-button
          :loading="loading"
          size="small"
          @click="reload"
        >
          <el-icon><Refresh /></el-icon>
          <span>刷新</span>
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="statusError"
      :title="statusError"
      :type="statusErrorType"
      show-icon
      :closable="false"
      class="head-alert"
    />

    <div class="zone-grid">
      <el-card
        v-loading="loading"
        shadow="never"
        class="zone-card"
      >
        <template #header>
          <div class="zone-head">
            <el-icon class="zone-icon danger">
              <WarningFilled />
            </el-icon>
            <span class="zone-title">已逾期</span>
            <el-tag
              size="small"
              type="danger"
              effect="plain"
            >
              {{ overdueList.length }}
            </el-tag>
          </div>
        </template>
        <div
          v-if="overdueList.length === 0 && !loading"
          class="zone-empty"
        >
          没有逾期提醒
        </div>
        <ul
          v-else
          class="fu-list"
        >
          <li
            v-for="r in overdueList"
            :id="'fu-' + r.id"
            :key="r.id"
            class="fu-item"
            :class="{ 'fu-active': r.id === routeId }"
          >
            <div class="fu-line">
              <el-tag
                size="small"
                :type="priorityType(r.priority)"
              >
                {{ priorityLabel(r.priority) }}
              </el-tag>
              <el-tag
                size="small"
                effect="plain"
                type="info"
              >
                {{ r.type }}
              </el-tag>
              <span class="fu-title">{{ r.title || '（无标题）' }}</span>
            </div>
            <div class="fu-meta">
              <span class="fu-due overdue">应于 {{ fmtTime(r.dueAt) }}</span>
              <span
                v-if="r.desc"
                class="fu-desc"
              >{{ r.desc }}</span>
            </div>
            <div class="fu-actions">
              <el-button
                size="small"
                type="primary"
                @click="openComplete(r)"
              >
                完成
              </el-button>
              <el-button
                size="small"
                @click="doCancel(r)"
              >
                取消
              </el-button>
            </div>
          </li>
        </ul>
      </el-card>

      <el-card
        v-loading="loading"
        shadow="never"
        class="zone-card"
      >
        <template #header>
          <div class="zone-head">
            <el-icon class="zone-icon">
              <Calendar />
            </el-icon>
            <span class="zone-title">今日日历</span>
            <span class="zone-date">{{ todayDate }}</span>
            <el-tag
              size="small"
              effect="plain"
            >
              {{ todayList.length }}
            </el-tag>
          </div>
        </template>
        <div
          v-if="todayList.length === 0 && !loading"
          class="zone-empty"
        >
          今天没有排程的跟进
        </div>
        <ul
          v-else
          class="fu-list"
        >
          <li
            v-for="r in todayList"
            :id="'fu-' + r.id"
            :key="r.id"
            class="fu-item"
            :class="{ 'fu-active': r.id === routeId }"
          >
            <div class="fu-line">
              <el-tag
                size="small"
                :type="priorityType(r.priority)"
              >
                {{ priorityLabel(r.priority) }}
              </el-tag>
              <el-tag
                size="small"
                effect="plain"
                type="info"
              >
                {{ r.type }}
              </el-tag>
              <span class="fu-title">{{ r.title || '（无标题）' }}</span>
            </div>
            <div class="fu-meta">
              <span class="fu-due">应于 {{ fmtTime(r.dueAt) }}</span>
              <span
                v-if="r.desc"
                class="fu-desc"
              >{{ r.desc }}</span>
            </div>
            <div class="fu-actions">
              <el-button
                size="small"
                type="primary"
                @click="openComplete(r)"
              >
                完成
              </el-button>
              <el-button
                size="small"
                @click="doCancel(r)"
              >
                取消
              </el-button>
            </div>
          </li>
        </ul>
      </el-card>

      <el-card
        v-loading="loading"
        shadow="never"
        class="zone-card"
      >
        <template #header>
          <div class="zone-head">
            <el-icon class="zone-icon">
              <List />
            </el-icon>
            <span class="zone-title">全部待办</span>
            <el-tag
              size="small"
              effect="plain"
            >
              {{ pendingList.length }}
            </el-tag>
          </div>
        </template>
        <div
          v-if="pendingList.length === 0 && !loading"
          class="zone-empty"
        >
          没有待办提醒
        </div>
        <ul
          v-else
          class="fu-list"
        >
          <li
            v-for="r in pendingList"
            :id="'fu-' + r.id"
            :key="r.id"
            class="fu-item"
            :class="{ 'fu-active': r.id === routeId }"
          >
            <div class="fu-line">
              <el-tag
                size="small"
                :type="priorityType(r.priority)"
              >
                {{ priorityLabel(r.priority) }}
              </el-tag>
              <el-tag
                size="small"
                effect="plain"
                type="info"
              >
                {{ r.type }}
              </el-tag>
              <span class="fu-title">{{ r.title || '（无标题）' }}</span>
            </div>
            <div class="fu-meta">
              <span
                class="fu-due"
                :class="{ overdue: isOverdue(r) }"
              >应于 {{ fmtTime(r.dueAt) }}</span>
              <span
                v-if="r.desc"
                class="fu-desc"
              >{{ r.desc }}</span>
            </div>
            <div class="fu-actions">
              <el-button
                size="small"
                type="primary"
                @click="openComplete(r)"
              >
                完成
              </el-button>
              <el-button
                size="small"
                @click="doCancel(r)"
              >
                取消
              </el-button>
            </div>
          </li>
        </ul>
      </el-card>
    </div>

    <el-dialog
      v-model="completeVisible"
      title="完成跟进"
      width="460px"
      append-to-body
    >
      <el-form label-width="80px">
        <el-form-item label="结果">
          <el-select
            v-model="completeForm.result"
            class="fu-select"
          >
            <el-option
              v-for="opt in resultOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="completeForm.note"
            type="textarea"
            :rows="3"
            placeholder="本次沟通结论（可空）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="completeVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="doComplete"
        >
          确认完成
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, WarningFilled, Calendar, List } from '@element-plus/icons-vue'
import { followupApi } from '@/api/followup.js'

// 今日跟进（A11）：跟进提醒读写口的第一批消费方。
// 三区并排（逾期/今日/全部待办）— 读口三条端点一一对应，完成/取消动作走
// 写口。/followups/:id 深链（sales_workbench 聚合待办生成）在此定位高亮。
const route = useRoute()

const loading = ref(false)
const submitting = ref(false)
const statusError = ref('')
const statusErrorType = ref('info')

const overdueList = ref([])
const todayList = ref([])
const pendingList = ref([])

const completeVisible = ref(false)
const completeForm = ref({ result: 'contacted', note: '' })
const activeReminder = ref(null)

const routeId = computed(() => route.params.id || '')
const todayDate = computed(() => {
  const d = new Date()
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${mm}-${dd}`
})

// 与后端 FollowUpResultInfo 键一一对应（后端七枚举，缺一会出现"能选但
// 后端 400"的半坏选项）。
const resultOptions = [
  { value: 'contacted', label: '已联系' },
  { value: 'interested', label: '有意向' },
  { value: 'quoted', label: '已报价' },
  { value: 'converted', label: '已转化' },
  { value: 'rejected', label: '已拒绝' },
  { value: 'lost', label: '已流失' },
  { value: 'no_response', label: '未回复' }
]

const priorityLabel = (p) => ({ 0: '低', 1: '普通', 2: '高', 3: '紧急' }[p] || '普通')
const priorityType = (p) => ({ 0: 'info', 1: 'info', 2: 'warning', 3: 'danger' }[p] || 'info')

const fmtTime = (s) => {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return String(s)
  const hh = String(d.getHours()).padStart(2, '0')
  const mi = String(d.getMinutes()).padStart(2, '0')
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:${mi}`
}

const isOverdue = (r) => !!r.dueAt && new Date(r.dueAt).getTime() < Date.now()

// HTTP 层状态码措辞分档：503=服务未装配（草稿/旅程运行时 off，不是没数据）、
// 400=参数、409 已处理透 err.message、其余给原始 message。
const describeError = (err) => {
  const status = err?.status ?? err?.response?.status
  const msg = err?.message || ''
  if (status === 503) return { text: '跟进服务未装配（草稿/旅程运行时未启用），非“没有跟进”', type: 'warning' }
  if (status === 400) return { text: `参数错误：${msg || '请检查筛选条件'}`, type: 'error' }
  if (status === 409) return { text: msg || '提醒已处理', type: 'warning' }
  if (status === 404) return { text: msg || '提醒不存在', type: 'warning' }
  return { text: msg || '加载失败', type: 'error' }
}

// ownerId：后端 owner_id 必带（空串=不过滤全员，读侧按当前登录人隔离）。
// 没有用户 id 时拿不到三区数据 —— 明确报错，不静默请求（后端 400）。
const currentOwnerId = () => {
  try {
    const raw = localStorage.getItem('user') || localStorage.getItem('userInfo')
    if (raw) {
      const u = JSON.parse(raw)
      return String(u?.id || u?.user_id || u?.userId || '')
    }
  } catch { /* 脏数据当未登录处理 */ }
  return ''
}

const locateRouteId = async () => {
  if (!routeId.value) return
  await nextTick()
  const el = document.getElementById(`fu-${routeId.value}`)
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    return
  }
  // 深链 id 不在任何一区：提醒不存在/已处理/非本人 —— 只提示不跳转（与
  // I11 goTodo 的"无落点不进 NotFound"相反：这里有落点但提醒已不在 pending）。
  ElMessage.info('该提醒不存在、已处理或不在你的待办中')
}

const reload = async () => {
  const ownerId = currentOwnerId()
  if (!ownerId) {
    statusError.value = '未识别当前登录用户，无法按 owner 隔离跟进数据'
    statusErrorType.value = 'error'
    return
  }
  loading.value = true
  statusError.value = ''
  try {
    const [today, pending, overdue] = await Promise.all([
      followupApi.today(ownerId),
      followupApi.pending(ownerId),
      followupApi.overdue(ownerId)
    ])
    todayList.value = today?.data?.list || []
    pendingList.value = pending?.data?.list || []
    overdueList.value = overdue?.data?.list || []
    await locateRouteId()
  } catch (err) {
    const e = describeError(err)
    statusError.value = e.text
    statusErrorType.value = e.type
  } finally {
    loading.value = false
  }
}

const openComplete = (r) => {
  activeReminder.value = r
  completeForm.value = { result: 'contacted', note: '' }
  completeVisible.value = true
}

const doComplete = async () => {
  if (!activeReminder.value) return
  submitting.value = true
  try {
    await followupApi.complete(activeReminder.value.id, completeForm.value.result, completeForm.value.note)
    ElMessage.success('已完成，旅程与销售事件已推进')
    completeVisible.value = false
    await reload()
  } catch (err) {
    const e = describeError(err)
    ElMessage.error(e.text)
    if (err?.status !== 409) completeVisible.value = false
  } finally {
    submitting.value = false
  }
}

const doCancel = async (r) => {
  try {
    await ElMessageBox.confirm(`确认取消「${r.title || r.id}」？取消后不可恢复。`, '取消跟进', {
      type: 'warning',
      confirmButtonText: '确认取消',
      cancelButtonText: '再想想'
    })
  } catch { return }
  try {
    await followupApi.cancel(r.id)
    ElMessage.success('已取消')
    await reload()
  } catch (err) {
    const e = describeError(err)
    ElMessage.error(e.text)
  }
}

watch(() => route.params.id, async () => { await locateRouteId() })

onMounted(reload)
</script>

<style scoped>
.followup-today { padding: 4px 0 24px; }
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; font-weight: 600; color: var(--el-text-color-primary); }
.page-sub { display: block; margin-top: 4px; font-size: 12px; color: var(--el-text-color-secondary); }
.head-alert { margin-bottom: 12px; }
.zone-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.zone-card { border-radius: 10px; }
.zone-head { display: flex; align-items: center; gap: 8px; }
.zone-title { font-weight: 600; }
.zone-date { font-size: 12px; color: var(--el-text-color-secondary); }
.zone-icon { color: var(--el-color-primary); }
.zone-icon.danger { color: var(--el-color-danger); }
.zone-empty { padding: 24px 0; text-align: center; font-size: 13px; color: var(--el-text-color-secondary); }
.fu-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
.fu-item { padding: 10px; border: 1px solid var(--el-border-color-lighter); border-radius: 8px; transition: border-color .15s, background .15s; }
.fu-item.fu-active { border-color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
.fu-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.fu-title { font-size: 14px; font-weight: 500; color: var(--el-text-color-primary); }
.fu-meta { display: flex; align-items: center; gap: 8px; margin-top: 6px; font-size: 12px; color: var(--el-text-color-secondary); flex-wrap: wrap; }
.fu-due.overdue { color: var(--el-color-danger); font-weight: 600; }
.fu-desc { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 60%; }
.fu-actions { display: flex; gap: 8px; margin-top: 8px; }
.fu-select { width: 100%; }
@media (max-width: 1100px) { .zone-grid { grid-template-columns: 1fr; } }
</style>
