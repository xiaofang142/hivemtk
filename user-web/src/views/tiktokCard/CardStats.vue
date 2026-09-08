<template>
  <div class="tiktok-card-stats-container">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <span>TikTok卡片统计 - {{ cardInfo.title || '加载中...' }}</span>
          <div class="header-actions">
            <el-button @click="goBack">{{ $t('返回') }}</el-button>
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

      
      <div class="card-info-section" v-if="cardInfo.id">
        <el-row :gutter="20">
          <el-col :span="6">
            <el-image 
              style="width: 100%; height: 200px" 
              :src="cardInfo.image_url" 
              :preview-src-list="[cardInfo.image_url]"
              fit="cover"
            />
          </el-col>
          <el-col :span="18">
            <el-descriptions :column="2" border>
              <el-descriptions-item label="卡片ID">{{ cardInfo.id }}</el-descriptions-item>
              <el-descriptions-item label="标题">{{ cardInfo.title }}</el-descriptions-item>
              <el-descriptions-item label="状态">
                <el-tag :type="cardInfo.is_active ? 'success' : 'danger'">
                  {{ cardInfo.is_active ? '激活' : '禁用' }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="创建时间">{{ cardInfo.created_at }}</el-descriptions-item>
              <el-descriptions-item label="描述" :span="2">{{ cardInfo.description }}</el-descriptions-item>
            </el-descriptions>
          </el-col>
        </el-row>
      </div>

      
      <div class="stats-overview">
        <el-row :gutter="20">
          <el-col :span="24">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">{{ formatNumber(cardStats.viewCount) }}</div>
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
          <el-col :span="24">
            <el-card>
              <template #header>
                <div class="card-header">
                  <span>最近活动</span>
                </div>
              </template>
              <el-table :data="cardStats.recentActivity.filter(item => item.action === 'view')" style="width: 100%">
                <el-table-column prop="username" label="用户" />
                <el-table-column prop="action" label="操作">
                  <template #default="scope">
                    <el-tag :type="getActionType(scope.row.action)">
                      {{ getActionText(scope.row.action) }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="ip_address" label="IP地址" />
                <el-table-column prop="created_at" label="时间">
                  <template #default="scope">
                    {{ formatDate(scope.row.created_at) }}
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
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { getTikTokCard, getTikTokCardStats } from '@/api/tiktokCard'

const route = useRoute();
const router = useRouter()

const loading = ref(false);
const cardId = ref(route.params.id)
const cardInfo = ref({})
const dateRange = ref([])
const groupBy = ref('day')

const cardStats = reactive({
  cardId: 0,
  title: '',
  viewCount: 0,
  dailyStats: [],
  recentActivity: []
});

const visitTrendChart = ref(null);
let visitChartInstance = null

const fetchCardInfo = async () => {
  try {
    const res = await getTikTokCard(cardId.value)
    cardInfo.value = res;
  } catch (error) {
    ElMessage.error(i18n.global.t('获取卡片信息失败'))
    console.error(error)
  }
};

const fetchCardStats = async () => {
  loading.value = true
  try {
    const params = {
      groupBy: groupBy.value
    }
    
    if (dateRange.value && dateRange.value.length === 2) {
      params.startDate = formatDate(dateRange.value[0])
      params.endDate = formatDate(dateRange.value[1])
    }
    
    const res = await getTikTokCardStats(cardId.value, params)
    Object.assign(cardStats, res);

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

const updateCharts = () => {
  if (visitChartInstance) {
    visitChartInstance.dispose()
  }
  
  visitChartInstance = safeInit(visitTrendChart.value)
  const visitOption = {
    title: {
      text: '访问量趋势',
      left: 'center'
    },
    tooltip: {
      trigger: 'axis'
    },
    xAxis: {
      type: 'category',
      data: cardStats.dailyStats.map(item => item.date)
    },
    yAxis: {
      type: 'value'
    },
    series: [
      {
        name: '浏览量',
        type: 'line',
        data: cardStats.dailyStats.map(item => item.view),
        smooth: true
      }
    ]
  }
  visitChartInstance.setOption(visitOption)
  
  window.addEventListener('resize', () => {
    if (visitChartInstance) visitChartInstance.resize()
  });
};

const handleDateChange = () => {
  fetchCardStats()
};

const handleGroupByChange = () => {
  fetchCardStats()
};

const refreshData = () => {
  fetchCardInfo()
  fetchCardStats()
};

const goBack = () => {
  router.go(-1)
};

const formatNumber = (num) => {
  if (!num) return '0'
  return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',')
};

const formatDate = (date) => {
  if (!date) return ''
  if (typeof date === 'string') return date
  
  const d = new Date(date)
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  
  return `${year}-${month}-${day}`
};

const getActionType = (action) => {
  const actionMap = {
    view: ''
  }
  return actionMap[action] || ''
};

const getActionText = (action) => {
  const actionMap = {
    view: '浏览'
  }
  return actionMap[action] || action
};

onMounted(() => {
  const endDate = new Date();
  const startDate = new Date()
  startDate.setDate(endDate.getDate() - 7)
  
  dateRange.value = [startDate, endDate]
  
  fetchCardInfo()
  fetchCardStats()
});
</script>

<style scoped>
.tiktok-card-stats-container {
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
}

.card-info-section {
  margin-bottom: 20px;
}

.stats-overview {
  margin-bottom: 20px;
}

.charts-section {
  margin-bottom: 20px;
}

.bottom-section {
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
  color: #909399;
}

.chart-container {
  height: 300px;
  width: 100%;
}
</style>