<template>
  <div class="custom-dashboard">
    <div class="toolbar">
      <h2 class="title">
        {{ t('navigationContent.dashboardBuilder.title') }}
      </h2>
      <div class="toolbar-actions">
        <el-button @click="addBlock()">
          {{ t('navigationContent.dashboardBuilder.add') }}
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="save"
        >
          {{ t('navigationContent.dashboardBuilder.save') }}
        </el-button>
      </div>
    </div>
    <el-alert
      :title="t('navigationContent.dashboardBuilder.notice')"
      type="info"
      :closable="false"
      show-icon
      class="example-notice"
    />
    <div class="public-settings">
      <el-switch
        v-model="screenPublic"
        :active-text="t('navigationContent.dashboardBuilder.public')"
        :aria-label="t('navigationContent.dashboardBuilder.public')"
        :disabled="saving"
      />
      <span>{{ t('navigationContent.dashboardBuilder.publicHint') }}</span>
    </div>

    <el-row :gutter="12">
      <el-col
        :xs="24"
        :sm="6"
      >
        <el-card class="palette">
          <template #header>
            {{ t('navigationContent.dashboardBuilder.palette') }}
          </template>
          <div
            v-for="blockType in blockTypes"
            :key="blockType.type"
            class="palette-item"
            role="button"
            tabindex="0"
            :aria-label="t('navigationContent.dashboardBuilder.addBlock', { label: blockLabel(blockType.type) })"
            draggable="true"
            @dragstart="onPaletteDragStart($event, blockType)"
            @click="addBlock(blockType.type)"
            @keydown.enter.prevent="addBlock(blockType.type)"
            @keydown.space.prevent="addBlock(blockType.type)"
          >
            <el-icon><component :is="blockType.icon" /></el-icon>
            <span>{{ blockLabel(blockType.type) }}</span>
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="24"
        :sm="18"
      >
        <div
          class="canvas"
          role="presentation"
          @drop="onDrop"
          @dragover.prevent
        >
          <el-empty
            v-if="blocks.length === 0"
            :description="t('navigationContent.dashboardBuilder.empty')"
          />
          <div
            v-for="(block, index) in blocks"
            :key="block.id"
            class="canvas-block"
            :style="blockStyle(block)"
          >
            <div class="block-toolbar">
              <el-tag
                type="info"
                size="small"
              >
                {{ t('navigationContent.dashboardBuilder.example') }}
              </el-tag>
              <el-button-group size="small">
                <el-button
                  :aria-label="t('navigationContent.dashboardBuilder.resize')"
                  @click="resize(index)"
                >
                  <el-icon><Rank /></el-icon>
                </el-button>
                <el-button
                  type="danger"
                  :aria-label="t('navigationContent.dashboardBuilder.remove')"
                  @click="removeBlock(index)"
                >
                  <el-icon><Close /></el-icon>
                </el-button>
              </el-button-group>
            </div>
            <div class="block-preview">
              <span
                v-if="block.type === 'kpi'"
                class="kpi"
              >—</span>
              <el-icon
                v-else
                :size="32"
              >
                <component :is="blockTypes.find(item => item.type === block.type).icon" />
              </el-icon>
              <p>{{ t('navigationContent.dashboardBuilder.noSource') }}</p>
            </div>
            <div class="block-title">
              {{ blockLabel(block.type) }}
            </div>
          </div>
        </div>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, markRaw } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DataLine, PieChart, Document, Histogram, TrendCharts, LocationInformation, Rank, Close
} from '@element-plus/icons-vue'
import { http } from '@/utils/request'

const { t } = useI18n()
const blockTypes = [
  { type: 'line', icon: markRaw(TrendCharts) },
  { type: 'bar', icon: markRaw(DataLine) },
  { type: 'pie', icon: markRaw(PieChart) },
  { type: 'kpi', icon: markRaw(Document) },
  { type: 'funnel', icon: markRaw(Histogram) },
  { type: 'map', icon: markRaw(LocationInformation) }
]
const blocks = ref([])
const saving = ref(false)
const screenPublic = ref(true)
let nextId = 1
const blockLabel = (type) => t('navigationContent.dashboardBuilder.' + type)

function onPaletteDragStart(event, blockType) {
  event.dataTransfer.setData('blockType', blockType.type)
}

function onDrop(event) {
  addBlock(event.dataTransfer.getData('blockType'))
}

function addBlock(type = 'line') {
  if (!blockTypes.some(blockType => blockType.type === type)) return
  blocks.value.push({
    id: nextId++,
    type,
    title: `${blockLabel(type)} · ${t('navigationContent.dashboardBuilder.example')}`,
    width: 6,
    height: 240,
    data: { is_example: true, data_source: null }
  })
  ElMessage.success(t('navigationContent.dashboardBuilder.added'))
}

function blockStyle(block) {
  return { width: `${(block.width / 12) * 100}%`, height: `${block.height}px` }
}

function resize(index) {
  blocks.value[index].width = blocks.value[index].width === 6 ? 12 : 6
}
function removeBlock(index) { blocks.value.splice(index, 1) }

async function save() {
  if (saving.value) return
  saving.value = true
  // 固化用户确认的布局和公开选项，等待确认期间的编辑留给下一次保存。
  const isPublic = screenPublic.value
  const layout = {
    kind: 'example',
    data_source: null,
    blocks: blocks.value.map(block => ({ ...block, data: { ...block.data } }))
  }
  const name = t('navigationContent.dashboardBuilder.autoName', {
    time: new Date().toISOString().slice(0, 16).replace('T', ' ')
  })
  try {
    await ElMessageBox.confirm(
      t(isPublic ? 'navigationContent.dashboardBuilder.confirmPublic' : 'navigationContent.dashboardBuilder.confirmPrivate', { name }),
      t('navigationContent.dashboardBuilder.confirmTitle'),
      { type: 'info' }
    )
    await http.post('/api/dashboards', { name, layout, theme: 'light', is_public: isPublic })
    ElMessage.success({
      message: t(isPublic ? 'navigationContent.dashboardBuilder.savedPublic' : 'navigationContent.dashboardBuilder.savedPrivate', { name }),
      duration: 5000
    })
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(t('navigationContent.dashboardBuilder.saveError'))
    }
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.custom-dashboard { padding: 16px; }
.toolbar, .toolbar-actions { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.toolbar { justify-content: space-between; margin-bottom: 16px; }
.title { margin: 0; font-size: 16px; font-weight: 600; }
.example-notice { margin-bottom: 12px; }
.public-settings { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 16px; }
.public-settings span { color: var(--el-text-color-regular); font-size: 14px; }
.palette-item { padding: 8px; margin-bottom: 4px; background: var(--el-fill-color-light); border-radius: 4px; cursor: grab; display: flex; align-items: center; gap: 6px; }
.palette-item:hover { background: var(--el-fill-color); }
.palette-item:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
.canvas { min-height: 600px; display: flex; flex-wrap: wrap; align-content: flex-start; background: var(--el-bg-color); border-radius: 8px; padding: 16px; }
.canvas-block { position: relative; box-sizing: border-box; background: var(--el-fill-color-light); border: 4px solid var(--el-bg-color); border-radius: 6px; padding: 12px; min-width: 0; }
.block-toolbar { display: flex; flex-wrap: wrap; gap: 8px; justify-content: space-between; }
.block-preview { height: 150px; display: flex; flex-direction: column; justify-content: center; align-items: center; color: var(--el-text-color-regular); text-align: center; }
.block-preview p { margin: 12px 0 0; overflow-wrap: anywhere; }
.block-title { text-align: center; font-size: 14px; color: var(--el-text-color-regular); }
.kpi { font-size: 32px; font-weight: 700; }
@media (max-width: 767px) {
  .palette { margin-bottom: 12px; }
  .canvas-block { width: 100% !important; }
}
</style>
