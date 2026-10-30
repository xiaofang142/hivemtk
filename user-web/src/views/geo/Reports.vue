<template>
  <div class="geo-page">
    <el-card
      shadow="never"
      class="filter-card"
    >
      <el-form
        :inline="true"
        :model="filter"
      >
        <el-form-item label="日期范围">
          <el-date-picker
            v-model="dateRange"
            type="daterange"
            range-separator="至"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            value-format="YYYY-MM-DD"
            style="width: 260px"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            @click="loadAll"
          >
            <el-icon><Search /></el-icon><span>查询</span>
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-row
      :gutter="16"
      class="summary-row"
    >
      <el-col
        v-for="s in summaryCards"
        :key="s.key"
        :xs="12"
        :sm="8"
        :md="4"
      >
        <el-card
          shadow="never"
          class="summary-card"
          :body-style="{ padding: '16px' }"
        >
          <div class="summary-label">
            {{ s.label }}
          </div>
          <div
            class="summary-value"
            :class="s.cls"
          >
            {{ s.value }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card
      shadow="never"
      class="cost-card"
    >
      <template #header>
        <span class="card-title">API 成本明细（按 Provider / Model）</span>
      </template>
      <el-table
        v-loading="costLoading"
        :data="costRows"
        stripe
        style="width: 100%"
      >
        <el-table-column
          prop="provider"
          label="Provider"
          width="140"
        />
        <el-table-column
          prop="model"
          label="Model"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          prop="call_count"
          label="调用次数"
          width="110"
          align="right"
        />
        <el-table-column
          label="输入 Token"
          width="120"
          align="right"
        >
          <template #default="{ row }">
            {{ formatInt(row.input_tokens) }}
          </template>
        </el-table-column>
        <el-table-column
          label="输出 Token"
          width="120"
          align="right"
        >
          <template #default="{ row }">
            {{ formatInt(row.output_tokens) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="cost_cny"
          label="成本(CNY)"
          width="120"
          align="right"
        >
          <template #default="{ row }">
            ¥{{ formatNum(row.cost_cny) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="cost_usd"
          label="成本(USD)"
          width="120"
          align="right"
        >
          <template #default="{ row }">
            ${{ formatNum(row.cost_usd) }}
          </template>
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!costRows.length && !costLoading"
        description="暂无 API 成本数据"
        :image-size="60"
      />
    </el-card>

    
    <el-card
      shadow="never"
      class="roi-card"
    >
      <template #header>
        <span class="card-title">ROI 分析（API 投入统计）</span>
      </template>
      <div v-loading="roiLoading">
        <el-row
          :gutter="16"
          class="roi-row"
        >
          <el-col
            :xs="12"
            :sm="6"
          >
            <div class="roi-item">
              <div class="roi-label">
                总调用次数
              </div>
              <div class="roi-value">
                {{ formatInt(roi.total_calls) }}
              </div>
            </div>
          </el-col>
          <el-col
            :xs="12"
            :sm="6"
          >
            <div class="roi-item">
              <div class="roi-label">
                Token 消耗
              </div>
              <div class="roi-value">
                {{ formatInt((roi.total_input_tokens || 0) + (roi.total_output_tokens || 0)) }}
              </div>
            </div>
          </el-col>
          <el-col
            :xs="12"
            :sm="6"
          >
            <div class="roi-item">
              <div class="roi-label">
                总投入(CNY)
              </div>
              <div class="roi-value">
                ¥{{ formatNum(roi.total_cost_cny) }}
              </div>
            </div>
          </el-col>
          <el-col
            :xs="12"
            :sm="6"
          >
            <div class="roi-item highlight">
              <div class="roi-label">
                单次均价(USD)
              </div>
              <div class="roi-value">
                ${{ formatNum(roi.avg_cost_per_call_usd) }}
              </div>
            </div>
          </el-col>
        </el-row>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { geoApi } from '@/api/geo'
import { isUserReportedError } from '@/utils/errorReporting'

const loading = ref(false)
const costLoading = ref(false)
const roiLoading = ref(false)
const dateRange = ref([])
const summary = ref({})
const costRows = ref([])
const roi = ref({})

const filter = reactive({})

const summaryCards = computed(() => [
  { key: 'articles', label: '文章总数', value: formatInt(summary.value.total_articles) },
  { key: 'keywords', label: '关键词数', value: formatInt(summary.value.total_keywords) },
  { key: 'optimizations', label: '优化次数', value: formatInt(summary.value.total_optimizations) },
  { key: 'verifications', label: '验证次数', value: formatInt(summary.value.total_verifications) },
  { key: 'api_calls', label: 'API 调用', value: formatInt(summary.value.total_api_calls) },
  { key: 'total_cost', label: '总成本(CNY)', value: '¥' + formatNum(summary.value.total_cost_cny), cls: 'cost' }
])

const formatInt = (n) => {
  const num = Number(n)
  if (!isFinite(num)) return '0'
  return num.toLocaleString('zh-CN')
}

const priorityType = (p) => {
  const map = { 高: 'danger', 中: 'warning', 低: 'success' }
  return map[p] || 'success'
}

const formatNum = (n) => {
  const num = Number(n)
  if (!isFinite(num)) return '0.00'
  return num.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const dateParams = () => {
  const [start, end] = dateRange.value || []
  return { start_date: start || '', end_date: end || '' }
}

const loadSummary = async () => {
  loading.value = true
  try {
    const res = await geoApi.getReport(dateParams())
    summary.value = res || {}
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '报表加载失败')
    summary.value = {}
  } finally {
    loading.value = false
  }
}

const loadCosts = async () => {
  costLoading.value = true
  try {
    const res = await geoApi.getAPICosts(dateParams())
    costRows.value = res?.list || res?.items || res || []
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '成本明细加载失败')
    costRows.value = []
  } finally {
    costLoading.value = false
  }
}

const loadROI = async () => {
  roiLoading.value = true
  try {
    const res = await geoApi.getROI(dateParams())
    roi.value = res || {}
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || 'ROI 分析加载失败')
    roi.value = {}
  } finally {
    roiLoading.value = false
  }
}

const loadAll = () => {
  loadSummary()
  loadCosts()
  loadROI()
}

onMounted(loadAll)
</script>

<style lang="scss" scoped>
.geo-page {
  padding: $spacing-lg 24px;
}
.page-header h2 {
  margin: 0 0 6px;
  font-size: $font-size-extra-large;
  font-weight: 700;
  color: $text-primary;
}
.page-header .sub {
  margin: 0 0 16px;
  color: $info-color;
  font-size: $font-size-small;
}
.filter-card,
.cost-card,
.roi-card {
  border: 1px solid $border-base;
  border-radius: 10px;
  margin-bottom: $spacing-md;
}
.card-title {
  font-weight: 600;
  color: $text-primary;
}
.summary-row {
  margin-bottom: $spacing-md;
}
.summary-card {
  border: 1px solid $border-base;
  border-radius: 10px;
  margin-bottom: $spacing-md;
}
.summary-label {
  font-size: $font-size-extra-small;
  color: $info-color;
}
.summary-value {
  font-size: $font-size-extra-large;
  font-weight: 700;
  color: $text-primary;
  margin-top: $spacing-xs;
}
.summary-value.cost {
  color: $danger-color;
}
.roi-row {
  margin-bottom: $spacing-md;
}
.roi-item {
  background: $bg-color-page;
  border-radius: 8px;
  padding: $spacing-md;
  text-align: center;
}
.roi-item.highlight {
  background: linear-gradient(135deg, $primary-color, $primary-color);
  color: $bg-color;
}
.roi-label {
  font-size: $font-size-extra-small;
  opacity: 0.85;
}
.roi-value {
  font-size: $font-size-extra-large;
  font-weight: 700;
  margin-top: $spacing-xs;
}
.roi-suggest {
  border-top: 1px dashed $border-base;
  padding-top: $spacing-md;
}
.suggest-title {
  font-weight: 600;
  margin-bottom: $spacing-sm;
  color: $text-primary;
}
.roi-suggest ul {
  margin: 0;
  padding-left: 0;
  list-style: none;
}
.roi-suggest li {
  display: flex;
  align-items: center;
  gap: $spacing-sm;
  padding: $spacing-xs 0;
  font-size: $font-size-small;
  color: $text-regular;
}
</style>
