<template>
  <div class="session-tag-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('会话标签') }}</h2>
        <p class="subtitle">
          {{ $t('维护客服会话分类标签，用于会话打标、复盘与统计') }}
        </p>
      </div>
      <el-button
        type="primary"
        @click="openCreate"
      >
        <el-icon><Plus /></el-icon>
        {{ $t('新增标签') }}
      </el-button>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-header">
          <el-space>
            <el-select
              v-model="filterGroup"
              :placeholder="$t('按分组筛选')"
              clearable
              style="width: 180px"
            >
              <el-option
                v-for="g in groups"
                :key="g"
                :label="g"
                :value="g"
              />
            </el-select>
            <el-input
              v-model="search"
              :placeholder="$t('搜索标签名')"
              clearable
              style="width: 220px"
            />
          </el-space>
        </div>
      </template>

      <el-table
        v-loading="loading"
        :data="filtered"
        stripe
      >
        <el-table-column
          prop="name"
          label="标签"
          min-width="140"
        >
          <template #default="{ row }">
            <el-tag
              :color="row.color"
              effect="dark"
              style="color: #fff"
            >
              {{ row.name }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="code"
          label="标识"
          min-width="140"
        />
        <el-table-column
          prop="group"
          label="分组"
          width="140"
        />
        <el-table-column
          prop="description"
          label="说明"
          min-width="200"
          show-overflow-tooltip
        />
        <el-table-column
          prop="sort_order"
          label="排序"
          width="80"
          align="center"
        />
        <el-table-column
          label="操作"
          width="180"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="danger"
              @click="handleDelete(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑标签' : '新增标签'"
      width="480px"
    >
      <el-form
        :model="form"
        label-width="100px"
      >
        <el-form-item
          label="标签名"
          required
        >
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item
          label="标识"
          required
        >
          <el-input
            v-model="form.code"
            :disabled="isEdit"
            placeholder="英文/拼音，e.g. vip"
          />
        </el-form-item>
        <el-form-item label="分组">
          <el-input
            v-model="form.group"
            placeholder="如：客户类型/意向度"
          />
        </el-form-item>
        <el-form-item label="颜色">
          <el-color-picker v-model="form.color" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
          />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number
            v-model="form.sort"
            :min="0"
            :max="999"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submit"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  getSessionTags,
  createSessionTag,
  updateSessionTag,
  deleteSessionTag
} from '@/api/customerService.js'

const loading = ref(false)
const search = ref('')
const filterGroup = ref('')
const tags = ref([])

const dialogVisible = ref(false)
const isEdit = ref(false)
const form = reactive({
  id: null,
  name: '',
  code: '',
  group: '',
  color: '#4F46E5',
  description: '',
  sort: 0
})

const groups = computed(() => [...new Set(tags.value.map((t) => t.group).filter(Boolean))])

const filtered = computed(() => {
  let list = tags.value
  if (filterGroup.value) list = list.filter((t) => t.group === filterGroup.value)
  if (search.value) {
    const kw = search.value.toLowerCase()
    list = list.filter((t) => t.name?.toLowerCase().includes(kw) || t.code?.toLowerCase().includes(kw))
  }
  return list
})

const load = async () => {
  loading.value = true
  try {
    const res = await getSessionTags()
    const data = res || []
    tags.value = Array.isArray(data) ? data : data.list || []
  } catch  {
    ElMessage.error(i18n.global.t('加载标签失败'))
    tags.value = []
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  isEdit.value = false
  Object.assign(form, {
    id: null, name: '', code: '', group: '', color: '#4F46E5', description: '', sort: 0
  })
  dialogVisible.value = true
}

const openEdit = (row) => {
  isEdit.value = true
  Object.assign(form, { ...row })
  dialogVisible.value = true
}

const submit = async () => {
  if (!form.name || !form.code) {
    ElMessage.warning(i18n.global.t('请填写标签名和标识'))
    return
  }
  const payload = {
    name: form.name,
    code: form.code,
    group: form.group,
    color: form.color,
    description: form.description,
    sort_order: form.sort
  }
  try {
    if (isEdit.value) {
      await updateSessionTag(form.id, payload)
      ElMessage.success(i18n.global.t('已更新'))
    } else {
      await createSessionTag(payload)
      ElMessage.success(i18n.global.t('已创建'))
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.message || '操作失败')
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除标签 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteSessionTag(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    await load()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(i18n.global.t('删除失败'))
  }
}

onMounted(() => load())
</script>

<style scoped lang="scss">
.session-tag-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
