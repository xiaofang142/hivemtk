<template>
  <div class="page" v-if="task">
    <div class="header">
      <h2>{{ task.name }} <el-tag :type="task.status === 'running' ? 'warning' : 'info'">{{ task.status }}</el-tag></h2>
      <div>
        <el-button type="success" :disabled="task.status === 'running'" @click="onRun">执行</el-button>
        <el-button v-if="task.status === 'draft'" @click="onPublish">发布</el-button>
        <el-button @click="$router.push(`/browser-automation/tasks/${task.id}/edit`)">编辑</el-button>
      </div>
    </div>

    <el-descriptions :column="3" border style="margin-top: 12px">
      <el-descriptions-item label="类型">{{ task.task_type }}</el-descriptions-item>
      <el-descriptions-item label="模式">{{ task.brain_mode ? 'Brain（LLM）' : '显式步骤' }}</el-descriptions-item>
      <el-descriptions-item label="URL">
        <a :href="task.url" target="_blank" rel="noopener">{{ task.url }}</a>
      </el-descriptions-item>
      <el-descriptions-item label="循环次数">{{ task.loop_count }}</el-descriptions-item>
      <el-descriptions-item label="步间间隔">{{ task.delay_ms }}ms</el-descriptions-item>
      <el-descriptions-item label="超时">{{ task.timeout_sec }}s</el-descriptions-item>
      <el-descriptions-item label="失败重试">{{ task.retry_on_fail ? `${task.retry_delay_sec}s × ${task.max_retry_times} 次` : '关闭' }}</el-descriptions-item>
      <el-descriptions-item label="写操作确认">{{ task.require_confirm ? '提交前需人工放行' : '全自动' }}</el-descriptions-item>
      <el-descriptions-item label="上次执行">{{ task.last_run_at ? new Date(task.last_run_at).toLocaleString('zh-CN') : '—' }}</el-descriptions-item>
      <el-descriptions-item label="上次结果">{{ task.last_result || '—' }}</el-descriptions-item>
      <el-descriptions-item v-if="task.error_msg" label="错误" :span="3">
        <span style="color: #ef4444">{{ task.error_msg }}</span>
      </el-descriptions-item>
    </el-descriptions>

    <el-card v-if="!task.brain_mode" header="步骤编排" style="margin-top: 16px">
      <el-table :data="task.steps || []">
        <el-table-column type="index" label="#" width="60" />
        <el-table-column prop="action" label="动作" width="140" />
        <el-table-column prop="target" label="目标" min-width="180" />
        <el-table-column prop="value" label="值" min-width="140" />
      </el-table>
    </el-card>

    <el-card v-if="task.brain_mode" header="Brain 目标" style="margin-top: 16px">
      {{ task.brain_goal }}
    </el-card>

    <el-card header="执行历史" style="margin-top: 16px">
      <el-table :data="sessions">
        <el-table-column prop="id" label="Session" width="90">
          <template #default="{ row }">
            <el-link type="primary" @click="$router.push(`/browser-automation/sessions/${row.id}`)">#{{ row.id }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="{ completed: 'success', failed: 'danger', stopped: 'info', active: 'warning' }[row.status] || 'info'">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="total_steps" label="步骤" width="120">
          <template #default="{ row }">{{ row.success_steps }}/{{ row.total_steps }} 成功</template>
        </el-table-column>
        <el-table-column prop="duration_ms" label="耗时" width="110">
          <template #default="{ row }">{{ sessionDurationText(row) }}</template>
        </el-table-column>
        <el-table-column prop="error_msg" label="错误" min-width="200" show-overflow-tooltip />
        <el-table-column prop="created_at" label="时间" width="170">
          <template #default="{ row }">{{ new Date(row.created_at).toLocaleString('zh-CN') }}</template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card header="触达回执" style="margin-top: 16px">
      <template v-if="receipts.length">
        <el-table :data="receipts">
          <el-table-column prop="created_at" label="时间" width="170">
            <template #default="{ row }">{{ new Date(row.created_at).toLocaleString('zh-CN') }}</template>
          </el-table-column>
          <el-table-column prop="platform" label="平台" width="120" />
          <el-table-column prop="action" label="动作" width="120" />
          <el-table-column label="帖子链接" min-width="220" show-overflow-tooltip>
            <template #default="{ row }">
              <a v-if="row.target_url" :href="row.target_url" target="_blank" rel="noopener">{{ row.target_url }}</a>
              <span v-else style="color: #909399">未取到（页面已跳转或快照失败）</span>
            </template>
          </el-table-column>
          <el-table-column prop="copy_snapshot" label="文案快照" min-width="200" show-overflow-tooltip />
          <el-table-column label="验证" width="120">
            <template #default="{ row }">
              <el-tag :type="row.verified ? 'success' : 'warning'">{{ row.verified ? '已确认发布' : '结果未知' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="截图" width="100">
            <template #default="{ row }">
              <a v-if="row.screenshot_url" :href="row.screenshot_url" target="_blank" rel="noopener">查看</a>
              <span v-else style="color: #909399">—</span>
            </template>
          </el-table-column>
        </el-table>
        <div class="cron-warn">
          「结果未知」= 平台侧回查没归因到这条评论，不等于没发出去——请人工核对后再决定是否重跑
        </div>
      </template>
      <div v-else style="color: #909399">暂无回执。该任务的触达步若已发布，收口时会自动留存截图、帖子链接与文案快照。</div>
    </el-card>

    <el-card v-if="task.task_type === 'cron'" header="定时触发器" style="margin-top: 16px">
      <template v-if="cron">
        <el-space>
          <span>cron：{{ cron.cron_expr }}</span>
          <el-tag>{{ cron.enabled ? '已启用' : '已停用' }}</el-tag>
          <el-button size="small" @click="toggleCron">{{ cron.enabled ? '停用' : '启用' }}</el-button>
          <el-button size="small" type="danger" @click="removeCron">删除</el-button>
        </el-space>
        <div v-if="cronBlocked" class="cron-warn">
          任务状态 {{ task.status }} 不可执行，到点只会空跑（不产生会话，服务端只写一条告警日志）——需先发布
        </div>
      </template>
      <template v-else>
        <el-space>
          <el-input v-model="newCronExpr" placeholder="*/5 * * * *（5 段）" style="width: 200px" />
          <el-select v-model="newCronTz" clearable placeholder="时区（默认服务器）" style="width: 170px; margin-left: 8px">
            <el-option v-for="tz in ['Asia/Shanghai','Asia/Tokyo','Europe/London','America/New_York','UTC']" :key="tz" :label="tz" :value="tz" />
          </el-select>
          <el-button type="primary" @click="addCron">添加触发器</el-button>
        </el-space>
      </template>
    </el-card>

    <el-dialog v-model="hostDialog.visible" title="本机 Chrome 未连接" width="520px">
      <HostInstallGuide />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getBrowserTask, publishBrowserTask, runBrowserTask,
  listBrowserTaskSessions, listBrowserTaskReceipts,
  createBrowserCron, listBrowserCron, enableBrowserCron, disableBrowserCron, deleteBrowserCron,
} from '@/api/browserAutomation'
import { classifyRunError, HOST_OFFLINE, TASK_BUSY, RUN_ERROR_TEXT } from './hostRunError'
import { sessionDurationText } from './durationText'
import HostInstallGuide from './HostInstallGuide.vue'

const route = useRoute()
const router = useRouter()
const task = ref(null)
const sessions = ref([])
const receipts = ref([])
const cron = ref(null)
const newCronExpr = ref('*/5 * * * *')
const newCronTz = ref('Asia/Shanghai')
const hostDialog = reactive({ visible: false })

const unpack = (res) => res?.data ?? res

// 与执行历史/触发器同一条任务状态预算：draft/archived 挂了启用中的触发器也不会产生会话
const RUNNABLE_STATUS = ['ready', 'paused', 'done', 'failed']
const cronBlocked = computed(() => !!cron.value?.enabled && !RUNNABLE_STATUS.includes(task.value?.status))

async function load() {
  const id = route.params.id
  const res = await getBrowserTask(id)
  task.value = unpack(res)
  const sRes = await listBrowserTaskSessions(id, { limit: 20 })
  const sData = unpack(sRes)
  sessions.value = sData?.list || []

  // 回执读取失败不许拖垮整页：会话历史与触发器是这一页的主干，
  // 而回执只是验收面——它读不到时应该显示「暂无」而不是白屏。
  try {
    const rRes = await listBrowserTaskReceipts(id)
    const rData = unpack(rRes)
    receipts.value = rData?.list || (Array.isArray(rData) ? rData : [])
  } catch {
    receipts.value = []
  }

  const cRes = await listBrowserCron()
  const cList = unpack(cRes)
  const list = Array.isArray(cList) ? cList : cList?.list || []
  cron.value = list.find((c) => c.task_id === Number(id)) || null
}

async function onRun() {
  try {
    const res = await runBrowserTask(task.value.id)
    // 点了执行就要看得见执行：直接进这条会话的监控页（放行闸门、步骤流都在那里）
    const sid = unpack(res)?.session_id
    ElMessage.success('已开始执行')
    load()
    if (sid) router.push(`/browser-automation/sessions/${sid}`)
  } catch (e) {
    const kind = classifyRunError(e)
    if (kind === HOST_OFFLINE) hostDialog.visible = true
    else if (kind === TASK_BUSY) ElMessage.warning(RUN_ERROR_TEXT(e))
    else ElMessage.error(RUN_ERROR_TEXT(e))
  }
}

async function onPublish() {
  try {
    await publishBrowserTask(task.value.id)
    ElMessage.success('已发布')
    load()
  } catch (e) { ElMessage.error(RUN_ERROR_TEXT(e)) }
}

async function addCron() {
  try {
    await createBrowserCron({ task_id: Number(route.params.id), cron_expr: newCronExpr.value, time_zone: newCronTz.value || '', enabled: true })
    ElMessage.success('触发器已添加')
    load()
  } catch (e) { ElMessage.error(String(e?.message || e)) }
}

// 启停/删除失败时必须回读后端真相：否则开关停在「看起来已经改了」的位置，
// 用户以为定时器已停用，实际下一个触发点照样会响。
async function toggleCron() {
  const next = !cron.value.enabled
  try {
    if (cron.value.enabled) await disableBrowserCron(cron.value.id)
    else await enableBrowserCron(cron.value.id)
    ElMessage.success(next ? '已启用' : '已停用')
  } catch (e) {
    ElMessage.error(`操作失败：${String(e?.message || e)}`)
  }
  await load()
}

async function removeCron() {
  try {
    await ElMessageBox.confirm('确认删除该触发器？', '提示', { type: 'warning' })
  } catch { return } // 用户取消不是失败
  try {
    await deleteBrowserCron(cron.value.id)
    ElMessage.success('触发器已删除')
  } catch (e) {
    ElMessage.error(`删除失败：${String(e?.message || e)}`)
  }
  await load()
}

onMounted(load)
</script>

<style scoped>
.page { padding: 16px; }
.header { display: flex; justify-content: space-between; align-items: center; }
.cron-warn { color: #e6a23c; font-size: 12px; line-height: 1.6; margin-top: 8px; }
</style>
