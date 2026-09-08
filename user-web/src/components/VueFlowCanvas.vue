<template>
  <div class="vue-flow-page">
    <div class="flow-header">
      <h2>{{ title }}</h2>
      <div class="flow-actions">
        <el-button @click="validateGraph">{{ $t('校验') }}</el-button>
        <el-button type="primary" @click="onSave">{{ $t('保存') }}</el-button>
      </div>
    </div>
    <VueFlow
      v-model:nodes="nodes"
      v-model:edges="edges"
      :default-viewport="{ x: 0, y: 0, zoom: 1 }"
      :min-zoom="0.2"
      :max-zoom="4"
      class="flow-canvas"
      @nodes-change="onNodesChange"
    >
      <Background pattern-color="#E2E8F0" :gap="16" />
      <Controls />
      <MiniMap />
    </VueFlow>

    <aside class="flow-node-panel">
      <h4>{{ $t('节点库') }}</h4>
      <div
        v-for="node in nodeTemplates"
        :key="node.type"
        class="node-template"
        :draggable="true"
        @dragstart="onDragStart($event, node)"
      >
        <el-icon><component :is="node.icon" /></el-icon>
        <span>{{ node.label }}</span>
      </div>
    </aside>
  </div>
</template>

<script setup>
import { ref, shallowRef } from 'vue';
import { VueFlow, Position } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import '@vue-flow/minimap/dist/style.css'

const props = defineProps({
  title: { type: String, default: '画布' },
  initialNodes: { type: Array, default: () => [] },
  initialEdges: { type: Array, default: () => [] },
  nodeTemplates: { type: Array, default: () => [] }
})

const emit = defineEmits(['save', 'validate'])

const nodes = ref(props.initialNodes)
const edges = ref(props.initialEdges)
const nextNodeId = shallowRef(1)

function onNodesChange(changes) {
  changes.forEach((c) => {
    if (c.type === 'position' && c.dragging) { /* no-op: 显式忽略该分支 */ }
  });
}

function onDragStart(event, template) {
  event.dataTransfer.setData('application/vueflow', JSON.stringify(template))
  event.dataTransfer.effectAllowed = 'move'
}

function onDragOver(event) {
  event.preventDefault()
  event.dataTransfer.dropEffect = 'move'
}

function onDrop(event) {
  const raw = event.dataTransfer.getData('application/vueflow')
  if (!raw) return
  const template = JSON.parse(raw)
  const id = `node_${nextNodeId.value++}`
  const rect = event.currentTarget.getBoundingClientRect()
  const position = {
    x: event.clientX - rect.left - 80,
    y: event.clientY - rect.top - 20
  }
  nodes.value.push({
    id,
    type: 'default',
    position,
    data: { label: template.label, type: template.type, ...template.defaultData },
    sourcePosition: Position.Right,
    targetPosition: Position.Left
  })
}

function validateGraph() {
  const adj = new Map();
  edges.value.forEach((e) => {
    if (!adj.has(e.source)) adj.set(e.source, [])
    adj.get(e.source).push(e.target)
  })
  const visited = new Set()
  const stack = new Set()
  let hasCycle = false
  function dfs(node) {
    visited.add(node)
    stack.add(node)
    for (const next of adj.get(node) || []) {
      if (!visited.has(next) && dfs(next)) return true
      if (stack.has(next)) return true
    }
    stack.delete(node)
    return false
  }
  for (const n of nodes.value) {
    if (!visited.has(n.id) && dfs(n.id)) {
      hasCycle = true
      break
    }
  }
  emit('validate', { hasCycle, nodeCount: nodes.value.length, edgeCount: edges.value.length })
}

function onSave() {
  emit('save', { nodes: nodes.value, edges: edges.value })
}
</script>

<style scoped>
.vue-flow-page {
  display: flex;
  height: calc(100vh - 120px);
  gap: 16px;
}
.flow-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: #fff;
  border-radius: 8px;
  margin-bottom: 12px;
}
.flow-header h2 { margin: 0; font-size: 18px; }
.flow-canvas {
  flex: 1;
  height: 100%;
  background: #F8FAFC;
  border-radius: 8px;
}
.flow-node-panel {
  width: 200px;
  background: #fff;
  border-radius: 8px;
  padding: 16px;
  overflow-y: auto;
}
.flow-node-panel h4 { margin: 0 0 12px; font-size: 14px; color: #475569; }
.node-template {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  margin-bottom: 8px;
  background: #F1F5F9;
  border-radius: 6px;
  cursor: grab;
  font-size: 13px;
}
.node-template:hover { background: #E2E8F0; }
</style>
