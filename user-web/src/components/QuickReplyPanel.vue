<template>
  <div class="quick-reply">
    <el-input
      v-model="searchText"
      :placeholder="$t('搜索快捷回复（支持 {{customer.name}} 变量）')"
      clearable
      class="quick-reply-search"
    >
      <template #prefix>
        <el-icon><Search /></el-icon>
      </template>
    </el-input>

    <el-scrollbar height="calc(100vh - 240px)">
      <div
        v-for="group in filteredGroups"
        :key="group.id"
        class="quick-reply-group"
      >
        <div class="group-title">{{ group.name }}</div>
        <el-card
          v-for="item in group.items"
          :key="item.id"
          shadow="hover"
          class="quick-reply-item"
          @click="onSelect(item)"
        >
          <div class="item-content">
            <div class="item-text">{{ item.content }}</div>
            <div v-if="item.variables && item.variables.length" class="item-vars">
              <el-tag v-for="v in item.variables" :key="v" size="small" type="info">
                {{ `\{\{${v}\}\}` }}
              </el-tag>
            </div>
          </div>
        </el-card>
      </div>
    </el-scrollbar>

    <!-- 变量面板 -->
    <el-dialog v-model="previewVisible" :title="$t('预览')" width="500px">
      <div v-if="previewResult" class="preview-content">
        <pre>{{ previewResult }}</pre>
      </div>
      <div v-else class="preview-empty">{{ $t('无可用变量') }}</div>
    </el-dialog>
  </div>
</template>

<script setup>

import { ref, computed } from 'vue';
import { Search } from '@element-plus/icons-vue'
import { render, BUILTIN_VARIABLES } from '@/utils/templateRender'

const props = defineProps({
  groups: { type: Array, default: () => [] },
  context: { type: Object, default: () => ({}) }
})

const emit = defineEmits(['select'])

const searchText = ref('')
const previewVisible = ref(false)
const previewResult = ref('')

const filteredGroups = computed(() => {
  if (!searchText.value) return props.groups
  const q = searchText.value.toLowerCase()
  return props.groups
    .map((g) => ({
      ...g,
      items: g.items.filter(
        (i) => i.content.toLowerCase().includes(q) || (i.variables || []).some((v) => v.includes(q))
      )
    }))
    .filter((g) => g.items.length > 0)
})

function onSelect(item) {
  try {
    const rendered = render(item.content, props.context, { missing: '{{' + (item.variables?.[0] || '') + '}}' })
    previewResult.value = rendered
    previewVisible.value = true
    setTimeout(() => emit('select', rendered, item), 100);
  } catch (e) {
    console.error('模板渲染失败', e)
  }
}

</script>

<style scoped>
.quick-reply {
  background: #fff;
  border-radius: 8px;
  padding: 12px;
  height: 100%;
}
.quick-reply-search {
  margin-bottom: 12px;
}
.quick-reply-group {
  margin-bottom: 16px;
}
.group-title {
  font-size: 12px;
  color: #64748B;
  font-weight: 600;
  margin-bottom: 8px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.quick-reply-item {
  margin-bottom: 6px;
  cursor: pointer;
  transition: all 0.2s ease;
}
.quick-reply-item:hover {
  border-color: #6366F1;
}
.item-content {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.item-text {
  font-size: 13px;
  color: #334155;
  line-height: 1.5;
}
.item-vars {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.preview-content pre {
  background: #F1F5F9;
  padding: 12px;
  border-radius: 6px;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  color: #0F172A;
}
.preview-empty {
  text-align: center;
  color: #94A3B8;
  padding: 24px 0;
}
</style>
