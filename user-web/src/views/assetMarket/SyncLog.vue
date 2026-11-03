<template>
  <div class="sync-log">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>同步日志</span>
          <el-button @click="$router.back()">
            返回
          </el-button>
        </div>
      </template>
      <el-form inline>
        <el-form-item label="资产 ID">
          <el-input
            v-model="assetId"
            clearable
            placeholder="可选"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="fetchList"
          >
            查询
          </el-button>
        </el-form-item>
      </el-form>
      <el-table
        v-loading="loading"
        :data="list"
        stripe
      >
        <el-table-column
          prop="created_at"
          label="时间"
          width="180">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="asset_id"
          label="资产"
          min-width="180"
        />
        <el-table-column
          prop="action"
          label="操作"
          width="120"
        />
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.status === 'success' ? 'success' : 'danger'"
              size="small"
            >
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="error_msg"
          label="错误信息"
          min-width="200"
          show-overflow-tooltip
        />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { syncLog } from '@/api/assetMarket'
import { fmtTime } from '@/utils/time'

const loading = ref(false)
const list = ref([])
const assetId = ref('')

const fetchList = async () => {
  loading.value = true
  try {
    const resp = await syncLog({ asset_id: assetId.value, limit: 100 })
    list.value = resp?.data || resp || []
  } finally {
    loading.value = false
  }
}

onMounted(fetchList)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
