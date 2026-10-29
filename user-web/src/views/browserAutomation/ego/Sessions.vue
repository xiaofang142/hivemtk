<template>
  <div class="page">
    <div class="head">
      <h2>通用浏览器会话</h2>
      <el-space>
        <el-button
          :loading="loading"
          @click="load"
        >
          刷新
        </el-button>
        <el-button
          type="primary"
          @click="createVisible = true"
        >
          新建会话
        </el-button>
      </el-space>
    </div>

    <el-alert
      type="info"
      :closable="false"
      show-icon
      style="margin: 12px 0"
      title="会话 = 多轮任务；每轮提交一组受限动作（ops），执行在你的本机 ego lite / 浏览器中。写动作会进入等待确认。"
    />

    <el-table
      v-loading="loading"
      :data="sessions"
      border
    >
      <el-table-column
        prop="id"
        label="ID"
        width="80"
      />
      <el-table-column
        prop="goal"
        label="目标"
        min-width="220"
        show-overflow-tooltip
      />
      <el-table-column
        prop="engine"
        label="引擎"
        width="90"
      />
      <el-table-column
        prop="state"
        label="状态"
        width="140"
      >
        <template #default="{ row }">
          <el-tag :type="stateType(row.state)">
            {{ row.state }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        prop="round"
        label="轮次"
        width="80"
      />
      <el-table-column
        prop="space_name"
        label="TaskSpace"
        min-width="180"
        show-overflow-tooltip
      />
      <el-table-column
        label="操作"
        width="280"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            @click="openDetail(row)"
          >
            详情
          </el-button>
          <el-button
            link
            type="danger"
            @click="stop(row)"
          >
            停止
          </el-button>
          <el-button
            link
            @click="exportAudit(row, 'json')"
          >
            导出JSON
          </el-button>
          <el-button
            link
            @click="exportAudit(row, 'csv')"
          >
            CSV
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-if="total > pageSize"
      style="margin-top: 12px"
      layout="prev, pager, next"
      :total="total"
      :page-size="pageSize"
      :current-page="page"
      @current-change="(p) => { page = p; load() }"
    />

    <!-- 新建 -->
    <el-dialog
      v-model="createVisible"
      title="新建通用会话"
      width="560px"
    >
      <el-form label-width="110px">
        <el-form-item label="目标">
          <el-input
            v-model="form.goal"
            placeholder="例如：在官网搜索并抓取前 5 条结果"
          />
        </el-form-item>
        <el-form-item label="域名白名单">
          <el-input
            v-model="form.domains"
            placeholder="example.com, *.example.org（逗号分隔；* 表示任意站点）"
          />
        </el-form-item>
        <el-form-item label="引擎">
          <el-select v-model="form.engine">
            <el-option
              value="auto"
              label="auto（ego 优先）"
            />
            <el-option
              value="ego"
              label="ego lite"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="写确认">
          <el-select v-model="form.write_confirm">
            <el-option
              value="require"
              label="require（推荐）"
            />
            <el-option
              value="none"
              label="none"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="轮次上限">
          <el-input-number
            v-model="form.max_rounds"
            :min="1"
            :max="200"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          @click="create"
        >
          创建
        </el-button>
      </template>
    </el-dialog>

    <!-- 详情 -->
    <el-drawer
      v-model="detailVisible"
      title="会话详情"
      size="60%"
    >
      <template v-if="current">
        <el-descriptions
          :column="3"
          border
          size="small"
        >
          <el-descriptions-item label="ID">
            {{ current.id }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="stateType(current.state)">
              {{ current.state }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="轮次">
            {{ current.round }}
          </el-descriptions-item>
          <el-descriptions-item
            label="目标"
            :span="3"
          >
            {{ current.goal }}
          </el-descriptions-item>
          <el-descriptions-item
            label="TaskSpace"
            :span="2"
          >
            {{ current.space_name }}
          </el-descriptions-item>
          <el-descriptions-item label="spaceId">
            {{ current.space_id || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <el-space style="margin: 12px 0">
          <el-button
            size="small"
            @click="reloadDetail"
          >
            刷新
          </el-button>
          <el-button
            v-if="current.state === 'waiting_confirm'"
            size="small"
            type="success"
            @click="confirm(true)"
          >
            允许写
          </el-button>
          <el-button
            v-if="current.state === 'waiting_confirm'"
            size="small"
            type="danger"
            @click="confirm(false)"
          >
            拒绝
          </el-button>
          <el-button
            size="small"
            @click="finish"
          >
            收尾
          </el-button>
        </el-space>

        <el-card
          shadow="never"
          style="margin-bottom: 12px"
        >
          <template #header>
            提交一轮 ops（JSON 数组）
          </template>
          <el-input
            v-model="opsText"
            type="textarea"
            :rows="7"
            placeholder="[{&quot;op&quot;:&quot;goto&quot;,&quot;url&quot;:&quot;https://example.com&quot;}]"
          />
          <div style="margin-top: 8px">
            <el-button
              type="primary"
              size="small"
              :loading="acting"
              @click="act"
            >
              提交本轮
            </el-button>
            <el-button
              size="small"
              @click="saveAsFlow"
            >
              沉淀为流程
            </el-button>
            <span class="hint">跨轮请用观测返回的 loc= 定位，不要使用快照 ref</span>
          </div>
        </el-card>

        <el-table
          :data="rounds"
          border
          size="small"
        >
          <el-table-column
            prop="round"
            label="轮"
            width="60"
          />
          <el-table-column
            label="结果"
            width="80"
          >
            <template #default="{ row }">
              <el-tag
                v-if="row.ok === true"
                type="success"
              >
                ok
              </el-tag>
              <el-tag
                v-else-if="row.ok === false"
                type="danger"
              >
                fail
              </el-tag>
              <el-tag
                v-else
                type="info"
              >
                pending
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            prop="url"
            label="URL"
            min-width="200"
            show-overflow-tooltip
          />
          <el-table-column
            prop="title"
            label="标题"
            min-width="140"
            show-overflow-tooltip
          />
          <el-table-column
            label="动作/耗时"
            width="120"
          >
            <template #default="{ row }">
              {{ row.usage?.actions || 0 }} / {{ row.usage?.ms || 0 }}ms
            </template>
          </el-table-column>
          <el-table-column
            label="观测"
            min-width="260"
          >
            <template #default="{ row }">
              <div
                v-if="row.error"
                class="err"
              >
                {{ row.error.code }}: {{ row.error.msg }}
              </div>
              <div
                v-if="row.targets?.length"
                class="targets"
              >
                <el-tag
                  v-for="t in row.targets.slice(0, 6)"
                  :key="t.ref"
                  size="small"
                  style="margin: 2px"
                >
                  {{ t.role }} · {{ t.name || t.loc || t.ref }}
                </el-tag>
              </div>
              <div
                v-if="row.text"
                class="text"
              >
                {{ row.text.slice(0, 160) }}
              </div>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listEgoSessions, createEgoSession, listEgoSessionRounds,
  runEgoSessionRound, confirmEgoSession, abortEgoSession, finishEgoSession, getEgoSession,
  exportEgoSessionAudit, saveEgoSessionAsFlow
} from '@/api/egolite'

const loading = ref(false)
const creating = ref(false)
const acting = ref(false)
const sessions = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20

const createVisible = ref(false)
const detailVisible = ref(false)
const current = ref(null)
const rounds = ref([])
const opsText = ref('[{"op":"goto","url":"https://example.com"}]')

const form = ref({ goal: '', domains: '', engine: 'auto', write_confirm: 'require', max_rounds: 30 })

const stateType = (s) => ({
  created: 'info', active: 'primary', waiting_confirm: 'warning',
  handed_off: 'warning', finished: 'success', failed: 'danger', stopped: 'info', timeout: 'danger'
}[s] || 'info')

async function load() {
  loading.value = true
  try {
    const res = await listEgoSessions({ page: page.value, page_size: pageSize })
    sessions.value = res?.list || res?.data?.list || []
    total.value = res?.total || res?.data?.total || 0
  } finally {
    loading.value = false
  }
}

async function create() {
  const domains = form.value.domains.split(',').map((s) => s.trim()).filter(Boolean)
  if (!domains.length) {
    ElMessage.warning('域名白名单必填（空 = 拒绝一切导航）')
    return
  }
  creating.value = true
  try {
    await createEgoSession({ ...form.value, domains, space_name: '' })
    ElMessage.success('会话已创建')
    createVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.message || '创建失败（runner 在线后才可创建）')
  } finally {
    creating.value = false
  }
}

async function openDetail(row) {
  current.value = row
  detailVisible.value = true
  await reloadDetail()
}

// 会话沉淀为流程（T23）：把已成功轮次编译成一条 flow（后端走「保存即扫描」同一道门）。
async function saveAsFlow() {
  if (!current.value) return
  try {
    const { value } = await ElMessageBox.prompt('流程名称（沉淀只合并已成功的轮次）', '沉淀为流程', {
      inputValue: current.value.goal ? String(current.value.goal).slice(0, 40) : '',
      inputPattern: /\S+/,
      inputErrorMessage: '名称必填'
    })
    const res = await saveEgoSessionAsFlow(current.value.id, value)
    ElMessage.success(`已沉淀为流程 #${res?.id ?? res?.data?.id ?? ''}，可在「我的流程」中查看/发布`)
  } catch (e) {
    if (e !== 'cancel' && e?.message) ElMessage.error(e.message)
  }
}

// 审计导出（T22）：会话→轮次→事件→产物 一条链。JSON 直接下载；CSV 后端给 blob。
async function exportAudit(row, format) {
  try {
    const res = await exportEgoSessionAudit(row.id, format)
    let blob
    let filename = `ego-session-${row.id}.${format}`
    if (format === 'csv') {
      blob = res instanceof Blob ? res : new Blob([res], { type: 'text/csv' })
    } else {
      const payload = res?.data ?? res
      blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
    }
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error(e?.message || '导出失败')
  }
}

async function reloadDetail() {
  if (!current.value) return
  const [sess, list] = await Promise.all([
    getEgoSession(current.value.id),
    listEgoSessionRounds(current.value.id, { limit: 50 })
  ])
  current.value = sess?.data || sess
  rounds.value = list?.list || list?.data?.list || []
}

async function act() {
  let ops
  try {
    ops = JSON.parse(opsText.value)
  } catch {
    ElMessage.error('ops 不是合法 JSON')
    return
  }
  acting.value = true
  try {
    await runEgoSessionRound(current.value.id, { ops })
    ElMessage.success('已派发本轮')
    await reloadDetail()
  } catch (e) {
    ElMessage.error(e?.message || '提交失败')
  } finally {
    acting.value = false
  }
}

async function confirm(allow) {
  try {
    await confirmEgoSession(current.value.id, allow)
    ElMessage.success(allow ? '已允许' : '已拒绝')
    await reloadDetail()
  } catch (e) {
    ElMessage.error(e?.message || '裁决失败')
  }
}

async function stop(row) {
  try {
    await ElMessageBox.confirm('停止后本轮进程会被终止，确定？', '停止会话', { type: 'warning' })
    await abortEgoSession(row.id)
    ElMessage.success('已停止')
    await load()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e?.message || '停止失败')
  }
}

async function finish() {
  try {
    await finishEgoSession(current.value.id, [])
    ElMessage.success('已收尾')
    await reloadDetail()
  } catch (e) {
    ElMessage.error(e?.message || '收尾失败')
  }
}

onMounted(load)
</script>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; }
.hint { margin-left: 8px; color: #909399; font-size: 12px; }
.err { color: #f56c6c; font-size: 12px; }
.text { color: #606266; font-size: 12px; white-space: pre-wrap; }
.targets { margin: 2px 0; }
</style>
