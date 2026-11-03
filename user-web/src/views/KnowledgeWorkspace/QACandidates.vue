<template>
  <div class="qa-candidates-page">
    <el-card class="toolbar-card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-radio-group
            v-model="filter.source_type"
            @change="loadList"
          >
            <el-radio-button value="">
              {{ $t('common.all') }}
            </el-radio-button>
            <el-radio-button value="crawl">
              {{ $t('knowledge.qaCandidate.sourceCrawl') }}
            </el-radio-button>
            <el-radio-button value="feedback">
              {{ $t('knowledge.qaCandidate.sourceFeedback') }}
            </el-radio-button>
            <el-radio-button value="manual">
              {{ $t('knowledge.qaCandidate.sourceManual') }}
            </el-radio-button>
          </el-radio-group>
          <el-select
            v-model="filter.status"
            :placeholder="$t('knowledge.qaCandidate.status')"
            clearable
            style="width: 140px"
            @change="loadList"
          >
            <el-option
              :label="$t('knowledge.qaCandidate.pending')"
              value="pending_review"
            />
            <el-option
              :label="$t('knowledge.qaCandidate.approved')"
              value="approved"
            />
            <el-option
              :label="$t('knowledge.qaCandidate.rejected')"
              value="rejected"
            />
            <el-option
              :label="$t('knowledge.qaCandidate.indexed')"
              value="indexed"
            />
          </el-select>
        </div>
        <el-button @click="loadList">
          {{ $t('common.refresh') }}
        </el-button>
      </div>
    </el-card>

    <el-card v-loading="loading">
      <el-table
        :data="items"
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          :label="$t('knowledge.qaCandidate.source')"
          width="110"
        >
          <template #default="{ row }">
            <el-tag :type="sourceTag(row.source_type)">
              {{ $t(sourceLabel(row.source_type)) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="question"
          :label="$t('knowledge.qaCandidate.question')"
          min-width="200"
          show-overflow-tooltip
        />
        <el-table-column
          prop="answer"
          :label="$t('knowledge.qaCandidate.answer')"
          min-width="240"
          show-overflow-tooltip
        />
        <el-table-column
          prop="answer_anchor"
          :label="$t('knowledge.qaCandidate.anchor')"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          :label="$t('knowledge.qaCandidate.status')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)">
              {{ $t(statusLabel(row.status)) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          :label="$t('knowledge.qaCandidate.createdAt')"
          width="160"
        >
          <template #default="{ row }">
            {{ formatDate(row.created_at || row.updated_at) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('knowledge.qaCandidate.actions')"
          width="220"
          fixed="right"
        >
          <template #default="{ row }">
            <template v-if="row.status === 'pending_review'">
              <el-button
                size="small"
                type="success"
                @click="act(kbQaCandidateApi.approve, row)"
              >
                {{ $t('knowledge.qaCandidate.approve') }}
              </el-button>
              <el-button
                size="small"
                type="danger"
                @click="act(kbQaCandidateApi.reject, row)"
              >
                {{ $t('knowledge.qaCandidate.reject') }}
              </el-button>
            </template>
            <el-button
              v-if="row.status === 'approved'"
              size="small"
              type="primary"
              @click="act(kbQaCandidateApi.publish, row)"
            >
              {{ $t('knowledge.qaCandidate.publish') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="prev, pager, next, total"
          @current-change="loadList"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import i18n from '@/i18n'
import { kbQaCandidateApi } from '@/api/kbCrawler'
import { isUserReportedError } from '@/utils/errorReporting'
import { fmtTime } from '@/utils/time'

const loading = ref(false)
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const filter = reactive({ source_type: '', status: '' })

const loadList = async () => {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize }
    if (filter.source_type) params.source_type = filter.source_type
    if (filter.status) params.status = filter.status
    const res = await kbQaCandidateApi.list(params)
    items.value = res?.items || res || []
    total.value = res?.total || 0
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('knowledge.qaCandidate.loadFailed') + ': ' + (e.message || ''))
  } finally {
    loading.value = false
  }
}

const act = async (fn, row) => {
  try {
    await fn(row.id)
    ElMessage.success(i18n.global.t('common.operationSuccess'))
    await loadList()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('common.operationFailed') + ': ' + (e.message || ''))
  }
}

const sourceTag = (s) => ({ crawl: 'warning', feedback: 'primary', manual: 'info' }[s] || 'info')
const sourceLabel = (s) => ({ crawl: 'knowledge.qaCandidate.sourceCrawl', feedback: 'knowledge.qaCandidate.sourceFeedback', manual: 'knowledge.qaCandidate.sourceManual' }[s] || s)
const statusTag = (s) => ({ pending_review: 'warning', approved: 'success', rejected: 'danger', indexed: 'success' }[s] || 'info')
const statusLabel = (s) => ({ pending_review: 'knowledge.qaCandidate.pending', approved: 'knowledge.qaCandidate.approved', rejected: 'knowledge.qaCandidate.rejected', indexed: 'knowledge.qaCandidate.indexed' }[s] || s)
const formatDate = (d) => fmtTime(d, undefined, '-')

onMounted(loadList)
</script>

<style scoped lang="scss">
.qa-candidates-page {
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

.pager {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>
