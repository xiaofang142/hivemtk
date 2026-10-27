<template>
  <div class="page">
    <div class="toolbar">
      <el-button
        :loading="loading"
        @click="load"
      >
        刷新
      </el-button>
      <span class="hint">
        资产 = 管理员审核上架的 ego 浏览器脚本（已过静态扫描）。点「运行」会在你自己的 ego-runner 上执行。
      </span>
    </div>

    <el-empty
      v-if="!loading && !list.length"
      description="暂无已上架资产（管理员在「资产发布台」发布后这里可见）"
    />

    <el-row
      v-else
      :gutter="12"
    >
      <el-col
        v-for="a in list"
        :key="a.id"
        :xs="24"
        :sm="12"
        :md="8"
        style="margin-bottom: 12px"
      >
        <el-card shadow="hover">
          <template #header>
            <div class="card-head">
              <span class="name">{{ a.name }}</span>
              <el-tag size="small">
                v{{ a.version }}
              </el-tag>
            </div>
          </template>
          <div class="row">
            <span class="label">编码</span><code>{{ a.asset_code }}</code>
          </div>
          <div class="row">
            <span class="label">域名</span>{{ domainsText(a.domains) }}
          </div>
          <div class="row">
            <span class="label">写操作</span>
            <template v-if="writesOf(a).length">
              <el-tag
                v-for="w in writesOf(a)"
                :key="w"
                size="small"
                type="warning"
                style="margin-right: 4px"
              >
                {{ w }}
              </el-tag>
              <span class="sub">（执行时逐步确认）</span>
            </template>
            <span
              v-else
              class="sub"
            >纯读（免确认）</span>
          </div>
          <div class="row">
            <span class="label">来源</span>
            {{ a.source === 'pack' ? '平台发布' : '用户提交审核' }}
            <span class="sub"> · 被运行 {{ a.install_count }} 次</span>
          </div>
          <div class="actions">
            <el-button
              type="primary"
              @click="openRun(a)"
            >
              运行
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog
      v-model="runVisible"
      :title="`运行资产：${current?.name || ''}`"
      width="560px"
    >
      <el-form label-width="110px">
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="运行契约"
        >
          <div>声明了写动作（{{ (writesOf(current) || []).join('、') }}）的资产服务端强制逐步确认：脚本在写提交点弹确认，你在「执行监控」页允许/拒绝。</div>
          <div>导航/外联仅限资产声明的域名白名单内，越域会被 runner 强制终止。</div>
        </el-alert>
        <el-form-item label="参数 JSON">
          <el-input
            v-model="runForm.paramsText"
            type="textarea"
            :rows="5"
            placeholder="可选；将作为 params 透传给脚本"
          />
        </el-form-item>
        <el-form-item label="space 名">
          <el-input
            v-model="runForm.spaceName"
            placeholder="可选；同名 space 会续跑，便于多轮任务延续"
          />
        </el-form-item>
        <el-form-item label="超时（秒）">
          <el-input-number
            v-model="runForm.timeoutSec"
            :min="30"
            :max="3600"
            :step="30"
          />
        </el-form-item>
      </el-form>
      <el-alert
        v-if="current && writesOf(current).length"
        type="warning"
        :closable="false"
        show-icon
        title="该资产声明了写操作：执行过程中每一步写都会要求你允许/拒绝（不可关闭）"
      />
      <template #footer>
        <el-button @click="runVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="running"
          @click="doRun"
        >
          下发执行
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { listEgoAssets, startEgoExecution } from '@/api/egolite'

const loading = ref(false)
const list = ref([])
const runVisible = ref(false)
const running = ref(false)
const current = ref(null)
const runForm = ref({ paramsText: '', spaceName: '', timeoutSec: 900 })

const domainsText = (domains) => {
  if (!domains) return '-'
  if (Array.isArray(domains)) return domains.join(', ')
  try {
    return JSON.parse(domains).join(', ')
  } catch {
    return '-'
  }
}

const writesOf = (asset) => {
  if (!asset?.writes) return []
  if (Array.isArray(asset.writes)) return asset.writes
  try {
    const arr = JSON.parse(asset.writes)
    return Array.isArray(arr) ? arr : []
  } catch {
    return []
  }
}

const load = async () => {
  loading.value = true
  try {
    const data = await listEgoAssets()
    list.value = data?.list || []
  } finally {
    loading.value = false
  }
}

const openRun = (asset) => {
  current.value = asset
  runForm.value = { paramsText: '', spaceName: '', timeoutSec: 900 }
  runVisible.value = true
}

const doRun = async () => {
  running.value = true
  try {
    const payload = {
      origin: 'market',
      asset_id: current.value.asset_code,
      entry: current.value.entry,
      space_name: runForm.value.spaceName || undefined,
      timeout_sec: runForm.value.timeoutSec
    }
    if (String(runForm.value.paramsText || '').trim()) {
      payload.params = JSON.parse(runForm.value.paramsText)
    }
    if (writesOf(current.value).length) payload.confirm_policy = 'require'
    const exec = await startEgoExecution(payload)
    runVisible.value = false
    ElMessage.success(`已下发：执行 #${exec?.id}（${exec?.state}）→ 去「执行监控」看事件`)
  } catch (e) {
    ElMessage.error(e?.message || '下发失败（runner 可能不在线）')
  } finally {
    running.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page { padding: 12px; }
.toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.hint { color: #909399; font-size: 12px; }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.name { font-weight: 600; }
.row { display: flex; gap: 6px; font-size: 13px; line-height: 2; flex-wrap: wrap; }
.label { color: #909399; min-width: 56px; }
.sub { color: #909399; font-size: 12px; }
.actions { margin-top: 10px; }
</style>
