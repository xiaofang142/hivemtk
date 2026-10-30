<template>
  <div class="custom-dashboard">
    <el-row
      :gutter="12"
      class="toolbar"
    >
      <el-col :span="12">
        <span class="title">自定义看板拖拽（USR-AN-01）</span>
      </el-col>
      <el-col
        :span="12"
        style="text-align: right"
      >
        <el-button @click="addBlock">
          + 添加组件
        </el-button>
        <el-button
          type="primary"
          @click="save"
        >
          保存看板
        </el-button>
      </el-col>
    </el-row>

    <el-row :gutter="12">
      <el-col :span="6">
        <el-card class="palette">
          <template #header>
            <span>组件库</span>  <el-dialog
              v-model="saveDialogVisible"
              title="保存看板"
              width="420px"
            >
              <el-form label-width="80px">
                <el-form-item label="看板名称">
                  <el-input
                    v-model="saveName"
                    placeholder="请输入看板名称"
                    maxlength="60"
                    @keyup.enter="confirmSave"
                  />
                </el-form-item>
                <el-form-item label="公开访问">
                  <el-switch
                    v-model="screenPublic"
                    active-text="公开投屏链接"
                  />
                </el-form-item>
              </el-form>
              <template #footer>
                <el-button @click="saveDialogVisible = false">
                  取消
                </el-button>
                <el-button
                  type="primary"
                  :loading="saving"
                  @click="confirmSave"
                >
                  保存
                </el-button>
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
        <div
          class="canvas"
          role="presentation"
          @drop="onDrop"
          @dragover.prevent
        >
          <div
            v-for="(block, i) in blocks"
            :key="block.id"
            class="canvas-block"
            :style="blockStyle(block)"
          >
            <div class="block-toolbar">
              <el-button-group size="small">
                <el-button @click="resize(i)">
                  ↔
                </el-button>
                <el-button
                  type="danger"
                  @click="removeBlock(i)"
                >
                  ×
                </el-button>
              </el-button-group>
            </div>
            <component
              :is="resolveChart(block.type)"
              :data="block.data"
            />
            <div class="block-title">
              {{ block.title }}
            </div>
          </div>
        </div>
      </el-col>
    </el-row>
    <!-- el-dialog 原先与根 div 平级，使本组件有两个根节点（fragment）。路由层的
       <Transition> 无法给 fragment 做动画，进页面必报 "Component inside <Transition>
       renders non-element root node that cannot be animated"（实测 17 条）。
       它带 append-to-body（teleport 到 body），所以挪进根 div 内视觉与行为完全不变。 -->
    <el-dialog
      v-model="saveDialogVisible"
      title="保存看板"
      width="420px"
      append-to-body
    >
      <el-form label-width="80px">
        <el-form-item label="看板名称">
          <el-input
            v-model="saveName"
            placeholder="请输入看板名称"
            maxlength="60"
            @keyup.enter="confirmSave"
          />
        </el-form-item>
        <el-form-item label="公开访问">
          <el-switch
            v-model="screenPublic"
            active-text="公开投屏链接"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="saveDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="confirmSave"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'
import { ref, reactive, markRaw, h, onMounted, onBeforeUnmount } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DataLine, PieChart, Document, Histogram, TrendCharts, LocationInformation
} from '@element-plus/icons-vue'
import * as echarts from 'echarts'
import { safeInit, safeDispose } from '@/utils/echarts'
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
  ElMessage.success(i18n.global.t('dashboardScreenPage.componentAdded'))
}

// 图表组件必须用渲染函数定义：本项目打包的是 vue 运行时版（不含模板编译器），
// 且 defineAsyncComponent 的 loader 必须返回 Promise，原先返回普通对象会抛
// "loader(...).catch is not a function" 导致整个看板白屏。
function resolveChart(type) {
  return markRaw({
    line: createChart('line'),
    bar: createChart('bar'),
    pie: createChart('pie'),
    kpi: createKpi(),
    funnel: createChart('funnel'),
    map: createChart('map')
  }[type] || markRaw({ render: () => h('div', { class: 'chart-area' }, '未知') }))
}

function createChart(type) {
  // echarts 5 不内置任何行政区划 GeoJSON，直接 series.type='map' 会抛
  // "Map not exists / Cannot read properties of undefined (reading 'regions')"。
  // 地图块先给出可读占位提示，待接入行政区划数据后再启用真实地图。
  if (type === 'map') {
    return markRaw({
      props: { data: { type: Object, default: () => ({}) } },
      setup() {
        return () => h('div', { class: 'chart-area map-placeholder' }, '地图组件需接入行政区划数据后显示')
      }
    })
  }
  return markRaw({
    props: { data: { type: Object, default: () => ({}) } },
    setup() {
      const el = ref(null)
      let chart = null
      onMounted(() => {
        if (!el.value) return
        chart = safeInit(el.value)
        if (!chart) return
        chart.setOption({
          xAxis: { type: 'category', data: ['A', 'B', 'C', 'D'] },
          yAxis: { type: 'value' },
          series: [{ type, data: [10, 20, 30, 40] }]
        })
      })
      // 组件销毁时释放 echarts 实例，避免看板反复增删组件后内存与事件监听堆积
      onBeforeUnmount(() => {
        if (chart) safeDispose(chart)
        chart = null
      })
      return () => h('div', { ref: el, class: 'chart-area' })
    }
  })
}

function createKpi() {
  return markRaw({
    props: { data: { type: Object, default: () => ({}) } },
    setup(props) {
      return () => h('div', { class: 'kpi' }, props.data?.value ?? 0)
    }
  })
}

function blockStyle(b) {
  return {
    width: `${(b.width / 12) * 100}%`,
    height: `${b.height}px`
  }
}

function addBlock(type) {
  // 「+ 添加组件」与组件库点击都走这里；空实现会让按钮与调色板全是死点击
  // （只有拖拽 onDrop 能加，鼠标点按无任何反馈）。默认落一个折线块。
  const wanted = type || 'line'
  const meta = blockTypes.find((b) => b.type === wanted) || blockTypes[0]
  if (!meta) return
  blocks.value.push({
    id: _id++,
    type: meta.type,
    title: meta.label,
    width: 6,
    height: 240,
    data: { value: Math.floor(Math.random() * 1000) }
  })
  ElMessage.success(i18n.global.t('dashboardScreenPage.componentAdded'))
}
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
.map-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  background: var(--el-fill-color-lighter);
  border-radius: 4px;
}
.block-title { text-align: center; font-size: 12px; color: #94A3B8; margin-top: 4px; }
.kpi { font-size: 32px; font-weight: 700; text-align: center; color: #4F46E5; }
</style>
