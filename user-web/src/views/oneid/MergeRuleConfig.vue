<template>
  <div class="merge-rule-config">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>OneID 合并规则（USR-CM-01b）</span>
          <el-button type="primary" @click="addRule">+ 新增规则</el-button>
        </div>
      </template>
      <el-alert type="info" :closable="false" style="margin-bottom: 16px">
        <p>配置自动合并规则。确定性规则：手机号 / 邮箱 / 微信 OpenID 完全匹配。概率规则：昵称 + 地区 + 设备指纹相似度。</p>
        <p>合并后可在「OneID 列表」/「OneID 冲突」页审核，规则置信度低于阈值时进入人工审核队列。</p>
      </el-alert>

      <el-table :data="rules" v-loading="loading">
        <el-table-column prop="name" label="规则名称" width="180" />
        <el-table-column prop="type" label="类型" width="120">
          <template #default="{ row }">
            <el-tag :type="row.type === 'deterministic' ? 'success' : 'warning'">
              {{ row.type === 'deterministic' ? '确定性' : '概率性' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="fields" label="匹配字段" min-width="200">
          <template #default="{ row }">
            <el-tag v-for="f in row.fields" :key="f" size="small" style="margin-right: 4px">
              {{ fieldLabel(f) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="threshold" label="置信度阈值" width="140">
          <template #default="{ row }">
            <el-progress :percentage="row.threshold" :stroke-width="6" />
          </template>
        </el-table-column>
        <el-table-column prop="priority" label="优先级" width="80" />
        <el-table-column label="启用" width="80">
          <template #default="{ row }">
            <el-switch v-model="row.enabled" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="editRule(row)">编辑</el-button>
            <el-button link type="danger" @click="deleteRule(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-dialog v-model="dialogVisible" :title="editing.id ? '编辑规则' : '新增规则'" width="640px">
      <el-form :model="editing" label-width="100px">
        <el-form-item label="规则名称">
          <el-input v-model="editing.name" />
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="editing.type">
            <el-radio label="deterministic">确定性</el-radio>
            <el-radio label="probabilistic">概率性</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="匹配字段">
          <el-checkbox-group v-model="editing.fields">
            <el-checkbox label="phone">手机号</el-checkbox>
            <el-checkbox label="email">邮箱</el-checkbox>
            <el-checkbox label="wechat_open_id">微信 OpenID</el-checkbox>
            <el-checkbox label="douyin_id">抖音 ID</el-checkbox>
            <el-checkbox label="nickname">昵称</el-checkbox>
            <el-checkbox label="region">地区</el-checkbox>
            <el-checkbox label="device_fingerprint">设备指纹</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="置信度阈值">
          <el-slider v-model="editing.threshold" :min="50" :max="100" :step="5" show-input />
        </el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="editing.priority" :min="0" :max="100" />
        </el-form-item>
        <el-form-item label="动作">
          <el-radio-group v-model="editing.action">
            <el-radio label="auto">自动合并</el-radio>
            <el-radio label="review">进入人工审核</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveRule">保存</el-button>
      </template>
    </el-dialog>

    
    <el-card v-if="preview" class="preview-card">
      <template #header>
        <span>命中预览</span>
      </template>
      <el-statistic :value="preview.candidateCount" title="将合并候选对" />
      <el-table :data="preview.samples" size="small" max-height="240">
        <el-table-column prop="from" label="From" />
        <el-table-column prop="to" label="To" />
        <el-table-column prop="score" label="相似度" width="120">
          <template #default="{ row }">
            <el-progress :percentage="row.score" :stroke-width="6" />
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/request'

const FIELD_LABELS = {
  phone: '手机号',
  email: '邮箱',
  wechat_open_id: '微信',
  douyin_id: '抖音',
  nickname: '昵称',
  region: '地区',
  device_fingerprint: '设备指纹'
}
const fieldLabel = (k) => FIELD_LABELS[k] || k

const loading = ref(false)
const rules = ref([])
const dialogVisible = ref(false)
const editing = reactive({ id: null, name: '', type: 'deterministic', fields: [], threshold: 95, priority: 50, action: 'review', enabled: true })
const preview = ref(null)

async function load() {
  loading.value = true
  try {
    const res = await http.get('/api/oneid/merge-rules')
    const list = Array.isArray(res)
      ? res
      : [...(res?.built_in || []), ...(res?.custom || [])];
    rules.value = list.map(r => ({
      ...r,
      type: r.type || 'deterministic',
      fields: Array.isArray(r.fields) ? r.fields : (r.field ? [r.field] : []),
      threshold: typeof r.threshold === 'number' ? r.threshold : 100
    }))
  } finally {
    loading.value = false
  }
}

async function saveRule() {
  if (!editing.name) {
    ElMessage.warning('请填写规则名称')
    return
  }
  if (editing.fields.length === 0) {
    ElMessage.warning('请选择至少一个匹配字段')
    return
  }
  await http.post('/api/oneid/merge-rules', editing)
  ElMessage.success('已保存')
  dialogVisible.value = false
  await load()
  await runPreview()
}

function addRule() {
  Object.assign(editing, { id: null, name: '', type: 'deterministic', fields: [], threshold: 95, priority: 50, action: 'review', enabled: true })
  dialogVisible.value = true
}

function editRule(row) {
  Object.assign(editing, row)
  dialogVisible.value = true
}

async function deleteRule(row) {
  try {
    await ElMessageBox.confirm(`确认删除规则「${row.name}」？`, '删除确认', { type: 'warning' })
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    throw e
  }
  await http.delete(`/api/oneid/merge-rules/${row.id}`)
  ElMessage.success('已删除')
  await load()
}

async function runPreview() {
  try {
    preview.value = await http.post('/api/oneid/merge-rules/preview', rules.value)
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

onMounted(async () => {
  await load()
  await runPreview()
})
</script>

<style scoped>
.merge-rule-config { padding: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.preview-card { margin-top: 16px; }
</style>
