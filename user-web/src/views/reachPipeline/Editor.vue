<template>
  <VueFlowCanvas
    :title="`Pipeline - ${pipelineId || '新建'}`"
    :initial-nodes="initialNodes"
    :initial-edges="initialEdges"
    :node-templates="nodeTemplates"
    @save="onSave"
    @validate="onValidate"
  />
</template>

<script setup>
import { onMounted, ref, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import VueFlowCanvas from '@/components/VueFlowCanvas.vue'
import { http } from '@/utils/request'

const route = useRoute()
const router = useRouter()
const pipelineId = computed(() => route.params.id)
const initialNodes = ref([])
const initialEdges = ref([])

const saving = ref(false)

const nodeTemplates = [
  { type: 'trigger', label: '触发器', icon: 'Trigger', defaultData: { event: 'customer.created' } },
  { type: 'channel', label: '渠道', icon: 'Promotion', defaultData: { channel: 'email' } },
  { type: 'delay', label: '延时', icon: 'Timer', defaultData: { seconds: 3600 } },
  { type: 'condition', label: '条件分支', icon: 'Branch', defaultData: { field: 'lifetime_value', op: '>', value: 1000 } },
  { type: 'abtest', label: 'A/B 实验', icon: 'DataAnalysis', defaultData: { experiment_id: null } },
  { type: 'ai', label: 'AI 生成', icon: 'MagicStick', defaultData: { prompt: '为 {{customer.name}} 生成个性化推送', agent_id: null } },
  { type: 'end', label: '结束', icon: 'CircleClose' }
]

async function load() {
  if (!pipelineId.value) return
  const p = await http.get(`/api/reach/pipelines/${pipelineId.value}`)
  initialNodes.value = p.nodes || []
  initialEdges.value = p.edges || []
}

async function onSave({ nodes, edges }) {
  // 无 id 时无可更新的 Pipeline（新建走 List 页 dialog），防 PUT 到 /undefined
  if (!pipelineId.value) {
    ElMessage.warning('请在 Pipeline 列表创建后再进入可视化编辑器编辑')
    return
  }
  if (saving.value) return
  saving.value = true
  try {
    await http.put(`/api/reach/pipelines/${pipelineId.value}`, { nodes, edges })
    ElMessage.success('已保存')
  } finally {
    saving.value = false
  }
}

function onValidate({ hasCycle }) {
  if (hasCycle) {
    ElMessage.error('检测到环状引用，请修复')
  } else {
    ElMessage.success('校验通过')
  }
}

onMounted(load)
</script>
