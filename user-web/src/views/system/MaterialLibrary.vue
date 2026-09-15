<template>
  <div class="material-library">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('素材库管理') }}</span>
          <div class="header-actions">
            <el-button type="primary" @click="handleUpload">{{ $t('上传素材') }}</el-button>
            <el-button type="success" @click="handleCreateCategory">{{ $t('新建分类') }}</el-button>
          </div>
        </div>
      </template>

      
      <div class="search-bar">
        <el-form :inline="true" :model="searchForm">
          <el-form-item label="分类">
            <el-select v-model="searchForm.categoryId" placeholder="全部分类" clearable>
              <el-option label="全部分类" value="" />
              <el-option
                v-for="category in categories"
                :key="category.id"
                :label="category.name"
                :value="category.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="类型">
            <el-select v-model="searchForm.type" placeholder="全部类型" clearable>
              <el-option label="全部类型" value="" />
              <el-option label="图片" value="image" />
              <el-option label="视频" value="video" />
              <el-option label="音频" value="audio" />
              <el-option label="文档" value="document" />
            </el-select>
          </el-form-item>
          <el-form-item label="关键词">
            <el-input v-model="searchForm.keyword" placeholder="请输入素材名称" clearable />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="handleSearch">搜索</el-button>
            <el-button @click="handleReset">重置</el-button>
          </el-form-item>
        </el-form>
      </div>

      
      <div class="material-grid" v-loading="loading">
        <div
          v-for="material in materialList"
          :key="material.id"
          class="material-item"
          role="button"
          tabindex="0"
          @click="handleSelect(material)"
          @keydown.enter.prevent="handleSelect(material)"
          @keydown.space.prevent="handleSelect(material)"
        >
          <div class="material-preview">
            <img
              v-if="material.type === 'image'"
              :src="material.url"
              :alt="material.name"
              @error="handleImageError"
            />
            <div v-else class="material-icon">
              <el-icon :size="48">
                <Document v-if="material.type === 'document'" />
                <VideoPlay v-else-if="material.type === 'video'" />
                <Headset v-else-if="material.type === 'audio'" />
                <Document v-else />
              </el-icon>
            </div>
            <div class="material-actions">
              <el-button type="primary" link @click.stop="handleCopyUrl(material.url)">复制链接</el-button>
              <el-button type="danger" link @click.stop="handleDelete(material)">删除</el-button>
            </div>
          </div>
          <div class="material-info">
            <div class="material-name">{{ material.name }}</div>
            <div class="material-meta">
              <span>{{ formatFileSize(material.size) }}</span>
              <span>{{ getTypeLabel(material.type) }}</span>
              <span>使用{{ material.usageCount }}次</span>
            </div>
          </div>
        </div>
      </div>

      
      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.current"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-sizes="[12, 24, 48, 96]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>

    
    <el-dialog
      title="上传素材"
      v-model="uploadDialogVisible"
      width="600px"
      @close="handleUploadClose"
    >
      <el-form ref="uploadFormRef" :model="uploadForm" label-width="100px">
        <el-form-item label="选择分类" prop="categoryId">
          <el-select v-model="uploadForm.categoryId" placeholder="请选择分类">
            <el-option
              v-for="category in categories"
              :key="category.id"
              :label="category.name"
              :value="category.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="上传文件">
          <el-upload
            ref="uploadRef"
            drag
            action="#"
            :auto-upload="false"
            :multiple="true"
            :file-list="fileList"
            :on-change="handleFileChange"
            :before-upload="beforeUpload"
            accept="image/*,video/*,audio/*,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx"
          >
            <el-icon class="el-icon--upload"><upload-filled /></el-icon>
            <div class="el-upload__text">
              拖拽文件到此处或 <em>点击上传</em>
            </div>
            <template #tip>
              <div class="el-upload__tip">
                支持图片、视频、音频、文档格式，单个文件不超过10MB
              </div>
            </template>
          </el-upload>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="uploadDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleUploadSubmit">开始上传</el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      title="分类管理"
      v-model="categoryDialogVisible"
      width="500px"
    >
      <div class="category-list">
        <div
          v-for="category in categories"
          :key="category.id"
          class="category-item"
        >
          <span>{{ category.name }}</span>
          <div class="category-actions">
            <el-button type="primary" link @click="handleEditCategory(category)">编辑</el-button>
            <el-button type="danger" link @click="handleDeleteCategory(category)">删除</el-button>
          </div>
        </div>
      </div>

      <div class="category-form" v-if="showCategoryForm">
        <el-input
          v-model="categoryForm.name"
          placeholder="请输入分类名称"
          style="width: 200px; margin-right: 10px"
        />
        <el-button type="primary" @click="handleSaveCategory">保存</el-button>
        <el-button @click="handleCancelCategory">取消</el-button>
      </div>

      <div class="category-add" v-else>
        <el-button type="primary" link @click="handleAddCategory">+ 新增分类</el-button>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, VideoPlay, Headset, UploadFilled } from '@element-plus/icons-vue'
import {
  getMaterialList,
  uploadMaterial,
  deleteMaterial,
  getMaterialCategories,
  createMaterialCategory,
  updateMaterialCategory,
  deleteMaterialCategory
} from '@/api/material'

const loading = ref(false)
const materialList = ref([])
const categories = ref([])
const uploadDialogVisible = ref(false)
const categoryDialogVisible = ref(false)
const showCategoryForm = ref(false)
const uploadRef = ref(null)
const uploadFormRef = ref(null)

const emit = defineEmits(['select'])

const searchForm = reactive({
  categoryId: '',
  type: '',
  keyword: ''
});

const uploadForm = reactive({
  categoryId: ''
})

const categoryForm = reactive({
  id: null,
  name: ''
})

const fileList = ref([])

const pagination = reactive({
  current: 1,
  pageSize: 12,
  total: 0
})

const getTypeLabel = (type) => {
  const labels = {
    image: '图片',
    video: '视频',
    audio: '音频',
    document: '文档'
  }
  return labels[type] || '其他'
}

const formatFileSize = (size) => {
  if (size < 1024) return size + ' B'
  if (size < 1024 * 1024) return (size / 1024).toFixed(1) + ' KB'
  if (size < 1024 * 1024 * 1024) return (size / (1024 * 1024)).toFixed(1) + ' MB'
  return (size / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}

const loadMaterialList = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.current,
      limit: pagination.pageSize,
      ...searchForm
    }
    const response = await getMaterialList(params)
    materialList.value = (response && response.list) || []
    pagination.total = (response && response.total) || 0
  } catch (error) {
    if (error.response?.status !== 401) {
      ElMessage.error(i18n.global.t('获取素材列表失败'))
    }
  } finally {
    loading.value = false
  }
}

const loadCategories = async () => {
  try {
    const response = await getMaterialCategories()
    categories.value = (response && response.list) || []
  } catch (error) {
    if (error.response?.status !== 401) {
      ElMessage.error(i18n.global.t('获取分类列表失败'))
    }
  }
}

const handleSearch = () => {
  pagination.current = 1
  loadMaterialList()
}

const handleReset = () => {
  searchForm.categoryId = ''
  searchForm.type = ''
  searchForm.keyword = ''
  handleSearch()
}

const handleSizeChange = (size) => {
  pagination.pageSize = size
  loadMaterialList()
}

const handleCurrentChange = (page) => {
  pagination.current = page
  loadMaterialList()
}

const handleUpload = () => {
  uploadDialogVisible.value = true
}

const handleCreateCategory = () => {
  categoryDialogVisible.value = true
}

const handleFileChange = (file, files) => {
  fileList.value = files
}

const beforeUpload = (file) => {
  const maxSize = 10 * 1024 * 1024;
  if (file.size > maxSize) {
    ElMessage.error(i18n.global.t('文件大小不能超过10MB'))
    return false
  }
  return true
}

const handleUploadSubmit = async () => {
  if (!uploadForm.categoryId) {
    ElMessage.error(i18n.global.t('请选择分类'))
    return
  }
  
  if (fileList.value.length === 0) {
    ElMessage.error(i18n.global.t('请选择文件'))
    return
  }

  try {
    for (const file of fileList.value) {
      const formData = new FormData()
      formData.append('file', file.raw)
      formData.append('category_id', uploadForm.categoryId)
      
      await uploadMaterial(formData)
    }
    
    ElMessage.success(i18n.global.t('上传成功'))
    uploadDialogVisible.value = false
    loadMaterialList()
  } catch (error) {
    ElMessage.error(i18n.global.t('上传失败'))
  }
}

const handleUploadClose = () => {
  fileList.value = []
  uploadForm.categoryId = ''
}

const handleCopyUrl = (url) => {
  navigator.clipboard.writeText(url).then(() => {
    ElMessage.success(i18n.global.t('链接已复制到剪贴板'))
  }).catch(() => {
    ElMessage.error(i18n.global.t('复制失败'))
  })
}

const handleDelete = async (material) => {
  try {
    await ElMessageBox.confirm('确定要删除该素材吗？', '提示', {
      type: 'warning'
    })
    
    await deleteMaterial(material.id)
    ElMessage.success(i18n.global.t('删除成功'))
    loadMaterialList()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(i18n.global.t('删除失败'))
    }
  }
}

const handleImageError = (event) => {
  event.target.src = 'data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iNDgiIGhlaWdodD0iNDgiIHZpZXdCb3g9IjAgMCA0OCA0OCIgZmlsbD0ibm9uZSIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj4KPHJlY3Qgd2lkdGg9IjQ4IiBoZWlnaHQ9IjQ4IiBmaWxsPSIjRjBGMkY1Ii8+CjxwYXRoIGQ9Ik0yNCAxNkMxOS41ODE3IDE2IDE2IDE5LjU4MTcgMTYgMjRDMTYgMjguNDE4MyAxOS41ODE3IDMyIDI0IDMyQzI4LjQxODMgMzIgMzIgMjguNDE4MyAzMiAyNEMzMiAxOS41ODE3IDI4LjQxODMgMTYgMjQgMTZaIiBmaWxsPSIjQ0NDQ0NDIi8+CjxwYXRoIGQ9Ik0yNCAxOVYyOU0xOSAyNEgyOU0yNCAxNkMxOS41ODE3IDE2IDE2IDE5LjU4MTcgMTYgMjRDMTYgMjguNDE4MyAxOS41ODE3IDMyIDI0IDMyQzI4LjQxODMgMzIgMzIgMjguNDE4MyAzMiAyNEMzMiAxOS41ODE3IDI4LjQxODMgMTYgMjQgMTZaIiBzdHJva2U9IiM5OTk5OTkiIHN0cm9rZS13aWR0aD0iMiIvPgo8L3N2Zz4K'
}

const handleSelect = (material) => {
  emit('select', material);
}

const handleEditCategory = (category) => {
  categoryForm.id = category.id
  categoryForm.name = category.name
  showCategoryForm.value = true
}

const handleDeleteCategory = async (category) => {
  try {
    await ElMessageBox.confirm('确定要删除该分类吗？', '提示', {
      type: 'warning'
    })
    
    await deleteMaterialCategory(category.id)
    ElMessage.success(i18n.global.t('删除成功'))
    loadCategories()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(i18n.global.t('删除失败'))
    }
  }
}

const handleAddCategory = () => {
  categoryForm.id = null
  categoryForm.name = ''
  showCategoryForm.value = true
}

const handleSaveCategory = async () => {
  if (!categoryForm.name.trim()) {
    ElMessage.error(i18n.global.t('请输入分类名称'))
    return
  }
  
  try {
    if (categoryForm.id) {
      await updateMaterialCategory(categoryForm.id, categoryForm)
      ElMessage.success(i18n.global.t('更新成功'))
    } else {
      await createMaterialCategory(categoryForm)
      ElMessage.success(i18n.global.t('创建成功'))
    }
    
    showCategoryForm.value = false
    loadCategories()
  } catch (error) {
    ElMessage.error(categoryForm.id ? '更新失败' : '创建失败')
  }
}

const handleCancelCategory = () => {
  showCategoryForm.value = false
  categoryForm.id = null
  categoryForm.name = ''
}

onMounted(() => {
  loadCategories()
  loadMaterialList()
})
</script>

<style scoped>
.material-library {
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

.search-bar {
  margin-bottom: 20px;
  padding: 15px;
  background-color: #f5f7fa;
  border-radius: 4px;
}

.material-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 20px;
  margin-bottom: 20px;
}

.material-item {
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  overflow: hidden;
  cursor: pointer;
  transition: all 0.3s;
}

.material-item:hover {
  border-color: #4F46E5;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
}

.material-item:hover .material-actions {
  opacity: 1;
}

.material-preview {
  position: relative;
  width: 100%;
  height: 150px;
  background-color: #f5f7fa;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

.material-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.material-icon {
  color: #909399;
}

.material-actions {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  opacity: 0;
  transition: opacity 0.3s;
}

.material-info {
  padding: 12px;
}

.material-name {
  font-size: 14px;
  color: #303133;
  margin-bottom: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.material-meta {
  font-size: 12px;
  color: #909399;
  display: flex;
  justify-content: space-between;
}

.pagination {
  display: flex;
  justify-content: center;
  margin-top: 20px;
}

.category-list {
  max-height: 300px;
  overflow-y: auto;
  margin-bottom: 20px;
}

.category-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px;
  border-bottom: 1px solid #e4e7ed;
}

.category-actions {
  display: flex;
  gap: 10px;
}

.category-form {
  display: flex;
  align-items: center;
  margin-top: 20px;
}

.category-add {
  margin-top: 20px;
  text-align: center;
}
</style>
