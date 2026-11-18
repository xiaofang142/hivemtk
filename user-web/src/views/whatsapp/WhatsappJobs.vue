<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>WhatsApp 群发任务</span>
          <el-button
            type="primary"
            :icon="Plus"
            @click="showCreate = true"
          >
            {{ $t('新建任务') }}
          </el-button>
        </div>
      </template>

      <el-form
        :inline="true"
        :model="search"
        class="search-form"
      >
        <el-form-item label="状态">
          <el-select
            v-model="search.status"
            placeholder="全部"
            clearable
          >
            <el-option
              label="待执行"
              value="pending"
            />
            <el-option
              label="执行中"
              value="running"
            />
            <el-option
              label="已完成"
              value="completed"
            />
            <el-option
              label="已失败"
              value="failed"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :icon="Search"
            @click="load"
          >
            搜索
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="jobs"
        style="width:100%"
        border
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="draft_title"
          label="草稿"
          min-width="200"
        />
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="scope">
            <el-tag :type="getStatusType(scope.row.status)">
              {{ getStatusText(scope.row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="total_count"
          label="总数"
          width="80"
          align="center"
        />
        <el-table-column
          prop="sent_count"
          label="已发"
          width="80"
          align="center"
        />
        <el-table-column
          prop="failed_count"
          label="失败"
          width="80"
          align="center"
        />
        <el-table-column
          prop="created_at"
          label="创建时间"
          width="180"
        >
          <template #default="scope">
            {{ formatDate(scope.row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="150"
          fixed="right"
        >
          <template #default="scope">
            <el-button
              size="small"
              type="primary"
              link
              @click="viewJob(scope.row)"
            >
              详情
            </el-button>
            <el-button
              size="small"
              type="danger"
              link
              @click="confirmDelete(scope.row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.page_size"
          :page-sizes="[10, 20, 50]"
          :total="pagination.total"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="load"
          @current-change="load"
        />
      </div>
    </el-card>

    
    <el-dialog
      v-model="showCreate"
      title="新建群发任务"
      width="500px"
    >
      <el-form
        :model="createForm"
        label-width="100px"
      >
        <el-form-item label="选择草稿">
          <el-select
            v-model="createForm.draft_id"
            placeholder="请选择"
            style="width: 100%"
          >
            <el-option
              v-for="d in drafts"
              :key="d.id"
              :label="d.title"
              :value="d.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="目标号码">
          <el-input
            v-model="createForm.phone_numbers"
            type="textarea"
            :rows="4"
            placeholder="每行一个手机号,或使用英文逗号分隔"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          @click="submit"
        >
          创建
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="showDetail"
      title="任务详情"
      width="500px"
    >
      <el-descriptions
        :column="2"
        border
      >
        <el-descriptions-item label="ID">
          {{ detail.id }}
        </el-descriptions-item>
        <el-descriptions-item label="草稿">
          {{ detail.draft_title }}
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(detail.status)">
            {{ getStatusText(detail.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="总数">
          {{ detail.total_count }}
        </el-descriptions-item>
        <el-descriptions-item label="已发">
          {{ detail.sent_count }}
        </el-descriptions-item>
        <el-descriptions-item label="失败">
          {{ detail.failed_count }}
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">
          {{ formatDate(detail.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="完成时间">
          {{ formatDate(detail.finished_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import api from '@/api/whatsapp'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { getOrderStatusLabel as _getOrderStatusLabel, getOrderStatusTagType as _getOrderStatusTagType } from '@/constants/orderStatus';

const loading = ref(false)
const creating = ref(false)
const jobs = ref([])
const drafts = ref([])
const showCreate = ref(false)
const showDetail = ref(false)
const detail = ref({})

const search = reactive({ status: '' })
const pagination = reactive({ page: 1, page_size: 10, total: 0 })
const createForm = reactive({ draft_id: '', phone_numbers: '' })

const formatDate = (d) => {
  if (!d) return '-'
  const date = new Date(d)
  return isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN', { hour12: false })
}

const getStatusType = (s) => {
  const types = { pending: 'info', running: 'warning', completed: 'success', failed: 'danger' }
  return types[s] || ''
}
const getStatusText = (s) => {
  const texts = { pending: '待执行', running: '执行中', completed: '已完成', failed: '已失败' }
  return texts[s] || s
}

const loadDrafts = async () => {
  try {
    const res = await api.listDrafts()
    drafts.value = Array.isArray(res) ? res : (res?.list || [])
  } catch  {
    ElMessage.error(i18n.global.t('加载草稿列表失败'))
    drafts.value = []
  }
}

const load = async () => {
  loading.value = true
  try {
    const res = await api.listJobs({
      page: pagination.page,
      page_size: pagination.page_size,
      status: search.status
    })
    if (Array.isArray(res)) {
      jobs.value = res
      pagination.total = res.length
    } else if (res && res.list) {
      jobs.value = res.list
      pagination.total = res.total || 0
    } else {
      jobs.value = []
      pagination.total = 0
    }
  } catch  {
    ElMessage.error(i18n.global.t('加载任务失败'))
    jobs.value = []
  } finally {
    loading.value = false
  }
}

const submit = async () => {
  if (!createForm.draft_id) { ElMessage.warning(i18n.global.t('请选择草稿')); return }
  if (!createForm.phone_numbers.trim()) { ElMessage.warning(i18n.global.t('请填写目标号码')); return }
  creating.value = true
  try {
    const phones = createForm.phone_numbers.split(/[\n,;]+/).map(s => s.trim()).filter(Boolean)
    await api.createJob({ draft_id: createForm.draft_id, phone_numbers: phones })
    ElMessage.success(i18n.global.t('任务已创建'))
    showCreate.value = false
    createForm.draft_id = ''
    createForm.phone_numbers = ''
    load()
  } catch (err) {
    ElMessage.error('创建失败: ' + (err.message || '未知错误'))
  } finally {
    creating.value = false
  }
}

const viewJob = async (row) => {
  try {
    const res = await api.getJob(row.id)
    detail.value = res || row
  } catch  {
    detail.value = row
  }
  showDetail.value = true
}

const confirmDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除任务 #${row.id}?`, '提示', { type: 'warning' })
    await api.deleteJob(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    load()
  } catch (err) {
    if (err !== 'cancel' && err?.message) {
      ElMessage.error('删除失败: ' + err.message)
    }
  }
}

onMounted(() => {
  loadDrafts()
  load()
})
</script>

<style scoped>
.page { padding: 20px; }
.card-header { display: flex; justify-content: space-between; align-items: center; font-weight: 600; }
.search-form { margin-bottom: 16px; }
.pagination { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
