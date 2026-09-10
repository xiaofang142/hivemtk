<template>
  <div class="geo-page">

    <div class="p-4" style="display:flex; gap:16px; min-height:calc(100vh - 180px)">
    
    <el-card style="width:260px; flex-shrink:0">
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-bold">Workflow 模板</span>
          <el-button size="small" type="primary" @click="onNewWorkflow">新建</el-button>
        </div>
      </template>
      <div class="space-y-2">
        <div
          v-for="w in workflows"
          :key="w.id || w.name"
          class="workflow-item"
          :class="{ active: currentWorkflow && (currentWorkflow.id === w.id || currentWorkflow.name === w.name) }"
          @click="selectWorkflow(w)"
        >
          <div class="font-medium">{{ w.name }}</div>
          <div class="text-xs text-gray-500">{{ w.description || `${(w.steps || []).length} 个步骤` }}</div>
        </div>
      </div>
    </el-card>

    
    <div style="flex:1; display:flex; flex-direction:column; gap:16px">
      
      <el-card style="flex:1">
        <template #header>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-3">
              <span class="font-bold">工作流：{{ currentWorkflow?.name || '未选择' }}</span>
              <el-input v-model="wfName" placeholder="工作流名称" size="small" style="width:200px" v-if="currentWorkflow" />
            </div>
            <div class="flex items-center gap-2">
              <el-button size="small" @click="onSaveWorkflow" :disabled="!currentWorkflow">保存</el-button>
              <el-button size="small" type="danger" plain @click="onDeleteWorkflow" :disabled="!currentWorkflow?.id">删除</el-button>
              <el-button size="small" type="primary" @click="onRunNow" :disabled="!currentWorkflow" :loading="running">Run Now</el-button>
            </div>
          </div>
        </template>

        
        <div class="steps-container">
          <div
            v-for="(step, idx) in steps"
            :key="idx"
            class="step-block"
            :class="'step-' + step.type"
          >
            <div class="step-index">{{ idx + 1 }}</div>
            <div class="step-body">
              <div class="step-title">
                <el-tag v-if="stepMeta(step.type, 'kind') === 'control'" size="small" type="info" effect="plain" style="margin-right:6px">{{ stepMeta(step.type, 'label') }}</el-tag>
                <el-tag v-else size="small" :type="stepMeta(step.type, 'color') || ''" effect="light" style="margin-right:6px">{{ stepMeta(step.type, 'label') }}</el-tag>
                <el-input v-model="step.name" size="small" placeholder="步骤名称" style="width:200px" />
              </div>
              <div class="step-config">
                
                <template v-if="step.type === 'trigger'">
                  <el-tag size="small" type="info">工作流入口节点，接收触发事件</el-tag>
                </template>
                <template v-else-if="step.type === 'end'">
                  <el-tag size="small" type="info">工作流结束节点，输出最终结果</el-tag>
                </template>
                <template v-else-if="step.type === 'decision'">
                  <div class="flex gap-2 items-center">
                    <el-input v-model="step.condition" size="small" placeholder="条件表达式，如 意向识别==high" style="flex:1" />
                    <el-input v-model="step.jump_to" size="small" placeholder="跳转至步骤名" style="width:160px" />
                  </div>
                </template>
                <template v-else-if="step.type === 'action'">
                  <el-tag size="small" type="info">通用动作节点 — 如需实际执行，请改为具体执行器类型</el-tag>
                </template>

                
                <template v-else-if="step.type === 'content_generate'">
                  <div class="flex gap-2 flex-wrap">
                    <el-input v-model="step.topic" size="small" placeholder="文章主题" style="flex:1;min-width:140px" />
                    <el-input v-model="step.keyword" size="small" placeholder="目标关键词" style="width:160px" />
                    <el-input v-model="step.brand" size="small" placeholder="品牌名" style="width:140px" />
                    <el-input v-model="step.platform" size="small" placeholder="发布平台" style="width:140px" />
                  </div>
                  <el-input v-model="step.advantages" size="small" placeholder="核心卖点，逗号分隔" class="mt-2" />
                </template>
                <template v-else-if="step.type === 'content_score'">
                  <div class="flex gap-2 items-center">
                    <span class="text-xs text-gray-500">最低分数：</span>
                    <el-input-number v-model="step.min_score" :min="0" :max="100" size="small" style="width:100px" />
                    <el-input v-model="step.brand" size="small" placeholder="品牌名" style="width:160px" />
                  </div>
                </template>
                <template v-else-if="step.type === 'eeat_enhance'">
                  <el-input v-model="step.brand" size="small" placeholder="品牌名" style="width:200px" />
                  <span class="text-xs text-gray-500 ml-2">自动从上一步结果提取内容进行 EEAT 增强</span>
                </template>
                <template v-else-if="step.type === 'fact_density_enhance'">
                  <div class="flex gap-2 items-center">
                    <span class="text-xs text-gray-500">目标密度：</span>
                    <el-slider v-model="step.target_density" :min="0.1" :max="1.0" :step="0.05" style="flex:1" />
                  </div>
                </template>
                <template v-else-if="step.type === 'verify'">
                  <div class="flex gap-2 flex-wrap">
                    <el-input v-model="step.brand" size="small" placeholder="要验证的品牌" style="width:180px" />
                  </div>
                  <div class="text-xs text-gray-500 mt-1">使用多轮 LLM 查询验证品牌在搜索结果中的提及率</div>
                </template>
                <template v-else-if="step.type === 'query_probe'">
                  <div class="flex gap-2 flex-wrap">
                    <el-input v-model="step.keyword" size="small" placeholder="探针关键词" style="flex:1;min-width:160px" />
                    <el-select v-model="step.engine" size="small" placeholder="引擎" style="width:140px">
                      <el-option label="Qwen" value="qwen" />
                      <el-option label="DeepSeek" value="deepseek" />
                      <el-option label="SenseNova" value="sensenova" />
                      <el-option label="Doubao" value="doubao" />
                    </el-select>
                  </div>
                </template>
                <template v-else-if="step.type === 'source_attribution'">
                  <el-tag size="small" type="info">自动分析上一步内容的信源来源并标注</el-tag>
                </template>
                <template v-else-if="step.type === 'content_gap_fill'">
                  <el-input v-model="step.keyword" size="small" placeholder="补全目标关键词" style="width:240px" />
                </template>
                <template v-else-if="step.type === 'capture_lead'">
                  <el-tag size="small" type="info">自动从搜索/对话中捕获潜在客户线索</el-tag>
                </template>
                <template v-else>
                  <div class="text-xs text-gray-400">未知步骤类型：{{ step.type }}</div>
                </template>
              </div>
            </div>
            <div class="step-actions">
              <el-button size="small" circle @click="moveStep(idx, -1)" :disabled="idx === 0">↑</el-button>
              <el-button size="small" circle @click="moveStep(idx, 1)" :disabled="idx === steps.length - 1">↓</el-button>
              <el-button size="small" circle type="danger" plain @click="removeStep(idx)">×</el-button>
            </div>
          </div>

          <div class="mt-4">
            <div class="text-xs text-gray-500 mb-1">控制节点：</div>
            <div class="flex gap-2 flex-wrap mb-2">
              <template v-for="(t, k) in STEP_TYPES" :key="'c-'+k">
                <el-button v-if="t.kind==='control'" size="small" plain @click="addStep(k)">+ {{ t.label }}</el-button>
              </template>
            </div>
            <div class="text-xs text-gray-500 mb-1">执行器节点：</div>
            <div class="flex gap-2 flex-wrap">
              <template v-for="(t, k) in STEP_TYPES" :key="'e-'+k">
                <el-button v-if="t.kind==='executor'" size="small" plain @click="addStep(k)">+ {{ t.label }}</el-button>
              </template>
            </div>
          </div>
        </div>
      </el-card>

      
      <el-card>
        <template #header><span class="font-bold">执行监控</span></template>
        <el-table :data="executions" v-loading="execLoading" size="small">
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column prop="workflow_name" label="Workflow" width="160" />
          <el-table-column prop="status" label="状态" width="120">
            <template #default="{ row }">
              <el-tag :type="execStatusType(row.status)" size="small">{{ row.status }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="current_step" label="当前步骤" width="140" />
          <el-table-column prop="progress" label="进度" width="160">
            <template #default="{ row }">
              <el-progress :percentage="row.progress || 0" size="small" />
            </template>
          </el-table-column>
          <el-table-column prop="started_at" label="开始时间" width="180" />
          <el-table-column prop="finished_at" label="结束时间" width="180" />
        </el-table>
      </el-card>
    </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { listWorkflows, saveWorkflow, deleteWorkflow, runWorkflow, listWorkflowExecutions } from '@/api/geoEntity.js'

const STEP_TYPES = {
  trigger:  { label: '触发器',     kind: 'control', default: {} },
  action:   { label: '动作步骤',   kind: 'control', default: {} },
  decision: { label: '条件分支',   kind: 'control', default: { condition: '', jump_to: '' } },
  end:      { label: '结束节点',   kind: 'control', default: {} },

  content_generate:     { label: 'LLM 内容生成', kind: 'executor', color: 'primary', default: { topic: '', brand: 'HiveMTK', keyword: '', advantages: '', platform: '' } },
  content_score:        { label: '内容质量评分', kind: 'executor', color: 'warning', default: { min_score: 70, brand: 'HiveMTK' } },
  eeat_enhance:         { label: 'EEAT 权威增强', kind: 'executor', color: 'success', default: { brand: 'HiveMTK' } },
  fact_density_enhance: { label: '事实密度增强', kind: 'executor', color: 'success', default: { target_density: 0.8 } },
  verify:               { label: '品牌提及验证', kind: 'executor', color: 'info', default: { brand: 'HiveMTK', queries: [] } },
  query_probe:          { label: '搜索引擎探针', kind: 'executor', color: 'primary', default: { keyword: '', engine: 'qwen' } },
  source_attribution:   { label: '来源归因分析', kind: 'executor', color: 'info', default: {} },
  content_gap_fill:     { label: '内容缺口补全', kind: 'executor', color: 'warning', default: { keyword: '' } },
  capture_lead:         { label: '线索自动捕获', kind: 'executor', color: 'danger', default: {} },
}

const workflows = ref([])
const currentWorkflow = ref(null)
const wfName = ref('')
const steps = ref([])
const running = ref(false)

const executions = ref([])
const execLoading = ref(false)

const stepMeta = (type, key) => {
  const meta = STEP_TYPES[type]
  if (!meta) return key === 'label' ? (type || '未知') : undefined
  return meta[key]
};

const selectWorkflow = (w) => {
  currentWorkflow.value = w
  wfName.value = w.name || ''
  steps.value = Array.isArray(w.steps) ? JSON.parse(JSON.stringify(w.steps)) : []
}

const onNewWorkflow = () => {
  currentWorkflow.value = { name: '新工作流', description: '', steps: [] }
  wfName.value = '新工作流'
  steps.value = []
}

const addStep = (type) => {
  steps.value.push({ type, ...JSON.parse(JSON.stringify(STEP_TYPES[type].default)) })
}

const removeStep = (idx) => steps.value.splice(idx, 1)
const moveStep = (idx, dir) => {
  const target = idx + dir
  if (target < 0 || target >= steps.value.length) return
  const [item] = steps.value.splice(idx, 1)
  steps.value.splice(target, 0, item)
}

const onSaveWorkflow = async () => {
  try {
    const payload = {
      id: currentWorkflow.value?.id,
      name: wfName.value,
      description: currentWorkflow.value?.description,
      steps: steps.value
    }
    const result = await saveWorkflow(payload)
    ElMessage.success('保存成功')
    loadWorkflows()
    if (result?.id) currentWorkflow.value.id = result.id
  } catch (e) {
    ElMessage.error('保存失败：' + (e?.message || e))
  }
}

const onDeleteWorkflow = async () => {
  if (!currentWorkflow.value?.id) return
  try {
    await deleteWorkflow(currentWorkflow.value.id)
    ElMessage.success('已删除')
    currentWorkflow.value = null
    steps.value = []
    loadWorkflows()
  } catch (e) {
    ElMessage.error(e?.message || '删除失败')
  }
}

const onRunNow = async () => {
  if (!currentWorkflow.value) return
  running.value = true
  try {
    await runWorkflow(currentWorkflow.value.id || wfName.value, { steps: steps.value })
    ElMessage.success('工作流已启动')
    loadExecutions()
  } catch (e) {
    ElMessage.error('执行失败：' + (e?.message || e))
  } finally {
    running.value = false
  }
}

const execStatusType = (s) => {
  if (s === 'running' || s === 'pending') return 'warning'
  if (s === 'success' || s === 'done') return 'success'
  if (s === 'failed' || s === 'error') return 'danger'
  return 'info'
}

const loadWorkflows = async () => {
  try {
    const data = await listWorkflows()
    workflows.value = Array.isArray(data) ? data : (data?.list || data?.items || [])
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}
const loadExecutions = async () => {
  execLoading.value = true
  try {
    const data = await listWorkflowExecutions(currentWorkflow.value?.id)
    executions.value = Array.isArray(data) ? data : (data?.list || data?.items || [])
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  execLoading.value = false
}

onMounted(() => { loadWorkflows(); loadExecutions() })
</script>

<style lang="scss" scoped>
.workflow-item {
  padding: $spacing-md;
  border-radius: 6px;
  cursor: pointer;
  background: var(--el-fill-color-light);
  transition: background .15s;
}
.workflow-item:hover { background: var(--el-fill-color); }
.workflow-item.active { background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-5); }
.step-block {
  display: flex;
  align-items: flex-start;
  gap: $spacing-md;
  padding: $spacing-md 16px;
  border-radius: 8px;
  margin-bottom: $spacing-md;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
}
.step-index {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--el-color-primary);
  color: $bg-color;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: $font-size-base;
  flex-shrink: 0;
}
.step-body { flex: 1; min-width: 0; }
.step-title { font-weight: 600; margin-bottom: $spacing-sm; }
.step-config .el-input, .step-config .el-select { width: 100%; }
.step-actions { display: flex; flex-direction: column; gap: $spacing-xs; }
.step-keywords { border-left: 3px solid $warning-color; }
.step-generate { border-left: 3px solid $primary-color; }
.step-optimize { border-left: 3px solid $success-color; }
.step-verify { border-left: 3px solid $primary-color; }
.step-publish { border-left: 3px solid $danger-color; }

/* 控制节点 */
.step-trigger  { border-left: 3px solid #909399; background: var(--el-fill-color-light); }
.step-action   { border-left: 3px solid #606266; background: var(--el-fill-color-light); }
.step-decision { border-left: 3px solid #e6a23c; background: var(--el-color-warning-light-9); }
.step-end      { border-left: 3px solid #67c23a; background: var(--el-color-success-light-9); }

/* 执行器节点 */
.step-content_generate     { border-left: 3px solid var(--el-color-primary); }
.step-content_score        { border-left: 3px solid var(--el-color-warning); }
.step-eeat_enhance         { border-left: 3px solid var(--el-color-success); }
.step-fact_density_enhance { border-left: 3px solid var(--el-color-success); }
.step-verify               { border-left: 3px solid var(--el-color-info); }
.step-query_probe          { border-left: 3px solid var(--el-color-primary); }
.step-source_attribution   { border-left: 3px solid var(--el-color-info); }
.step-content_gap_fill     { border-left: 3px solid var(--el-color-warning); }
.step-capture_lead         { border-left: 3px solid var(--el-color-danger); }
</style>
