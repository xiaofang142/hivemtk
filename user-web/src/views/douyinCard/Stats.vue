<template>
  <div class="douyin-card-stats-container">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('抖音卡片统计') }}</span>
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
          <el-col :span="8">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ overallStats.total_cards || 0 }}</div>
                <div class="stats-label">总卡片数</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ overallStats.active_cards || 0 }}</div>
                <div class="stats-label">激活卡片数</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ formatNumber(overallStats.total_views) }}</div>
                <div class="stats-label">总浏览量</div>
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
              <div ref="visitTrendChart" class="chart-container"></div>
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
              <el-table :data="overallStats.top_cards" style="width: 100%">
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
              <el-table :data="overallStats.recent_activity" style="width: 100%">
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

import { ref, reactive, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { getDouyinCardOverallStats } from '@/api/douyinCard'

const router = useRouter();

const loading = ref(false);
const dateRange = ref([])
const groupBy = ref('day')
const overallStats = reactive({
  total_cards: 0,
  active_cards: 0,
  total_views: 0,
  top_cards: [],
  stats_by_time: [],
  recent_activity: []
})

const visitTrendChart = ref(null);
let visitChartInstance = null

const fetchOverallStats = async () => {
  loading.value = true
  try {
    const params = {
      groupBy: groupBy.value
    }
    
    if (dateRange.value && dateRange.value.length === 2) {
      params.startDate = formatDate(dateRange.value[0])
      params.endDate = formatDate(dateRange.value[1])
    }
    
    const res = await getDouyinCardOverallStats(params)
    const data = res;
    Object.assign(overallStats, {
      total_cards: data.totalCards || 0,
      active_cards: data.activeCards || 0,
      total_views: data.totalViews || 0,
      top_cards: data.popularCards || [],
      stats_by_time: data.dailyStats || [],
      recent_activity: data.recentActivity || []
    })

    nextTick(() => {
      updateCharts()
    });
  } catch (error) {
    ElMessage.error(i18n.global.t('获取统计数据失败'))
    console.error(error)
  } finally {
    loading.value = false
  }
};

const refreshData = () => {
  fetchOverallStats()
};

const handleDateChange = () => {
  fetchOverallStats()
};

const handleGroupByChange = () => {
  fetchOverallStats()
};

const updateCharts = () => {
  if (visitTrendChart.value) {
    if (visitChartInstance) {
      visitChartInstance.dispose()
    }
    
    visitChartInstance = safeInit(visitTrendChart.value)
    const dates = overallStats.stats_by_time.map(item => item.date)
    const views = overallStats.stats_by_time.map(item => item.viewCount || 0)
    
    visitChartInstance.setOption({
      tooltip: {
        trigger: 'axis'
      },
      xAxis: {
        type: 'category',
        data: dates
      },
      yAxis: {
        type: 'value'
      },
      series: [{
        name: '浏览量',
        type: 'line',
        data: views,
        smooth: true,
        areaStyle: {
          opacity: 0.3
        }
      }]
    })
  }
};

const goToCardStats = (cardId) => {
  router.push(`/douyin-card-stats/${cardId}`)
};

const formatNumber = (num) => {
  if (!num) return '0'
  return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',')
};

const formatPercent = (active, total) => {
  if (!total || total === 0) return '0%'
  return Math.round((active / total) * 100) + '%'
}

const formatDate = (date) => {
  if (!date) return ''
  const d = new Date(date)
  const year = d.getFullYear()
  const month = (d.getMonth() + 1).toString().padStart(2, '0')
  const day = d.getDate().toString().padStart(2, '0')
  return `${year}-${month}-${day}`
}

const formatTime = (timeStr) => {
  if (!timeStr) return ''
  const date = new Date(timeStr)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))
  
  if (days === 0) {
    const hours = Math.floor(diff / (1000 * 60 * 60))
    if (hours === 0) {
      const minutes = Math.floor(diff / (1000 * 60))
      return minutes <= 1 ? '刚刚' : `${minutes}分钟前`
    }
    return `${hours}小时前`
  } else if (days === 1) {
    return '昨天'
  } else if (days < 7) {
    return `${days}天前`
  } else {
    return formatDate(date)
  }
}

// el-tag 的 type 只接受 primary/success/info/warning/danger（Element Plus 2.14 tagProps.values），
// 传 '' 会触发 Vue prop 校验告警并回落到 default='primary'。中性动作用 'info'，
// 与 kuaishouCard/CardStats.vue 的既有写法保持一致。
const getActionType = (action) => {
  const types = {
    view: 'info'
  }
  return types[action] || 'info'
}

const getActionText = (action) => {
  const texts = {
    view: '浏览'
  }
  return texts[action] || action
}

onMounted(() => {
  const endDate = new Date();
  const startDate = new Date()
  startDate.setDate(endDate.getDate() - 7)
  dateRange.value = [startDate, endDate]
  
  fetchOverallStats()
});
</script>

<style scoped>
.douyin-card-stats-container {
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

.stats-overview {
  margin-bottom: 20px;
}

.stats-card {
  text-align: center;
}

.stats-item {
  padding: 10px 0;
}

.stats-value {
  font-size: 24px;
  font-weight: bold;
  color: #4F46E5;
  margin-bottom: 5px;
}

.stats-label {
  font-size: 14px;
  color: #606266;
}

.charts-section {
  margin-bottom: 20px;
}

.chart-container {
  height: 300px;
}

.bottom-section {
  margin-top: 20px;
}
</style>