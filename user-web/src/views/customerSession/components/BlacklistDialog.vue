<template>
  <el-dialog
    :model-value="modelValue"
    title="黑名单管理"
    width="680px"
    :close-on-click-modal="false"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div class="blacklist-toolbar">
      <span class="count">共 {{ blacklistItems.length }} 条记录</span>
      <el-button
        size="small"
        :loading="blacklistLoading"
        @click="$emit('reload')"
      >
        刷新
      </el-button>
    </div>
    <el-table
      v-loading="blacklistLoading"
      :data="blacklistItems"
      size="small"
      max-height="420"
      empty-text="暂无黑名单记录"
    >
      <el-table-column
        label="访客ID"
        min-width="130"
      >
        <template #default="{ row }">
          {{ row.user_id ?? row.UserID ?? row.userId ?? '-' }}
        </template>
      </el-table-column>
      <el-table-column
        label="平台"
        width="100"
      >
        <template #default="{ row }">
          {{ catalog.channelLabel(row.platform ?? row.Platform) }}
        </template>
      </el-table-column>
      <el-table-column
        label="拉黑原因"
        min-width="160"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          {{ row.reason ?? row.Reason ?? '-' }}
        </template>
      </el-table-column>
      <el-table-column
        label="来源"
        width="90"
      >
        <template #default="{ row }">
          <el-tag
            size="small"
            :type="(row.source ?? row.Source) === 'auto' ? 'warning' : 'info'"
          >
            {{ row.source ?? row.Source ?? 'manual' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="拉黑时间"
        width="140"
      >
        <template #default="{ row }">
          {{ formatTime(row.created_at ?? row.CreatedAt) }}
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="90"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            size="small"
            type="danger"
            link
            @click="$emit('unblacklist', row)"
          >
            解除
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">
        关闭
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
// 黑名单管理弹窗(由 List.vue 原样迁出,渲染与交互保持一致)
import { onMounted } from 'vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'
import { formatTime } from '../composables/useSessionFilters'

const catalog = useChannelCatalogStore()
onMounted(() => { catalog.ensure() })

defineProps({
  modelValue: { type: Boolean, default: false },
  blacklistItems: { type: Array, default: () => [] },
  blacklistLoading: { type: Boolean, default: false }
})

defineEmits(['update:modelValue', 'reload', 'unblacklist'])
</script>

<style scoped>
/* C2：黑名单管理弹窗工具栏 */
.blacklist-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.blacklist-toolbar .count {
  font-size: 13px;
  color: #606266;
}
</style>
