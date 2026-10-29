<template>
  <div class="page">
    <div class="toolbar">
      <el-button
        :loading="loading"
        @click="load"
      >
        刷新
      </el-button>
      <el-switch
        v-model="autoRefresh"
        active-text="自动刷新(3s)"
      />
      <el-select
        v-model="filterState"
        placeholder="状态"
        clearable
        style="width: 150px"
      >
        <el-option
          v-for="s in stateOptions"
          :key="s.value"
          :label="s.label"
          :value="s.value"
        />
      </el-select>
      <span class="hint">点行看事件流；waiting_confirm 的执行需要你逐步允许/拒绝写动作。</span>
    </div>

    <el-table
      v-loading="loading"
      :data="filtered"
      border
      @row-click="openDetail"
    >
      <el-table-column
        prop="id"
        label="ID"
        width="70"
      />
      <el-table-column
        prop="origin"
        label="来源"
        width="90"
      />
      <el-table-column
        label="目标"
        min-width="200"
      >
        <template #default="{ row }">
          <div>{{ row.origin === 'market' ? `资产 ${row.asset_id}` : `流程 #${row.flow_id}` }}</div>
          <div class="sub">
            entry: {{ row.entry }}
          </div>
        </template>
      </el-table-column>
      <el-table-column
        label="状态"
        width="130"
      >
        <template #default="{ row }">
          <el-tag :type="stateTag(row.state)">
            {{ stateText(row.state) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        prop="device_id"
        label="runner"
        width="180"
        show-overflow-tooltip
      />
      <el-table-column
        label="确认"
        width="90"
      >
        <template #default="{ row }">
          {{ row.confirm_policy === 'require' ? '逐步' : '免确认' }}
        </template>
      </el-table-column>
      <el-table-column
        prop="created_at"
        label="创建时间"
        width="180"
      />
      <el-table-column
        label="操作"
        width="150"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            size="small"
            @click.stop="openDetail(row)"
          >
            详情
          </el-button>
          <el-button
            v-if="row.state === 'created'"
            size="small"
            type="warning"
            @click.stop="onRedispatch(row)"
          >
            重发
          </el-button>
          <el-button
            size="small"
            type="danger"
            :disabled="isTerminal(row.state)"
            @click.stop="onAbort(row)"
          >
            停止
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-drawer
      v-model="drawerVisible"
      :title="current ? `执行 #${current.id}` : '执行详情'"
      size="640px"
    >
      <template v-if="current">
        <el-descriptions
          :column="2"
          border
        >
          <el-descriptions-item label="状态">
            {{ stateText(current.state) }}
          </el-descriptions-item>
          <el-descriptions-item label="轮次">
            {{ current.round }}
          </el-descriptions-item>
          <el-descriptions-item label="来源">
            {{ current.origin }}
          </el-descriptions-item>
          <el-descriptions-item label="入口">
            {{ current.entry }}
          </el-descriptions-item>
          <el-descriptions-item label="资产/流程">
            {{ current.origin === 'market' ? current.asset_id : `#${current.flow_id}` }}
          </el-descriptions-item>
          <el-descriptions-item label="space">
            {{ current.space_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="下发时间">
            {{ current.dispatched_at || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="结束时间">
            {{ current.ended_at || '-' }}
          </el-descriptions-item>
          <el-descriptions-item
            label="失败原因"
            :span="2"
          >
            {{ current.fail_reason || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <div
          v-if="current.state === 'waiting_confirm'"
          class="confirm-bar"
        >
          <el-alert
            type="warning"
            :closable="false"
            show-icon
            title="脚本请求执行写操作，等待你裁决"
          />
          <el-space>
            <el-button
              type="primary"
              @click="onConfirm(true)"
            >
              允许
            </el-button>
            <el-button
              type="danger"
              @click="onConfirm(false)"
            >
              拒绝
            </el-button>
          </el-space>
        </div>

        <template v-if="artifacts.length">
          <h4>产物</h4>
          <div class="artifacts">
            <div
              v-for="(a, i) in artifacts"
              :key="`${a.kind}-${i}`"
              class="artifact"
            >
              <el-image
                v-if="a.image"
                :src="a.image"
                :preview-src-list="imageList"
                :initial-index="a.imageIndex"
                fit="cover"
                class="thumb"
                preview-teleported
              />
              <div class="artifact-meta">
                <el-tag
                  size="small"
                  :type="a.kind === 'artifact' ? 'success' : 'info'"
                >
                  {{ a.kind }}
                </el-tag>
                <a
                  v-if="a.image"
                  :href="a.image"
                  target="_blank"
                  rel="noopener"
                  class="sub"
                >打开原图</a>
                <span
                  v-if="a.summary"
                  class="sub"
                >{{ a.summary }}</span>
              </div>
            </div>
          </div>
        </template>

        <h4>
          事件流<span
            v-if="streaming"
            class="sub live"
          > · 实时</span><span
            v-else-if="streamFailed"
            class="sub"
          > · 轮询兜底</span>
        </h4>
        <el-timeline v-if="events.length">
          <el-timeline-item
            v-for="ev in events"
            :key="ev.id"
            :timestamp="`#${ev.seq} ${ev.type} · ${ev.created_at}`"
          >
            <div>
              <span v-if="ev.action">{{ ev.action }} </span>
              <el-tag
                v-if="ev.ok === true"
                type="success"
                size="small"
              >
                ok
              </el-tag>
              <el-tag
                v-else-if="ev.ok === false"
                type="danger"
                size="small"
              >
                fail
              </el-tag>
              <el-tag
                v-else
                size="small"
                type="info"
              >
                无结论
              </el-tag>
              <span
                v-if="ev.duration_ms"
                class="sub"
              > {{ ev.duration_ms }}ms</span>
            </div>
            <pre class="payload">{{ pretty(ev.payload) }}</pre>
          </el-timeline-item>
        </el-timeline>
        <el-empty
          v-else
          description="暂无事件（runner 未上线或脚本无输出）"
        />
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  abortEgoExecution,
  confirmEgoExecution,
  listEgoEvents,
  listEgoExecutions,
  redispatchEgoExecution,
  streamEgoEvents
} from '@/api/egolite'

const loading = ref(false)
const list = ref([])
const filterState = ref('')
const autoRefresh = ref(true)
const drawerVisible = ref(false)
const current = ref(null)
const events = ref([])
const streaming = ref(false)
const streamFailed = ref(false)
let timer = null
let stream = null

const stateOptions = [
  { value: 'created', label: '待下发' },
  { value: 'dispatching', label: '下发中' },
  { value: 'running', label: '执行中' },
  { value: 'waiting_confirm', label: '等待确认' },
  { value: 'completed', label: '已完成' },
  { value: 'failed', label: '失败' },
  { value: 'stopped', label: '已停止' },
  { value: 'timeout', label: '超时' }
]

const stateText = (s) => stateOptions.find((x) => x.value === s)?.label || s || '-'
const stateTag = (s) =>
  ({
    created: 'info',
    dispatching: 'warning',
    running: '',
    waiting_confirm: 'warning',
    completed: 'success',
    failed: 'danger',
    stopped: 'info',
    timeout: 'danger'
  }[s] || 'info')
const isTerminal = (s) => ['completed', 'failed', 'stopped', 'timeout'].includes(s)

const filtered = computed(() =>
  filterState.value ? list.value.filter((x) => x.state === filterState.value) : list.value
)

const pretty = (payload) => {
  if (!payload) return ''
  try {
    return JSON.stringify(payload, null, 2)
  } catch {
    return String(payload)
  }
}

// 产物预览（S-13）：产物本来就是事件（runner 上报 screenshot/extract），
// 从事件流里就地取，不再让用户去翻 payload——图片直接看图，其余给一行摘要。
const artifacts = computed(() =>
  events.value
    .filter((ev) => ev.type === 'artifact' || ev.type === 'snapshot')
    .map((ev) => {
      const p = ev.payload || {}
      const image =
        typeof p.url === 'string' && /^https?:|^\/(api|uploads|static)\//.test(p.url) ? p.url : ''
      const summary = p.summary || p.note || p.key || p.title || ''
      return { kind: ev.type, image, summary: String(summary || ''), imageIndex: 0 }
    })
)
const imageList = computed(() => artifacts.value.filter((a) => a.image).map((a) => a.image))

const stopStream = () => {
  if (stream) {
    try {
      stream.abort()
    } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
    stream = null
  }
  streaming.value = false
}

// 事件流：先补历史（一次 HTTP），再从游标处接 SSE 增量。断流/不支持时退回 3s 轮询。
const startStream = () => {
  stopStream()
  if (!current.value) return
  streamFailed.value = false
  streaming.value = true
  const id = current.value.id
  try {
    stream = streamEgoEvents(id, {
      afterId: lastEventId.value,
      onEvent: (ev) => {
        if (!ev || !ev.id) return
        if (ev.id <= lastEventId.value) return
        events.value = events.value.concat(ev).slice(-1000)
        lastEventId.value = ev.id
        // waiting_confirm 之类需要用户动作的状态要立刻反映到详情头
        if (ev.type === 'confirm') load()
      },
      onEnd: (info) => {
        if (info && current.value) {
          current.value.state = info.state || current.value.state
          current.value.fail_reason = info.fail_reason || current.value.fail_reason
        }
        stopStream()
        load()
      }
    })
    stream.done.catch(() => {
      // 流不可用（反代关缓冲/网关超时/服务端重启）：静默退回轮询，不打扰用户
      streaming.value = false
      streamFailed.value = true
    })
  } catch (_) {
    streaming.value = false
    streamFailed.value = true
  }
}

const load = async () => {
  loading.value = true
  try {
    const data = await listEgoExecutions({ page: 1, page_size: 100 })
    list.value = data?.list || []
    if (current.value) {
      const fresh = list.value.find((x) => x.id === current.value.id)
      if (fresh) current.value = fresh
    }
  } finally {
    loading.value = false
  }
}

const openDetail = async (row) => {
  current.value = row
  drawerVisible.value = true
  lastEventId.value = 0
  events.value = []
  await loadEvents(true)
  startStream()
}

// 事件流增量轮询（after_id 游标）：抽屉开着时随 3s 自动刷新只拉新增帧，
// 执行中的时间线才跟得上脚本；全量重拉在长执行下既慢又会截断到前 200 条。
const lastEventId = ref(0)
const loadEvents = async (reset) => {
  if (!current.value) return
  const params = reset
    ? { limit: 200 }
    : { after_id: lastEventId.value, limit: 200 }
  const data = await listEgoEvents(current.value.id, params)
  const fresh = data?.list || []
  if (reset) {
    events.value = fresh
  } else if (fresh.length) {
    events.value = events.value.concat(fresh).slice(-1000)
  }
  if (fresh.length) lastEventId.value = fresh[fresh.length - 1].id
}

const onAbort = async (row) => {
  await abortEgoExecution(row.id)
  ElMessage.success('已停止')
  await load()
}

const onRedispatch = async (row) => {
  await redispatchEgoExecution(row.id)
  ElMessage.success('已重发；runner 在线即可执行')
  await load()
}

const onConfirm = async (allow) => {
  await confirmEgoExecution(current.value.id, allow)
  ElMessage.success(allow ? '已允许' : '已拒绝')
  await load()
  await openDetail(current.value)
}

onMounted(() => {
  load()
  timer = setInterval(() => {
    if (!autoRefresh.value) return
    load()
    // 事件优先走 SSE；只有流不可用时才退回 3s 增量轮询
    if (drawerVisible.value && !streaming.value) loadEvents(false)
  }, 3000)
})
onBeforeUnmount(() => {
  timer && clearInterval(timer)
  stopStream()
})

watch(drawerVisible, (v) => {
  if (!v) stopStream()
})
</script>

<style scoped>
.page { padding: 12px; }
.toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.hint { color: #909399; font-size: 12px; }
.sub { color: #909399; font-size: 12px; }
.confirm-bar { margin: 16px 0; display: flex; flex-direction: column; gap: 8px; }
.artifacts { display: flex; flex-wrap: wrap; gap: 10px; margin: 8px 0 4px; }
.artifact { width: 180px; }
.thumb { width: 180px; height: 110px; border: 1px solid #ebeef5; border-radius: 4px; background: #f5f7fa; }
.artifact-meta { display: flex; align-items: center; gap: 6px; margin-top: 4px; flex-wrap: wrap; }
.live { color: #67c23a; }
.payload {
  margin: 4px 0 0;
  padding: 6px 8px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 12px;
  max-height: 220px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
