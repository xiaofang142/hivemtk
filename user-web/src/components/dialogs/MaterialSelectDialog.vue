<template>
  <el-dialog  :model-value="visible" @update:model-value="$emit('update:visible', $event)" :title="$t('素材库选择')" width="800px" class="material-library-dialog">
    <div class="material-library-content" style="display: flex;">
      <div style="width:200px; height: 400px;overflow-y: auto;">
          <div class="material-list">
            <div class="material-item"  @click="handleCategoryClick(0)" :class="{ active: 0 === searchCategoryId }">
                <div class="material-info">
                  <div class="material-name">{{ $t('全部') }}</div>
                </div>
            </div>
          </div>
          <div class="material-list">
            <div v-for="category in categorys" :key="category.id" class="material-item"  @click="handleCategoryClick(category.id)" :class="{ active: category.id === searchCategoryId }">
                <div class="material-info">
                  <div class="material-name">{{ category.name }}</div>
                </div>
            </div>
          </div>
      </div>
      <div style="flex:1;height: 400px;overflow-y: auto;">
        <el-input  v-model="searchKeyword" clearable :placeholder="$t('输入文件名搜索')" prefix-icon="Search" />
        <el-checkbox-group v-model="selectedMaterials">
          <div class="material-list">
            <div
              v-for="material in filteredMaterials"
              :key="material.id"
              class="material-item"
            >
              <el-checkbox :label="material.id" class="material-checkbox">
                <div class="material-info">
                  <div class="material-name">{{ material.filename }}</div>
                  <div class="material-details">
                    <span class="file-size">{{ material.filesize }}MB</span>
                    <span class="upload-time">{{ material.upload_time }}</span>
                  </div>
                </div>
              </el-checkbox>
            </div>
          </div>
        </el-checkbox-group>
      </div>
    </div>
    <template #footer>
      <div class="dialog-footer">
        <el-button @click="handleCancel">{{ $t('取消') }}</el-button>
        <el-button type="primary" @click="handleConfirm">{{ $t('确定') }}</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import i18n from '@/i18n'

import { defineProps, defineEmits, ref, computed,reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const categorys = computed(() => appStore.categorys)

defineProps({
  visible: Boolean
})

const emit = defineEmits(['update:visible', 'confirm'])

const selectedMaterials = ref([])

const handleCancel = () => {
  emit('update:visible', false)
}

const handleConfirm = () => {
  const ids = selectedMaterials.value
  const list = []
  if(ids && ids.length > 0){
    for (let i = ids.length - 1; i >= 0; i--) {
      const id = ids[i]
      const material = appStore.materials.find(m => m.id === id)
      if (material) {
        list.push(material)
      }
    }
  }
  emit('confirm', list)
  emit('update:visible', false)
  selectedMaterials.value = []
}

const handleCategoryClick = (id) => {
  searchCategoryId.value = id
}

const searchKeyword = ref('');
const searchCategoryId = ref(0)
const filteredMaterials = computed(() => {
  const keyword = searchKeyword.value.toLowerCase()
  const categoryid = searchCategoryId.value
  if (!keyword && categoryid < 1) return appStore.materials
  return appStore.materials.filter(material => 
    (categoryid < 1 || (material.category_id == categoryid)) && (!keyword || (material.filename.toLowerCase().includes(keyword)) )
  );
})
</script>

<style scoped>
.active{
  background-color: aquamarine;
}
/* 样式从原文件迁移 */
.material-library-content {
  max-height: 400px;
  overflow-y: auto;
}

.material-item {
  padding: 10px;
  border-bottom: 1px solid #f0f0f0;
  cursor: pointer;
}

.material-info {
  margin-left: 10px;
}

.material-name {
  font-weight: 500;
  margin-bottom: 5px;
}

.material-details {
  font-size: 12px;
  color: #909399;
}

.file-size {
  margin-right: 15px;
}
</style>