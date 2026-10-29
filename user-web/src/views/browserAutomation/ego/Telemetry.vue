<template>
  <div class="page">
    <h2>遥测看板（平台端）</h2>
    <p class="hint">
      runner 遥测的聚合视图：按事件类型（kind）的量、成功率、p95 耗时。
      口径是「永不收页面内容」——只有低基数枚举与数值进遥测，这里**没有单条明细面**。
    </p>

    <div class="toolbar">
      <el-radio-group
        v-model="hours"
        @change="load"
      >
        <el-radio-button :value="1">近 1 小时</el-radio-button>
        <el-radio-button :value="24">近 24 小时</el-radio-button>
        <el-radio-button :value="168">近 7 天</el-radio-button>
        <el-radio-button :value="720">近 30 天</el-radio-button>
      </el-radio-group>
      <el-button
        :loading="loading"
        class="ml-8"
        @click="load"
      >
        刷新
      </el-button>
      <span
        v-if="truncated"
        class="hint warn"
      >
        窗口内样本量超出聚合上限，读数按前 {{ total }} 条计算（已截断标注，不静默）
      </span>
    </div>

    <el-table
      v-loading="loading"
      :data="buckets"
      border
    >
      <el-table-column
        prop="kind"
        label="事件类型"
        width="120"
      >
        <template #default="{ row }">
          {{ kindLabel(row.kind) }}
        </template>
      </el-table-column>
      <el-table-column
        prop="count"
        label="样本量"
        width="100"
      />
      <el-table-column
        prop="ok_count"
        label="成功"
        width="90"
      />
      <el-table-column
        prop="fail_count"
        label="失败"
        width="90"
      />
      <el-table-column
        prop="success_pct"
        label="成功率"
        width="110"
      >
        <template #default="{ row }">
          {{ row.success_pct.toFixed(1) }}%
        </template>
      </el-table-column>
      <el-table-column
        prop="p95_ms"
        label="p95 耗时"
        width="120"
      >
        <template #default="{ row }">
          {{ row.p95_ms > 0 ? row.p95_ms + ' ms' : '—' }}
        </template>
      </el-table-column>
    </el-table>

    <p
      v-if="!loading && buckets.length === 0"
      class="hint"
    >
      窗口内暂无样本。样本由在线 runner 自动上报（run/install/upgrade/sync 四类，
      30s 批量一次）；长期为空说明 runner 没连上或上报通道故障。
    </p>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { getEgoTelemetrySummary } from '@/api/egolite'

const hours = ref(24)
const buckets = ref([])
const total = ref(0)
const truncated = ref(false)
const loading = ref(false)

const KIND_LABELS = {
  run: '执行',
  install: '资产安装',
  upgrade: 'ego 升级',
  sync: '资产同步'
}

function kindLabel (k) {
  return KIND_LABELS[k] || k
}

async function load () {
  loading.value = true
  try {
    const res = await getEgoTelemetrySummary(hours.value)
    buckets.value = res?.buckets || []
    total.value = res?.total || 0
    truncated.value = !!res?.truncated
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.warn {
  color: #b45309;
}
</style>
