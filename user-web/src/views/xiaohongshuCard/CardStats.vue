<template>
  <div class="xiaohongshu-card-stats-container">
    <el-alert
      v-if="cardLoadFailed || statsLoadFailed"
      class="stats-alert"
      type="warning"
      show-icon
      :closable="false"
      :title="cardLoadFailed ? '这张卡片读不到了' : '统计数据读取失败'"
      :description="cardLoadFailed ? '卡片已被删除，或链接里的 id 不属于当前账号。上方卡片的空白与下方的 0、空图是「没读到」，不是「真的没有」。' : '卡片本身还在，但这一轮的统计没读到。下方的 0 与空图是「没读到」，不是「真的没有」——请点「刷新数据」重试。'"
    />

    <div class="chat-message-container">
      <div class="chat-message">
        <div class="message-avatar">
          <el-avatar
            :size="50"
            src=""
          >
            X
          </el-avatar>
        </div>
        <div class="message-content">
          <div class="message-header">
            <span class="author-name">{{ $t('小红书卡片') }}</span>
            <span class="message-time">{{ formatDate(cardInfo.created_at) }}</span>
          </div>
          <div class="message-body">
            <div class="message-title">
              {{ cardInfo.title }}
            </div>
            <div class="message-description">
              {{ cardInfo.description }}
            </div>
            <div
              v-if="cardInfo.image_url"
              class="message-image"
            >
              <el-image
                v-if="cardInfo.image_url"
                :src="cardInfo.image_url"
                :preview-src-list="[cardInfo.image_url]"
                fit="cover"
                style="width: 100%; height: 200px; border-radius: 8px;"
              >
                <template #error>
                  <div class="img-error-fallback">
                    {{ $t('图片加载失败') }}
                  </div>
                </template>
              </el-image>
            </div>
            <div
              v-if="cardInfo.tags"
              class="message-tags"
            >
              <el-tag
                v-for="tag in cardInfo.tags.split(',')"
                :key="tag"
                size="small"
                type="info"
                style="margin-right: 5px; margin-bottom: 5px;"
              >
                {{ tag }}
              </el-tag>
            </div>
          </div>
          <div class="message-stats">
            <span class="stat-item">
              <el-icon><View /></el-icon>
              {{ formatNumber(cardStats.view_count) }}
            </span>
          </div>
        </div>
      </div>
    </div>

    
    <el-row
      :gutter="20"
      class="stats-cards"
    >
      <el-col :span="8">
        <el-card class="stats-card">
          <div class="stats-content">
            <div
              class="stats-icon"
              style="background-color: #F59E0B;"
            >
              <el-icon><View /></el-icon>
            </div>
            <div class="stats-info">
              <div class="stats-value">
                {{ formatNumber(cardStats.view_count) }}
              </div>
              <div class="stats-label">
                {{ $t('总浏览量') }}
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stats-card">
          <div class="stats-content">
            <div
              class="stats-icon"
              style="background-color: #F59E0B;"
            >
              <el-icon><TrendCharts /></el-icon>
            </div>
            <div class="stats-info">
              <div class="stats-value">
                {{ formatPercent(cardStats.engagement_rate) }}%
              </div>
              <div class="stats-label">
                {{ $t('互动率') }}
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stats-card">
          <div class="stats-content">
            <div
              class="stats-icon"
              style="background-color: #10B981;"
            >
              <el-icon><User /></el-icon>
            </div>
            <div class="stats-info">
              <div class="stats-value">
                {{ formatNumber(cardStats.unique_visitors) }}
              </div>
              <div class="stats-label">
                {{ $t('独立访客数') }}
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row
      :gutter="20"
      class="stats-cards"
    >
      <el-col :span="8">
        <el-card class="stats-card">
          <div class="stats-content">
            <div
              class="stats-icon"
              style="background-color: #EF4444;"
            >
              <el-icon><Clock /></el-icon>
            </div>
            <div class="stats-info">
              <div class="stats-value">
                {{ formatNumber(cardStats.avg_view_time) }}s
              </div>
              <div class="stats-label">
                {{ $t('平均浏览时长') }}
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="filter-card">
      <el-form
        :model="filterForm"
        inline
      >
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
          <el-select
            v-model="filterForm.groupBy"
            :placeholder="$t('请选择分组方式')"
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
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="refreshData"
          >
            {{ $t('刷新数据') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-row
      :gutter="20"
      class="chart-row"
    >
      <el-col :span="24">
        <el-card class="chart-card">
          <div class="chart-header">
            <span>{{ $t('访问趋势') }}</span>
          </div>
          <div
            id="viewChart"
            class="chart-container"
          />
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="activity-card">
      <div class="activity-header">
        <span>{{ $t('最近活动') }}</span>
      </div>
      <el-table
        v-loading="loading"
        :data="recentActivities"
        stripe
        style="width: 100%"
      >
        <el-table-column
          prop="date"
          :label="$t('日期')"
          width="120"
        >
          <template #default="scope">
            {{ formatDay(scope.row.date) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="view"
          :label="$t('浏览量')"
          width="80"
        />
      </el-table>
      
      
      <div class="pagination-container">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="pagination.total"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { safeInit } from '@/utils/echarts'
import { getXiaohongshuCard, getXiaohongshuCardStats } from '@/api/xiaohongshuCard'

const route = useRoute();
const router = useRouter()

const loading = ref(false);
const cardLoadFailed = ref(false)
const statsLoadFailed = ref(false)
let viewChartInstance = null

const cardInfo = reactive({
  id: null,
  title: '',
  description: '',
  author: '',
  category: '',
  image_url: '',
  redirect_url: '',
  tags: '',
  is_active: true,
  created_at: '',
  updated_at: ''
});

const cardStats = reactive({
  view_count: 0,
  engagement_rate: 0,
  unique_visitors: 0,
  avg_view_time: 0,
  view_trend: []
});

const filterForm = reactive({
  dateRange: [],
  groupBy: 'day'
});

const recentActivities = ref([]);

const pagination = reactive({
  page: 1,
  pageSize: 10,
  total: 0
});

const fetchCardInfo = async () => {
  const cardId = route.params.id
  if (!cardId) {
    cardLoadFailed.value = true
    return
  }
  
  try {
    const res = await getXiaohongshuCard(cardId)
    Object.assign(cardInfo, res || {});
    cardLoadFailed.value = !res?.id
  } catch (error) {
    // 拦截器已经按状态码弹过一次，这里再弹一条是两句同义 toast；
    // 页面用常驻告警说结论——toast 三秒就没了，卡片框的空标题不会。
    cardLoadFailed.value = true
    console.warn(error)
  }
};

const fetchCardStats = async () => {
  const cardId = route.params.id
  if (!cardId) {
    statsLoadFailed.value = true
    return
  }
  
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize
    }
    
    if (filterForm.dateRange && filterForm.dateRange.length === 2) {
      params.start_date = filterForm.dateRange[0]
      params.end_date = filterForm.dateRange[1]
    }
    
    params.group_by = filterForm.groupBy;
    
    const res = await getXiaohongshuCardStats(cardId, params)
    statsLoadFailed.value = false
    if (res.card) {
      Object.assign(cardStats, res.card)
    } else if (res.viewCount !== undefined) {
      cardStats.view_count = res.viewCount
    }

    // 这里放的是 dto.DailyStat 行（{date, view}），不是 dto.Activity：
    // 横轴和表格都按日浏览读数用，所以表格列的 prop 必须是单数 view。
    recentActivities.value = res.dailyStats || [];
    pagination.total = res.dailyStats ? res.dailyStats.length : 0

    nextTick(() => {
      updateCharts()
    });
  } catch (error) {
    // 同上：统计读失败时拦截器已经出声，页面把「这一屏的 0 不是真值」常驻说出口。
    statsLoadFailed.value = true
    console.warn(error)
  } finally {
    loading.value = false
  }
};

const refreshData = () => {
  fetchCardStats()
};

const handleDateChange = () => {
  pagination.page = 1
  fetchCardStats()
};

const handleGroupByChange = () => {
  pagination.page = 1
  fetchCardStats()
};

const handleSizeChange = (val) => {
  pagination.pageSize = val
  pagination.page = 1
  fetchCardStats()
};

const handleCurrentChange = (val) => {
  pagination.page = val
  fetchCardStats()
};

const updateCharts = () => {
  // 模板里的 ref 挂的是 DOM 节点，原先却把它当 echarts 实例用（viewChart.value.dispose()），
  // 又把实例赋值回这个 ref ⇒ 下一次刷新时对 DOM 调 .dispose()。实例与容器分开存。
  const viewChartDom = document.getElementById('viewChart')
  if (!viewChartDom) return
  viewChartInstance = safeInit(viewChartDom)
  if (!viewChartInstance) return
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
      data: recentActivities.value.map(item => formatDay(item.date))
    },
    yAxis: {
      type: 'value'
    },
    series: [
      {
        name: '浏览量',
        type: 'line',
        data: recentActivities.value.map(item => item.view),
        smooth: true,
        itemStyle: {
          color: '#4F46E5'
        }
      }
    ]
  }
  viewChartInstance.setOption(viewOption)
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

// dto.DailyStat.date 这条链路给的是完整时间戳（"2026-09-30T00:00:00Z"），
// 表格里是「日期」列、图上是类目轴，两处都只要日 ⇒ 直接截前 10 位，
// 不要走 toLocaleDateString（那会跟着浏览器语言变成 9/30/2026）。
const formatDay = (value) => (value ? String(value).slice(0, 10) : '')

const formatTime = (time) => {
  if (!time) return ''
  return new Date(time).toLocaleString()
}

const getActionType = (type) => {
  return 'view'
}

const getActionText = (type) => {
  return '浏览'
}

const handleResize = () => {
  if (viewChartInstance) viewChartInstance.resize()
}

onMounted(() => {
  const end = new Date();
  const start = new Date()
  start.setDate(start.getDate() - 7)
  
  filterForm.dateRange = [
    start.toISOString().split('T')[0],
    end.toISOString().split('T')[0]
  ]

  window.addEventListener('resize', handleResize)
  
  fetchCardInfo()
  fetchCardStats()
});

// 实例挂在会被组件销毁的 DOM 上：不 dispose 就是每次进这一页留一个孤儿 echarts 实例。
onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  if (viewChartInstance) {
    viewChartInstance.dispose()
    viewChartInstance = null
  }
});
</script>

<style scoped>
.xiaohongshu-card-stats-container {
  padding: 20px;
}

.stats-alert {
  margin-bottom: 20px;
}

/* 聊天消息样式 */
.chat-message-container {
  margin-bottom: 20px;
  background-color: #f5f5f5;
  border-radius: 12px;
  padding: 15px;
  max-width: 600px;
  margin-left: auto;
  margin-right: auto;
}

.chat-message {
  display: flex;
  gap: 12px;
}

.message-avatar {
  flex-shrink: 0;
}

.message-content {
  flex: 1;
  background-color: white;
  border-radius: 12px;
  padding: 12px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
  position: relative;
}

.message-content::before {
  content: '';
  position: absolute;
  left: -8px;
  top: 15px;
  width: 0;
  height: 0;
  border-top: 8px solid transparent;
  border-bottom: 8px solid transparent;
  border-right: 8px solid white;
}

.message-header {
  display: flex;
  justify-content: space-between;
  margin-bottom: 8px;
}

.author-name {
  font-weight: bold;
  color: #ff2442;
  font-size: 14px;
}

.message-time {
  font-size: 12px;
  color: #999;
}

.message-body {
  margin-bottom: 10px;
}

.message-title {
  font-weight: bold;
  margin-bottom: 5px;
  font-size: 16px;
}

.message-description {
  color: #333;
  margin-bottom: 10px;
  line-height: 1.4;
}

.message-image {
  margin-bottom: 10px;
}

.message-tags {
  margin-bottom: 10px;
}

.message-stats {
  display: flex;
  gap: 15px;
  font-size: 12px;
  color: #666;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 3px;
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

.activity-card {
  margin-bottom: 20px;
}

.activity-header {
  font-size: 16px;
  font-weight: bold;
  margin-bottom: 15px;
  padding-bottom: 10px;
  border-bottom: 1px solid #EBEEF5;
}

.pagination-container {
  display: flex;
  justify-content: center;
  margin-top: 20px;
}
</style>