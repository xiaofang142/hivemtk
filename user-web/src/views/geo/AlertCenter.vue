<template>
  <div class="geo-page">
    <div class="p-4">
      <el-row
        :gutter="16"
        class="mb-4"
      >
        <el-col :span="8">
          <el-card>
            <el-statistic
              title="未确认告警"
              :value="unreadCount"
            >
              <template #suffix>
                <el-tag
                  v-if="unreadCount > 0"
                  type="danger"
                  size="small"
                  effect="dark"
                  class="ml-2"
                >
                  需处理
                </el-tag>
              </template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :span="8">
          <el-card>
            <el-statistic
              title="告警总数"
              :value="total"
            />
          </el-card>
        </el-col>
        <el-col :span="8">
          <el-card>
            <div class="flex flex-col gap-2">
              <el-button
                type="primary"
                :loading="loading"
                @click="load"
              >
                刷新
              </el-button>
              <el-button
                :disabled="!hasUnackInPage"
                @click="ackAllVisible"
              >
                本页全部确认
              </el-button>
            </div>
          </el-card>
        </el-col>
      </el-row>

      
      <el-card class="mb-4">
        <div class="flex items-center gap-3 flex-wrap">
          <el-select
            v-model="filterType"
            placeholder="告警类型"
            clearable
            style="width:180px"
            @change="load"
          >
            <el-option
              label="负面监控"
              value="negative_monitor"
            />
            <el-option
              label="SOV 下降"
              value="sov_drop"
            />
            <el-option
              label="实体异常"
              value="entity_anomaly"
            />
          </el-select>
          <el-select
            v-model="filterLevel"
            placeholder="级别"
            clearable
            style="width:140px"
            @change="load"
          >
            <el-option
              label="Info"
              value="info"
            />
            <el-option
              label="Warning"
              value="warning"
            />
            <el-option
              label="Critical"
              value="critical"
            />
          </el-select>
        </div>
      </el-card>

      
      <el-card>
        <template #header>
          <span class="font-bold">GEO 告警（AI 引擎负面命中 / 异常）</span>
        </template>
        <el-table
          v-loading="loading"
          :data="alerts"
          size="small"
        >
          <el-table-column
            label="状态"
            width="90"
          >
            <template #default="{ row }">
              <el-tag
                v-if="!row.notified"
                type="danger"
                size="small"
              >
                未确认
              </el-tag>
              <el-tag
                v-else
                type="info"
                size="small"
              >
                已确认
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="级别"
            width="100"
          >
            <template #default="{ row }">
              <el-tag
                :type="levelTagType(row.level)"
                size="small"
              >
                {{ row.level }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="类型"
            width="130"
          >
            <template #default="{ row }">
              <el-tag
                size="small"
                effect="plain"
              >
                {{ typeLabel(row.type) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            prop="engine"
            label="引擎"
            width="110"
          />
          <el-table-column
            prop="query"
            label="触发查询"
            min-width="180"
            show-overflow-tooltip
          />
          <el-table-column
            prop="snippet"
            label="命中片段"
            min-width="240"
            show-overflow-tooltip
          />
          <el-table-column
            label="时间"
            width="170"
          >
            <template #default="{ row }">
              {{ formatTime(row.created_at) }}
            </template>
          </el-table-column>
          <el-table-column
            label="操作"
            width="150"
            fixed="right"
          >
            <template #default="{ row }">
              <el-button
                v-if="!row.notified"
                size="small"
                type="primary"
                link
                @click="ack(row)"
              >
                确认
              </el-button>
              <el-button
                size="small"
                type="danger"
                link
                @click="remove(row)"
              >
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <div class="flex justify-end mt-3">
          <el-pagination
            v-model:current-page="page"
            :page-size="limit"
            :total="total"
            layout="prev, pager, next, total"
            @current-change="load"
          />
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listGeoAlerts, getGeoAlertsUnreadCount, ackGeoAlert, deleteGeoAlert } from '@/api/geoAlert'
import { isUserReportedError } from '@/utils/errorReporting'

const alerts = ref([])
const total = ref(0)
const unreadCount = ref(0)
const loading = ref(false)
const page = ref(1)
const limit = ref(20)
const filterType = ref('')
const filterLevel = ref('')
let pollTimer = null

const hasUnackInPage = computed(() => alerts.value.some((a) => !a.notified))

const levelTagType = (level) => ({ critical: 'danger', warning: 'warning', info: 'info' }[level] || 'info')
const typeLabel = (type) => ({ negative_monitor: '负面监控', sov_drop: 'SOV下降', entity_anomaly: '实体异常' }[type] || type)

const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}

const load = async () => {
  loading.value = true
  try {
    const res = await listGeoAlerts({
      page: page.value,
      limit: limit.value,
      type: filterType.value || undefined,
      level: filterLevel.value || undefined
    })
    const data = res?.data || res
    alerts.value = data?.list || []
    total.value = data?.total || 0
    const unreadRes = await getGeoAlertsUnreadCount()
    const unreadData = unreadRes?.data || unreadRes
    unreadCount.value = unreadData?.count || 0
  } catch (e) {
    console.error('加载告警失败', e)
  } finally {
    loading.value = false
  }
}

const ack = async (row) => {
  try {
    await ackGeoAlert(row.id)
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '确认告警失败')
    return
  }
  ElMessage.success('已确认')
  await load()
}

const ackAllVisible = async () => {
  const targets = alerts.value.filter((a) => !a.notified)
  if (!targets.length) return
  // 取消/关闭确认框时 ElMessageBox 会 reject，必须吞掉，否则控制台留 unhandled rejection
  try {
    await ElMessageBox.confirm(`确认本页 ${targets.length} 条告警？`, '批量确认')
  } catch {
    return
  }
  try {
    await Promise.all(targets.map((a) => ackGeoAlert(a.id)))
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '批量确认失败')
    return
  }
  ElMessage.success(`已确认 ${targets.length} 条`)
  await load()
}

const remove = async (row) => {
  // 同上：取消确认框不是错误，直接返回
  try {
    await ElMessageBox.confirm('确定删除该告警？', '删除')
  } catch {
    return
  }
  try {
    await deleteGeoAlert(row.id)
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '删除告警失败')
    return
  }
  ElMessage.success('已删除')
  await load()
}

onMounted(() => {
  load()
  pollTimer = setInterval(() => {
    if (document.visibilityState === 'visible')
      load();
  }, 60000)
})
onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>
