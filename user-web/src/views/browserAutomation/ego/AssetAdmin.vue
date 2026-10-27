<template>
  <div class="page">
    <h2>资产发布台（平台端）</h2>
    <p class="hint">
      发布 = 往本实例资产目录写一行（脚本快照 + sha256 + 扫描结论），**不经过任何外部平台**。
      发布后全租户用户立即可在「资产中心」看到并运行。
    </p>

    <el-tabs v-model="tab">
      <!-- 审核用户流程 -->
      <el-tab-pane
        label="待审流程"
        name="review"
      >
        <div class="toolbar">
          <el-button
            :loading="loadingFlows"
            @click="loadFlows"
          >
            刷新
          </el-button>
          <span class="hint">用户「提交审核」后的流程在这里出现；发布时会按当前扫描规则复扫一遍。</span>
        </div>
        <el-table
          v-loading="loadingFlows"
          :data="flows"
          border
        >
          <el-table-column
            prop="id"
            label="ID"
            width="70"
          />
          <el-table-column
            prop="name"
            label="名称"
            min-width="160"
          />
          <el-table-column
            prop="user_id"
            label="提交人"
            width="90"
          />
          <el-table-column
            prop="version"
            label="版本"
            width="80"
          />
          <el-table-column
            label="域名"
            min-width="150"
          >
            <template #default="{ row }">
              {{ domainsText(row.domains) }}
            </template>
          </el-table-column>
          <el-table-column
            label="操作"
            width="230"
            fixed="right"
          >
            <template #default="{ row }">
              <el-button
                size="small"
                @click="previewFlow(row)"
              >
                查看脚本
              </el-button>
              <el-button
                size="small"
                type="primary"
                @click="onPublishFlow(row)"
              >
                审核发布
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 直接发布脚本包 -->
      <el-tab-pane
        label="直接发布脚本包"
        name="pack"
      >
        <el-form
          :model="packForm"
          label-width="120px"
          style="max-width: 760px"
        >
          <el-form-item label="资产编码">
            <el-input
              v-model="packForm.asset_code"
              placeholder="留空按名称 slug 化，例如 github-repo-search（执行侧 asset_id）"
            />
          </el-form-item>
          <el-form-item
            label="名称"
            required
          >
            <el-input v-model="packForm.name" />
          </el-form-item>
          <el-form-item
            label="域名白名单"
            required
          >
            <el-input
              v-model="packForm.domainsText"
              placeholder="github.com, api.github.com（逗号或换行分隔）"
            />
          </el-form-item>
          <el-form-item label="写动作声明">
            <el-input
              v-model="packForm.writesText"
              placeholder="例如：comment, submit（非空 ⇒ 执行强制逐步确认）"
            />
          </el-form-item>
          <el-form-item label="入口文件">
            <el-input
              v-model="packForm.entry"
              placeholder="留空自动推导 tools/{编码}.js"
            />
          </el-form-item>
          <el-form-item
            label="脚本"
            required
          >
            <el-input
              v-model="packForm.script"
              type="textarea"
              :rows="14"
              class="mono"
              placeholder="ego-browser ES module 脚本"
            />
          </el-form-item>
          <el-form-item>
            <el-button
              type="primary"
              :loading="publishing"
              @click="onPublishPack"
            >
              发布（含扫描门）
            </el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <!-- 资产目录全量 -->
      <el-tab-pane
        label="资产目录"
        name="list"
      >
        <div class="toolbar">
          <el-button
            :loading="loadingAssets"
            @click="loadAssets"
          >
            刷新
          </el-button>
        </div>
        <el-table
          v-loading="loadingAssets"
          :data="assets"
          border
        >
          <el-table-column
            prop="asset_code"
            label="编码"
            width="180"
          />
          <el-table-column
            prop="name"
            label="名称"
            min-width="150"
          />
          <el-table-column
            prop="version"
            label="版本"
            width="80"
          />
          <el-table-column
            label="状态"
            width="100"
          >
            <template #default="{ row }">
              <el-tag :type="row.status === 'published' ? 'success' : 'info'">
                {{ row.status === 'published' ? '已上架' : '已下线' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="来源"
            width="120"
          >
            <template #default="{ row }">
              {{ row.source === 'pack' ? '脚本包' : `流程 #${row.source_flow_id}` }}
            </template>
          </el-table-column>
          <el-table-column
            label="扫描"
            width="90"
          >
            <template #default="{ row }">
              <el-tag :type="row.scan_passed ? 'success' : 'danger'">
                {{ row.scan_passed ? '通过' : '未过' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            prop="install_count"
            label="运行次数"
            width="100"
          />
          <el-table-column
            label="操作"
            width="120"
            fixed="right"
          >
            <template #default="{ row }">
              <el-button
                size="small"
                type="danger"
                :disabled="row.status !== 'published'"
                @click="onOffline(row)"
              >
                下线
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <el-dialog
      v-model="scriptVisible"
      :title="`流程 #${scriptFlow?.id || ''} 脚本`"
      width="820px"
    >
      <pre class="script">{{ scriptFlow?.script }}</pre>
      <template #footer>
        <el-button @click="scriptVisible = false">
          关闭
        </el-button>
        <el-button
          type="primary"
          @click="onPublishFlow(scriptFlow)"
        >
          审核发布
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listEgoAdminAssets,
  listEgoAdminFlows,
  offlineEgoAsset,
  publishEgoAssetFromFlow,
  publishEgoAssetPack
} from '@/api/egolite'

const tab = ref('review')
const loadingFlows = ref(false)
const loadingAssets = ref(false)
const publishing = ref(false)
const flows = ref([])
const assets = ref([])
const scriptVisible = ref(false)
const scriptFlow = ref(null)

const packForm = ref({
  asset_code: '',
  name: '',
  domainsText: '',
  writesText: '',
  entry: '',
  script: ''
})

const toArray = (text) =>
  String(text || '')
    .split(/[,，\n]/)
    .map((x) => x.trim())
    .filter(Boolean)

const domainsText = (domains) => {
  if (!domains) return '-'
  if (Array.isArray(domains)) return domains.join(', ')
  try {
    return JSON.parse(domains).join(', ')
  } catch {
    return '-'
  }
}

const loadFlows = async () => {
  loadingFlows.value = true
  try {
    const data = await listEgoAdminFlows({ status: 'submitted', page: 1, page_size: 100 })
    flows.value = data?.list || []
  } finally {
    loadingFlows.value = false
  }
}

const loadAssets = async () => {
  loadingAssets.value = true
  try {
    const data = await listEgoAdminAssets({ page: 1, page_size: 100 })
    assets.value = data?.list || []
  } finally {
    loadingAssets.value = false
  }
}

const previewFlow = (row) => {
  scriptFlow.value = row
  scriptVisible.value = true
}

const onPublishFlow = async (row) => {
  await ElMessageBox.confirm(
    `审核发布流程「${row.name}」为目录资产？发布时会按当前扫描规则复扫，通过后全租户可见。`,
    '审核发布',
    { type: 'info' }
  )
  try {
    const asset = await publishEgoAssetFromFlow(row.id)
    scriptVisible.value = false
    ElMessage.success(`已上架：${asset?.asset_code} v${asset?.version}`)
    await Promise.all([loadFlows(), loadAssets()])
  } catch (e) {
    ElMessage.error(e?.message || '发布失败（复扫可能未通过）')
  }
}

const onPublishPack = async () => {
  publishing.value = true
  try {
    const payload = {
      asset_code: packForm.value.asset_code || undefined,
      name: packForm.value.name,
      domains: toArray(packForm.value.domainsText),
      writes: toArray(packForm.value.writesText),
      entry: packForm.value.entry || undefined,
      script: packForm.value.script
    }
    const asset = await publishEgoAssetPack(payload)
    ElMessage.success(`已上架：${asset?.asset_code} v${asset?.version}`)
    packForm.value = { asset_code: '', name: '', domainsText: '', writesText: '', entry: '', script: '' }
    await loadAssets()
  } catch (e) {
    ElMessage.error(e?.message || '发布失败（扫描门未通过？）')
  } finally {
    publishing.value = false
  }
}

const onOffline = async (row) => {
  await ElMessageBox.confirm(
    `下线资产「${row.name}」？下线后用户端橱窗立即不可见、也不能再发起执行（已装副本仍在用户本机）。`,
    '下线资产',
    { type: 'warning' }
  )
  await offlineEgoAsset(row.id)
  ElMessage.success('已下线')
  await loadAssets()
}

watch(tab, (v) => {
  if (v === 'review') loadFlows()
  if (v === 'list') loadAssets()
})

onMounted(() => {
  loadFlows()
  loadAssets()
})
</script>

<style scoped>
.page { padding: 12px; }
.toolbar { display: flex; align-items: center; gap: 12px; margin: 8px 0; }
.hint { color: #909399; font-size: 12px; }
.mono :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
.script {
  margin: 0;
  padding: 10px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 12px;
  max-height: 60vh;
  overflow: auto;
  white-space: pre-wrap;
}
</style>
