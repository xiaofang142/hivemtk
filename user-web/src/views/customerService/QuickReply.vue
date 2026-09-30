<template>
  <div class="quick-reply-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('快捷回复') }}</h2>
        <p class="subtitle">{{ $t('维护客服常用话术与按渠道分组的回复模板') }}</p>
      </div>
      <el-button type="primary" @click="openCreate">
        <el-icon><Plus /></el-icon>
        {{ $t('新增快捷回复') }}
      </el-button>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-header">
          <el-space>
            <el-select v-model="filterCategory" :placeholder="$t('按分类筛选')" clearable style="width: 180px">
              <!-- categories 的后端契约是 []string（service/quick_reply.go: GetCategories 返回
                   []string，实测 /api/quick-replies/categories 返回 ["R2-闭环测试话术", ...]），
                   这里原先却按对象取 c.code / c.name，两个字段在字符串上都是 undefined，
                   于是每个选项的 label 与 value 全是 undefined：Vue 每次渲染报
                   "Invalid prop: type check failed for prop value ... got Undefined"，
                   且该筛选下拉实际渲染成空白、无法选中任何分类。 -->
              <el-option
                v-for="c in categories"
                :key="c"
                :label="c"
                :value="c"
              />
            </el-select>
            <el-input v-model="search" :placeholder="$t('搜索标题/内容')" clearable style="width: 220px" />
          </el-space>
        </div>
      </template>

      <el-table :data="filtered" v-loading="loading" stripe>
        <el-table-column prop="title" label="标题" min-width="160" />
        <el-table-column prop="category" label="分类" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ row.category || '未分类' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="content" label="内容" min-width="280" show-overflow-tooltip />
        <el-table-column label="适用渠道" width="120">
          <template #default="{ row }">
            {{ getChannelLabel(row.channel) }}
          </template>
        </el-table-column>
        <el-table-column prop="sort_order" label="排序" width="80" align="center" />
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑快捷回复' : '新增快捷回复'" width="560px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="标题" required>
          <el-input v-model="form.title" />
        </el-form-item>
        <el-form-item label="分类">
          <el-input v-model="form.category" placeholder="如：问候/报价/跟进" />
        </el-form-item>
        <el-form-item label="内容" required>
          <el-input v-model="form.content" type="textarea" :rows="5" />
        </el-form-item>
        <el-form-item label="适用渠道">
          <el-select v-model="form.channel" clearable style="width: 100%">
            <el-option label="通用" value="" />
            <el-option label="WhatsApp" value="whatsapp" />
            <el-option label="企业微信" value="wecom" />
            <el-option label="飞书" value="feishu" />
            <el-option label="Telegram" value="telegram" />
            <el-option label="邮件" value="email" />
            <el-option label="短信" value="sms" />
          </el-select>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" :max="999" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit">保存</el-button>
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
  getQuickReplies,
  getQuickReplyCategories,
  createQuickReply,
  updateQuickReply,
  deleteQuickReply
} from '@/api/customerService.js'
import { getChannelLabel } from '@/constants/channel'

const loading = ref(false)
const search = ref('')
const filterCategory = ref('')
const replies = ref([])
const categories = ref([])

const dialogVisible = ref(false)
const isEdit = ref(false)
const form = reactive({
  id: null,
  title: '',
  category: '',
  content: '',
  channel: '',
  sort: 0
})

const filtered = computed(() => {
  let list = replies.value
  if (filterCategory.value) list = list.filter((r) => r.category === filterCategory.value)
  if (search.value) {
    const kw = search.value.toLowerCase()
    list = list.filter(
      (r) => r.title?.toLowerCase().includes(kw) || r.content?.toLowerCase().includes(kw)
    )
  }
  return list
})

const load = async () => {
  loading.value = true
  try {
    const [rRes, cRes] = await Promise.all([
      getQuickReplies(),
      getQuickReplyCategories().catch(() => ({ data: [] }))
    ])
    replies.value = Array.isArray(rRes) ? rRes : (rRes?.list || [])
    const cData = Array.isArray(cRes) ? cRes : (cRes?.list || [])
    categories.value = cData
  } catch (e) {
    ElMessage.error(i18n.global.t('加载快捷回复失败'))
    replies.value = []
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  isEdit.value = false
  Object.assign(form, { id: null, title: '', category: '', content: '', channel: '', sort: 0 })
  dialogVisible.value = true
}

const openEdit = (row) => {
  isEdit.value = true
  Object.assign(form, { ...row })
  dialogVisible.value = true
}

const submit = async () => {
  if (!form.title || !form.content) {
    ElMessage.warning(i18n.global.t('请填写标题和内容'))
    return
  }
  const payload = {
    title: form.title,
    category: form.category,
    content: form.content,
    channel: form.channel,
    sort_order: form.sort
  }
  try {
    if (isEdit.value) {
      await updateQuickReply(form.id, payload)
      ElMessage.success(i18n.global.t('已更新'))
    } else {
      await createQuickReply(payload)
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
    await ElMessageBox.confirm(`确定删除 "${row.title}"？`, '确认', { type: 'warning' })
    await deleteQuickReply(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    await load()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(i18n.global.t('删除失败'))
  }
}

onMounted(() => load())
</script>

<style scoped lang="scss">
.quick-reply-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
