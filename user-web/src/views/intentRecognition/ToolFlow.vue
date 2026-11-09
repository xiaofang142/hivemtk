<template>
  <div class="tool-flow-page">
    <!-- 页头说明 -->
    <el-card shadow="never" class="intro-card">
      <div class="intro">
        <div class="intro-title">意图 → 工具调用流编排</div>
        <div class="intro-desc">
          为识别到的意图配置要调用的工具序列（如：查订单 → 查物流）。保存即生效（约 30 秒内热更新，无需重启服务）；
          参数支持上下文变量，白名单外的变量会被拒绝。未配置调用流的意图沿用系统默认行为。
        </div>
        <div class="intro-vars" v-if="vocab.template_vars && vocab.template_vars.length">
          <span class="vars-label">可用变量：</span>
          <el-tag v-for="v in vocab.template_vars" :key="v" size="small" type="info" class="var-tag">{{ varLabel(v) }}</el-tag>
        </div>
      </div>
    </el-card>

    <el-card shadow="never">
      <div class="toolbar">
        <el-select v-model="filterIntent" placeholder="按意图筛选" clearable filterable style="width: 240px">
          <el-option v-for="i in vocab.intents || []" :key="i" :label="intentLabel(i)" :value="i" />
        </el-select>
        <el-button type="primary" @click="openCreate">新增调用流</el-button>
        <el-button @click="loadList">刷新</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column label="名称" prop="name" min-width="160" show-overflow-tooltip />
        <el-table-column label="意图" min-width="150">
          <template #default="{ row }">
            <el-tag size="small">{{ row.intent_code }}</el-tag>
            <el-tag size="small" type="warning" class="family-tag" v-if="familyOf(row.intent_code)">{{ familyText(familyOf(row.intent_code)) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="工具调用流" min-width="240">
          <template #default="{ row }">
            <span class="flow-steps">{{ stepsPreview(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="启用" width="80">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="(v) => toggleEnabled(row, v)" />
          </template>
        </el-table-column>
        <el-table-column label="更新时间" prop="updated_at" width="170" show-overflow-tooltip />
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="removeRow(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>暂无调用流配置</template>
      </el-table>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑调用流' : '新增调用流'" width="760px" destroy-on-close>
      <el-form :model="form" label-width="92px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="如：快递/物流查询调用流" maxlength="100" />
        </el-form-item>
        <el-form-item label="意图" required>
          <el-select v-model="form.intent_code" filterable placeholder="选择识别意图" style="width: 100%">
            <el-option-group v-for="g in intentGroups" :key="g.label" :label="g.label">
              <el-option v-for="i in g.items" :key="i" :label="intentLabel(i)" :value="i" />
            </el-option-group>
          </el-select>
          <div class="form-hint">已绑定其他调用流的意图会被拒绝（一个意图一条调用流）；重复键表示两套识别体系共用该意图。</div>
        </el-form-item>

        <el-form-item label="工具调用流" required>
          <div class="steps-editor">
            <div v-for="(st, idx) in form.steps" :key="idx" class="step-row">
              <div class="step-head">
                <span class="step-no">{{ idx + 1 }}</span>
                <el-select v-model="st.tool_name" filterable placeholder="选择工具" style="width: 260px">
                  <el-option v-for="t in vocab.tools || []" :key="t" :label="t" :value="t" />
                </el-select>
                <el-checkbox v-model="st.enabled">启用</el-checkbox>
                <div class="step-ops">
                  <el-button link :disabled="idx === 0" @click="moveStep(idx, -1)">上移</el-button>
                  <el-button link :disabled="idx === form.steps.length - 1" @click="moveStep(idx, 1)">下移</el-button>
                  <el-button link type="danger" @click="removeStep(idx)">删除</el-button>
                </div>
              </div>
              <div class="args-editor" v-if="Object.keys(st.args || {}).length || argsEditing[idx]">
                <div v-for="(val, key) in st.args" :key="key" class="arg-row">
                  <el-input :model-value="key" disabled class="arg-key" />
                  <el-input v-model="st.args[key]" placeholder="参数值，可含变量占位符" class="arg-val" />
                  <el-button link type="danger" @click="removeArg(st, key)">删</el-button>
                </div>
                <div class="arg-add">
                  <el-input v-model="argsDraft[idx].key" placeholder="参数名" class="arg-key" />
                  <el-input v-model="argsDraft[idx].value" placeholder="参数值，可含 {{var}}" class="arg-val" />
                  <el-button link type="primary" @click="addArg(st, idx)">添加</el-button>
                </div>
              </div>
              <div class="arg-toggle">
                <el-button link @click="toggleArgsEditor(idx)">{{ argsEditing[idx] ? '收起参数' : '展开参数' }}</el-button>
                <span v-if="vocab.templateVars && vocab.templateVars.length" class="arg-hint">{{ varsHint() }}</span>
              </div>
            </div>
            <el-button type="primary" plain @click="addStep">添加步骤</el-button>
            <div class="form-hint">按顺序执行；只引导模型优先按此链调用，工具不适用时模型可跳过并说明。查询类工具（只读）可直接启用；写操作类工具需走审批档。</div>
          </div>
        </el-form-item>

        <el-form-item label="回复提示">
          <el-input v-model="form.reply_hint" type="textarea" :rows="3" placeholder="留空则使用系统默认提示。可写该意图下模型应当如何组织回答的指引。" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import {
  createIntentToolChain,
  deleteIntentToolChain,
  getIntentToolChainVocab,
  listIntentToolChains,
  updateIntentToolChain,
} from '@/api/intentToolChain';

const loading = ref(false);
const saving = ref(false);
const dialogVisible = ref(false);
const editingId = ref(null);

const vocab = ref({ intents: [], intent_families: {}, template_vars: [], tools: [] });
const rows = ref([]);
const filterIntent = ref('');

const form = ref({ name: '', intent_code: '', steps: [], reply_hint: '', enabled: true });
const argsEditing = ref({});
const argsDraft = ref({});

const FAMILY_TEXT = { runtime: '推理闭环', service: '客服主链', both: '共用' };

const intentGroups = computed(() => {
  const groups = { runtime: [], service: [], both: [] };
  for (const i of vocab.value.intents || []) {
    const f = familyOf(i) || 'runtime';
    (groups[f] || groups.runtime).push(i);
  }
  for (const k of Object.keys(groups)) groups[k].sort();
  return [
    { label: '共用意图（两套识别体系）', items: groups.both },
    { label: '客服主链意图', items: groups.service },
    { label: '推理闭环意图', items: groups.runtime },
  ].filter((g) => g.items.length);
});

function varLabel(v) {
  return '{{' + v + '}}';
}
function varsHint() {
  return '变量：' + (vocab.value.template_vars || []).map((v) => '{{' + v + '}}').join(' / ');
}
function familyOf(code) {
  return (vocab.value.intent_families || {})[code] || '';
}
function familyText(f) {
  return FAMILY_TEXT[f] || f;
}
function intentLabel(code) {
  const f = familyOf(code);
  return f ? `${code}（${familyText(f)}）` : code;
}
function stepsPreview(row) {
  let steps;
  try {
    steps = JSON.parse(row.steps_json || '[]');
  } catch {
    return '-';
  }
  const names = steps
    .filter((s) => s.enabled)
    .map((s) => s.tool_name);
  return names.length ? names.join(' → ') : '（全部禁用）';
}

async function loadVocab() {
  const res = await getIntentToolChainVocab();
  vocab.value = res?.data || { intents: [], intent_families: {}, template_vars: [], tools: [] };
}

async function loadList() {
  loading.value = true;
  try {
    const params = {};
    if (filterIntent.value) params.intent_code = filterIntent.value;
    const res = await listIntentToolChains(params);
    rows.value = res?.data?.list || [];
  } finally {
    loading.value = false;
  }
}

function blankStep() {
  return { tool_name: '', args: {}, priority: 0, enabled: true };
}

function openCreate() {
  editingId.value = null;
  form.value = { name: '', intent_code: '', steps: [blankStep()], reply_hint: '', enabled: true };
  argsEditing.value = { 0: false };
  argsDraft.value = { 0: { key: '', value: '' } };
  dialogVisible.value = true;
}

function openEdit(row) {
  let steps;
  try {
    steps = JSON.parse(row.steps_json || '[]');
  } catch {
    steps = [];
  }
  if (!steps.length) steps = [blankStep()];
  form.value = {
    name: row.name || '',
    intent_code: row.intent_code || '',
    steps: steps.map((s) => ({ tool_name: s.tool_name || '', args: s.args || {}, priority: s.priority || 0, enabled: !!s.enabled })),
    reply_hint: row.reply_hint || '',
    enabled: !!row.enabled,
  };
  const editing = {};
  const draft = {};
  form.value.steps.forEach((s, i) => {
    editing[i] = Object.keys(s.args || {}).length > 0;
    draft[i] = { key: '', value: '' };
  });
  argsEditing.value = editing;
  argsDraft.value = draft;
  editingId.value = row.id;
  dialogVisible.value = true;
}

function addStep() {
  form.value.steps.push(blankStep());
  const idx = form.value.steps.length - 1;
  argsEditing.value[idx] = false;
  argsDraft.value[idx] = { key: '', value: '' };
}
function removeStep(idx) {
  form.value.steps.splice(idx, 1);
  argsEditing.value = reindex(argsEditing.value, idx);
  argsDraft.value = reindex(argsDraft.value, idx);
}
function moveStep(idx, delta) {
  const target = idx + delta;
  if (target < 0 || target >= form.value.steps.length) return;
  const steps = form.value.steps;
  [steps[idx], steps[target]] = [steps[target], steps[idx]];
  const ae = argsEditing.value;
  [ae[idx], ae[target]] = [ae[target], ae[idx]];
  const ad = argsDraft.value;
  [ad[idx], ad[target]] = [ad[target], ad[idx]];
}
function reindex(map, removedIdx) {
  const out = {};
  Object.keys(map)
    .map(Number)
    .sort((a, b) => a - b)
    .forEach((k) => {
      out[k > removedIdx ? k - 1 : k] = map[k];
    });
  delete out[Object.keys(map).length - 1];
  return out;
}
function toggleArgsEditor(idx) {
  argsEditing.value[idx] = !argsEditing.value[idx];
}
function addArg(step, idx) {
  const key = (argsDraft.value[idx]?.key || '').trim();
  if (!key) {
    ElMessage.warning('请填写参数名');
    return;
  }
  if (!step.args) step.args = {};
  if (Object.prototype.hasOwnProperty.call(step.args, key)) {
    ElMessage.warning(`参数 ${key} 已存在`);
    return;
  }
  step.args[key] = argsDraft.value[idx]?.value || '';
  argsDraft.value[idx] = { key: '', value: '' };
}
function removeArg(step, key) {
  delete step.args[key];
}

// 客户端前置校验（与服务端 ValidateChainArgs 同口径；服务端仍会兜底拒绝）
function validate() {
  if (!form.value.name.trim()) return '请填写名称';
  if (!form.value.intent_code) return '请选择意图';
  if (!form.value.steps.length) return '至少一个工具步骤';
  const seen = new Set();
  for (let i = 0; i < form.value.steps.length; i++) {
    const st = form.value.steps[i];
    if (!st.tool_name.trim()) return `第 ${i + 1} 步未选择工具`;
    if (seen.has(st.tool_name)) return `工具 ${st.tool_name} 在链中重复`;
    seen.add(st.tool_name);
    for (const [k, v] of Object.entries(st.args || {})) {
      if (typeof v === 'string') {
        const re = /\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}/g;
        let m;
        while ((m = re.exec(v)) !== null) {
          if (!(vocab.value.template_vars || []).includes(m[1])) {
            return `第 ${i + 1} 步参数 ${k} 使用了白名单外变量 {{${m[1]}}}`;
          }
        }
        if (v.includes('{{') && !/\{\{\s*[a-zA-Z_][a-zA-Z0-9_]*\s*\}\}/.test(v)) {
          return `第 ${i + 1} 步参数 ${k} 的模板占位符未闭合`;
        }
      }
    }
  }
  return '';
}

async function save() {
  const err = validate();
  if (err) {
    ElMessage.warning(err);
    return;
  }
  saving.value = true;
  try {
    const payload = {
      name: form.value.name.trim(),
      intent_code: form.value.intent_code,
      steps: form.value.steps.map((s, i) => ({ tool_name: s.tool_name.trim(), args: s.args || {}, priority: i + 1, enabled: !!s.enabled })),
      reply_hint: form.value.reply_hint || '',
      enabled: !!form.value.enabled,
    };
    if (editingId.value) {
      await updateIntentToolChain(editingId.value, payload);
      ElMessage.success('已更新，约 30 秒内热生效');
    } else {
      await createIntentToolChain(payload);
      ElMessage.success('已创建，约 30 秒内热生效');
    }
    dialogVisible.value = false;
    await loadList();
  } catch (e) {
    // http 层已统一弹出后端 message（4000/4009 中文原因），此处不再重复提示
  } finally {
    saving.value = false;
  }
}

async function toggleEnabled(row, v) {
  let steps;
  try {
    steps = JSON.parse(row.steps_json || '[]');
  } catch {
    steps = [];
  }
  await updateIntentToolChain(row.id, {
    name: row.name,
    intent_code: row.intent_code,
    steps,
    reply_hint: row.reply_hint || '',
    enabled: !!v,
  });
  ElMessage.success(v ? '已启用，约 30 秒内热生效' : '已停用');
  await loadList();
}

async function removeRow(row) {
  try {
    await ElMessageBox.confirm(`确认删除意图 ${row.intent_code} 的调用流「${row.name}」？`, '删除确认', { type: 'warning' });
  } catch {
    return;
  }
  await deleteIntentToolChain(row.id);
  ElMessage.success('已删除');
  await loadList();
}

onMounted(async () => {
  await loadVocab();
  await loadList();
});
</script>

<style scoped>
.tool-flow-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.intro-card .intro-title {
  font-size: 16px;
  font-weight: 600;
  margin-bottom: 6px;
}
.intro-desc {
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}
.intro-vars {
  margin-top: 8px;
}
.vars-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.var-tag {
  margin-right: 6px;
  font-family: monospace;
}
.toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.family-tag {
  margin-left: 6px;
}
.flow-steps {
  font-family: monospace;
}
.form-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
  margin-top: 4px;
}
.steps-editor {
  width: 100%;
}
.step-row {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  padding: 8px;
  margin-bottom: 8px;
}
.step-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.step-no {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  flex-shrink: 0;
}
.step-ops {
  margin-left: auto;
}
.args-editor {
  margin: 8px 0 4px 30px;
}
.arg-row,
.arg-add {
  display: flex;
  gap: 8px;
  margin-bottom: 6px;
}
.arg-key {
  width: 180px;
  flex-shrink: 0;
}
.arg-val {
  flex: 1;
  font-family: monospace;
}
.arg-toggle {
  margin-left: 30px;
}
.arg-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-left: 8px;
  font-family: monospace;
}
</style>
