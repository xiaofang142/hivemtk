<template>
  <div class="chat-input">
    
    <div v-if="pendingAttachment" class="attachment-preview">
      <img v-if="pendingAttachment.mediaType === 'image'" :src="pendingAttachment.preview" class="preview-img" alt="附件预览" />
      <div v-else class="preview-file">
        <div class="file-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg></div>
        <div class="file-info">
          <div class="file-name">{{ pendingAttachment.name }}</div>
          <div class="file-size">{{ formatSize(pendingAttachment.size) }}</div>
        </div>
      </div>
      <button class="remove-btn" @click="removeAttachment" :aria-label="$t('移除附件')">×</button>
      <div v-if="uploading" class="upload-progress">
        <div class="progress-bar" :style="{ width: uploadProgress + '%' }"></div>
        <span class="progress-text">上传中 {{ uploadProgress }}%</span>
      </div>
    </div>

    <textarea
      ref="textareaRef"
      v-model="text"
      class="input"
      aria-label="消息输入框"
      rows="1"
      :placeholder="placeholder"
      :maxlength="maxLength"
      :disabled="uploading"
      @input="autoResize"
      @keydown.enter.exact.prevent="onSend"
    ></textarea>

    <div class="actions">
      <input
        ref="fileInputRef"
        type="file"
        style="display: none"
        aria-label="选择附件"
        :accept="acceptString"
        @change="onFileSelected"
      />
      <button
        class="action-btn attach"
        :disabled="sending || uploading"
        @click="triggerFileSelect"
        :title="$t('发送附件')"
        :aria-label="$t('发送附件')"
      >
        <svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true">
          <path d="M8.5 1.5a3.5 3.5 0 0 0-4.95 4.95l5.5 5.5a2 2 0 0 0 2.83-2.83l-4.5-4.5a.5.5 0 1 1 .71-.71l4.5 4.5a3 3 0 0 1-4.24 4.24l-5.5-5.5A4.5 4.5 0 0 1 8.5.5l5.5 5.5a.5.5 0 0 1-.7.7L8.5 1.5z"/>
        </svg>
      </button>
      <button
        class="action-btn send"
        :style="{ background: color }"
        :disabled="!canSend || uploading"
        @click="onSend"
      >
        {{ uploading ? '上传中' : sending ? '发送中' : '发送' }}
      </button>
    </div>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, nextTick } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'
import { getUploadToken } from '@/api/chat'

const props = defineProps({
  color: { type: String, default: '#1989fa' },
  sending: { type: Boolean, default: false },
  placeholder: { type: String, default: '请输入消息...' },
  maxLength: { type: Number, default: 2000 }
})

const emit = defineEmits(['send', 'upload', 'progress'])

const text = ref('')
const textareaRef = ref(null)
const fileInputRef = ref(null)
const uploading = ref(false)
const uploadProgress = ref(0)

const acceptString = '.jpg,.jpeg,.png,.gif,.webp,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt,.zip,.rar,.mp3,.wav,.mp4,.webm'

const pendingAttachment = ref(null);

const canSend = computed(() => (text.value.trim().length > 0 || pendingAttachment.value) && !props.sending && !uploading.value)

const onSend = () => {
  if (!canSend.value) return
  const payload = {
    text: text.value.trim(),
    attachment: pendingAttachment.value
  }
  emit('send', payload)
  text.value = ''
  pendingAttachment.value = null
  if (fileInputRef.value) fileInputRef.value.value = ''
  nextTick(autoResize)
}

const autoResize = () => {
  if (!textareaRef.value) return
  textareaRef.value.style.height = 'auto'
  textareaRef.value.style.height = Math.min(textareaRef.value.scrollHeight, 100) + 'px'
}

const triggerFileSelect = () => {
  if (uploading.value) return
  fileInputRef.value?.click()
}

const formatSize = (bytes) => {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

const getFileType = (file) => {
  if (file.type.startsWith('image/')) return 'image'
  if (file.type.startsWith('audio/')) return 'audio'
  if (file.type.startsWith('video/')) return 'video'
  return 'file'
}

const getExt = (name) => {
  const m = name.match(/\.([^.]+)$/)
  return m ? m[1].toLowerCase() : ''
}

const onFileSelected = async (e) => {
  const file = e.target.files?.[0]
  if (!file) return

  if (file.size > 20 * 1024 * 1024) {
    ElMessage.warning(i18n.global.t('文件不能超过 20MB'))
    e.target.value = ''
    return
  }

  let preview = '';
  if (file.type.startsWith('image/')) {
    preview = await new Promise((resolve) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result)
      reader.readAsDataURL(file)
    })
  }

  pendingAttachment.value = {
    file,
    name: file.name,
    size: file.size,
    mediaType: getFileType(file),
    ext: getExt(file.name),
    preview
  }

  await uploadAttachment();
}

const uploadAttachment = async () => {
  if (!pendingAttachment.value) return
  const att = pendingAttachment.value
  uploading.value = true
  uploadProgress.value = 0

  try {
    const tokenData = await getUploadToken({ file_type: att.mediaType, ext: att.ext, size: att.size });
    const { upload_url, token, key, public_url } = tokenData

    const form = new FormData();
    form.append('file', att.file)
    form.append('token', token)
    form.append('key', key)
    const r = await axios.post(upload_url, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
      onUploadProgress: (e) => {
        if (e.total) {
          uploadProgress.value = Math.round((e.loaded / e.total) * 100)
        }
      }
    })
    if (r.status !== 200) {
      throw new Error('七牛返回 ' + r.status)
    }

    pendingAttachment.value = {
      ...att,
      url: public_url.startsWith('http') ? public_url : `https://${public_url}`,
      key
    };
    uploadProgress.value = 100
    emit('upload', pendingAttachment.value)
  } catch (err) {
    ElMessage.error('上传失败：' + (err?.message || err))
    pendingAttachment.value = null
  } finally {
    uploading.value = false
    if (fileInputRef.value) fileInputRef.value.value = ''
  }
}

const removeAttachment = () => {
  pendingAttachment.value = null
  if (fileInputRef.value) fileInputRef.value.value = ''
}

defineExpose({ focus: () => textareaRef.value?.focus() })
</script>

<style scoped>
.chat-input {
  border-top: 1px solid #ebeef5;
  background: #fff;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex-shrink: 0;
}
.input {
  width: 100%;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  padding: 8px 10px;
  font-size: 14px;
  font-family: inherit;
  resize: none;
  outline: none;
  transition: border-color 0.2s;
  box-sizing: border-box;
  min-height: 36px;
  max-height: 100px;
  line-height: 1.5;
}
.input:focus {
  border-color: #1989fa;
}
.actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  align-items: center;
}
.action-btn {
  border: none;
  border-radius: 4px;
  padding: 6px 14px;
  font-size: 13px;
  cursor: pointer;
  transition: opacity 0.2s, background 0.2s;
  color: #fff;
}
.action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.action-btn.attach {
  background: #fff;
  color: #909399;
  border: 1px solid #dcdfe6;
  padding: 6px 10px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.action-btn.attach:hover:not(:disabled) {
  border-color: #1989fa;
  color: #1989fa;
}
.action-btn.send {
  background: #1989fa;
}
.attachment-preview {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px;
  background: #f5f7fa;
  border-radius: 6px;
  border: 1px solid #ebeef5;
}
.preview-img {
  width: 48px;
  height: 48px;
  object-fit: cover;
  border-radius: 4px;
}
.preview-file {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}
.file-icon {
  width: 36px;
  height: 36px;
  background: #fff;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #6366F1;
  flex-shrink: 0;
}
.file-icon svg { width: 20px; height: 20px; }
.file-info {
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
.file-name {
  font-size: 13px;
  color: #303133;
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
}
.file-size {
  font-size: 11px;
  color: #909399;
}
.remove-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 20px;
  height: 20px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  border: none;
  border-radius: 50%;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  line-height: 1;
}
.upload-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 16px;
  background: rgba(255, 255, 255, 0.9);
  border-bottom-left-radius: 6px;
  border-bottom-right-radius: 6px;
  overflow: hidden;
  display: flex;
  align-items: center;
}
.progress-bar {
  height: 100%;
  background: rgba(25, 137, 250, 0.3);
  transition: width 0.2s;
}
.progress-text {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  font-size: 11px;
  color: #1989fa;
}
</style>
