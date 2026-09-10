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
          <template #default="{ row }">{{ (row.duration_ms / 1000).toFixed(1) }}s</template>
        </el-table-column>
        <el-table-column prop="error_msg" label="错误" min-width="200" show-overflow-tooltip />
        <el-table-column prop="created_at" label="时间" width="170">
          <template #default="{ row }">{{ new Date(row.created_at).toLocaleString('zh-CN') }}</template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card v-if="task.task_type === 'cron'" header="定时触发器" style="margin-top: 16px">
      <template v-if="cron">
        <el-space>
          <span>cron：{{ cron.cron_expr }}</span>
          <el-tag>{{ cron.enabled ? '已启用' : '已停用' }}</el-tag>
          <el-button size="small" @click="toggleCron">{{ cron.enabled ? '停用' : '启用' }}</el-button>
          <el-button size="small" type="danger" @click="removeCron">删除</el-button>
        </el-space>
      </template>
      <template v-else>
        <el-space>
          <el-input v-model="newCronExpr" placeholder="*/5 * * * *（5 段）" style="width: 200px" />
          <el-button type="primary" @click="addCron">添加触发器</el-button>
        </el-space>
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getBrowserTask, publishBrowserTask, runBrowserTask,
  listBrowserTaskSessions,
  createBrowserCron, listBrowserCron, enableBrowserCron, disableBrowserCron, deleteBrowserCron,
} from '@/api/browserAutomation'

const route = useRoute()
const task = ref(null)
const sessions = ref([])
const cron = ref(null)
const newCronExpr = ref('*/5 * * * *')

const unpack = (res) => res?.data ?? res

async function load() {
  const id = route.params.id
  const res = await getBrowserTask(id)
  task.value = unpack(res)
  const sRes = await listBrowserTaskSessions(id, { limit: 20 })
  const sData = unpack(sRes)
  sessions.value = sData?.list || []

  const cRes = await listBrowserCron()
  const cList = unpack(cRes)
  const list = Array.isArray(cList) ? cList : cList?.list || []
  cron.value = list.find((c) => c.task_id === Number(id)) || null
}

async function onRun() {
  try {
    await runBrowserTask(task.value.id)
    ElMessage.success('已开始执行')
    setTimeout(load, 500)
  } catch (e) { ElMessage.error(String(e?.message || e)) }
}

async function onPublish() {
  await publishBrowserTask(task.value.id)
  ElMessage.success('已发布')
  load()
}

async function addCron() {
  try {
    await createBrowserCron({ task_id: Number(route.params.id), cron_expr: newCronExpr.value, enabled: true })
    ElMessage.success('触发器已添加')
    load()
  } catch (e) { ElMessage.error(String(e?.message || e)) }
}

async function toggleCron() {
  if (cron.value.enabled) await disableBrowserCron(cron.value.id)
  else await enableBrowserCron(cron.value.id)
  load()
}

async function removeCron() {
  await ElMessageBox.confirm('确认删除该触发器？', '提示', { type: 'warning' })
  await deleteBrowserCron(cron.value.id)
  load()
}

onMounted(load)
</script>

<style scoped>
.page { padding: 16px; }
.header { display: flex; justify-content: space-between; align-items: center; }
</style>
