<template>
  <div class="douyin-card-stats-container">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <span>抖音卡片统计 - {{ cardInfo.title ?? (cardLoadFailed ? $t('加载失败') : '加载中...') }}</span>
          <div class="header-actions">
            <el-button @click="goBack">
              {{ $t('返回') }}
            </el-button>
            <el-date-picker
              v-model="dateRange"
              type="daterange"
              range-separator="至"
              start-placeholder="开始日期"
              end-placeholder="结束日期"
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
        v-if="cardLoadFailed || statsLoadFailed"
        class="stats-alert"
        type="warning"
        show-icon
        :closable="false"
        :title="cardLoadFailed ? '这张卡片读不到了' : '统计数据读取失败'"
        :description="cardLoadFailed ? '卡片已被删除，或链接里的 id 不属于当前账号。下方的 0 与空图是「没读到」，不是「真的没有」。' : '卡片本身还在，但这一轮的统计没读到。下方的 0 与空图是「没读到」，不是「真的没有」——请点「刷新」重试。'"
      />

      
      <div
        v-if="cardInfo.id"
        class="card-info-section"
      >
        <el-row :gutter="20">
          <el-col :span="6">
            <el-image
              v-if="cardInfo.image_url"
              style="width: 100%; height: 200px"
              :src="cardInfo.image_url"
              :preview-src-list="[cardInfo.image_url]"
              fit="cover"
            >
              <template #error>
                <div class="img-error-fallback">
                  {{ $t('图片加载失败') }}
                </div>
              </template>
            </el-image>
          </el-col>
          <el-col :span="18">
            <el-descriptions
              :column="2"
              border
            >
              <el-descriptions-item label="卡片ID">
                {{ cardInfo.id }}
              </el-descriptions-item>
              <el-descriptions-item label="标题">
                {{ cardInfo.title }}
              </el-descriptions-item>
              <el-descriptions-item label="状态">
                <el-tag :type="cardInfo.is_active ? 'success' : 'danger'">
                  {{ cardInfo.is_active ? '激活' : '禁用' }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="创建时间">
                {{ fmtTime(cardInfo.created_at) }}
              </el-descriptions-item>
              <el-descriptions-item
                label="描述"
                :span="2"
              >
                {{ cardInfo.description }}
              </el-descriptions-item>
            </el-descriptions>
          </el-col>
        </el-row>
      </div>

      
      <div class="stats-overview">
        <el-row :gutter="20">
          <el-col :span="24">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ formatNumber(cardStats.viewCount) }}
                </div>
                <div class="stats-label">
                  总浏览量
                </div>
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
              <div
                ref="visitTrendChart"
                class="chart-container"
              />
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
              <el-table
                :data="(cardStats.recentActivity || []).filter(item => item.action === 'view')"
                style="width: 100%"
              >
                <el-table-column
                  prop="username"
                  label="用户"
                />
                <el-table-column
                  prop="action"
                  label="操作"
                >
                  <template #default="scope">
                    <el-tag :type="getActionType(scope.row.action)">
                      {{ getActionText(scope.row.action) }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column
                  prop="ipAddress"
                  label="IP地址"
                />
                <el-table-column
                  prop="createdAt"
                  label="时间"
                >
                  <template #default="scope">
                    {{ formatDate(scope.row.createdAt) }}
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
import { ref, reactive, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { safeInit } from '@/utils/echarts'
import { getDouyinCard, getDouyinCardStats } from '@/api/douyinCard'
import { fmtTime } from '@/utils/time'

const route = useRoute();
const router = useRouter()

const loading = ref(false);
const cardId = ref(route.params.id)
const cardInfo = ref({})
// 表头那句「加载中...」必须有出路：卡片被删后 /api/douyin/:id 回 404，cardInfo 永远是 {}，
// 只靠 cardInfo.title 判断的话这句话会一直挂着，把"这条不在了"读成"还在转"。
const cardLoadFailed = ref(false)
// 两条读口各自失败：卡片在、统计挂了时，"这张卡片读不到了"就是说谎。
const statsLoadFailed = ref(false)
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
    const res = await getDouyinCard(cardId.value)
    cardInfo.value = res || {}
    cardLoadFailed.value = !res?.id
  } catch (error) {
    // 拦截器已经按状态码弹过一次（404 → 「资源不存在」），这里再弹一条是两句同义 toast。
    // 页面改用常驻的 cardLoadFailed 说结论：toast 三秒就没了，标题的「加载中...」不会。
    cardInfo.value = {}
    cardLoadFailed.value = true
    console.warn(error)
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
    
    const res = await getDouyinCardStats(cardId.value, params)
    Object.assign(cardStats, res);
    statsLoadFailed.value = false

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

// safeInit 在没有容器时回 null（图在告警/卡片信息之后渲染，v-if 关掉时拿不到 dom）。
// 之前这里不判空直接 .setOption，于是"卡片被删 + 统计回来了"这一格会抛 TypeError：
// 那是一条未捕获异常（控制台红），且页面停在旧图上。
const updateCharts = () => {
  if (visitChartInstance) {
    visitChartInstance.dispose()
  }

  visitChartInstance = safeInit(visitTrendChart.value)
  if (!visitChartInstance) return
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
      // dto.DailyStat.date 在这条链路上是完整时间戳，直接当类目标签会印一长串 ISO。
      data: (cardStats.dailyStats || []).map(item => formatDate(item.date))
    },
    yAxis: {
      type: 'value'
    },
    series: [
      {
        name: '浏览量',
        type: 'line',
        data: (cardStats.dailyStats || []).map(item => item.view),
        smooth: true
      }
    ]
  }
  visitChartInstance.setOption(visitOption)
};

// 每次刷新都 addEventListener 一个新的匿名回调 = 监听器只增不减（DOM 只对同一函数引用去重），
// 离开本页后还在给已 dispose 的实例喊 resize。命名 + 挂一次 + 卸载时摘掉。
const handleResize = () => {
  if (visitChartInstance && !visitChartInstance.isDisposed()) visitChartInstance.resize()
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
  // 两个调用点：查询参数传 Date，接口回显传字符串（后端 DTO 里是 RFC3339）。
  // 字符串原先原样透出 ⇒ 表格与横轴上是 "2026-09-30T00:00:00Z" 这种长串，这里统一成 YYYY-MM-DD。
  const d = new Date(date)
  if (Number.isNaN(d.getTime())) return typeof date === 'string' ? date : ''
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate())
  
  return `${year}-${month}-${String(day).padStart(2, '0')}`
};

// el-tag 的 type 只接受 primary/success/info/warning/danger，传 '' 会触发 Vue prop 校验告警
// 并回落到 default='primary'。中性动作用 'info'，与 kuaishouCard/CardStats.vue 既有写法一致。
const getActionType = (action) => {
  const actionMap = {
    view: 'info'
  }
  return actionMap[action] || 'info'
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

  window.addEventListener('resize', handleResize)

  fetchCardInfo()
  fetchCardStats()
});

onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  if (visitChartInstance && !visitChartInstance.isDisposed()) visitChartInstance.dispose()
  visitChartInstance = null
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
}

.card-info-section {
  margin-bottom: 20px;
}

.stats-alert {
  margin-bottom: 16px;
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