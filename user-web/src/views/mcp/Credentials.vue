<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>MCP 凭证（Client ID + API Key）</span>
          <el-button type="primary" size="small" @click="openCreate">签发新凭证</el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="供外部 AI Skill / Agent 通过 MCP 入口（POST /api/mcp）接入：请求头携带 X-Client-Id 与 X-API-Key。一个 Client ID 可挂多把 API Key（点「密钥」管理）；明文加密存储，签发后也可在钥匙列表查看/复制。"
        style="margin-bottom: 12px"
      />

      <el-table v-loading="loading" :data="rows" border size="small">
        <el-table-column label="Client ID" width="200">
          <template #default="{ row }">
            <span class="mono">{{ row.client_id }}</span>
            <el-button link size="small" @click="copyText(row.client_id, 'Client ID')">复制</el-button>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="备注" min-width="120" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="过期时间" width="170">
          <template #default="{ row }">
            <el-tag v-if="row.expires_at" :type="isExpired(row.expires_at) ? 'danger' : 'warning'" size="small">
              {{ fmtTime(row.expires_at) }}{{ isExpired(row.expires_at) ? '（已过期）' : '' }}
            </el-tag>
            <el-tag v-else type="info" size="small">永不过期</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="绑定账户" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.owner_user_id" size="small">用户 #{{ row.owner_user_id }}</el-tag>
            <span v-else class="muted">未绑定</span>
          </template>
        </el-table-column>
        <el-table-column label="工具范围" min-width="160">
          <template #default="{ row }">
            <el-tag v-for="t in (row.allowed_tools || '').split(',').filter(Boolean)" :key="t" size="small" style="margin-right: 4px">
              {{ t }}
            </el-tag>
            <span v-if="!row.allowed_tools" class="muted">不限制</span>
          </template>
        </el-table-column>
        <el-table-column prop="last_used_at" label="最近使用" width="160">
          <template #default="{ row }">{{ fmtTime(row.last_used_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" plain @click="openKeys(row)">密钥</el-button>
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" @click="toggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 签发弹窗 -->
    <el-dialog v-model="createVisible" title="签发 MCP 凭证" width="520px">
      <el-form label-width="110px">
        <el-form-item label="备注">
          <el-input v-model="createForm.name" placeholder="如：Alan 的 AI 助手" maxlength="100" />
        </el-form-item>
        <el-form-item label="Client ID">
          <el-input v-model="createForm.client_id" placeholder="留空自动生成（mcp- 开头）" maxlength="64" />
        </el-form-item>
        <el-form-item label="绑定账户">
          <el-input v-model="createForm.owner_user_id" placeholder="system_users.id，留空不绑定" style="width: 200px" />
        </el-form-item>
        <el-form-item label="过期时间">
          <el-radio-group v-model="createForm.expiryMode">
            <el-radio value="default">默认 90 天</el-radio>
            <el-radio value="never">永不过期</el-radio>
            <el-radio value="custom">自定义</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="createForm.expiryMode === 'custom'" label="过期时间">
          <el-date-picker
            v-model="createForm.expires_at"
            type="datetime"
            placeholder="选择过期时间"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="doCreate">签发</el-button>
      </template>
    </el-dialog>

    <!-- 编辑弹窗（改 ID / 续期 / 工具范围） -->
    <el-dialog v-model="editVisible" title="编辑凭证" width="520px">
      <el-form label-width="110px">
        <el-form-item label="Client ID">
          <el-input v-model="editForm.client_id" maxlength="64" />
        </el-form-item>
        <el-form-item label="过期时间">
          <el-radio-group v-model="editForm.expiryMode">
            <el-radio value="keep">保持当前</el-radio>
            <el-radio value="custom">续期到</el-radio>
            <el-radio value="never">永不过期</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="editForm.expiryMode === 'custom'" label="续期到">
          <el-date-picker
            v-model="editForm.expires_at"
            type="datetime"
            placeholder="选择新的过期时间"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
          />
        </el-form-item>
        <el-form-item label="工具范围">
          <el-input v-model="editForm.allowed_tools" placeholder="逗号分隔，如：customer.read,reach.send（留空=不限制）" />
        </el-form-item>
        <el-form-item label="绑定账户">
          <el-input v-model="editForm.owner_user_id" placeholder="system_users.id，留空不绑定" style="width: 200px" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 签发结果（明文只显示一次） -->
    <el-dialog v-model="issuedVisible" title="凭证已签发" width="520px" :close-on-click-modal="false">
      <el-alert type="warning" :closable="false" show-icon title="请立即复制保存。此后可在「密钥」列表中查看/复制已存储的明文（查看动作会记入管理审计）。" style="margin-bottom: 12px" />
      <el-descriptions :column="1" border>
        <el-descriptions-item label="Client ID">{{ issued.client_id }}</el-descriptions-item>
        <el-descriptions-item label="API Key">
          <span class="mono">{{ issued.api_key }}</span>
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button type="primary" @click="copyAll">复制并关闭</el-button>
      </template>
    </el-dialog>

    <!-- 钥匙管理抽屉（1:N）：一个 Client ID 名下多把 API Key -->
    <el-drawer v-model="keysVisible" :title="`密钥管理 — ${keysCred.client_id || ''}`" size="720px">
      <div style="margin-bottom: 12px">
        <el-button type="primary" size="small" @click="openKeyCreate">新建密钥</el-button>
        <span class="muted" style="margin-left: 8px">同一 Client ID 可持多把钥匙（多机分发 / 灰度轮换）；禁用/停用/删除立即生效。</span>
      </div>
      <el-table v-loading="keysLoading" :data="keys" border size="small">
        <el-table-column prop="name" label="名称" min-width="110" show-overflow-tooltip />
        <el-table-column label="API Key" min-width="230">
          <template #default="{ row }">
            <span v-if="revealed[row.id]" class="mono">{{ revealed[row.id] }}</span>
            <span v-else class="mono">{{ row.key_masked }}</span>
            <el-button
              v-if="row.can_reveal"
              link
              size="small"
              @click="toggleReveal(row)"
            >{{ revealed[row.id] ? '隐藏' : '显示' }}</el-button>
            <el-tag v-else size="small" type="info">旧密钥（明文未存）</el-tag>
            <el-button link size="small" @click="copyKey(row)">复制</el-button>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.expired" type="danger" size="small">已过期</el-tag>
            <el-tag v-else-if="row.status === 'active'" type="success" size="small">启用</el-tag>
            <el-tag v-else-if="row.status === 'disabled'" type="warning" size="small">禁用</el-tag>
            <el-tag v-else-if="row.status === 'suspended'" type="warning" size="small">已暂停</el-tag>
            <el-tag v-else type="info" size="small">已停用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="过期时间" width="160">
          <template #default="{ row }">
            <span v-if="row.expires_at">{{ fmtTime(row.expires_at) }}</span>
            <span v-else class="muted">跟随凭证</span>
          </template>
        </el-table-column>
        <el-table-column label="最近使用" width="150">
          <template #default="{ row }">{{ fmtTime(row.last_used_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="210" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status === 'disabled'" size="small" type="success" plain @click="keyEnable(row)">启用</el-button>
            <el-button v-if="row.status === 'active'" size="small" @click="keyDisable(row)">禁用</el-button>
            <el-button v-if="row.status === 'active' || row.status === 'disabled'" size="small" type="warning" plain @click="keySuspend(row)">暂停</el-button>
            <el-button v-if="row.status !== 'revoked'" size="small" type="warning" plain @click="keyRevoke(row)">停用</el-button>
            <el-button size="small" type="danger" plain @click="keyRemove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>

    <!-- 新建密钥弹窗 -->
    <el-dialog v-model="keyCreateVisible" title="新建 API Key" width="480px" append-to-body>
      <el-form label-width="90px">
        <el-form-item label="名称">
          <el-input v-model="keyForm.name" placeholder="如：jenkins-bot / 轮换-2026Q4" maxlength="100" />
        </el-form-item>
        <el-form-item label="过期时间">
          <el-radio-group v-model="keyForm.expiryMode">
            <el-radio value="default">默认 90 天</el-radio>
            <el-radio value="never">永不过期</el-radio>
            <el-radio value="custom">自定义</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="keyForm.expiryMode === 'custom'" label="过期时间">
          <el-date-picker
            v-model="keyForm.expires_at"
            type="datetime"
            placeholder="选择过期时间"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="keyCreateVisible = false">取消</el-button>
        <el-button type="primary" :loading="keyCreating" @click="doKeyCreate">签发</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { mcpCredentialApi, mcpKeyApi } from '@/api/mcpCredential'

const rows = ref([])
const loading = ref(false)
const createVisible = ref(false)
const editVisible = ref(false)
const issuedVisible = ref(false)
const creating = ref(false)
const saving = ref(false)
const createForm = ref({})
const editForm = ref({})
const issued = ref({})

// —— 钥匙抽屉（1:N）——
const keysVisible = ref(false)
const keysLoading = ref(false)
const keysCred = ref({})
const keys = ref([])
const keyCreateVisible = ref(false)
const keyCreating = ref(false)
const keyForm = ref({})
// revealed[keyId] 仅存于内存：显示/隐藏是"拉明文↔收起"的开关，
// 刷新抽屉即清空；服务端对每次 reveal 都落管理审计。
const revealed = reactive({})

const fmtTime = (v) => (v ? String(v).replace('T', ' ').slice(0, 19) : '—')
const isExpired = (v) => v && new Date(v).getTime() < Date.now()

const toISO = (v) => {
  if (!v) return undefined
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return undefined
  return d.toISOString()
}

const load = async () => {
  loading.value = true
  try {
    const res = await mcpCredentialApi.list()
    rows.value = res?.data ?? res ?? []
  } finally {
    loading.value = false
  }
}

const copyText = async (text, label) => {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(`${label || '内容'}已复制`)
  } catch {
    ElMessage.info('复制失败，请手动选择复制')
  }
}

const openCreate = () => {
  createForm.value = { name: '', client_id: '', owner_user_id: '', expiryMode: 'default', expires_at: undefined }
  createVisible.value = true
}

const openEdit = (row) => {
  editForm.value = {
    id: row.id,
    client_id: row.client_id,
    allowed_tools: row.allowed_tools || '',
    owner_user_id: row.owner_user_id ?? '',
    expiryMode: row.expires_at ? 'keep' : 'never',
    expires_at: undefined
  }
  editVisible.value = true
}

const doCreate = async () => {
  creating.value = true
  try {
    const payload = { name: createForm.value.name }
    if (createForm.value.client_id) payload.client_id = createForm.value.client_id.trim()
    if (createForm.value.owner_user_id) payload.owner_user_id = Number(createForm.value.owner_user_id)
    if (createForm.value.expiryMode === 'never') {
      // 永不过期：显式 no_expiry，后端不落默认 90 天
      payload.no_expiry = true
    } else if (createForm.value.expiryMode === 'custom' && createForm.value.expires_at) {
      payload.expires_at = toISO(createForm.value.expires_at)
    }
    const res = await mcpCredentialApi.create(payload)
    // 注意：@/utils/request 的响应拦截器已经把 {code,data} 里的 data 拆出来了，
    // res 本身就是 {id, client_id, api_key, api_hint}。这里再写 res?.data ?? {} 会
    // 在 res.data 为 undefined 时兜成空对象 ⇒ 弹窗与「复制并关闭」印出
    // "Client ID: undefined / API Key: undefined"（真实报障）。保留 res 兜底。
    issued.value = res?.data ?? res ?? {}
    createVisible.value = false
    issuedVisible.value = true
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '签发失败')
  } finally {
    creating.value = false
  }
}

const doEdit = async () => {
  saving.value = true
  try {
    const payload = {}
    if (editForm.value.client_id) payload.client_id = editForm.value.client_id.trim()
    if (editForm.value.owner_user_id) payload.owner_user_id = Number(editForm.value.owner_user_id)
    payload.allowed_tools = editForm.value.allowed_tools || ''
    if (editForm.value.expiryMode === 'custom' && editForm.value.expires_at) {
      payload.expires_at = toISO(editForm.value.expires_at)
    } else if (editForm.value.expiryMode === 'never') {
      // 显式清除过期（永不过期）；与"保持当前"（keep）由后端布尔区分
      payload.clear_expires = true
    }
    const res = await mcpCredentialApi.update(editForm.value.id, payload)
    ElMessage.success(res?.message || '已保存')
    editVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

const copyAll = async () => {
  const text = `Client ID: ${issued.value.client_id}\nAPI Key: ${issued.value.api_key}`
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.info('请手动复制')
  }
  issuedVisible.value = false
}

const toggle = async (row) => {
  await mcpCredentialApi.update(row.id, { enabled: !row.enabled })
  ElMessage.success(row.enabled ? '已停用' : '已启用')
  await load()
}

const remove = async (row) => {
  await ElMessageBox.confirm(`确认删除凭证 ${row.client_id}？名下全部 API Key 将一并删除，使用它的 Skill/Agent 立即失效。`, '删除确认', { type: 'warning' })
  await mcpCredentialApi.remove(row.id)
  ElMessage.success('已删除')
  await load()
}

// —— 钥匙管理 ——

const loadKeys = async () => {
  keysLoading.value = true
  try {
    const res = await mcpKeyApi.list(keysCred.value.id)
    const data = res?.data ?? res ?? {}
    keys.value = data.list ?? []
  } finally {
    keysLoading.value = false
  }
}

const openKeys = (row) => {
  keysCred.value = row
  Object.keys(revealed).forEach((k) => delete revealed[k])
  keysVisible.value = true
  loadKeys()
}

const toggleReveal = async (row) => {
  if (revealed[row.id]) {
    delete revealed[row.id]
    return
  }
  try {
    const res = await mcpKeyApi.reveal(keysCred.value.id, row.id)
    const data = res?.data ?? res ?? {}
    if (data.api_key) {
      revealed[row.id] = data.api_key
    } else {
      ElMessage.error('明文获取失败')
    }
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '该密钥明文不可查看')
  }
}

const copyKey = async (row) => {
  let plain = revealed[row.id]
  if (!plain) {
    if (!row.can_reveal) {
      ElMessage.info('旧密钥明文未存储，请新建密钥完成轮换')
      return
    }
    try {
      const res = await mcpKeyApi.reveal(keysCred.value.id, row.id)
      plain = (res?.data ?? res ?? {}).api_key
    } catch (e) {
      ElMessage.error(e?.response?.data?.message || '明文获取失败')
      return
    }
  }
  await copyText(plain, 'API Key')
}

const openKeyCreate = () => {
  keyForm.value = { name: '', expiryMode: 'default', expires_at: undefined }
  keyCreateVisible.value = true
}

const doKeyCreate = async () => {
  keyCreating.value = true
  try {
    const payload = { name: keyForm.value.name }
    if (keyForm.value.expiryMode === 'never') {
      payload.no_expiry = true
    } else if (keyForm.value.expiryMode === 'custom' && keyForm.value.expires_at) {
      payload.expires_at = toISO(keyForm.value.expires_at)
    }
    const res = await mcpKeyApi.create(keysCred.value.id, payload)
    const data = res?.data ?? res ?? {}
    keyCreateVisible.value = false
    // 明文只完整显示这一次（此后走列表的显示/复制）。
    await ElMessageBox.alert(
      `<div style="word-break:break-all;font-family:monospace">${data.api_key || ''}</div>`,
      '新密钥已签发（明文只显示这一次）',
      { dangerouslyUseHTMLString: true, confirmButtonText: '我已保存' }
    )
    await loadKeys()
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '签发失败')
  } finally {
    keyCreating.value = false
  }
}

const keyStatusAction = async (row, action, confirmText) => {
  await ElMessageBox.confirm(confirmText, '确认', { type: 'warning' })
  await mcpKeyApi[action](keysCred.value.id, row.id)
  ElMessage.success('已更新')
  await loadKeys()
}

const keyEnable = (row) => keyStatusAction(row, 'enable', `确认启用密钥「${row.name}」？`)
const keyDisable = (row) => keyStatusAction(row, 'disable', `确认禁用密钥「${row.name}」？禁用后该钥匙立即无法调用（可再启用）。`)

const keySuspend = (row) => keyStatusAction(row, 'suspend', `确认暂停密钥「${row.name}」？暂停后该钥匙立即无法调用（可再启用）。`)
const keyRevoke = (row) => keyStatusAction(row, 'revoke', `确认停用（吊销）密钥「${row.name}」？吊销为终态，不可重新启用，仅可删除。`)

const keyRemove = async (row) => {
  await ElMessageBox.confirm(`确认删除密钥「${row.name}」？该操作不可恢复（管理审计保留记录）。`, '删除确认', { type: 'warning' })
  await mcpKeyApi.remove(keysCred.value.id, row.id)
  ElMessage.success('已删除')
  await loadKeys()
}

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.mono {
  font-family: monospace;
  user-select: all;
}
.muted {
  color: #999;
  font-size: 12px;
}
</style>
