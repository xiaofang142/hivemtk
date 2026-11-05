<template>
  <div class="geo-page">
    <div
      class="p-4"
      style="display:flex; gap:16px; min-height:calc(100vh - 180px)"
    >
      <div style="flex:1; min-width:0; display:flex; flex-direction:column; gap:16px">
        <el-row :gutter="16">
          <el-col :span="8">
            <el-card>
              <el-statistic
                title="文档总数"
                :value="docs.length"
              />
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card>
              <el-statistic
                title="A 级文档"
                :value="aLevelCount"
              />
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card>
              <el-statistic
                title="总实体数"
                :value="entityTotal"
              />
            </el-card>
          </el-col>
        </el-row>

        <el-card style="flex:1">
          <template #header>
            <div class="flex items-center justify-between">
              <span class="font-bold">GEO 知识库</span>
              <div class="flex items-center gap-2">
                <el-input
                  v-model="keyword"
                  placeholder="搜索标题/内容"
                  size="small"
                  style="width:220px"
                  clearable
                  @clear="loadDocs"
                  @keyup.enter="loadDocs"
                />
                <el-upload
                  :show-file-list="false"
                  :before-upload="onUpload"
                  :http-request="() => {}"
                >
                  <el-button
                    size="small"
                    type="primary"
                  >
                    上传文档
                  </el-button>
                </el-upload>
              </div>
            </div>
          </template>
          <el-table
            v-loading="loading"
            :data="docs"
            size="small"
          >
            <el-table-column
              prop="title"
              label="文档"
              min-width="200"
            >
              <template #default="{ row }">
                <div class="font-medium">
                  {{ row.title || row.filename }}
                </div>
                <div class="text-xs text-gray-500">
                  {{ row.doc_id || row.id }}
                </div>
              </template>
            </el-table-column>
            <el-table-column
              label="等级"
              width="140"
            >
              <template #default="{ row }">
                <el-select
                  v-model="row.source_level"
                  size="small"
                  style="width:90px"
                  @change="onUpdateLevel(row)"
                >
                  <el-option
                    label="A"
                    value="A"
                  />
                  <el-option
                    label="B"
                    value="B"
                  />
                  <el-option
                    label="C"
                    value="C"
                  />
                  <el-option
                    label="D"
                    value="D"
                  />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column
              label="公开 FAQ"
              width="100"
            >
              <template #default="{ row }">
                <el-switch
                  v-model="row.is_public"
                  size="small"
                  @change="onTogglePublic(row)"
                />
              </template>
            </el-table-column>
            <el-table-column
              label="上传时间"
              width="170">
              <template #default="{ row }">
                {{ fmtTime(row.created_at || row.uploaded_at) }}
              </template>
            </el-table-column>
            <el-table-column
              label="操作"
              width="200"
            >
              <template #default="{ row }">
                <el-button
                  size="small"
                  type="primary"
                  plain
                  :loading="extractLoading === row.id"
                  @click="onExtract(row)"
                >
                  抽取实体
                </el-button>
                <el-button
                  size="small"
                  type="danger"
                  text
                  @click="onDelete(row)"
                >
                  删除
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </div>

    
      <el-card style="width:400px; flex-shrink:0; display:flex; flex-direction:column">
        <template #header>
          <span class="font-bold">Ask GEO KB</span>
        </template>
        <div
          class="chat-panel"
          style="flex:1; overflow-y:auto; padding-right:4px"
        >
          <div
            v-for="(m, i) in messages"
            :key="i"
            class="chat-message"
            :class="m.role"
          >
            <div class="chat-avatar">
              {{ m.role === 'user' ? '我' : 'AI' }}
            </div>
            <div class="chat-bubble">
              {{ m.content }}
            </div>
          </div>
          <div
            v-if="messages.length === 0"
            class="text-gray-400 text-sm text-center py-4"
          >
            在下方输入问题，从 GEO 知识库中问答
          </div>
        </div>
        <div class="mt-3">
          <el-input
            v-model="askInput"
            type="textarea"
            :rows="3"
            placeholder="输入问题..."
            resize="none"
            @keyup.enter.ctrl="onAsk"
          />
          <div class="flex justify-end mt-2">
            <el-button
              type="primary"
              :loading="asking"
              :disabled="!askInput.trim()"
              @click="onAsk"
            >
              发送
            </el-button>
          </div>
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listKBDocuments, saveKBDocument, deleteKBDocument, askKB, extractEntities, listEntities } from '@/api/geoEntity.js'
import { isUserReportedError } from '@/utils/errorReporting'
import { fmtTime } from '@/utils/time'

const docs = ref([])
const loading = ref(false)
const keyword = ref('')
const extractLoading = ref(null)

const aLevelCount = computed(() => docs.value.filter(d => d.source_level === 'A').length)
// 后端 KB 文档行没有 entity_count 字段，实体数与文档无外键关联，
// 因此「总实体数」改用实体库真实总数（/api/geo/entity/list 的 total），原来按行求和永远是 0。
const entityTotal = ref(0)

const messages = ref([])
const askInput = ref('')
const asking = ref(false)

const loadDocs = async () => {
  loading.value = true
  try {
    const data = await listKBDocuments(keyword.value)
    docs.value = Array.isArray(data) ? data : (data?.list || data?.items || [])
  } catch { docs.value = [] }
  loading.value = false
}

// 拉取实体库总数（只取 1 条用于拿 total，避免整表传输）
const loadEntityTotal = async () => {
  try {
    const res = await listEntities('', '')
    const total = Array.isArray(res) ? res.length : Number(res?.total || 0)
    entityTotal.value = total || 0
  } catch { entityTotal.value = 0 }
}

const onUpload = async (file) => {
  const rawFile = file.raw || file
  const ext = (rawFile.name || '').split('.').pop()?.toLowerCase() || '';
  const isText = ['txt', 'md', 'markdown', 'json'].includes(ext) ||
    rawFile.type?.startsWith('text/') ||
    rawFile.type === 'application/json'
  if (!isText) {
    ElMessage.warning('仅支持 txt / md / json 等文本文件')
    return false
  }
  try {
    const content = await new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result || ''))
      reader.onerror = () => reject(new Error('文件读取失败'))
      reader.readAsText(rawFile)
    })
    if (!content.trim()) {
      ElMessage.warning('文件内容为空')
      return false
    }
    const title = rawFile.name.replace(/\.[^.]+$/, '') || '未命名文档'
    // 上传的文档默认私有；需要出现在公开承接页 FAQ 时再手动打开。
    await saveKBDocument({ title, content, source_level: 'C', is_public: false })
    ElMessage.success('文档上传成功')
    await loadDocs()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '上传失败')
  }
  return false
}

const onTogglePublic = async (row) => {
  try {
    await saveKBDocument({
      id: row.id || row.doc_id,
      title: row.title || row.filename,
      content: row.content || '',
      source_level: row.source_level,
      is_public: !!row.is_public
    })
    ElMessage.success(row.is_public ? '已对公开 FAQ 开放' : '已设为私有')
  } catch (e) {
    row.is_public = !row.is_public
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '更新失败')
  }
}

const onUpdateLevel = async (row) => {
  try {
    await saveKBDocument({ id: row.id || row.doc_id, source_level: row.source_level, title: row.title || row.filename, content: row.content || '', is_public: !!row.is_public })
    ElMessage.success('已更新')
  } catch (e) { if (!isUserReportedError(e)) ElMessage.error(e?.message || '更新失败') }
}

const onDelete = async (row) => {
  // 知识库文档删除是不可撤销操作，先二次确认（与其余页面一致），取消时静默返回
  try {
    await ElMessageBox.confirm(`确认删除文档「${row.title || row.filename || row.id || row.doc_id}」？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await deleteKBDocument(row.id || row.doc_id)
    ElMessage.success('已删除')
    loadDocs()
  } catch (e) { if (!isUserReportedError(e)) ElMessage.error(e?.message || '删除失败') }
}

const onExtract = async (row) => {
  extractLoading.value = row.id
  try {
    await extractEntities(row.id || row.doc_id)
    ElMessage.success('实体抽取完成')
    // 实体数统计来自实体库总数，不是文档列表；只刷 loadDocs 会一直显示 0。
    await Promise.all([loadDocs(), loadEntityTotal()])
  } catch (e) { if (!isUserReportedError(e)) ElMessage.error('抽取失败：' + (e?.message || e)) }
  extractLoading.value = null
}

const onAsk = async () => {
  const q = askInput.value.trim()
  if (!q) return
  messages.value.push({ role: 'user', content: q })
  askInput.value = ''
  asking.value = true
  try {
    const answer = await askKB(q)
    const content = typeof answer === 'string' ? answer : (answer?.answer || answer?.data || JSON.stringify(answer))
    messages.value.push({ role: 'assistant', content })
  } catch (e) {
    messages.value.push({ role: 'assistant', content: '抱歉，暂时无法回答：' + (e?.message || e) })
  }
  asking.value = false
}

onMounted(() => {
  loadDocs()
  loadEntityTotal()
})
</script>

<style lang="scss" scoped>
.chat-panel {
  display: flex;
  flex-direction: column;
  gap: $spacing-md;
  min-height: 300px;
}
.chat-message {
  display: flex;
  gap: $spacing-sm;
  align-items: flex-start;
}
.chat-message.user { flex-direction: row-reverse; }
.chat-avatar {
  width: 28px; height: 28px; border-radius: 50%;
  background: var(--el-color-primary); color: $bg-color;
  display: flex; align-items: center; justify-content: center;
  font-size: $font-size-extra-small; flex-shrink: 0;
}
.chat-message.assistant .chat-avatar { background: $success-color; }
.chat-bubble {
  max-width: 75%;
  padding: $spacing-md 14px;
  border-radius: 10px;
  background: var(--el-fill-color-light);
  font-size: $font-size-base;
  white-space: pre-wrap;
  word-break: break-word;
}
.chat-message.user .chat-bubble {
  background: var(--el-color-primary-light-9);
  color: var(--el-text-color-primary);
}
</style>
