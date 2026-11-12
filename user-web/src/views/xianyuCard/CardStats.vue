<template>
  <div class="xianyu-card-detail-stats">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('闲鱼卡片详情统计') }}</span>
          <div class="header-actions">
            <el-button
              icon="ArrowLeft"
              @click="goBack"
            >
              {{ $t('返回') }}
            </el-button>
            <el-date-picker
              v-model="dateRange"
              type="daterange"
              range-separator="至"
              start-placeholder="开始日期"
              end-placeholder="结束日期"
              value-format="YYYY-MM-DD"
              @change="handleDateChange"
            />
            <el-select
              v-model="groupBy"
              :placeholder="$t('分组方式')"
              @change="handleGroupByChange"
            >
              <el-option
                :label="$t('按天')"
                value="day"
              />
              <el-option
                :label="$t('按周')"
                value="week"
              />
              <el-option
                :label="$t('按月')"
                value="month"
              />
            </el-select>
            <el-button
              type="primary"
              @click="refreshData"
            >
              {{ $t('刷新') }}
            </el-button>
          </div>
        </div>
      </template>

      
      <el-alert
        v-if="cardLoadFailed"
        class="stats-alert"
        type="warning"
        show-icon
        :closable="false"
        title="这张卡片读不到了"
        description="卡片已被删除，或链接里的 id 不属于当前账号。下方信息栏因此整块隐藏，统计里的 0 与空图是「没读到」，不是「真的没有」。"
      />
      <el-alert
        v-else-if="statsLoadFailed"
        class="stats-alert"
        type="error"
        show-icon
        :closable="false"
        title="统计数据读取失败"
        description="卡片本身还在，但这一轮的统计没读到。下方的 0 与空图是「没读到」，不是「真的没有」——请点「刷新」重试。"
      />

      <div
        v-if="cardInfo"
        class="card-info"
      >
        <el-descriptions
          :column="3"
          border
        >
          <el-descriptions-item label="卡片标题">
            {{ cardInfo.title }}
          </el-descriptions-item>
          <el-descriptions-item label="描述">
            {{ cardInfo.description }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="cardInfo.is_active ? 'success' : 'danger'">
              {{ cardInfo.is_active ? '激活' : '禁用' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">
            {{ fmtTime(cardInfo.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="总浏览量">
            {{ cardInfo.view_count || 0 }}
          </el-descriptions-item>
          <el-descriptions-item label="总点击量">
            {{ cardInfo.click_count || 0 }}
          </el-descriptions-item>
        </el-descriptions>
      </div>

      
      <div class="stats-overview">
        <el-row :gutter="20">
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ formatNumber(statsData.total_views) }}
                </div>
                <div class="stats-label">
                  浏览量
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ formatNumber(statsData.total_clicks) }}
                </div>
                <div class="stats-label">
                  点击量
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ formatNumber(statsData.total_shares) }}
                </div>
                <div class="stats-label">
                  分享量
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ formatPercent(statsData.click_rate) }}
                </div>
                <div class="stats-label">
                  点击率
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>
      </div>

      
      <div class="charts-container">
        <el-row :gutter="20">
          <el-col :span="12">
            <el-card>
              <div class="chart-title">
                浏览量趋势
              </div>
              <div
                ref="viewsChart"
                class="chart"
              />
            </el-card>
          </el-col>
          <el-col :span="12">
            <el-card>
              <div class="chart-title">
                点击量趋势
              </div>
              <div
                ref="clicksChart"
                class="chart"
              />
            </el-card>
          </el-col>
        </el-row>
      </div>

      
      <div class="detail-table">
        <el-card>
          <div class="table-title">
            详细访问记录
          </div>
          <el-table
            v-loading="loading"
            :data="detailData"
            style="width: 100%"
          >
            <el-table-column
              prop="date"
              label="日期"
              width="120"
            >
              <template #default="scope">
                {{ toBusinessDate(scope.row.date) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="views"
              label="浏览量"
              width="100"
            />
            <el-table-column
              prop="clicks"
              label="点击量"
              width="100"
            />
            <el-table-column
              prop="shares"
              label="分享量"
              width="100"
            />
            <el-table-column
              prop="click_rate"
              label="点击率"
              width="100"
            >
              <template #default="scope">
                {{ formatPercent(scope.row.click_rate) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="unique_visitors"
              label="独立访客"
              width="100"
            />
          </el-table>
          
          
          <div class="pagination-container">
            <el-pagination
              v-model:current-page="pagination.page"
              v-model:page-size="pagination.page_size"
              :page-sizes="[10, 20, 50, 100]"
              :total="pagination.total"
              layout="total, sizes, prev, pager, next, jumper"
              @size-change="handleSizeChange"
              @current-change="handleCurrentChange"
            />
          </div>
        </el-card>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { safeInit } from '@/utils/echarts'
import { getXianyuCard, getXianyuCardStats } from '@/api/xianyuCard'
import { fmtTime } from '@/utils/time'

const router = useRouter()
const route = useRoute()
const loading = ref(false)

const cardId = ref(route.params.id);

// 这两条原先只在 fetch 的 catch 里弹 toast（和拦截器的那一句重复），
// 现在改成常驻告警：toast 三秒就没了，整块消失的卡片信息和满屏的 0 会一直留着。
const cardLoadFailed = ref(false)
const statsLoadFailed = ref(false)

const dateRange = ref([]);
const groupBy = ref('day')

const cardInfo = ref(null);

const statsData = reactive({
  total_views: 0,
  total_clicks: 0,
  total_shares: 0,
  click_rate: 0
});

const chartData = reactive({
  views: [],
  clicks: [],
  dates: []
})

const detailData = ref([])

const pagination = reactive({
  page: 1,
  page_size: 10,
  total: 0
});

let viewsChartInstance = null;
let clicksChartInstance = null

const viewsChart = ref(null)
const clicksChart = ref(null)

const formatNumber = (num) => {
  if (!num) return '0'
  if (num >= 10000) {
    return (num / 10000).toFixed(1) + '万'
  }
  return num.toString()
};

const formatPercent = (rate) => {
  if (!rate) return '0%'
  return (rate * 100).toFixed(2) + '%'
};

// 接口按业务日（YYYY-MM-DD）解析日期。选择器在没有 value-format 时回的是 Date 对象，
// axios 会把它序列化成 "2026-08-31T16:00:00.000Z"，后端 ParseBusinessDate 直接失败，
// 而控制器把这条错误兜成了 200 + 全 0 ⇒ 用户挑完日期整页统计清零，看着像"这张卡没人看过"。
const toBusinessDate = (value) => {
  if (!value) return ''
  if (typeof value === 'string') return value.slice(0, 10)
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
};

const goBack = () => {
  router.back()
};

const initCharts = () => {
  if (!viewsChart.value || !clicksChart.value)
    return;
  viewsChartInstance = safeInit(viewsChart.value);
  const viewsOption = {
    tooltip: {
      trigger: 'axis'
    },
    xAxis: {
      type: 'category',
      data: chartData.dates
    },
    yAxis: {
      type: 'value',
      name: '浏览量'
    },
    series: [{
      data: chartData.views,
      type: 'line',
      smooth: true,
      areaStyle: {
        opacity: 0.3
      },
      itemStyle: {
        color: '#FF6B35'
      }
    }]
  }
  viewsChartInstance.setOption(viewsOption)

  clicksChartInstance = safeInit(clicksChart.value);
  const clicksOption = {
    tooltip: {
      trigger: 'axis'
    },
    xAxis: {
      type: 'category',
      data: chartData.dates
    },
    yAxis: {
      type: 'value',
      name: '点击量'
    },
    series: [{
      data: chartData.clicks,
      type: 'line',
      smooth: true,
      areaStyle: {
        opacity: 0.3
      },
      itemStyle: {
        color: '#1890ff'
      }
    }]
  }
  clicksChartInstance.setOption(clicksOption)
};

const fetchCardInfo = async () => {
  try {
    const res = await getXianyuCard(cardId.value)
    cardInfo.value = res || null
    cardLoadFailed.value = !res?.id
  } catch (error) {
    // 拦截器已经按状态码弹过一次，这里不再补第二句同义 toast。
    cardLoadFailed.value = true
    console.warn(error)
  }
};

const fetchStats = async () => {
  loading.value = true
  try {
    const params = {
      start_date: toBusinessDate(dateRange.value?.[0]),
      end_date: toBusinessDate(dateRange.value?.[1]),
      group_by: groupBy.value,
      page: pagination.page,
      page_size: pagination.page_size
    }
    
    const res = await getXianyuCardStats(cardId.value, params)
    statsLoadFailed.value = false
    Object.assign(statsData, res.stats || {});

    // 接口回的是完整时间戳，类目轴直接印会是一串 ISO，统一截成业务日。
    chartData.dates = (res.chart?.dates || []).map(toBusinessDate);
    chartData.views = res.chart?.views || []
    chartData.clicks = res.chart?.clicks || []

    detailData.value = res.details?.list || [];
    pagination.total = res.details?.total || 0

    if (viewsChartInstance && clicksChartInstance) {
      initCharts()
    }
  } catch (error) {
    statsLoadFailed.value = true
    console.warn(error)
  } finally {
    loading.value = false
  }
};

const handleDateChange = () => {
  pagination.page = 1
  fetchStats()
};

const handleGroupByChange = () => {
  pagination.page = 1
  fetchStats()
};

const refreshData = () => {
  fetchStats()
};

const handleSizeChange = (size) => {
  pagination.page_size = size
  fetchStats()
};

const handleCurrentChange = (page) => {
  pagination.page = page
  fetchStats()
};

const handleResize = () => {
  if (viewsChartInstance) {
    viewsChartInstance.resize()
  }
  if (clicksChartInstance) {
    clicksChartInstance.resize()
  }
};

onMounted(() => {
  const endDate = new Date();
  const startDate = new Date()
  startDate.setDate(startDate.getDate() - 30)
  dateRange.value = [startDate.toISOString().split('T')[0], endDate.toISOString().split('T')[0]]
  
  fetchCardInfo()
  fetchStats()
  
  setTimeout(() => {
    initCharts()
  }, 100);
  
  window.addEventListener('resize', handleResize);
})

onUnmounted(() => {
  if (viewsChartInstance) {
    viewsChartInstance.dispose()
  }
  if (clicksChartInstance) {
    clicksChartInstance.dispose()
  }
  
  window.removeEventListener('resize', handleResize);
})
</script>

<style scoped>
.xianyu-card-detail-stats {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.card-info {
  margin-bottom: 20px;
}

.stats-alert {
  margin-bottom: 20px;
}

.stats-overview {
  margin-bottom: 20px;
}

.stats-card {
  text-align: center;
}

.stats-item {
  padding: 20px;
}

.stats-value {
  font-size: 32px;
  font-weight: bold;
  color: #FF6B35;
  margin-bottom: 10px;
}

.stats-label {
  font-size: 14px;
  color: #666;
}

.charts-container {
  margin-bottom: 20px;
}

.chart-title,
.table-title {
  font-size: 16px;
  font-weight: 500;
  margin-bottom: 15px;
}

.chart {
  width: 100%;
  height: 300px;
}

.detail-table {
  margin-top: 20px;
}

.pagination-container {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>