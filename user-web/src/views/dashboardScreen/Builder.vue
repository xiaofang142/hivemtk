<template>
  <div class="custom-dashboard">
    <el-row :gutter="12" class="toolbar">
      <el-col :span="12">
        <span class="title">自定义看板拖拽（USR-AN-01）</span>
      </el-col>
      <el-col :span="12" style="text-align: right">
        <el-button @click="addBlock">+ 添加组件</el-button>
        <el-button type="primary" @click="save">保存看板</el-button>
      </el-col>
    </el-row>

    <el-row :gutter="12">
      <el-col :span="6">
        <el-card class="palette">
          <template #header><span>组件库</span>  <el-dialog v-model="saveDialogVisible" title="保存看板" width="420px">
    <el-form label-width="80px">
      <el-form-item label="看板名称">
        <el-input v-model="saveName" placeholder="请输入看板名称" maxlength="60" @keyup.enter="confirmSave" />
      </el-form-item>
      <el-form-item label="公开访问">
        <el-switch v-model="screenPublic" active-text="公开投屏链接" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="saveDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="confirmSave">保存</el-button>
    </template>
  </el-dialog>
</template>
          <div
            v-for="b in blockTypes"
            :key="b.type"
            class="palette-item"
            role="button"
            tabindex="0"
            :aria-label="`添加${b.label}组件`"
            draggable="true"
            @dragstart="onPaletteDragStart($event, b)"
            @click="addBlock(b.type)"
            @keydown.enter.prevent="addBlock(b.type)"
            @keydown.space.prevent="addBlock(b.type)"
          >
            <el-icon><component :is="b.icon" /></el-icon>
            <span>{{ b.label }}</span>
          </div>
        </el-card>
      </el-col>
      <el-col :span="18">
        <div class="canvas" role="presentation" @drop="onDrop" @dragover.prevent>
          <div
            v-for="(block, i) in blocks"
            :key="block.id"
            class="canvas-block"
            :style="blockStyle(block)"
          >
            <div class="block-toolbar">
              <el-button-group size="small">
                <el-button @click="resize(i)">↔</el-button>
                <el-button type="danger" @click="removeBlock(i)">×</el-button>
              </el-button-group>
            </div>
            <component :is="resolveChart(block.type)" :data="block.data" />
            <div class="block-title">{{ block.title }}</div>
          </div>
        </div>
      </el-col>
    </el-row>
  </div>
  <el-dialog v-model="saveDialogVisible" title="保存看板" width="420px" append-to-body>
    <el-form label-width="80px">
      <el-form-item label="看板名称">
        <el-input v-model="saveName" placeholder="请输入看板名称" maxlength="60" @keyup.enter="confirmSave" />
      </el-form-item>
      <el-form-item label="公开访问">
        <el-switch v-model="screenPublic" active-text="公开投屏链接" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="saveDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="confirmSave">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, reactive, markRaw } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DataLine, PieChart, Document, Histogram, TrendCharts, LocationInformation
} from '@element-plus/icons-vue'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { http } from '@/utils/request'

const blockTypes = [
  { type: 'line', label: '折线', icon: markRaw(TrendCharts) },
  { type: 'bar', label: '柱状', icon: markRaw(DataLine) },
  { type: 'pie', label: '饼图', icon: markRaw(PieChart) },
  { type: 'kpi', label: '数字卡', icon: markRaw(Document) },
  { type: 'funnel', label: '漏斗', icon: markRaw(Histogram) },
  { type: 'map', label: '地图', icon: markRaw(LocationInformation) }
]

const blocks = ref([])
const saveDialogVisible = ref(false)
const saveName = ref('')
const saving = ref(false)
const screenPublic = ref(true)
let _id = 1

function onPaletteDragStart(e, b) {
  e.dataTransfer.setData('blockType', b.type)
}

function onDrop(e) {
  const type = e.dataTransfer.getData('blockType')
  if (!type) return
  blocks.value.push({
    id: _id++,
    type,
    title: blockTypes.find((b) => b.type === type).label,
    width: 6,
    height: 240,
    data: { value: Math.floor(Math.random() * 1000) }
  })
  ElMessage.success('已添加组件')
}

function resolveChart(type) {
  return markRaw({
    line: defineAsyncComponent(() => loadChart(type)),
    bar: defineAsyncComponent(() => loadChart(type)),
    pie: defineAsyncComponent(() => loadChart(type)),
    kpi: markRaw({ template: '<div class="kpi">{{ data.value }}</div>' }),
    funnel: defineAsyncComponent(() => loadChart(type)),
    map: defineAsyncComponent(() => loadChart(type))
  }[type] || markRaw({ template: '未知' }))
}

import { defineAsyncComponent } from 'vue'
function loadChart(type) {
  return {
    template: `<div ref="chart" class="chart-area"></div>`,
    mounted() {
      const c = safeInit(this.$refs.chart)
      c.setOption({
        xAxis: { type: 'category', data: ['A', 'B', 'C', 'D'] },
        yAxis: { type: 'value' },
        series: [{ type: '${type}', data: [10, 20, 30, 40] }]
      })
    }
  }
}

function blockStyle(b) {
  return {
    width: `${(b.width / 12) * 100}%`,
    height: `${b.height}px`
  }
}

function addBlock() {}
function resize(i) {
  blocks.value[i].width = blocks.value[i].width === 6 ? 12 : 6
}
function removeBlock(i) { blocks.value.splice(i, 1) }

async function save() {
  // R5-2 修复：CreateScreenRequest 需要 name（required），且布局字段是 layout
  try {
    // R5-4：本页面内 ElMessageBox.prompt 触发 Vue 挂载崩溃（parent.provides undefined），
    // 改用 confirm + 自动命名绕开；名称含时间戳保证可辨识
    const autoName = `看板-${new Date().toISOString().slice(0, 16).replace('T', ' ')}`
    await ElMessageBox.confirm(`保存为「${autoName}」？（公开投屏语义，保存后即可通过公开链接访问）`, '保存看板', { type: 'info' })
    await http.post('/api/dashboards', {
      name: autoName,
      layout: { blocks: blocks.value },
      theme: 'light',
      is_public: screenPublic.value // 大屏语义=公开投屏；false 会使公开路由 404（R5-3）
    })
    // 注意：不持有响应对象引用——拦截器返回的对象进入响应式链会触发
    // Vue Object.create(undefined)（R5-4，堆栈 vue Object.create cf/ae/Q）
    ElMessage.success(`看板「${autoName}」已保存（公开可访问）`, { duration: 5000 })
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') {
      // 非 HTTP 错误（如 JSON 序列化失败）兜底提示；HTTP 错误由拦截器统一 toast
      if (!String(e).includes('Request failed')) {
        localStorage.setItem('r5_save_err', JSON.stringify({msg: e?.message || String(e), stack: e?.stack || ''}))
        ElMessage.error('保存失败：' + (e?.message || e))
      }
    }
  }
}
</script>

<style scoped>
.custom-dashboard { padding: 16px; }
.toolbar { margin-bottom: 16px; }
.title { font-size: 16px; font-weight: 600; }
.palette-item { padding: 8px; margin-bottom: 4px; background: #F8FAFC; border-radius: 4px; cursor: grab; display: flex; align-items: center; gap: 6px; }
.canvas { min-height: 600px; background: #fff; border-radius: 8px; padding: 16px; }
.canvas-block { position: relative; display: inline-block; background: #F8FAFC; border-radius: 6px; padding: 12px; margin: 4px; vertical-align: top; }
.block-toolbar { position: absolute; right: 4px; top: 4px; }
.chart-area { width: 100%; height: 180px; }
.block-title { text-align: center; font-size: 12px; color: #94A3B8; margin-top: 4px; }
.kpi { font-size: 32px; font-weight: 700; text-align: center; color: #4F46E5; }
</style>
