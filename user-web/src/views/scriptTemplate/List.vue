<template>
  <div class="script-template-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('话术模板') }}</h2>
        <p class="subtitle">
          {{ $t('管理营销话术、销售话术模板') }}
        </p>
      </div>
      <el-button
        type="primary"
        @click="showCreateDialog"
      >
        <el-icon><Plus /></el-icon>
        {{ $t('新增话术') }}
      </el-button>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('话术列表') }}</span>
          <div>
            <el-select
              v-model="filterScenario"
              :placeholder="$t('场景')"
              clearable
              style="width: 130px; margin-right: 10px"
            >
              <el-option
                :label="$t('开场白')"
                value="opening"
              />
              <el-option
                :label="$t('跟进')"
                value="follow"
              />
              <el-option
                :label="$t('异议处理')"
                value="objection"
              />
              <el-option
                :label="$t('促成成交')"
                value="closing"
              />
            </el-select>
            <el-input
              v-model="searchKeyword"
              :placeholder="$t('搜索话术')"
              clearable
              style="width: 200px"
            />
          </div>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="filteredScripts"
        stripe
      >
        <el-table-column
          prop="title"
          label="标题"
          min-width="180"
        />
        <el-table-column
          prop="scenario"
          label="场景"
          width="100"
        >
          <template #default="{ row }">
            <el-tag>{{ getScenarioText(row.scenario) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="content"
          label="内容"
          min-width="300"
          show-overflow-tooltip
        />
        <el-table-column
          prop="useCount"
          label="使用次数"
          width="100"
        />
        <el-table-column
          prop="effectiveness"
          label="效果评分"
          width="120"
        >
          <template #default="{ row }">
            <el-rate
              v-model="row.effectiveness"
              disabled
              size="small"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="updatedAt"
          label="更新时间"
          width="180"
        >
          <template #default="{ row }">
            {{ fmtTime(row.updatedAt) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="250"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="useScript(row)"
            >
              使用
            </el-button>
            <el-button
              link
              type="primary"
              @click="editScript(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="primary"
              @click="copyScript(row)"
            >
              复制
            </el-button>
            <el-button
              link
              type="danger"
              @click="deleteScript(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="700px"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-width="100px"
      >
        <el-form-item
          label="话术标题"
          prop="title"
        >
          <el-input v-model="form.title" />
        </el-form-item>
        <el-form-item
          label="使用场景"
          prop="scenario"
        >
          <el-select
            v-model="form.scenario"
            style="width: 100%"
          >
            <el-option
              label="开场白"
              value="opening"
            />
            <el-option
              label="跟进"
              value="follow"
            />
            <el-option
              label="异议处理"
              value="objection"
            />
            <el-option
              label="促成成交"
              value="closing"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="话术分类"
          prop="category"
        >
          <el-select
            v-model="form.category"
            placeholder="请选择或输入分类"
            filterable
            allow-create
            default-first-option
            style="width: 100%"
          >
            <el-option
              v-for="cat in categories"
              :key="cat.id"
              :label="cat.name"
              :value="cat.name"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="话术内容"
          prop="content"
        >
          <el-input
            v-model="form.content"
            type="textarea"
            :rows="6"
          />
        </el-form-item>
        <el-form-item label="变量">
          <el-input
            v-model="form.variables"
            placeholder="多个变量用逗号分隔，如: 客户姓名,产品名"
          />
        </el-form-item>
        <el-form-item label="标签">
          <el-input
            v-model="form.tags"
            placeholder="多个标签用逗号分隔"
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.remark"
            type="textarea"
            :rows="2"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitForm"
        >
          确定
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getScriptCategories, getScripts, createScript, updateScript, deleteScript as deleteScriptApi, useScript as useScriptApi } from '@/api/scriptTemplate.js'
import { toList } from '@/utils/list.js'
import { copyToClipboard } from '@/utils/clipboard.js'
import { fmtTime } from '@/utils/time'

const loading = ref(false)
const searchKeyword = ref('')
const filterScenario = ref('')
const scripts = ref([])
const dialogVisible = ref(false)
const dialogTitle = ref('新增话术')
const formRef = ref()
const form = ref({
  id: 0,
  title: '',
  scenario: 'opening',
  category: '',
  content: '',
  variables: '',
  tags: '',
  remark: ''
})
const formRules = {
  title: [{ required: true, message: i18n.global.t('请输入标题'), trigger: 'blur' }],
  scenario: [{ required: true, message: i18n.global.t('请选择场景'), trigger: 'change' }],
  category: [{ required: true, message: i18n.global.t('请选择或输入分类'), trigger: 'change' }],
  content: [{ required: true, message: i18n.global.t('请输入内容'), trigger: 'blur' }]
}

const categories = ref([]);
const loadCategories = async () => {
  try {
    const res = await getScriptCategories()
    categories.value = toList(res)
  } catch (e) {
    categories.value = []
  }
}

const filteredScripts = computed(() => {
  let result = scripts.value
  if (filterScenario.value) result = result.filter(s => s.scenario === filterScenario.value)
  if (searchKeyword.value) result = result.filter(s => s.title.includes(searchKeyword.value))
  return result
})

const getScenarioText = (s) => {
  const map = { opening: '开场白', follow: '跟进', objection: '异议处理', closing: '促成成交' }
  return map[s] || s
}

const refreshData = async () => {
  loading.value = true
  try {
    const res = await getScripts()
    scripts.value = toList(res)
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  form.value = { id: 0, title: '', scenario: 'opening', category: '', content: '', variables: '', tags: '', remark: '' }
  dialogTitle.value = '新增话术'
  dialogVisible.value = true
}

const editScript = (row) => {
  form.value = { ...row }
  dialogTitle.value = '编辑话术'
  dialogVisible.value = true
}

const submitForm = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    try {
      const payload = {
        title: form.value.title,
        category: form.value.category,
        scenario: form.value.scenario || '',
        content: form.value.content,
        variables: (form.value.variables || '')
          .split(',').map(s => String(s).trim()).filter(Boolean),
        tags: form.value.tags || '',
        is_public: false
      };
      if (form.value.id) {
        await updateScript(form.value.id, payload)
      } else {
        await createScript(payload)
      }
      ElMessage.success(i18n.global.t('保存成功'))
      dialogVisible.value = false
      refreshData()
    } catch (error) {
      ElMessage.error(i18n.global.t('保存失败'))
    }
  })
}

const useScript = async (row) => {
  await useScriptApi(row.id)
  const copied = await copyToClipboard(row.content)
  if (copied) {
    ElMessage.success(i18n.global.t('话术已复制到剪贴板'))
  } else {
    ElMessage.warning(i18n.global.t('复制失败，请手动复制'))
  }
}

const copyScript = async (row) => {
  const copied = await copyToClipboard(row.content)
  if (copied) {
    ElMessage.success(i18n.global.t('已复制'))
  } else {
    ElMessage.warning(i18n.global.t('复制失败，请手动复制'))
  }
}

const deleteScript = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除话术 "${row.title}"？`, '确认', { type: 'warning' })
    await deleteScriptApi(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    refreshData()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(i18n.global.t('删除失败'))
  }
}

onMounted(() => { loadCategories(); refreshData() })
</script>

<style scoped lang="scss">
.script-template-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
