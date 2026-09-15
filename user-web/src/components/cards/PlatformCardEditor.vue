<template>
  <div class="card-editor">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>卡片编辑器（{{ platformLabel }}）USR-RC-01</span>
          <el-radio-group v-model="platform" size="small" @change="onPlatformChange">
            <el-radio-button v-for="p in platforms" :key="p.value" :label="p.value">
              {{ p.label }}
            </el-radio-button>
          </el-radio-group>
        </div>
      </template>

      <el-form :model="form" label-width="100px">
        <el-form-item label="标题">
          <el-input v-model="form.title" :placeholder="`${platformLabel} 卡片标题`" />
        </el-form-item>
        <el-form-item label="封面图">
          <el-input v-model="form.cover" placeholder="图片 URL" />
        </el-form-item>
        <el-form-item label="正文">
          <el-input v-model="form.content" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="按钮">
          <div v-for="(btn, i) in form.buttons" :key="i" class="button-row">
            <el-input v-model="btn.text" placeholder="按钮文字" style="width: 200px" />
            <el-input v-model="btn.url" placeholder="链接 URL" style="width: 280px; margin-left: 8px" />
            <el-button link type="danger" @click="form.buttons.splice(i, 1)" style="margin-left: 8px">删除</el-button>
          </div>
          <el-button @click="form.buttons.push({ text: '', url: '' })" size="small" style="margin-top: 8px">
            + 添加按钮
          </el-button>
        </el-form-item>
        <el-form-item label="平台特性">
          <el-input v-model="form.platformExtra" type="textarea" :rows="2" :placeholder="platformHint" />
        </el-form-item>
        <el-form-item>
          <el-button @click="resetForm">重置</el-button>
          <el-button @click="saveDraft">存草稿</el-button>
          <el-button type="primary" @click="publish">发布到 {{ platformLabel }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card class="preview-card">
      <template #header><span>预览（实时）</span></template>
      <div class="card-preview">
        <img v-if="form.cover" :src="form.cover" class="cover" :alt="form.title || '封面'" />
        <h3 class="title">{{ form.title || '标题' }}</h3>
        <p class="content">{{ form.content || '正文' }}</p>
        <div class="buttons">
          <el-button v-for="(btn, i) in form.buttons" :key="i" type="primary" size="small">
            {{ btn.text || `按钮 ${i + 1}` }}
          </el-button>
        </div>
        <pre v-if="form.platformExtra" class="platform-extra">{{ form.platformExtra }}</pre>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue';
import { ElMessage } from 'element-plus'
import { http } from '@/utils/request'

const props = defineProps({
  modelValue: { type: Object, default: () => null },
  defaultPlatform: { type: String, default: 'douyin' }
})
const emit = defineEmits(['update:modelValue', 'publish'])

const platforms = [
  { value: 'douyin', label: '抖音', hint: '抖音卡片支持 3 个按钮、最多 100 字' },
  { value: 'kuaishou', label: '快手', hint: '快手卡片支持 2 个按钮' },
  { value: 'xiaohongshu', label: '小红书', hint: '小红书卡片限制 200 字' },
  { value: 'xianyu', label: '闲鱼', hint: '闲鱼卡片支持商品链接' },
  { value: 'tiktok', label: 'TikTok', hint: 'TikTok 海外版本规则' }
]

const platform = ref(props.modelValue?.platform || props.defaultPlatform)
const platformLabel = computed(() => platforms.find((p) => p.value === platform.value)?.label)
const platformHint = computed(() => platforms.find((p) => p.value === platform.value)?.hint)

const form = reactive({
  title: props.modelValue?.title || '',
  cover: props.modelValue?.cover || '',
  content: props.modelValue?.content || '',
  buttons: props.modelValue?.buttons || [],
  platformExtra: props.modelValue?.platformExtra || ''
})

watch(() => props.modelValue, (v) => {
  if (v) Object.assign(form, v)
}, { deep: true })

function onPlatformChange(val) {
  emit('update:modelValue', { ...form, platform: val })
}

function resetForm() {
  form.title = ''
  form.cover = ''
  form.content = ''
  form.buttons = []
  form.platformExtra = ''
}

function saveDraft() {
  ElMessage.success('草稿已保存')
}

function publish() {
  const apiMap = {
    douyin: 'douyinCard',
    kuaishou: 'kuaishouCard',
    xiaohongshu: 'xiaohongshuCard',
    xianyu: 'xianyuCard',
    tiktok: 'tiktokCard'
  };
  emit('publish', { platform: platform.value, data: form })
  ElMessage.success(`已发布到 ${platformLabel.value}`)
}
</script>

<style scoped>
.card-editor { display: grid; grid-template-columns: 2fr 1fr; gap: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.button-row { display: flex; align-items: center; margin-bottom: 4px; }
.card-preview { padding: 16px; background: #F8FAFC; border-radius: 8px; }
.card-preview .cover { width: 100%; max-height: 200px; object-fit: cover; border-radius: 6px; }
.card-preview .title { margin: 12px 0 6px; }
.card-preview .content { color: #475569; white-space: pre-wrap; }
.card-preview .buttons { margin-top: 12px; display: flex; gap: 8px; flex-wrap: wrap; }
.card-preview .platform-extra {
  margin-top: 12px; padding: 8px;
  background: #fff; border-radius: 4px;
  font-size: 12px; color: #94A3B8;
}
</style>
