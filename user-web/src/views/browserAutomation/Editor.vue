<template>
  <div class="page">
    <h2>{{ isEdit ? '编辑任务' : '新建任务' }}</h2>
    <el-form :model="form" label-width="120px" style="max-width: 860px">
      <el-form-item label="名称" required>
        <el-input v-model="form.name" maxlength="256" />
      </el-form-item>
      <el-form-item label="描述">
        <el-input v-model="form.description" type="textarea" :rows="2" />
      </el-form-item>
      <el-form-item label="平台" required>
        <el-select v-model="form.platform" style="width: 220px" @change="onPlatformChange">
          <el-option v-for="p in platforms" :key="p.identifier" :value="p.identifier"
            :label="platformLabel(p)" :disabled="false" />
        </el-select>
        <span v-if="currentPlatform && !currentPlatform.can_post_comment" class="platform-hint">
          {{ platformLabel(currentPlatform) }} 暂不支持发评论（接口未公开/待 M3）
        </span>
      </el-form-item>
      <el-form-item label="起始 URL" required>
        <el-input v-model="form.url" placeholder="https://..." />
      </el-form-item>
      <el-form-item label="任务类型">
        <el-select v-model="form.task_type" style="width: 200px">
          <el-option label="单次 (one_shot)" value="one_shot" />
          <el-option label="循环 (loop)" value="loop" />
          <el-option label="定时 (cron)" value="cron" />
          <el-option label="工作流 (workflow)" value="workflow" />
        </el-select>
      </el-form-item>
      <el-form-item label="执行模式">
        <el-radio-group v-model="form.brain_mode">
          <el-radio :value="false">显式步骤编排</el-radio>
          <el-radio :value="true">Brain 目标驱动（LLM）</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="form.brain_mode" label="Brain 目标" required>
        <el-input v-model="form.brain_goal" type="textarea" :rows="3" placeholder="例：打开页面搜索“机械键盘”，记录前 10 个结果的标题与价格" />
      </el-form-item>

      <template v-if="!form.brain_mode">
        <el-form-item>
          <el-button size="small" type="primary" plain @click="applyPreset">平台预设模板</el-button>
          <span class="preset-hint">按当前平台生成读链路编排（标题/作者/评论提取 + 截图），可再手动调整</span>
        </el-form-item>
        <el-form-item label="步骤编排">
          <div class="steps-editor">
            <div v-for="(step, i) in form.steps" :key="i" class="step-row">
              <span class="step-index">{{ i + 1 }}</span>
              <el-select v-model="step.action" style="width: 160px" placeholder="动作">
                <el-option v-for="a in actions" :key="a.value" :label="a.label" :value="a.value" />
              </el-select>
              <el-input v-if="['open_tab','click','type','extract','query'].includes(step.action)" v-model="step.target"
                :placeholder="step.action === 'open_tab' ? 'URL（可选，默认任务 URL）' : 'CSS selector 或 @eN'" style="width: 240px" />
              <el-input v-if="step.action === 'type'" v-model="step.value" placeholder="输入内容" style="width: 180px" />
              <el-input v-if="step.action === 'post_comment'" v-model="step.value" placeholder="评论文本" style="width: 220px" />
              <el-input v-if="step.action === 'wait'" v-model.number="step.ms" placeholder="毫秒" style="width: 100px" />
              <el-input v-if="step.action === 'wait_for_selector'" v-model="step.selector" placeholder="selector" style="width: 160px" />
              <el-input v-if="step.action === 'wait_for_selector'" v-model.number="step.timeout_ms" placeholder="超时ms" style="width: 100px" />
              <el-select v-if="step.action === 'scroll'" v-model="step.direction" style="width: 90px">
                <el-option v-for="d in ['up','down','left','right']" :key="d" :label="d" :value="d" />
              </el-select>
              <el-input v-if="step.action === 'scroll'" v-model.number="step.amount" placeholder="px" style="width: 90px" />
              <el-select v-if="step.action === 'assert'" v-model="step.assert_kind" style="width: 150px">
                <el-option label="包含文本" value="contains_text" />
                <el-option label="元素存在" value="selector_exists" />
              </el-select>
              <el-input v-if="step.action === 'assert'" v-model="step.value" placeholder="断言文本/selector" style="width: 180px" />
              <el-select v-if="step.action === 'query'" v-model="step.query_kind" style="width: 130px">
                <el-option label="取文本" value="text" />
                <el-option label="是否存在" value="exists" />
                <el-option label="计数" value="count" />
                <el-option label="取属性" value="attr" />
              </el-select>
              <el-checkbox v-model="step.continue_on_error" label="失败继续" />
              <el-input-number v-model="step.retry_count" :min="0" :max="10" size="small" style="width: 90px" />
              <el-button size="small" type="danger" icon="Delete" circle @click="removeStep(i)" />
            </div>
            <el-button size="small" icon="Plus" @click="addStep">添加步骤</el-button>
          </div>
        </el-form-item>
      </template>

      <el-form-item label="循环次数">
        <el-input-number v-model="form.loop_count" :min="1" :max="1000" />
      </el-form-item>
      <el-form-item label="步间间隔(ms)">
        <el-input-number v-model="form.delay_ms" :min="0" :max="60000" :step="500" />
      </el-form-item>
      <el-form-item label="超时(秒)">
        <el-input-number v-model="form.timeout_sec" :min="10" :max="3600" :step="10" />
      </el-form-item>

      <el-form-item label="失败自动重试">
        <el-switch v-model="form.retry_on_fail" />
        <template v-if="form.retry_on_fail">
          <el-input-number v-model="form.retry_delay_sec" :min="30" :max="86400" :step="30" style="margin-left: 12px" />
          <span style="margin: 0 8px">秒后重试，最多</span>
          <el-input-number v-model="form.max_retry_times" :min="1" :max="10" />
          <span style="margin-left: 8px">次</span>
        </template>
      </el-form-item>

      <el-form-item v-if="form.task_type === 'workflow'" label="依赖前置任务">
        <el-select v-model="form.depends_on_task_id" clearable placeholder="选择已发布任务" style="width: 300px">
          <el-option v-for="t in readyTasks" :key="t.id" :label="`#${t.id} ${t.name}`" :value="t.id" />
        </el-select>
        <el-select v-if="form.depends_on_task_id" v-model="form.depends_on_mode" style="width: 160px; margin-left: 12px">
          <el-option label="前置完成 (all_done)" value="all_done" />
          <el-option label="前置曾成功 (any_success)" value="any_success" />
        </el-select>
      </el-form-item>

      <el-form-item>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        <el-button @click="$router.back()">取消</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getBrowserTask, createBrowserTask, updateBrowserTask, listBrowserTasks, listPlatforms } from '@/api/browserAutomation'

const route = useRoute()
const router = useRouter()
const isEdit = computed(() => !!route.params.id)
const saving = ref(false)
const readyTasks = ref([])
const platforms = ref([])

const actions = [
  { value: 'open_tab', label: '打开标签页' }, { value: 'click', label: '点击' },
  { value: 'type', label: '输入' }, { value: 'post_comment', label: '发评论' },
  { value: 'snapshot', label: '页面快照' },
  { value: 'markdown', label: '页面 Markdown' }, { value: 'screenshot', label: '截图' },
  { value: 'wait', label: '等待' }, { value: 'wait_for_selector', label: '等待元素' },
  { value: 'scroll', label: '滚动' }, { value: 'extract', label: '提取数据' },
  { value: 'assert', label: '断言' }, { value: 'query', label: '查询元素' },
  { value: 'close_tab', label: '关闭标签页' },
]

const platformNames = { xiaohongshu: '小红书', douyin: '抖音', xianyu: '闲鱼' }
const platformLabel = (p) => platformNames[p?.identifier] || p?.identifier || p
const currentPlatform = computed(() => platforms.value.find((p) => p.identifier === form.value.platform))

const emptyStep = () => ({
  action: 'click', target: '', value: '', ms: 0, clear_first: false, submit_on_enter: false,
  direction: 'down', amount: 400, selector: '', timeout_ms: 10000,
  assert_kind: 'contains_text', query_kind: 'text',
  continue_on_error: false, retry_count: 0, retry_backoff_ms: 1000,
})

const form = ref({
  name: '', description: '', url: '', platform: 'xiaohongshu', task_type: 'one_shot',
  brain_mode: false, brain_goal: '', steps: [],
  loop_count: 1, delay_ms: 1000, timeout_sec: 120,
  retry_on_fail: false, retry_delay_sec: 300, max_retry_times: 3,
  depends_on_task_id: null, depends_on_mode: 'all_done',
})

const addStep = () => form.value.steps.push(emptyStep())
const removeStep = (i) => form.value.steps.splice(i, 1)

// 平台预设模板：按平台生成读链路编排（L3 Locators 知识落在模板，平台差异收敛于此）
function applyPreset() {
  const pid = form.value.platform
  const presets = {
    xiaohongshu: [
      { action: 'open_tab' }, { action: 'wait', ms: 4000 },
      { action: 'extract', target: '', selectors: { titles: '.note-content .title, #detail-title', authors: '.author-wrapper .name, .username', comments: '.parent-comment .note-text, .comment-item .note-text' } },
      { action: 'screenshot' },
    ],
    douyin: [
      { action: 'open_tab' }, { action: 'wait', ms: 5000 },
      { action: 'extract', target: '', selectors: { titles: '[class*=video-title], h1', authors: '[class*=author] a, [class*=author-name]', comments: '[class*=comment-item] span:not([class*=time])' } },
      { action: 'screenshot' },
    ],
    xianyu: [
      { action: 'open_tab' }, { action: 'wait', ms: 5000 },
      { action: 'extract', target: '', selectors: { titles: '[class*=item-title], .title, h1', prices: '[class*=price], [class*=Price]', descs: '[class*=desc], [class*=description]' } },
      { action: 'screenshot' },
    ],
  }
  form.value.steps = (presets[pid] || presets.xiaohongshu).map((s) => ({ ...emptyStep(), ...s }))
  ElMessage.success(`已套用 ${platformLabel(currentPlatform.value)} 读链路预设`)
}

// 切平台时若已套过预设则提示重新套用（不做静默覆盖，用户编排可能手动改过）
function onPlatformChange(pid) {
  const p = platforms.value.find((x) => x.identifier === pid)
  if (p && !p.can_post_comment && form.value.steps.some((s) => s.action === 'post_comment')) {
    ElMessage.warning(`${platformLabel(p)} 暂不支持发评论，post_comment 步骤执行时会失败`)
  }
}

async function loadTask() {
  if (!isEdit.value) return
  const res = await getBrowserTask(route.params.id)
  const t = res?.data ?? res
  if (t) {
    form.value = { ...form.value, ...t, steps: Array.isArray(t.steps) ? t.steps.map((s) => ({ ...emptyStep(), ...s })) : [] }
  }
}

async function save() {
  saving.value = true
  try {
    const payload = { ...form.value }
    if (payload.depends_on_task_id == null) { delete payload.depends_on_task_id }
    if (isEdit.value) {
      await updateBrowserTask(route.params.id, payload)
    } else {
      await createBrowserTask(payload)
    }
    ElMessage.success('已保存')
    router.push('/browser-automation/tasks')
  } catch (e) {
    ElMessage.error(String(e?.message || e))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await loadTask()
  // workflow 依赖选择列表
  const res = await listBrowserTasks({ status: 'ready', limit: 100 })
  const data = res?.data ?? res
  readyTasks.value = data?.list || []
  // 平台注册表（L3 实时读取）
  try {
    const pres = await listPlatforms()
    platforms.value = pres?.data ?? pres ?? []
  } catch { /* 平台接口失败不阻塞编辑器，下拉走空态 */ }
})
</script>

<style scoped>
.page { padding: 16px; }
.steps-editor { width: 100%; }
.step-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
.step-index { width: 20px; text-align: right; color: #999; }
.platform-hint { margin-left: 12px; color: #e6a23c; font-size: 12px; }
.preset-hint { margin-left: 12px; color: #999; font-size: 12px; }
</style>
