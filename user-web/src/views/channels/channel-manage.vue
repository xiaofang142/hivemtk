<template>
  <div class="channel-manage">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('channels.catalog') }}</span>
          <el-alert
            type="info"
            :closable="false"
            :title="$t('channels.toggleOnlyHint')"
          />
        </div>
      </template>

      <el-form
        :inline="true"
        :model="filter"
        size="small"
      >
        <el-form-item label="关键字">
          <el-input
            v-model="filter.keyword"
            placeholder="渠道名称/编码"
            clearable
            style="width: 200px"
          />
        </el-form-item>
        <el-form-item>
          <el-button @click="loadList">
            查询
          </el-button>
          <el-button @click="resetFilter">
            重置
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="platformRows"
        size="small"
        border
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          label="渠道名称"
          min-width="140"
        >
          <template #default="{ row }">
            {{ catalog.channelLabel(row.code) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="code"
          label="编码"
          min-width="140"
        />
        <el-table-column
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="本租户开关"
          width="130"
          align="center"
        >
          <template #default="{ row }">
            <el-switch
              :model-value="!!row.enabled"
              :loading="togglingId === row.id"
              @change="(val) => toggleEnabled(row, val)"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="remark"
          label="备注"
          min-width="160"
          show-overflow-tooltip
        />
      </el-table>

      <section
        v-if="serviceBindings.length"
        class="service-bindings"
        data-testid="legacy-service-bindings"
      >
        <h2>{{ $t('channels.legacyBindings') }}</h2>
        <p>{{ $t('channels.legacyBindingsHint') }}</p>
        <ul>
          <li
            v-for="binding in serviceBindings"
            :key="binding.id"
          >
            <strong>{{ binding.name }}</strong>
            <span>SageX · QQ / {{ $t('channels.personalWechat') }}</span>
          </li>
        </ul>
      </section>
    </el-card>
  </div>
</template>

<script setup>
import { computed, ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import channelAccountApi from '@/api/channelAccount.js'
import { useChannelCatalogStore } from '@/stores/channelCatalog'
import { isLegacyConnectionService } from '@/constants/channel'

const catalog = useChannelCatalogStore()
const loading = ref(false)
const togglingId = ref(null)
const rows = ref([])
const platformRows = computed(() => rows.value.filter(row => !isLegacyConnectionService(row.code)))
const serviceBindings = computed(() => rows.value.filter(row => isLegacyConnectionService(row.code)))
const filter = reactive({ keyword: '' })

async function loadList() {
  loading.value = true
  try {
    const res = await channelAccountApi.listChannels({ keyword: filter.keyword })
    rows.value = res?.list || res || []
  } catch (e) {
    ElMessage.error('加载渠道失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

function resetFilter() {
  filter.keyword = ''
  loadList()
}

async function toggleEnabled(row, enabled) {
  togglingId.value = row.id
  try {
    await catalog.setTenantEnabled(row.id, !!enabled)
    ElMessage.success(enabled ? '已启用' : '已停用')
    await loadList()
  } catch (e) {
    ElMessage.error('操作失败：' + (e.message || '未知错误'))
  } finally {
    togglingId.value = null
  }
}

onMounted(async () => {
  await catalog.ensure()
  await loadList()
})
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.service-bindings { margin-top: 24px; }
.service-bindings h2 { margin: 0 0 8px; font-size: 16px; }
.service-bindings p { color: var(--el-text-color-secondary); line-height: 1.6; }
.service-bindings ul { margin: 0; padding: 0; list-style: none; }
.service-bindings li { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 8px 0; }
.service-bindings li span { color: var(--el-text-color-secondary); }
</style>
