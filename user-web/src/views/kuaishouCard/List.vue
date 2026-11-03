<template>
  <div class="kuaishou-card-container">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('快手卡片管理') }}</span>
          <div class="header-actions">
            <el-button
              type="primary"
              icon="DataAnalysis"
              @click="goToStats"
            >
              {{ $t('统计分析') }}
            </el-button>
            <el-button
              type="primary"
              icon="Plus"
              @click="handleAdd"
            >
              {{ $t('添加卡片') }}
            </el-button>
          </div>
        </div>
      </template>

      
      <el-form
        :inline="true"
        :model="searchForm"
        class="search-form"
      >
        <el-form-item label="关键词">
          <el-input
            v-model="searchForm.keyword"
            placeholder="标题、描述或标签"
            clearable
          />
        </el-form-item>
        <el-form-item label="状态">
          <el-select
            v-model="searchForm.is_active"
            placeholder="请选择"
            clearable
          >
            <el-option
              label="激活"
              :value="true"
            />
            <el-option
              label="禁用"
              :value="false"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="handleSearch"
          >
            搜索
          </el-button>
          <el-button @click="resetSearch">
            重置
          </el-button>
        </el-form-item>
      </el-form>

      
      <el-table
        v-loading="loading"
        :data="tableData"
        style="width: 100%"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="title"
          label="标题"
          min-width="200"
        >
          <template #default="scope">
            <span class="title-text">{{ scope.row.title }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="image_url"
          label="图片"
          width="120"
        >
          <template #default="scope">
            <el-image
              v-if="scope.row.image_url"
              :src="scope.row.image_url"
              :preview-src-list="[scope.row.image_url]"
              fit="cover"
              style="width: 80px; height: 60px; border-radius: 4px;"
            >
              <template #error>
                <div class="img-error-fallback">
                  {{ $t('图片加载失败') }}
                </div>
              </template>
            </el-image>
            <span
              v-else
              class="no-image"
            >无图片</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="description"
          label="描述"
          min-width="300"
        >
          <template #default="scope">
            <el-tooltip
              :content="scope.row.description"
              placement="top"
            >
              <div class="description-text">
                {{ scope.row.description }}
              </div>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column
          prop="redirect_url"
          label="跳转链接"
          min-width="200"
        >
          <template #default="scope">
            <el-tooltip
              :content="scope.row.redirect_url"
              placement="top"
            >
              <div class="redirect-url">
                {{ scope.row.redirect_url }}
              </div>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column
          prop="view_count"
          label="浏览量"
          width="100"
        />
        <el-table-column
          prop="like_count"
          label="点赞量"
          width="100"
        />
        <el-table-column
          prop="share_count"
          label="分享量"
          width="100"
        />
        <el-table-column
          prop="is_active"
          label="状态"
          width="100"
        >
          <template #default="scope">
            <el-switch
              v-model="scope.row.is_active"
              active-color="#2684FF"
              inactive-color="#dcdfe6"
              @change="handleStatusChange(scope.row)"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="创建时间"
          width="160"
        >
          <template #default="scope">
            {{ formatDate(scope.row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="280"
          fixed="right"
        >
          <template #default="scope">
            <el-button
              size="small"
              type="primary"
              @click="handleEdit(scope.row)"
            >
              编辑
            </el-button>
            <el-button
              size="small"
              type="info"
              @click="handleStats(scope.row)"
            >
              统计
            </el-button>
            <el-button
              size="small"
              type="warning"
              @click="handleCopyLink(scope.row)"
            >
              复制链接
            </el-button>
            <el-button
              size="small"
              type="danger"
              @click="handleDelete(scope.row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无数据" />
        </template>
      </el-table>

      
      <div class="pagination-container">
        <el-pagination
          v-model:current-page="pagination.currentPage"
          v-model:page-size="pagination.pageSize"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          :total="pagination.total"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>

    
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="1000px"
      :close-on-click-modal="false"
    >
      <div class="dialog-content">
        <div class="form-section">
          <el-form
            ref="formRef"
            :model="formData"
            label-width="100px"
            :rules="rules"
          >
            <el-form-item
              label="卡片标题"
              prop="title"
            >
              <el-input
                v-model="formData.title"
                placeholder="请输入卡片标题"
                maxlength="100"
                show-word-limit
              />
            </el-form-item>
            <el-form-item
              label="卡片描述"
              prop="description"
            >
              <el-input
                v-model="formData.description"
                placeholder="请输入卡片描述"
                type="textarea"
                :rows="4"
                maxlength="500"
                show-word-limit
              />
            </el-form-item>
            <el-form-item
              label="卡片图片"
              prop="image_url"
            >
              <el-input
                v-model="formData.image_url"
                placeholder="请输入图片URL"
                maxlength="500"
                show-word-limit
              />
              <div class="image-preview">
                <el-image
                  v-if="formData.image_url"
                  :src="formData.image_url"
                  fit="cover"
                  style="width: 200px; height: 150px; border-radius: 4px; margin-top: 10px;"
                >
                  <template #error>
                    <div class="img-error-fallback">
                      {{ $t('图片加载失败') }}
                    </div>
                  </template>
                </el-image>
              </div>
            </el-form-item>
            <el-form-item
              label="跳转链接"
              prop="redirect_url"
            >
              <el-input
                v-model="formData.redirect_url"
                placeholder="请输入跳转链接"
                maxlength="500"
                show-word-limit
              />
            </el-form-item>
            <el-form-item
              label="域名池"
              prop="domain_pool_id"
            >
              <el-select 
                v-model="formData.domain_pool_id" 
                placeholder="请选择域名池" 
                clearable
                style="width: 100%"
              >
                <el-option
                  v-for="item in domainPoolOptions"
                  :key="item.id"
                  :label="item.name"
                  :value="item.id"
                >
                  <span style="float: left">{{ item.name }}</span>
                  <span style="float: right; color: #8492a6; font-size: 13px">{{ item.domain }}</span>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item
              label="标签"
              prop="tags"
            >
              <el-input 
                v-model="formData.tags" 
                placeholder="请输入标签，多个标签用逗号分隔" 
                maxlength="255"
                show-word-limit
              />
            </el-form-item>
            <el-form-item label="是否激活">
              <el-switch v-model="formData.is_active" />
            </el-form-item>
          </el-form>
        </div>
        <div class="preview-section">
          <KuaishouCardPreview :card-data="formData" />
        </div>
      </div>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="handleSubmit"
        >
          确认
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { useRouter } from 'vue-router'
import KuaishouCardPreview from '@/components/KuaishouCardPreview.vue'
import { domainPoolApi } from '@/api/domainPool'
import * as kuaishouCardApi from '@/api/kuaishouCard'
import { copyToClipboard } from '@/utils/clipboard.js'
import { isUserReportedError } from '@/utils/errorReporting'

const formatDate = (date, format) => {
  if (!date) return ''
  const d = new Date(date)
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  
  if (format === 'YYYY-MM-DD') {
    return `${year}-${month}-${day}`
  }
  return new Date(date).toLocaleDateString()
};

const router = useRouter();

const loading = ref(false);
const dialogVisible = ref(false)
const formRef = ref(null)
const currentCardId = ref(null)

const domainPoolOptions = ref([]);

const searchForm = reactive({
  keyword: '',
  is_active: null
});

const pagination = reactive({
  currentPage: 1,
  pageSize: 10,
  total: 0
});

const tableData = ref([]);

const formData = reactive({
  title: '',
  description: '',
  image_url: '',
  redirect_url: '',
  tags: '',
  is_active: true,
  domain_pool_id: null
});

const rules = {
  title: [
    { required: true, message: i18n.global.t('请输入卡片标题'), trigger: 'blur' },
    { min: 2, max: 100, message: i18n.global.t('标题长度在 2 到 100 个字符'), trigger: 'blur' }
  ],
  description: [
    { required: true, message: i18n.global.t('请输入卡片描述'), trigger: 'blur' },
    { min: 5, max: 500, message: i18n.global.t('描述长度在 5 到 500 个字符'), trigger: 'blur' }
  ],
  image_url: [
    { required: true, message: i18n.global.t('请输入图片URL'), trigger: 'blur' },
    { type: 'url', message: i18n.global.t('请输入有效的图片URL'), trigger: 'blur' }
  ],
  redirect_url: [
    { required: true, message: i18n.global.t('请输入跳转链接'), trigger: 'blur' },
    { type: 'url', message: i18n.global.t('请输入有效的URL'), trigger: 'blur' }
  ],
  tags: [
    { max: 255, message: i18n.global.t('标签长度不能超过255个字符'), trigger: 'blur' }
  ]
};

const dialogTitle = computed(() => {
  return currentCardId.value ? '编辑快手卡片' : '添加快手卡片'
});

const loadData = async () => {
  loading.value = true
  try {
    const params = {
      ...searchForm,
      page: pagination.currentPage,
      page_size: pagination.pageSize
    }
    const res = await kuaishouCardApi.getKuaishouCardList(params)
    tableData.value = res.list || [];
    pagination.total = res.total || 0
  } catch (error) {
    // 拦截器已弹过后端文案，这里只在没弹过时兜底
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('获取数据失败'))
    console.warn('获取快手卡片列表失败:', error)
  } finally {
    loading.value = false
  }
};

const handleSearch = () => {
  pagination.currentPage = 1
  loadData()
}

const resetSearch = () => {
  Object.keys(searchForm).forEach(key => {
    searchForm[key] = ''
  })
  searchForm.is_active = null
  handleSearch()
}

const handleSizeChange = (size) => {
  pagination.pageSize = size
  loadData()
}

const handleCurrentChange = (current) => {
  pagination.currentPage = current
  loadData()
}

const handleAdd = () => {
  currentCardId.value = null
  resetForm()
  dialogVisible.value = true
}

const handleEdit = (row) => {
  currentCardId.value = row.id
  formData.title = row.title
  formData.description = row.description
  formData.image_url = row.image_url
  formData.redirect_url = row.redirect_url
  formData.tags = row.tags || ''
  formData.is_active = row.is_active
  formData.domain_pool_id = row.domain_pool_id || null
  dialogVisible.value = true
}

const handleStats = (row) => {
  router.push(`/kuaishou-card-stats/${row.id}`)
}

const handleCopyLink = async (row) => {
  const cardLink = `${window.location.origin}/kuaishou/card/${row.id}`;
  const copied = await copyToClipboard(cardLink)
  if (copied) {
    ElMessage.success(i18n.global.t('链接已复制到剪贴板'))
  } else {
    ElMessage.error(i18n.global.t('复制链接失败'))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除卡片「${row.title}」吗？`,
      '删除确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
    const res = await kuaishouCardApi.deleteKuaishouCard(row.id)
    ElMessage.success(i18n.global.t('删除成功'));
    loadData()
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('删除失败'))
  }
}

const handleStatusChange = async (row) => {
  try {
    // 后端 KuaishouCardUpdateRequest.Title 为 required，漏传会 400
    const res = await kuaishouCardApi.updateKuaishouCard({
      id: row.id,
      title: row.title,
      description: row.description || '',
      image_url: row.image_url || '',
      redirect_url: row.redirect_url || '',
      domain_pool_id: row.domain_pool_id || 0,
      tags: row.tags || '',
      like_count: row.like_count || 0,
      share_count: row.share_count || 0,
      view_count: row.view_count || 0,
      is_active: row.is_active
    });
  } catch (error) {
    row.is_active = !row.is_active
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('更新失败'))
  }
}

const handleSubmit = async () => {
  if (!formRef.value) return
  // 表单校验失败时 validate() reject 的是字段对象（不是 false），
  // 之前直接落进 catch 会误弹「操作失败」——校验不通过不应报错，只显示行内提示。
  const valid = await formRef.value.validate().then(() => true).catch(() => false)
  if (!valid) return
  try {
    let res
    if (currentCardId.value) {
      res = await kuaishouCardApi.updateKuaishouCard({
        ...formData,
        id: currentCardId.value
      })
    } else {
      res = await kuaishouCardApi.createKuaishouCard(formData)
    }
    ElMessage.success(currentCardId.value ? '更新成功' : '创建成功');
    dialogVisible.value = false
    loadData()
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('操作失败'))
  }
}



const handleImageUrlChange = () => {
  if (formRef.value && formData.image_url) {
    formRef.value.validateField('image_url')
  }
};

const resetForm = () => {
  if (formRef.value) {
    formRef.value.resetFields()
  }
  formData.title = ''
  formData.description = ''
  formData.image_url = ''
  formData.redirect_url = ''
  formData.tags = ''
  formData.is_active = true
  formData.domain_pool_id = null
}

const getDomainPoolList = async () => {
  try {
    const res = await domainPoolApi.getList({ page: 1, page_size: 100 })
    domainPoolOptions.value = res.list || [];
  } catch (error) {
    ElMessage.error(i18n.global.t('获取域名池列表失败'))
    console.error('获取域名池列表失败:', error)
  }
};

const goToStats = () => {
  router.push('/kuaishou/stats')
}

onMounted(async () => {
  await getDomainPoolList();
  loadData()
});
</script>

<style scoped>
.kuaishou-card-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 10px;
}

.search-form {
  margin-bottom: 20px;
  padding: 10px;
  background-color: #f5f7fa;
  border-radius: 4px;
}

.pagination-container {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

.title-text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.4;
}

.description-text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.4;
}

.redirect-url {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.4;
}

.no-image {
  display: inline-block;
  width: 80px;
  height: 60px;
  background-color: #f0f0f0;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #999;
  font-size: 12px;
}

.dialog-content {
  display: flex;
  gap: 20px;
}

.form-section {
  flex: 1;
}

.preview-section {
  width: 300px;
  flex-shrink: 0;
}

.image-preview {
  margin-top: 10px;
}

/* 保留图片样式 */
.image-preview .el-image {
  border: 1px solid #d9d9d9;
}

.short-link-container {
  padding: 20px 0;
}
</style>