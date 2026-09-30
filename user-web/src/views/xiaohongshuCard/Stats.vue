<template>
  <div class="xiaohongshu-stats-container">
    
    <el-row :gutter="20" class="stats-cards">
      <el-col :span="8">
        <el-card class="stats-card">
          <div class="stats-content">
            <div class="stats-icon" style="background-color: #4F46E5;">
              <el-icon><Document /></el-icon>
            </div>
            <div class="stats-info">
              <div class="stats-value">{{ formatNumber(overallStats.total_cards) }}</div>
              <div class="stats-label">{{ $t('总卡片数') }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stats-card">
          <div class="stats-content">
            <div class="stats-icon" style="background-color: #10B981;">
              <el-icon><Check /></el-icon>
            </div>
            <div class="stats-info">
              <div class="stats-value">{{ formatNumber(overallStats.active_cards) }}</div>
              <div class="stats-label">{{ $t('激活卡片数') }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stats-card">
          <div class="stats-content">
            <div class="stats-icon" style="background-color: #F59E0B;">
              <el-icon><View /></el-icon>
            </div>
            <div class="stats-info">
              <div class="stats-value">{{ formatNumber(overallStats.total_views) }}</div>
              <div class="stats-label">{{ $t('总浏览量') }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="filter-card">
      <el-form :model="filterForm" inline>
        <el-form-item :label="$t('日期范围')">
          <el-date-picker
            v-model="filterForm.dateRange"
            type="daterange"
            range-separator="至"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            format="YYYY-MM-DD"
            value-format="YYYY-MM-DD"
            @change="handleDateChange"
          />
        </el-form-item>
        <el-form-item :label="$t('分组方式')">
          <el-select v-model="filterForm.groupBy" :placeholder="$t('请选择分组方式')" @change="handleGroupByChange">
            <el-option :label="$t('按天')" value="day" />
            <el-option :label="$t('按周')" value="week" />
            <el-option :label="$t('按月')" value="month" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="refreshData">{{ $t('刷新数据') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-row :gutter="20" class="chart-row">
      <el-col :span="24">
        <el-card class="chart-card">
          <div class="chart-header">
            <span>{{ $t('访问趋势') }}</span>
          </div>
          <div id="viewChart" class="chart-container"></div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-row :gutter="20" class="table-row">
      <el-col :span="12">
        <el-card class="table-card">
          <div class="table-header">
            <span>{{ $t('热门卡片') }}</span>
          </div>
          <el-table :data="overallStats.top_cards" stripe style="width: 100%">
            <el-table-column prop="title" :label="$t('标题')" min-width="150" show-overflow-tooltip />
            <el-table-column prop="author" :label="$t('作者')" width="100" />
            <el-table-column prop="view_count" :label="$t('浏览量')" width="80" />
            <el-table-column :label="$t('操作')" width="100" fixed="right">
              <template #default="scope">
                <el-button size="small" type="primary" @click="goToCardStats(scope.row.id)">{{ $t('统计') }}</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card class="table-card">
          <div class="table-header">
            <span>最近活动</span>
          </div>
          <el-table :data="overallStats.recent_activities.filter(item => item.action_type === 'view')" stripe style="width: 100%">
            <el-table-column prop="card_title" label="卡片标题" min-width="150" show-overflow-tooltip />
            <el-table-column prop="action_type" label="操作类型" width="100">
              <template #default="scope">
                <el-tag :type="getActionType(scope.row.action_type)">
                  {{ getActionText(scope.row.action_type) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="user_ip" label="用户IP" width="120" />
            <el-table-column prop="created_at" label="时间" width="180">
              <template #default="scope">
                {{ formatTime(scope.row.created_at) }}
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { getXiaohongshuCardOverallStats } from '@/api/xiaohongshuCard'

const router = useRouter();

const loading = ref(false);
const viewChart = ref(null)

const filterForm = reactive({
  dateRange: [],
  groupBy: 'day'
});

const overallStats = reactive({
  total_cards: 0,
  active_cards: 0,
  total_views: 0,
  top_cards: [],
  recent_activities: [],
  view_trend: []
});

const fetchOverallStats = async () => {
  loading.value = true
  try {
    const params = {}
    
    if (filterForm.dateRange && filterForm.dateRange.length === 2) {
      params.start_date = filterForm.dateRange[0]
      params.end_date = filterForm.dateRange[1]
    }
    
    params.group_by = filterForm.groupBy;
    
    const res = await getXiaohongshuCardOverallStats(params)
    overallStats.total_cards = res.totalCards || 0;
    overallStats.active_cards = res.activeCards || 0
    overallStats.total_views = res.totalViews || 0
    overallStats.top_cards = (res.popularCards || []).map(p => ({
      id: p.id,
      title: p.title,
      author: '',
      view_count: p.viewCount,
      created_at: p.createdAt
    }))
    overallStats.recent_activities = (res.recentActivity || []).map(a => ({
      card_title: a.cardId,
      action_type: a.action,
      user_ip: a.ipAddress,
      created_at: a.createdAt
    }))
    overallStats.view_trend = (res.dailyStats || []).map(d => ({ date: d.date, count: d.view }))

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
  if (viewChart.value) {
    viewChart.value.dispose()
  }
  
  const viewChartDom = document.getElementById('viewChart')
  if (viewChartDom) {
    viewChart.value = safeInit(viewChartDom)
    const viewOption = {
      title: {
        text: '访问趋势',
        left: 'center'
      },
      tooltip: {
        trigger: 'axis'
      },
      xAxis: {
        type: 'category',
        data: overallStats.view_trend.map(item => item.date)
      },
      yAxis: {
        type: 'value'
      },
      series: [
        {
          name: '浏览量',
          type: 'line',
          data: overallStats.view_trend.map(item => item.count),
          smooth: true,
          itemStyle: {
            color: '#4F46E5'
          }
        }
      ]
    }
    viewChart.value.setOption(viewOption)
  }
};

const goToCardStats = (cardId) => {
  router.push(`/xiaohongshu-card-stats/${cardId}`)
};

const formatNumber = (num) => {
  if (!num) return 0
  return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',')
};

const formatPercent = (num) => {
  if (!num) return 0
  return (num * 100).toFixed(2)
}

const formatDate = (date) => {
  if (!date) return ''
  return new Date(date).toLocaleDateString()
}

const formatTime = (time) => {
  if (!time) return ''
  return new Date(time).toLocaleString()
}

// el-tag 的 type 只接受 primary/success/info/warning/danger，传 '' 会触发 Vue prop 校验告警
// 并回落到 default='primary'。中性动作用 'info'，与 kuaishouCard/CardStats.vue 既有写法一致。
const getActionType = (type) => {
  const typeMap = {
    view: 'info'
  }
  return typeMap[type] || 'info'
}

const getActionText = (type) => {
  const textMap = {
    view: '浏览'
  }
  return textMap[type] || type
}

onMounted(() => {
  const end = new Date();
  const start = new Date()
  start.setDate(start.getDate() - 7)
  
  filterForm.dateRange = [
    start.toISOString().split('T')[0],
    end.toISOString().split('T')[0]
  ]
  
  fetchOverallStats()
});
</script>

<style scoped>
.xiaohongshu-stats-container {
  padding: 20px;
}

.stats-cards {
  margin-bottom: 20px;
}

.stats-card {
  height: 100px;
}

.stats-content {
  display: flex;
  align-items: center;
  height: 100%;
}

.stats-icon {
  width: 60px;
  height: 60px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-right: 15px;
  color: white;
  font-size: 24px;
}

.stats-info {
  flex: 1;
}

.stats-value {
  font-size: 24px;
  font-weight: bold;
  margin-bottom: 5px;
}

.stats-label {
  font-size: 14px;
  color: #909399;
}

.filter-card {
  margin-bottom: 20px;
}

.chart-row {
  margin-bottom: 20px;
}

.chart-card {
  height: 400px;
}

.chart-header {
  font-size: 16px;
  font-weight: bold;
  margin-bottom: 15px;
  padding-bottom: 10px;
  border-bottom: 1px solid #EBEEF5;
}

.chart-container {
  height: 320px;
}

.table-row {
  margin-bottom: 20px;
}

.table-card {
  height: 400px;
}

.table-header {
  font-size: 16px;
  font-weight: bold;
  margin-bottom: 15px;
  padding-bottom: 10px;
  border-bottom: 1px solid #EBEEF5;
}
</style>