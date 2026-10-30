<template>
  <div class="crawler-sources-page">
    <el-card class="toolbar-card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-select
            v-model="filter.kb_id"
            :placeholder="$t('knowledge.crawlerSource.selectKB')"
            clearable
            filterable
            style="width: 220px"
            @change="loadSources"
          >
            <el-option
              v-for="kb in kbList"
              :key="kb.id"
              :label="kb.name"
              :value="kb.id"
            />
          </el-select>
          <el-select
            v-model="filter.status"
            :placeholder="$t('knowledge.crawlerSource.status')"
            clearable
            style="width: 120px"
            @change="loadSources"
          >
            <el-option
              :label="$t('knowledge.crawlerSource.enabled')"
              value="active"
            />
            <el-option
              :label="$t('knowledge.crawlerSource.paused')"
              value="paused"
            />
            <el-option
              :label="$t('knowledge.crawlerSource.error')"
              value="error"
            />
          </el-select>
        </div>
        <div class="toolbar-right">
          <el-button
            type="primary"
            @click="showCreate = true"
          >
            {{ $t('knowledge.crawlerSource.create') }}
          </el-button>
          <el-button @click="loadSources">
            {{ $t('common.refresh') }}
          </el-button>
        </div>
      </div>
    </el-card>

    <el-card v-loading="loading">
      <el-table
        :data="sources"
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="70"
        />
        <el-table-column
          :label="$t('knowledge.crawlerSource.kb')"
          width="140"
        >
          <template #default="{ row }">
            {{ kbName(row.kb_id) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('knowledge.crawlerSource.type')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="row.source_type === 'sitemap' ? 'warning' : 'primary'">
              {{ row.source_type === 'sitemap' ? 'Sitemap' : 'URL' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="start_url"
          :label="$t('knowledge.crawlerSource.startUrl')"
          min-width="220"
          show-overflow-tooltip
        />
        <el-table-column
          :label="$t('knowledge.crawlerSource.siteVerify')"
          width="110"
        >
          <template #default="{ row }">
            <el-tag :type="row.site_verified ? 'success' : 'danger'">
              {{ row.site_verified ? $t('knowledge.crawlerSource.verified') : $t('knowledge.crawlerSource.unverified') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('knowledge.crawlerSource.status')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)">
              {{ $t(statusLabel(row.status)) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('knowledge.crawlerSource.interval')"
          width="100"
        >
          <template #default="{ row }">
            {{ row.interval_hours }}h
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('knowledge.crawlerSource.lastSync')"
          width="160"
        >
          <template #default="{ row }">
            {{ formatDate(row.last_crawl_at) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('knowledge.crawlerSource.actions')"
          width="300"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              size="small"
              type="primary"
              :disabled="!row.site_verified"
              @click="handleSync(row)"
            >
              {{ $t('knowledge.crawlerSource.syncNow') }}
            </el-button>
            <el-button
              size="small"
              @click="handleVerify(row)"
            >
              {{ $t('knowledge.crawlerSource.verifySite') }}
            </el-button>
            <el-button
              size="small"
              @click="openBatches(row)"
            >
              {{ $t('knowledge.crawlerSource.batches') }}
            </el-button>
            <el-button
              v-if="row.status === 'active'"
              size="small"
              type="warning"
              @click="handlePause(row)"
            >
              {{ $t('knowledge.crawlerSource.pause') }}
            </el-button>
            <el-button
              v-else
              size="small"
              type="success"
              @click="handleResume(row)"
            >
              {{ $t('knowledge.crawlerSource.resume') }}
            </el-button>
            <el-button
              size="small"
              type="danger"
              @click="handleDelete(row)"
            >
              {{ $t('knowledge.crawlerSource.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建源 -->
    <el-dialog
      v-model="showCreate"
      :title="$t('knowledge.crawlerSource.create')"
      width="560px"
    >
      <el-form
        :model="form"
        label-width="110px"
      >
        <el-form-item :label="$t('knowledge.crawlerSource.kb')">
          <el-select
            v-model="form.kb_id"
            filterable
            style="width: 100%"
          >
            <el-option
              v-for="kb in kbList"
              :key="kb.id"
              :label="kb.name"
              :value="kb.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('knowledge.crawlerSource.sourceType')">
          <el-radio-group v-model="form.source_type">
            <el-radio value="url">
              URL
            </el-radio>
            <el-radio value="sitemap">
              Sitemap
            </el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="$t('knowledge.crawlerSource.startUrl')">
          <el-input
            v-model="form.start_url"
            :placeholder="form.source_type === 'sitemap' ? 'https://example.com/sitemap.xml' : 'https://example.com/docs'"
          />
        </el-form-item>
        <el-form-item :label="$t('knowledge.crawlerSource.includePatterns')">
          <el-input
            v-model="form.include_patterns"
            :placeholder="$t('knowledge.crawlerSource.includePatternsPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="$t('knowledge.crawlerSource.maxPages')">
          <el-input-number
            v-model="form.max_pages"
            :min="10"
            :max="2000"
          />
        </el-form-item>
        <el-form-item :label="$t('knowledge.crawlerSource.intervalHours')">
          <el-input-number
            v-model="form.interval_hours"
            :min="1"
            :max="720"
          />
        </el-form-item>
        <el-form-item :label="$t('knowledge.crawlerSource.qaSplit')">
          <el-switch v-model="form.qa_split_enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">
          {{ $t('common.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          @click="handleCreate"
        >
          {{ $t('knowledge.crawlerSource.submit') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 批次抽屉 -->
    <el-drawer
      v-model="batchDrawer"
      :title="$t('knowledge.crawlerSource.batchHistory') + (currentSource ? ' #' + currentSource.id : '')"
      size="640px"
    >
      <el-table
        v-loading="batchLoading"
        :data="batches"
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="70"
        />
        <el-table-column
          prop="trigger"
          :label="$t('knowledge.crawlerSource.trigger')"
          width="80"
        />
        <el-table-column
          :label="$t('knowledge.crawlerSource.status')"
          width="110"
        >
          <template #default="{ row }">
            <el-tag :type="batchTag(row.status)">
              {{ $t(batchLabel(row.status)) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('knowledge.crawlerSource.pagesStats')"
          width="190"
        >
          <template #default="{ row }">
            {{ row.pages_found }} / {{ row.pages_new }} / {{ row.pages_changed }} / {{ row.pages_unchanged }} / {{ row.pages_failed }}
          </template>
        </el-table-column>
        <el-table-column
          prop="error_msg"
          :label="$t('knowledge.crawlerSource.errorMsg')"
          min-width="100"
          show-overflow-tooltip
        />
        <el-table-column
          :label="$t('knowledge.crawlerSource.actions')"
          width="90"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'failed'"
              size="small"
              type="primary"
              :disabled="row.retry_count >= 3"
              @click="handleRetry(row)"
            >
              {{ $t('knowledge.crawlerSource.retry') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import i18n from '@/i18n'
import { kbCrawlerApi } from '@/api/kbCrawler'
import { isUserReportedError } from '@/utils/errorReporting'
import { listKBs } from '@/api/knowledgeBase'

const loading = ref(false)
const creating = ref(false)
const sources = ref([])
const kbList = ref([])
// 表格里显示知识库名称，而不是裸 ID（kb_id=12 这种读不出是哪一份库）
const kbName = (id) => {
  if (id === null || id === undefined || id === '') return '-'
  const kb = kbList.value.find((k) => String(k.id) === String(id))
  return kb?.name || String(id)
}
const filter = reactive({ kb_id: '', status: '' })
const showCreate = ref(false)
const form = reactive({
  kb_id: '',
  source_type: 'url',
  start_url: '',
  include_patterns: '',
  max_pages: 200,
  interval_hours: 168,
  qa_split_enabled: true
})

// 批次抽屉
const batchDrawer = ref(false)
const batchLoading = ref(false)
const batches = ref([])
const currentSource = ref(null)

const loadKBs = async () => {
  const res = await listKBs({ page: 1, page_size: 100 })
  // request.js 拦截器已解包为 data.data，分页键是 list（对齐 Connectors.vue）
  kbList.value = res?.list || res?.items || res?.data?.list || []
}

const loadSources = async () => {
  loading.value = true
  try {
    const params = {}
    if (filter.kb_id) params.kb_id = filter.kb_id
    if (filter.status) params.status = filter.status
    const res = await kbCrawlerApi.listSources(params)
    sources.value = res?.items || res || []
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('knowledge.crawlerSource.loadFailed') + ': ' + (e.message || ''))
  } finally {
    loading.value = false
  }
}

const handleCreate = async () => {
  if (!form.kb_id || !form.start_url) {
    ElMessage.warning(i18n.global.t('knowledge.crawlerSource.requireKbAndUrl'))
    return
  }
  creating.value = true
  try {
    const data = { ...form }
    if (!data.include_patterns) delete data.include_patterns
    await kbCrawlerApi.createSource(data)
    ElMessage.success(i18n.global.t('knowledge.crawlerSource.createdNeedVerify'))
    showCreate.value = false
    await loadSources()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('knowledge.crawlerSource.createFailed') + ': ' + (e.message || ''))
  } finally {
    creating.value = false
  }
}

const handleVerify = async (row) => {
  try {
    await kbCrawlerApi.verifySite(row.id)
    ElMessage.success(i18n.global.t('knowledge.crawlerSource.verifyOk'))
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('knowledge.crawlerSource.verifyFailed') + ': ' + (e.message || ''))
  }
  await loadSources()
}

const handleSync = async (row) => {
  try {
    const res = await kbCrawlerApi.syncSource(row.id)
    ElMessage.success(i18n.global.t('knowledge.crawlerSource.syncCreated') + `: ${res?.batch_no || ''}`)
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('knowledge.crawlerSource.syncFailed') + ': ' + (e.message || ''))
  }
}

const handlePause = async (row) => {
  await kbCrawlerApi.pauseSource(row.id)
  await loadSources()
}

const handleResume = async (row) => {
  await kbCrawlerApi.resumeSource(row.id)
  await loadSources()
}

const handleDelete = (row) => {
  ElMessageBox.confirm(i18n.global.t('knowledge.crawlerSource.confirmDelete'), i18n.global.t('common.confirm'), { type: 'warning' })
    .then(async () => {
      await kbCrawlerApi.deleteSource(row.id)
      await loadSources()
    })
    .catch(() => {})
}

const openBatches = async (row) => {
  currentSource.value = row
  batchDrawer.value = true
  batchLoading.value = true
  try {
    const res = await kbCrawlerApi.listBatches({ source_id: row.id, page: 1, page_size: 50 })
    batches.value = res?.items || res || []
  } finally {
    batchLoading.value = false
  }
}

const handleRetry = async (row) => {
  try {
    await kbCrawlerApi.retryBatch(row.id)
    ElMessage.success(i18n.global.t('knowledge.crawlerSource.retryCreated'))
    if (currentSource.value) await openBatches(currentSource.value)
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('knowledge.crawlerSource.retryFailed') + ': ' + (e.message || ''))
  }
}

const statusTag = (s) => ({ active: 'success', paused: 'info', error: 'danger' }[s] || 'info')
const statusLabel = (s) => ({ active: 'knowledge.crawlerSource.enabled', paused: 'knowledge.crawlerSource.paused', error: 'knowledge.crawlerSource.error' }[s] || s)
const batchTag = (s) => ({ pending: 'info', running: 'warning', succeeded: 'success', partially_failed: 'warning', failed: 'danger' }[s] || 'info')
const batchLabel = (s) => ({ pending: 'knowledge.crawlerSource.batchPending', running: 'knowledge.crawlerSource.batchRunning', succeeded: 'knowledge.crawlerSource.batchSucceeded', partially_failed: 'knowledge.crawlerSource.batchPartiallyFailed', failed: 'knowledge.crawlerSource.batchFailed' }[s] || s)
const formatDate = (d) => d ? new Date(d).toLocaleString('zh-CN') : '-'

onMounted(async () => {
  await loadKBs()
  await loadSources()
})
</script>

<style scoped lang="scss">
.crawler-sources-page {
  padding: 16px;
}

.toolbar-card {
  margin-bottom: 16px;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.toolbar-left,
.toolbar-right {
  display: flex;
  gap: 8px;
  align-items: center;
}
</style>
