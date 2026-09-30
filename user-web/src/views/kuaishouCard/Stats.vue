<template>
  <div class="kuaishou-card-stats-container">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('快手卡片统计') }}</span>
          <div class="header-actions">
            <el-date-picker
              v-model="dateRange"
              type="daterange"
              range-separator="至"
              start-placeholder="开始日期"
              end-placeholder="结束日期"
              @change="handleDateChange"
            />
            <el-select v-model="groupBy" :placeholder="$t('分组方式')" @change="handleGroupByChange">
              <el-option :label="$t('按天')" value="day" />
              <el-option :label="$t('按周')" value="week" />
              <el-option :label="$t('按月')" value="month" />
            </el-select>
            <el-button type="primary" @click="refreshData">{{ $t('刷新') }}</el-button>
          </div>
        </div>
      </template>

      
      <div class="stats-overview">
        <el-row :gutter="20">
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ overallStats.totalCards || 0 }}</div>
                <div class="stats-label">总卡片数</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ overallStats.activeCards || 0 }}</div>
                <div class="stats-label">激活卡片数</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ formatNumber(overallStats.totalViews) }}</div>
                <div class="stats-label">总浏览量</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ formatPercent(overallStats.activeCards, overallStats.totalCards) }}</div>
                <div class="stats-label">激活率</div>
              </div>
            </el-card>
          </el-col>
        </el-row>
      </div>

      
      <div class="charts-section">
        <el-row :gutter="20">
          <el-col :span="24">
            <el-card>
              <template #header>
                <div class="card-header">
                  <span>访问趋势</span>
                </div>
              </template>
              <div ref="visitTrendChartRef" class="chart-container"></div>
            </el-card>
          </el-col>
        </el-row>
      </div>

      
      <div class="bottom-section">
        <el-row :gutter="20">
          <el-col :span="12">
            <el-card>
              <template #header>
                <div class="card-header">
                  <span>热门卡片</span>
                </div>
              </template>
              <el-table :data="overallStats.popularCards" style="width: 100%">
                <el-table-column prop="title" label="卡片标题" />
                <el-table-column prop="viewCount" label="浏览量">
                  <template #default="scope">
                    {{ formatNumber(scope.row.viewCount) }}
                  </template>
                </el-table-column>
                <el-table-column label="操作" width="100">
                  <template #default="scope">
                    <el-button size="small" type="primary" @click="goToCardStats(scope.row.id)">
                      统计
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </el-card>
          </el-col>
          <el-col :span="12">
            <el-card>
              <template #header>
                <div class="card-header">
                  <span>最近活动</span>
                </div>
              </template>
              <el-table :data="overallStats.recentActivities" style="width: 100%">
                <el-table-column prop="cardTitle" label="卡片标题" />
                <el-table-column prop="action" label="操作">
                  <template #default="scope">
                    <el-tag :type="getActionType(scope.row.action)">
                      {{ getActionText(scope.row.action) }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="username" label="用户" />
                <el-table-column prop="createdAt" label="时间">
                  <template #default="scope">
                    {{ formatTime(scope.row.createdAt) }}
                  </template>
                </el-table-column>
              </el-table>
            </el-card>
          </el-col>
        </el-row>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { ElMessage } from 'element-plus'
import { getKuaishouCardOverallStats } from '@/api/kuaishouCard'

const router = useRouter()
const loading = ref(false)
const dateRange = ref([])
const groupBy = ref('day')

let visitTrendChart = null;

const visitTrendChartRef = ref(null);

const overallStats = ref({
  totalCards: 0,
  activeCards: 0,
  totalViews: 0,
  activationRate: 0,
  popularCards: [],
  recentActivities: []
});

const fetchOverallStats = async () => {
  if (!dateRange.value || dateRange.value.length !== 2) {
    ElMessage.warning(i18n.global.t('请选择日期范围'))
    return
  }

  loading.value = true
  try {
    const [startDate, endDate] = dateRange.value
    const params = {
      start_date: formatDate(startDate),
      end_date: formatDate(endDate),
      group_by: groupBy.value
    }

    const response = await getKuaishouCardOverallStats(params)
    overallStats.value = {
      totalCards: response.totalCards || 0,
      activeCards: response.activeCards || 0,
      totalViews: response.totalViews || 0,
      activationRate: response.totalCards > 0 ? (response.activeCards / response.totalCards) * 100 : 0,
      popularCards: response.popularCards || [],
      recentActivities: response.recentActivities || [],
      trend_data: {
        dates: (response.dailyStats && Array.isArray(response.dailyStats)) ? response.dailyStats.map(item => item.date) : [],
        views: (response.dailyStats && Array.isArray(response.dailyStats)) ? response.dailyStats.map(item => item.views) : []
      }
    };

    updateCharts();
  } catch (error) {
    console.error('获取统计数据失败:', error)
    ElMessage.error(i18n.global.t('获取统计数据失败'))
  } finally {
    loading.value = false
  }
};

const updateCharts = () => {
  if (!overallStats.value.trend_data) return

  if (!overallStats.value.trend_data.dates || overallStats.value.trend_data.dates.length === 0) {
    const today = new Date();
    const dates = []
    const views = []
    
    for (let i = 6; i >= 0; i--) {
      const date = new Date(today)
      date.setDate(today.getDate() - i)
      dates.push(formatDate(date))
      views.push(0)
    }
    
    overallStats.value.trend_data = {
      dates,
      views
    }
  }

  if (visitTrendChart) {
    const option = {
      title: {
        text: '访问趋势'
      },
      tooltip: {
        trigger: 'axis'
      },
      legend: {
        data: ['浏览量']
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '3%',
        containLabel: true
      },
      xAxis: {
        type: 'category',
        boundaryGap: false,
        data: overallStats.value.trend_data.dates || []
      },
      yAxis: {
        type: 'value'
      },
      series: [
        {
          name: '浏览量',
          type: 'line',
          stack: 'Total',
          smooth: true,
          data: overallStats.value.trend_data.views || []
        }
      ]
    }
    visitTrendChart.setOption(option)
  }
};

const initCharts = () => {
  if (visitTrendChartRef.value) {
    visitTrendChart = safeInit(visitTrendChartRef.value)
  }

  window.addEventListener('resize', () => {
    if (visitTrendChart) visitTrendChart.resize()
  });
};

const handleDateChange = () => {
  fetchOverallStats()
};

const handleGroupByChange = () => {
  fetchOverallStats()
};

const refreshData = () => {
  fetchOverallStats()
};

const goToCardStats = (cardId) => {
  router.push(`/kuaishou-card/stats/${cardId}`)
};

const formatNumber = (num) => {
  if (!num) return '0'
  return Number(num).toLocaleString()
};

const formatPercent = (num, total) => {
  if (!total || total === 0) return '0%'
  return `${((num / total) * 100).toFixed(1)}%`
};

const formatDate = (date) => {
  if (!date) return ''
  const d = new Date(date)
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
};

const formatTime = (time) => {
  if (!time) return ''
  const date = new Date(time)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))
  
  if (days === 0) {
    const hours = Math.floor(diff / (1000 * 60 * 60))
    if (hours === 0) {
      const minutes = Math.floor(diff / (1000 * 60))
      return minutes === 0 ? '刚刚' : `${minutes}分钟前`
    }
    return `${hours}小时前`
  } else if (days === 1) {
    return '昨天'
  } else if (days < 7) {
    return `${days}天前`
  } else {
    return formatDate(date)
  }
};

// el-tag 的 type 只接受 primary/success/info/warning/danger，传 '' 会触发 Vue prop 校验告警
// 并回落到 default='primary'。中性动作用 'info'，与本模块 CardStats.vue 既有写法一致。
const getActionType = (action) => {
  const actionTypeMap = {
    view: 'info'
  }
  return actionTypeMap[action] || 'info'
};

const getActionText = (action) => {
  const actionTextMap = {
    view: '浏览'
  }
  return actionTextMap[action] || action
};

onMounted(() => {
  const endDate = new Date();
  const startDate = new Date()
  startDate.setDate(endDate.getDate() - 7)
  
  dateRange.value = [startDate, endDate]
  
  nextTick(() => {
    initCharts();
    
    fetchOverallStats();
  });
});

onUnmounted(() => {
  if (visitTrendChart) {
    visitTrendChart.dispose()
    visitTrendChart = null
  }
  window.removeEventListener('resize', () => {})
});
</script>

<style scoped>
.kuaishou-card-stats-container {
  padding: 20px;
}

.box-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 10px;
}

/* 统计概览区域 */
.stats-overview {
  margin-bottom: 20px;
}

.stats-card {
  text-align: center;
  height: 100px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.stats-item {
  width: 100%;
}

.stats-value {
  font-size: 24px;
  font-weight: bold;
  color: #4F46E5;
  margin-bottom: 5px;
}

.stats-label {
  font-size: 14px;
  color: #909399;
}

/* 图表区域 */
.charts-section {
  margin-bottom: 20px;
}

.chart-container {
  height: 300px;
  width: 100%;
}

/* 底部区域 */
.bottom-section {
  margin-top: 20px;
}

/* 响应式布局 */
@media (max-width: 768px) {
  .header-actions {
    flex-direction: column;
    gap: 5px;
  }
  
  .chart-container {
    height: 250px;
  }
}
</style>