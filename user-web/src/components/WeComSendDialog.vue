<template>
  <el-dialog
    :model-value="visible"
    title="测试发送消息"
    width="520px"
    @update:model-value="(v) => emit('update:visible', v)"
  >
    <el-form :model="sendForm" label-width="96px">
      <el-form-item label="接收人ID" required>
        <el-input v-model="sendForm.to_user" placeholder="客户 external_userid，多个用 | 分隔" />
      </el-form-item>
      <el-form-item label="消息类型">
        <el-select v-model="sendForm.msg_type">
          <el-option label="文本" value="text" />
          <el-option label="图片" value="image" />
          <el-option label="图文链接" value="link" />
        </el-select>
      </el-form-item>
      <el-form-item label="消息内容" required v-if="sendForm.msg_type === 'text'">
        <el-input v-model="sendForm.content" type="textarea" :rows="4" placeholder="文本消息内容" />
      </el-form-item>
      <el-form-item label="媒体ID" required v-else-if="sendForm.msg_type === 'image'">
        <el-input v-model="sendForm.media_id" placeholder="media_id" />
      </el-form-item>
      <template v-else>
        <el-form-item label="标题" required><el-input v-model="sendForm.title" placeholder="链接标题" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="sendForm.desc" placeholder="链接描述" /></el-form-item>
        <el-form-item label="链接" required><el-input v-model="sendForm.url" placeholder="https://..." /></el-form-item>
        <el-form-item label="封面图"><el-input v-model="sendForm.pic_url" placeholder="图片 URL" /></el-form-item>
      </template>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:visible', false)">取消</el-button>
      <el-button type="primary" :loading="loading" @click="submit">发送</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { wecomAccountApi } from '@/api/wecomAccount'

const props = defineProps({
  visible: { type: Boolean, default: false },
  accountId: { type: [Number, String], default: null },
  externalUserid: { type: String, default: '' }
})
const emit = defineEmits(['update:visible', 'sent'])

const loading = ref(false)
const sendForm = reactive({
  to_user: '',
  msg_type: 'text',
  content: '',
  media_id: '',
  title: '',
  desc: '',
  url: '',
  pic_url: ''
})

const resetForm = () => {
  sendForm.to_user = ''
  sendForm.msg_type = 'text'
  sendForm.content = ''
  sendForm.media_id = ''
  sendForm.title = ''
  sendForm.desc = ''
  sendForm.url = ''
  sendForm.pic_url = ''
}

watch(
  () => props.visible,
  (v) => {
    if (v) {
      resetForm()
      if (props.externalUserid) sendForm.to_user = props.externalUserid
    }
  }
);

const submit = async () => {
  if (!props.accountId) {
    ElMessage.warning('缺少账号 ID')
    return
  }
  if (!sendForm.to_user) {
    ElMessage.warning('请输入接收人 external_userid')
    return
  }
  if (sendForm.msg_type === 'text' && !sendForm.content) {
    ElMessage.warning('请输入消息内容')
    return
  }
  if (sendForm.msg_type === 'image' && !sendForm.media_id) {
    ElMessage.warning('请输入 media_id')
    return
  }
  if (sendForm.msg_type === 'link' && !sendForm.url) {
    ElMessage.warning('请输入链接地址')
    return
  }
  loading.value = true
  try {
    const payload = { to_user: sendForm.to_user, msg_type: sendForm.msg_type }
    if (sendForm.msg_type === 'text') payload.content = sendForm.content
    if (sendForm.msg_type === 'image') payload.media_id = sendForm.media_id
    if (sendForm.msg_type === 'link') {
      payload.title = sendForm.title
      payload.desc = sendForm.desc
      payload.url = sendForm.url
      payload.pic_url = sendForm.pic_url
    }
    await wecomAccountApi.sendMessageById(props.accountId, payload)
    ElMessage.success('发送成功')
    emit('sent')
    emit('update:visible', false)
  } catch (e) {
    ElMessage.error('发送失败：' + (e.message || e))
  } finally {
    loading.value = false
  }
}
</script>
