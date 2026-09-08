<template>
  <div class="workflow-editor-page">
    <el-page-header :content="`工作流编辑 - ${workflowId}`" @back="$router.back()" />

    <el-card class="editor-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('流程定义') }}</span>
          <div class="header-actions">
            <el-select
              v-model="currentVersion"
              style="width: 140px"
              :placeholder="$t('选择版本')"
              @change="onVersionChange"
            >
              <el-option
                v-for="v in versions"
                :key="v.id"
                :label="`${v.version}v - ${statusText(v.status)}`"
                :value="v"
              />
            </el-select>
            <el-button
              v-if="currentVersion && currentVersion.status === 'draft'"
              type="success"
              @click="publishCurrent"
              >{{ $t('发布此版本') }}</el-button
            >
            <el-button type="primary" @click="save">{{ $t('保存') }}</el-button>
            <el-button @click="saveAsNew">{{ $t('保存为新版本') }}</el-button>
          </div>
        </div>
      </template>

      <div class="editor-body">
        
        <div class="palette">
          <h4>{{ $t('节点类型') }}</h4>
          <div
            v-for="type in nodeTypes"
            :key="type.value"
            class="palette-item"
            :class="{ active: selectedNodeType === type.value }"
            @click="selectedNodeType = type.value"
          >
            <el-icon><component :is="type.icon" /></el-icon>
            <span>{{ type.label }}</span>
          </div>

          <el-divider />
          <h4>{{ $t('添加节点') }}</h4>
          <el-button type="primary" @click="addNode">{{ $t('添加到画布') }}</el-button>

          <el-divider />

          <h4>{{ $t('画布操作') }}</h4>
          <el-button @click="autoLayout" :disabled="nodes.length === 0">{{ $t('自动布局') }}</el-button>
          <el-button type="danger" plain @click="clearAll">{{ $t('清空画布') }}</el-button>

          <el-divider />
          <div class="palette-tip">
            <p>{{ $t('提示：拖拽节点两侧的小圆点即可创建连线；双击连线可编辑标签。') }}</p>
          </div>
        </div>

        
        <div class="canvas">
          <div class="canvas-header">
            <span>{{ $t('画布') }} · 节点 {{ nodes.length }} · 连线 {{ edges.length }}</span>
          </div>
          <div
            ref="canvasBodyRef"
            class="canvas-body"
            @pointermove="onPointerMove"
            @pointerup="onPointerUp"
          >
            
            <svg class="edge-svg">
              <defs>
                <marker
                  id="wf-arrow"
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="8"
                  markerHeight="8"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="#909399" />
                </marker>
              </defs>
              <path
                v-for="(edge, idx) in edgePaths"
                :key="'ep-' + idx"
                :d="edge.path"
                class="edge-path"
                marker-end="url(#wf-arrow)"
                @click="removeEdge(idx)"
                @dblclick.stop="editEdgeLabel(idx)"
              />
              <text
                v-for="(edge, idx) in edgePaths"
                :key="'el-' + idx"
                :x="edge.labelX"
                :y="edge.labelY"
                class="edge-label"
              >{{ edge.label || '' }}</text>
              
              <path
                v-if="linking.active"
                :d="tempEdgePath"
                class="edge-path temp-edge"
              />
            </svg>

            <div
              v-for="(node, idx) in nodes"
              :key="node.id"
              class="node-box"
              :style="{ left: node.x + 'px', top: node.y + 'px' }"
              :class="[`node-${node.type}`, { selected: idx === selectedNodeIdx, 'link-target': linking.active && linking.sourceIdx !== idx && !isLinkedFromSource(idx) }]"
              @click="selectedNodeIdx = idx"
              @pointerdown.stop="onPointerDown(idx, $event)"
              @pointerenter="onNodeEnter(idx)"
              @pointerleave="onNodeLeave(idx)"
            >
              <div class="node-type-tag">{{ nodeTypeLabel(node.type) }}</div>
              <div class="node-label">{{ node.label || `节点 ${idx + 1}` }}</div>
              <el-button
                class="node-delete"
                type="danger"
                size="small"
                circle
                @click.stop="removeNode(idx)"
              >
                <el-icon><Delete /></el-icon>
              </el-button>
              
              <span
                class="node-handle handle-out"
                @pointerdown.stop.prevent="onLinkStart(idx, $event)"
                :title="$t('拖拽创建连线')"
              ></span>
            </div>

            <el-empty v-if="nodes.length === 0" :description="$t('从左侧选择节点类型，点击添加到画布')" />
          </div>
        </div>

        
        <div class="properties">
          <h4>{{ $t('节点属性') }}</h4>
          <template v-if="selectedNodeIdx >= 0 && nodes[selectedNodeIdx]">
            <el-form :model="nodes[selectedNodeIdx]" label-width="90px" size="small">
              <el-form-item :label="$t('标签')">
                <el-input v-model="nodes[selectedNodeIdx].label" />
              </el-form-item>
              <el-form-item :label="$t('节点 ID')">
                <el-input v-model="nodes[selectedNodeIdx].id" disabled />
              </el-form-item>
              <el-form-item :label="$t('类型')">
                <el-tag>{{ nodeTypeLabel(nodes[selectedNodeIdx].type) }}</el-tag>
              </el-form-item>
              <el-form-item :label="$t('X 坐标')">
                <el-input-number v-model="nodes[selectedNodeIdx].x" :min="0" :max="800" />
              </el-form-item>
              <el-form-item :label="$t('Y 坐标')">
                <el-input-number v-model="nodes[selectedNodeIdx].y" :min="0" :max="500" />
              </el-form-item>
              <el-form-item :label="$t('配置')">
                <el-input
                  v-model="nodes[selectedNodeIdx].config_text"
                  type="textarea"
                  :rows="4"
                  placeholder='{"action": "send_email"}'
                />
              </el-form-item>
            </el-form>
          </template>
          <el-empty v-else :description="$t('请选择一个节点')" />

          <el-divider />
          <h4>{{ $t('JSON 预览') }}</h4>
          <el-input
            type="textarea"
            :model-value="definitionJson"
            :rows="8"
            readonly
            placeholder="保存后显示 JSON"
          />
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Lightning, Operation, Share, ForkSpoon } from '@element-plus/icons-vue'
import { workflowOrchestratorApi } from '@/api/workflowOrchestrator.js'

const route = useRoute()
const workflowId = computed(() => route.params.workflow_id || '')

const nodeTypes = [
  { value: 'trigger', label: '触发器', icon: Lightning },
  { value: 'action', label: '动作', icon: Operation },
  { value: 'condition', label: '条件', icon: ForkSpoon },
  { value: 'subflow', label: '子流程', icon: Share }
]

const nodeTypeLabel = (v) => nodeTypes.find(t => t.value === v)?.label || v
const selectedNodeType = ref('trigger')
const selectedNodeIdx = ref(-1)

const versions = ref([])
const currentVersion = ref(null)
const definition = reactive({ nodes: [], edges: [] })

const NODE_W = 140;
const NODE_H = 56

const canvasBodyRef = ref(null)

const dragging = reactive({
  active: false,
  idx: -1,
  offsetX: 0,
  offsetY: 0,
  pointerId: -1
});

const linking = reactive({
  active: false,
  sourceIdx: -1,
  sourceId: '',
  pointerId: -1,
  curX: 0,
  curY: 0,
  hoverTargetIdx: -1
});

const nodes = computed({
  get: () => definition.nodes,
  set: (v) => { definition.nodes = v }
})
const edges = computed({
  get: () => definition.edges,
  set: (v) => { definition.edges = v }
})

const definitionJson = computed(() => JSON.stringify(definition, null, 2))

const edgePaths = computed(() => {
  return definition.edges.map((edge) => {
    const sNode = definition.nodes.find((n) => n.id === edge.source)
    const tNode = definition.nodes.find((n) => n.id === edge.target)
    if (!sNode || !tNode) {
      return { path: '', labelX: 0, labelY: 0, label: edge.label || '' }
    }
    const sx = (sNode.x || 0) + NODE_W
    const sy = (sNode.y || 0) + NODE_H / 2
    const tx = (tNode.x || 0)
    const ty = (tNode.y || 0) + NODE_H / 2
    const dx = Math.max(Math.abs(tx - sx) / 2, 30)
    const c1x = sx + dx
    const c2x = tx - dx
    const path = `M ${sx} ${sy} C ${c1x} ${sy}, ${c2x} ${ty}, ${tx} ${ty}`
    const labelX = (sx + tx) / 2
    const labelY = (sy + ty) / 2 - 4
    return { path, labelX, labelY, label: edge.label || '' }
  })
});

const onPointerDown = (idx, ev) => {
  const node = definition.nodes[idx]
  if (!node) return
  dragging.active = true
  dragging.idx = idx
  dragging.pointerId = ev.pointerId
  const canvasRect = canvasBodyRef.value?.getBoundingClientRect()
  const canvasLeft = canvasRect ? canvasRect.left : 0
  const canvasTop = canvasRect ? canvasRect.top : 0
  dragging.offsetX = ev.clientX - canvasLeft - (node.x || 0);
  dragging.offsetY = ev.clientY - canvasTop - (node.y || 0)
  try { ev.target.setPointerCapture?.(ev.pointerId) } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
};

const onPointerMove = (ev) => {
  if (dragging.active && dragging.idx >= 0) {
    const node = definition.nodes[dragging.idx]
    if (!node) { dragging.active = false; dragging.idx = -1; return }
    const canvasRect = canvasBodyRef.value?.getBoundingClientRect()
    if (!canvasRect) return
    const localX = ev.clientX - canvasRect.left
    const localY = ev.clientY - canvasRect.top
    const x = Math.max(0, Math.min(localX - dragging.offsetX, canvasRect.width - NODE_W))
    const y = Math.max(0, Math.min(localY - dragging.offsetY, canvasRect.height - NODE_H))
    node.x = Math.round(x)
    node.y = Math.round(y)
    return
  }
  if (linking.active) {
    const canvasRect = canvasBodyRef.value?.getBoundingClientRect()
    if (!canvasRect) return
    linking.curX = ev.clientX - canvasRect.left
    linking.curY = ev.clientY - canvasRect.top
  }
}

const onPointerUp = (ev) => {
  if (dragging.active) {
    try { ev.target?.releasePointerCapture?.(dragging.pointerId) } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
    dragging.active = false
    dragging.idx = -1
  }
  if (linking.active) {
    try { ev.target?.releasePointerCapture?.(linking.pointerId) } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
    const targetIdx = linking.hoverTargetIdx;
    if (targetIdx >= 0 && targetIdx !== linking.sourceIdx) {
      const sourceId = linking.sourceId
      const targetNode = definition.nodes[targetIdx]
      const targetId = targetNode?.id
      if (sourceId && targetId && sourceId !== targetId) {
        const exists = definition.edges.some(e =>
          (e.source || e.from) === sourceId && (e.target || e.to) === targetId
        );
        if (exists) {
          ElMessage.warning('已存在相同连线')
        } else {
          definition.edges.push({ source: sourceId, target: targetId, label: '' })
          ElMessage.success('已添加连线')
        }
      }
    }
    linking.active = false
    linking.sourceIdx = -1
    linking.sourceId = ''
    linking.hoverTargetIdx = -1
    linking.pointerId = -1
  }
}

onBeforeUnmount(() => {
  dragging.active = false
  linking.active = false
})

const onLinkStart = (idx, ev) => {
  const node = definition.nodes[idx]
  if (!node) return
  linking.active = true
  linking.sourceIdx = idx
  linking.sourceId = node.id
  linking.pointerId = ev.pointerId
  const canvasRect = canvasBodyRef.value?.getBoundingClientRect()
  linking.curX = ev.clientX - (canvasRect?.left || 0)
  linking.curY = ev.clientY - (canvasRect?.top || 0)
  try { ev.target.setPointerCapture?.(ev.pointerId) } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
};

const onNodeEnter = (idx) => {
  if (linking.active) linking.hoverTargetIdx = idx
}
const onNodeLeave = (idx) => {
  if (linking.active && linking.hoverTargetIdx === idx) linking.hoverTargetIdx = -1
}

const isLinkedFromSource = (idx) => {
  if (!linking.active) return false
  const targetId = definition.nodes[idx]?.id
  const sourceId = linking.sourceId
  return definition.edges.some(e =>
    (e.source || e.from) === sourceId && (e.target || e.to) === targetId
  )
}

const tempEdgePath = computed(() => {
  if (!linking.active) return ''
  const sNode = definition.nodes[linking.sourceIdx]
  if (!sNode) return ''
  const sx = (sNode.x || 0) + NODE_W
  const sy = (sNode.y || 0) + NODE_H / 2
  const tx = linking.curX
  const ty = linking.curY
  const dx = Math.max(Math.abs(tx - sx) / 2, 30)
  const c1x = sx + dx
  const c2x = tx - dx
  return `M ${sx} ${sy} C ${c1x} ${sy}, ${c2x} ${ty}, ${tx} ${ty}`
});

const editEdgeLabel = async (idx) => {
  const edge = definition.edges[idx]
  if (!edge) return
  try {
    const { value } = await ElMessageBox.prompt('请输入连线标签（可选，用于条件分支）', '连线标签', {
      inputPlaceholder: '留空表示默认分支',
      inputValue: edge.label || '',
      inputValidator: () => true
    })
    edge.label = (value || '').trim()
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

let idCounter = 1;
const addNode = () => {
  const type = selectedNodeType.value
  const newNode = {
    id: `node_${Date.now()}_${idCounter++}`,
    type,
    label: nodeTypeLabel(type),
    x: 100 + nodes.value.length * 40,
    y: 100 + nodes.value.length * 30,
    config_text: ''
  }
  definition.nodes.push(newNode)
  selectedNodeIdx.value = definition.nodes.length - 1
}

const removeNode = (idx) => {
  const removedId = definition.nodes[idx]?.id
  definition.nodes.splice(idx, 1)
  if (removedId) {
    definition.edges = definition.edges.filter(e => (e.source || e.from) !== removedId && (e.target || e.to) !== removedId)
  }
  if (selectedNodeIdx.value >= definition.nodes.length) selectedNodeIdx.value = definition.nodes.length - 1
}

const removeEdge = (idx) => {
  definition.edges.splice(idx, 1)
}

const clearAll = async () => {
  try {
    await ElMessageBox.confirm('确认清空所有节点？', '确认', { type: 'warning' })
    definition.nodes = []
    definition.edges = []
    selectedNodeIdx.value = -1
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const autoLayout = () => {
  const nodeCount = definition.nodes.length
  if (nodeCount === 0) return
  const inDegree = {};
  const adj = {}
  definition.nodes.forEach(n => { inDegree[n.id] = 0; adj[n.id] = [] })
  definition.edges.forEach(e => {
    const s = e.source || e.from
    const t = e.target || e.to
    if (s && t && inDegree[t] !== undefined) {
      inDegree[t] += 1
      if (adj[s]) adj[s].push(t)
    }
  })
  const layers = {};
  let frontier = definition.nodes.filter(n => inDegree[n.id] === 0).map(n => n.id)
  frontier.forEach(id => { layers[id] = 0 })
  let iter = 0;
  while (frontier.length > 0 && iter < nodeCount * 2) {
    const next = []
    frontier.forEach(s => {
      (adj[s] || []).forEach(t => {
        const candidate = (layers[s] || 0) + 1
        if (layers[t] === undefined || layers[t] < candidate) {
          layers[t] = candidate
        }
        next.push(t)
      })
    })
    frontier = [...new Set(next)]
    iter++
  }
  definition.nodes.forEach(n => {
    if (layers[n.id] === undefined) layers[n.id] = 0
  });
  const byLayer = {};
  definition.nodes.forEach(n => {
    const d = layers[n.id]
    if (!byLayer[d]) byLayer[d] = []
    byLayer[d].push(n)
  })
  const COL_W = 200
  const ROW_H = 90
  const START_X = 40
  const START_Y = 40
  Object.keys(byLayer).sort((a, b) => a - b).forEach((d) => {
    const group = byLayer[d]
    group.forEach((n, i) => {
      n.x = START_X + Number(d) * COL_W
      n.y = START_Y + i * ROW_H
    })
  })
};

const loadVersions = async () => {
  try {
    const result = await workflowOrchestratorApi.listVersions(workflowId.value)
    versions.value = Array.isArray(result)
      ? result
      : (Array.isArray(result?.list) ? result.list
        : (Array.isArray(result?.data) ? result.data : []));
    if (versions.value.length > 0) {
      const draft = versions.value.find(v => v.status === 'draft');
      currentVersion.value = draft || versions.value[versions.value.length - 1]
      loadDefinition()
    }
  } catch (e) {
    ElMessage.error('加载版本失败: ' + (e?.message || ''))
  }
};

const loadDefinition = () => {
  if (!currentVersion.value) return
  const def = currentVersion.value.definition || {}
  const rawNodes = Array.isArray(def.nodes) ? def.nodes : []
  const rawEdges = Array.isArray(def.edges) ? def.edges : []
  definition.nodes = rawNodes.map((n) => {
    const cfg = n.config || {}
    let configText
    try {
      configText = Object.keys(cfg).length ? JSON.stringify(cfg) : (n.config_text || '')
    } catch (_) {
      // 序列化失败时回退到原始 config_text
      configText = n.config_text || ''
    }
    return {
      ...n,
      x: typeof n.x === 'number' ? n.x : 100,
      y: typeof n.y === 'number' ? n.y : 100,
      config_text: configText
    }
  });
  definition.edges = rawEdges.map((e) => ({
    source: e.source || e.from || '',
    target: e.target || e.to || '',
    label: e.label || ''
  }))
}

const onVersionChange = () => {
  loadDefinition()
}

const statusText = (s) => ({ draft: '草稿', published: '已发布', archived: '已归档' }[s] || s)

const validateDefinition = () => {
  const ids = new Set(definition.nodes.map(n => n.id))
  if (ids.size !== definition.nodes.length) {
    return '存在重复的节点 ID'
  }
  const seen = new Set();
  for (const e of definition.edges) {
    const s = e.source || e.from
    const t = e.target || e.to
    if (!s || !t) return '连线缺少 source/target'
    if (!ids.has(s) || !ids.has(t)) return `连线引用了不存在的节点：${s} -> ${t}`
    if (s === t) return `节点不能连接到自身：${s}`
    const key = `${s}|${t}|${e.label || ''}`
    if (seen.has(key)) return `存在重复连线：${s} -> ${t}`
    seen.add(key)
  }
  return ''
};

const save = async () => {
  if (!currentVersion.value) {
    ElMessage.warning('没有版本可保存')
    return
  }
  const err = validateDefinition()
  if (err) {
    ElMessage.error(err)
    return
  }
  const payloadNodes = definition.nodes.map((n) => {
    let config = {}
    if (n.config_text) {
      try {
        config = JSON.parse(n.config_text)
      } catch (e) {
        ElMessage.error(`节点 ${n.id} 的配置不是合法 JSON: ${e.message}`)
        throw e
      }
    }
    return {
      id: n.id,
      type: n.type,
      name: n.label || n.name || n.id,
      label: n.label || '',
      x: n.x || 0,
      y: n.y || 0,
      config
    }
  });
  const payloadEdges = definition.edges.map((e) => ({
    source: e.source || e.from || '',
    target: e.target || e.to || '',
    label: e.label || ''
  }))
  const payloadDefinition = { nodes: payloadNodes, edges: payloadEdges }
  try {
    await workflowOrchestratorApi.updateVersion(currentVersion.value.id, {
      name: currentVersion.value.name,
      description: currentVersion.value.description,
      definition: payloadDefinition
    })
    ElMessage.success('保存成功')
    loadVersions()
  } catch (e) {
    ElMessage.error('保存失败: ' + (e?.message || ''))
  }
}

const saveAsNew = async () => {
  const err = validateDefinition()
  if (err) {
    ElMessage.error(err)
    return
  }
  try {
    const result = await workflowOrchestratorApi.createVersion({
      workflow_id: workflowId.value,
      name: `${workflowId.value}_v${versions.value.length + 1}`,
      description: currentVersion.value?.description || '',
      definition
    })
    ElMessage.success('新版本创建成功')
    loadVersions()
  } catch (e) {
    ElMessage.error('创建失败: ' + (e?.message || ''))
  }
}

const publishCurrent = async () => {
  if (!currentVersion.value) return
  try {
    await ElMessageBox.confirm('确认发布此版本？', '发布确认', { type: 'warning' })
    await workflowOrchestratorApi.publishVersion(currentVersion.value.id)
    ElMessage.success('发布成功')
    loadVersions()
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

onMounted(loadVersions)
</script>

<style scoped>
.workflow-editor-page {
  padding: 16px;
}
.editor-card {
  margin-top: 16px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-actions {
  display: flex;
  gap: 8px;
}
.editor-body {
  display: grid;
  grid-template-columns: 200px 1fr 280px;
  gap: 16px;
  min-height: 500px;
}
.palette {
  border-right: 1px solid var(--el-border-color-light);
  padding-right: 16px;
}
.palette h4, .properties h4 {
  margin: 8px 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.palette-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 4px;
  cursor: pointer;
  margin-bottom: 6px;
  background: var(--el-fill-color-light);
  transition: background 0.2s;
}
.palette-item:hover {
  background: var(--el-fill-color);
}
.palette-item.active {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}
.canvas {
  border: 1px dashed var(--el-border-color);
  border-radius: 8px;
  overflow: hidden;
  position: relative;
}
.canvas-header {
  background: var(--el-fill-color-light);
  padding: 6px 12px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  border-bottom: 1px solid var(--el-border-color-light);
}
.canvas-body {
  position: relative;
  min-height: 400px;
  padding: 20px;
}
.edge-svg {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
  z-index: 0;
}
.edge-path {
  fill: none;
  stroke: #909399;
  stroke-width: 2;
  pointer-events: stroke;
  cursor: pointer;
}
.edge-path:hover {
  stroke: #f56c6c;
}
.edge-label {
  font-size: 11px;
  fill: #606266;
  pointer-events: none;
}
.node-box {
  position: absolute;
  width: 140px;
  padding: 10px 12px;
  border-radius: 8px;
  border: 2px solid var(--el-border-color);
  background: white;
  cursor: move;
  transition: border-color 0.2s;
  z-index: 1;
}
.node-box.selected {
  border-color: var(--el-color-primary);
  box-shadow: 0 2px 8px rgba(64, 158, 255, 0.2);
}
.node-box.node-trigger {
  border-color: #E6A23C;
  background: #fdf6ec;
}
.node-box.node-action {
  border-color: #409EFF;
  background: #ecf5ff;
}
.node-box.node-condition {
  border-color: #67C23A;
  background: #f0f9eb;
}
.node-box.node-subflow {
  border-color: #909399;
  background: #f4f4f5;
}
.node-type-tag {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}
.node-label {
  font-size: 13px;
  font-weight: 500;
  word-break: break-all;
}
.node-delete {
  position: absolute;
  top: -10px;
  right: -10px;
  display: none;
}
.node-box:hover .node-delete {
  display: block;
}
.edge-line {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  display: flex;
  align-items: center;
  gap: 4px;
  z-index: 0;
}
.properties {
  border-left: 1px solid var(--el-border-color-light);
  padding-left: 16px;
}
.palette-tip {
  margin-top: 12px;
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.palette-tip p { margin: 4px 0; }
.node-handle {
  position: absolute;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--el-color-primary);
  border: 2px solid white;
  box-shadow: 0 1px 3px rgba(0,0,0,0.15);
  cursor: crosshair;
  transition: transform 0.1s, background 0.1s;
  z-index: 2;
}
.handle-out {
  right: -6px;
  top: 50%;
  transform: translateY(-50%);
}
.node-handle:hover {
  transform: translateY(-50%) scale(1.3);
  background: #f56c6c;
}
.node-box.link-target {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 2px var(--el-color-primary-light-9);
}
.edge-path.temp-edge {
  stroke: var(--el-color-primary);
  stroke-dasharray: 6 4;
  stroke-width: 2;
  animation: dash 0.6s linear infinite;
}
@keyframes dash {
  to { stroke-dashoffset: -10; }
}
</style>
