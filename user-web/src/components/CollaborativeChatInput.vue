<template>
  <div class="chat-input-area">
    
    <div class="mode-bar">
      <el-radio-group v-model="mode" size="small">
        <el-radio-button label="reply">{{ $t('对外回复') }}</el-radio-button>
        <el-radio-button label="note">{{ $t('内部备注') }}</el-radio-button>
      </el-radio-group>
      <div v-if="editingLock && editingLock.holder !== 'me'" class="lock-warn">
        <el-icon><Lock /></el-icon>
        {{ editingLock.holderName }} {{ $t('正在编辑') }}
      </div>
      <div v-else-if="mode === 'note'" class="note-tip">
        <el-icon><InfoFilled /></el-icon>
        {{ $t('内部备注仅团队可见，不会发送给客户') }}
      </div>
    </div>

    
    <el-popover
      v-model:visible="mentionVisible"
      :width="280"
      placement="top-start"
      trigger="manual"
    >
      <div class="mention-list">
        <div
          v-for="u in mentionResults"
          :key="u.id"
          class="mention-item"
          @click="selectMention(u)"
        >
          <el-avatar :size="24">{{ u.name.charAt(0) }}</el-avatar>
          <span>{{ u.name }}</span>
          <el-tag size="small">{{ u.role }}</el-tag>
        </div>
      </div>
    </el-popover>

    
    <el-input
      v-model="text"
      type="textarea"
      :rows="3"
      :placeholder="placeholder"
      @input="onInput"
      @keydown.enter.exact="onSend"
    >
      <template #prepend>
        <el-button :icon="Promotion" @click="openMention">@</el-button>
      </template>
    </el-input>

    <div class="action-bar">
      <div class="left-actions">
        <el-button :icon="Picture">{{ $t('图片') }}</el-button>
        <el-button :icon="Document">{{ $t('文件') }}</el-button>
        <el-button :icon="Emoji">{{ $t('表情') }}</el-button>
      </div>
      <el-button
        :type="mode === 'note' ? 'warning' : 'primary'"
        @click="onSend"
      >
        {{ mode === 'note' ? $t('保存内部备注') : $t('发送') }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { Promotion, Picture, Document, Emoji, Lock, InfoFilled } from '@element-plus/icons-vue'
import {
  sendCollaborativeMessage,
  addInternalNote,
  searchMentionUsers,
  acquireEditLock,
  releaseEditLock,
  getEditLock
} from '@/api/collaboration'

const props = defineProps({
  sessionId: { type: String, required: true }
})
const emit = defineEmits(['sent'])

const text = ref('')
const mode = ref('reply');
const mentionVisible = ref(false)
const mentionResults = ref([])
const editingLock = ref(null)
const mentionQuery = ref('')
let lockTimer = null

const placeholder = computed(() =>
  mode.value === 'note' ? '输入内部备注（仅团队可见）...' : '输入回复消息，输入 @ 提及同事...'
)

async function checkLock() {
  try {
    const lock = await getEditLock(props.sessionId)
    editingLock.value = lock
    if (lock && lock.holder !== 'me' && lock.expiresAt > Date.now()) { /* no-op */ }
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

async function acquireLock() {
  if (mode.value === 'note') return
  try {
    await acquireEditLock(props.sessionId)
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

async function releaseLock() {
  try {
    await releaseEditLock(props.sessionId)
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

onMounted(() => {
  checkLock()
  lockTimer = setInterval(acquireLock, 30000);
})
onUnmounted(() => {
  if (lockTimer) clearInterval(lockTimer)
  releaseLock()
})

function onInput(val) {
  const cursor = val.length;
  const lastAt = val.lastIndexOf('@', cursor - 1)
  if (lastAt >= 0) {
    const query = val.substring(lastAt + 1, cursor)
    if (!query.includes(' ')) {
      mentionQuery.value = query
      searchMentionUsers({ q: query }).then((res) => {
        mentionResults.value = res || []
        if (mentionResults.value.length > 0) {
          mentionVisible.value = true
        }
      })
    }
  } else {
    mentionVisible.value = false
  }
}

function selectMention(user) {
  const cursor = text.value.length;
  const lastAt = text.value.lastIndexOf('@', cursor - 1)
  if (lastAt >= 0) {
    text.value = text.value.substring(0, lastAt) + `@${user.name} `
  }
  mentionVisible.value = false
}

function openMention() {
  const cursor = text.value.length
  text.value = text.value + '@'
  searchMentionUsers({ q: '' }).then((res) => {
    mentionResults.value = res || []
    mentionVisible.value = true
  });
}

async function onSend() {
  if (!text.value.trim()) return
  try {
    if (mode.value === 'note') {
      await addInternalNote(props.sessionId, { content: text.value })
    } else {
      const mentions = extractMentions(text.value);
      await sendCollaborativeMessage(props.sessionId, {
        content: text.value,
        is_internal_note: false,
        mentions
      })
    }
    text.value = ''
    emit('sent')
  } catch (err) {
    console.error('发送失败', err)
  }
}

function extractMentions(text) {
  const re = /@(\S+)/g
  const names = []
  let m
  while ((m = re.exec(text))) names.push(m[1])
  return names
}

watch(() => mode.value, (val) => {
  if (val === 'reply') acquireLock()
  else releaseLock()
})
</script>

<style scoped>
.chat-input-area {
  background: #fff;
  border-radius: 8px;
  padding: 12px;
}
.mode-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.lock-warn {
  color: #F59E0B;
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 4px;
}
.note-tip {
  color: #64748B;
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 4px;
}
.mention-list {
  max-height: 240px;
  overflow-y: auto;
}
.mention-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  cursor: pointer;
  border-radius: 4px;
}
.mention-item:hover { background: #F1F5F9; }
.action-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
}
.left-actions { display: flex; gap: 4px; }
</style>
