<template>
  <div class="customer-profile">
    <el-card>
      <template #header>
        <div class="profile-header">
          <span><el-icon><UserFilled /></el-icon> 客户 360°</span>
          <el-button
            link
            size="small"
            @click="$emit('refresh')"
          >
            刷新
          </el-button>
        </div>
      </template>


      <div class="profile-section">
        <div class="profile-row">
          <span class="label">客户ID</span>
          <span class="value">{{ currentSession.customerId || currentSession.sessionId }}</span>
        </div>
        <div class="profile-row">
          <span class="label">渠道</span>
          <span class="value">{{ catalog.channelLabel(currentSession.channel) }}</span>
        </div>
        <div class="profile-row">
          <span class="label">首次接入</span>
          <span class="value">{{ formatTime(currentSession.createdAt) }}</span>
        </div>
        <div class="profile-row">
          <span class="label">消息数</span>
          <span class="value">{{ profileStats.messageCount || 0 }}</span>
        </div>
        <div class="profile-row">
          <span class="label">历史会话</span>
          <span class="value">{{ profileStats.sessionCount || 0 }}</span>
        </div>
        <div class="profile-row">
          <span class="label">AI 回复</span>
          <span class="value">{{ profileStats.aiReplyCount || 0 }}</span>
        </div>
      </div>


      <div class="profile-section">
        <div class="section-title">
          长期画像
        </div>
        <div
          v-if="profileTags.length === 0"
          class="empty-hint"
        >
          暂无画像标签
        </div>
        <div class="tag-cloud">
          <el-tag
            v-for="t in profileTags"
            :key="t.id || t.name"
            size="small"
            effect="plain"
            style="margin: 2px 4px 2px 0"
          >
            {{ t.name || t }}
          </el-tag>
        </div>
      </div>


      <div class="profile-section">
        <div class="section-title">
          当前 SOP 阶段
        </div>
        <el-radio-group
          :model-value="sopStage"
          size="small"
          style="display: flex; flex-direction: column; gap: 6px"
          @update:model-value="$emit('update:sopStage', $event)"
        >
          <el-radio value="presale">
            售前询价
          </el-radio>
          <el-radio value="lead">
            引导留资
          </el-radio>
          <el-radio value="aftersale">
            售后查单
          </el-radio>
          <el-radio value="refund">
            投诉退款
          </el-radio>
        </el-radio-group>
        <el-button
          size="small"
          type="primary"
          style="margin-top: 8px; width: 100%"
          :disabled="!sopStage"
          @click="$emit('save-sop-stage')"
        >
          保存阶段
        </el-button>
      </div>


      <div class="profile-section">
        <div class="section-title">
          快捷操作
        </div>
        <el-button-group style="width: 100%; display: grid; grid-template-columns: 1fr 1fr; gap: 6px">
          <el-button
            size="small"
            @click="$emit('send-coupon')"
          >
            <el-icon><Ticket /></el-icon> 发券
          </el-button>
          <el-button
            size="small"
            @click="$emit('send-product-card')"
          >
            <el-icon><Goods /></el-icon> 发商品卡
          </el-button>
          <el-button
            size="small"
            @click="$emit('blacklist')"
          >
            <el-icon><CircleClose /></el-icon> 拉黑
          </el-button>
          <el-button
            size="small"
            type="danger"
            @click="$emit('close-session')"
          >
            <el-icon><SwitchButton /></el-icon> 结束
          </el-button>
          <el-button
            size="small"
            type="primary"
            plain
            @click="$emit('ai-summary')"
          >
            <el-icon><MagicStick /></el-icon> AI摘要
          </el-button>
          <el-button
            size="small"
            @click="$emit('export-transcript')"
          >
            <el-icon><Download /></el-icon> 转录
          </el-button>
          <el-button
            size="small"
            @click="$emit('snooze')"
          >
            <el-icon><Clock /></el-icon> 暂缓2h
          </el-button>
          <el-button
            size="small"
            @click="$emit('set-priority')"
          >
            <el-icon><Top /></el-icon> 优先级
          </el-button>
        </el-button-group>
      </div>
    </el-card>
  </div>
</template>

<script setup>
// 客户 360° 侧栏(由 List.vue 原样迁出,纯展示组件,操作通过事件上抛)
import { UserFilled, Ticket, Goods, CircleClose, SwitchButton, MagicStick, Download, Clock, Top } from '@element-plus/icons-vue'
import { onMounted } from 'vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'
import { formatTime } from '../composables/useSessionFilters'

const catalog = useChannelCatalogStore()
onMounted(() => { catalog.ensure() })

defineProps({
  currentSession: { type: Object, required: true },
  profileStats: { type: Object, required: true },
  profileTags: { type: Array, default: () => [] },
  sopStage: { type: String, default: '' }
})

defineEmits([
  'update:sopStage',
  'refresh',
  'save-sop-stage',
  'send-coupon',
  'send-product-card',
  'blacklist',
  'close-session',
  'ai-summary',
  'export-transcript',
  'snooze',
  'set-priority'
])
</script>

<style scoped>
/* 方向10：右栏 客户 360° 样式 */
.customer-profile {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.customer-profile :deep(.el-card) {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.customer-profile :deep(.el-card__body) {
  padding: 12px 16px;
  overflow-y: auto;
}
.profile-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}
.profile-header .el-icon {
  vertical-align: middle;
  margin-right: 4px;
}
.profile-section {
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px dashed #ebeef5;
}
.profile-section:last-child {
  border-bottom: none;
  margin-bottom: 0;
}
.section-title {
  font-size: 13px;
  font-weight: 600;
  color: #303133;
  margin-bottom: 8px;
}
.profile-row {
  display: flex;
  justify-content: space-between;
  padding: 4px 0;
  font-size: 13px;
}
.profile-row .label {
  color: #909399;
}
.profile-row .value {
  color: #303133;
  font-weight: 500;
}
.tag-cloud {
  display: flex;
  flex-wrap: wrap;
}
.empty-hint {
  color: #c0c4cc;
  font-size: 12px;
  padding: 4px 0;
}
</style>
