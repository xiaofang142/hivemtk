<template>
  <el-dialog
    v-model="visible"
    :title="$t('绑定 AI 智能体')"
    width="640px"
    :close-on-click-modal="false"
    @open="onOpen"
  >
    <el-descriptions
      :column="2"
      border
      size="small"
      class="mb-16"
    >
      <el-descriptions-item :label="$t('渠道类型')">
        {{ channelTypeLabel }}
      </el-descriptions-item>
      <el-descriptions-item :label="$t('账号ID')">
        {{ accountId }}
      </el-descriptions-item>
      <el-descriptions-item :label="$t('账号名称')">
        {{ accountLabel || '-' }}
      </el-descriptions-item>
      <el-descriptions-item :label="$t('账号状态')">
        <el-tag
          :type="accountEnabled ? 'success' : 'info'"
          size="small"
        >
          {{ accountEnabled ? '正常' : '停用' }}
        </el-tag>
      </el-descriptions-item>
    </el-descriptions>

    <el-divider content-position="left">
      {{ $t('当前绑定') }}
    </el-divider>

    <el-table
      v-loading="loading"
      :data="bindings"
      stripe
      size="small"
      border
    >
      <el-table-column
        prop="id"
        label="ID"
        width="60"
        align="center"
      />
      <el-table-column
        :label="$t('智能体')"
        min-width="160"
      >
        <template #default="{ row }">
          <span>{{ agentNameMap[row.agent_id] || `#${row.agent_id}` }}</span>
        </template>
      </el-table-column>
      <el-table-column
        label="主绑定"
        width="80"
        align="center"
      >
        <template #default="{ row }">
          <el-tag
            v-if="row.is_primary"
            type="success"
            size="small"
          >
            主
          </el-tag>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column
        label="启用"
        width="70"
        align="center"
      >
        <template #default="{ row }">
          <el-tag
            :type="row.enabled ? 'success' : 'info'"
            size="small"
          >
            {{ row.enabled ? '是' : '否' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="150"
        align="center"
      >
        <template #default="{ row }">
          <el-button
            v-if="!row.is_primary"
            link
            type="primary"
            size="small"
            @click="setPrimary(row)"
          >
            设为主
          </el-button>
          <el-button
            link
            type="danger"
            size="small"
            @click="removeBinding(row)"
          >
            解绑
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty
          description="暂无绑定，请在下方添加"
          :image-size="60"
        />
      </template>
    </el-table>

    <el-divider content-position="left">
      添加新绑定
    </el-divider>

    <el-form
      :model="addForm"
      label-width="100px"
      inline
    >
      <el-form-item label="AI智能体">
        <el-select
          v-model="addForm.agent_id"
          placeholder="选择智能体"
          filterable
          style="width: 220px"
        >
          <el-option
            v-for="a in enabledAgents"
            :key="a.id"
            :label="`${a.name}（${a.agent_code}）`"
            :value="a.id"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="主绑定">
        <el-switch v-model="addForm.is_primary" />
      </el-form-item>
      <el-form-item>
        <el-button
          type="primary"
          :loading="adding"
          :disabled="!addForm.agent_id"
          @click="addBinding"
        >
          <el-icon><Plus /></el-icon>
          添加绑定
        </el-button>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">
        关闭
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { listBindings, createBinding, updateBinding, deleteBinding } from '@/api/channelAgentBinding.js'
import { listEnabledAgents } from '@/api/aiAgent.js'
import { getChannelLabel } from '@/constants/channel'
import { isUserReportedError } from '@/utils/errorReporting'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  channelType: { type: String, required: true },
  accountId: { type: [String, Number], required: true },
  accountLabel: { type: String, default: '' },
  accountEnabled: { type: Boolean, default: true }
})
const emit = defineEmits(['update:modelValue'])

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

const channelTypeLabel = computed(() => getChannelLabel(props.channelType))

const loading = ref(false)
const adding = ref(false)
const bindings = ref([])
const enabledAgents = ref([])
const agentNameMap = ref({})

const addForm = reactive({ agent_id: null, is_primary: true })

const onOpen = async () => {
  await Promise.all([loadBindings(), loadEnabledAgents()])
}

const loadBindings = async () => {
  if (!props.accountId) return
  loading.value = true
  try {
    const res = await listBindings({ channel_type: props.channelType, account_id: String(props.accountId) })
    bindings.value = res?.list || []
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('加载绑定失败：' + (e.message || '未知错误'))
    bindings.value = []
  } finally {
    loading.value = false
  }
}

const loadEnabledAgents = async () => {
  try {
    const res = await listEnabledAgents()
    enabledAgents.value = res?.list || []
    const map = {}
    enabledAgents.value.forEach(a => { map[a.id] = a.name })
    agentNameMap.value = map
  } catch (e) {
    console.warn('加载智能体列表失败：', e?.message)
  }
}

const addBinding = async () => {
  if (!addForm.agent_id) {
    ElMessage.warning(i18n.global.t('请选择智能体'))
    return
  }
  adding.value = true
  try {
    await createBinding({
      channel_type: props.channelType,
      account_id: String(props.accountId),
      agent_id: addForm.agent_id,
      is_primary: addForm.is_primary,
      enabled: true
    })
    ElMessage.success(i18n.global.t('绑定成功'))
    addForm.agent_id = null
    addForm.is_primary = true
    await loadBindings()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('绑定失败：' + (e.message || '未知错误'))
  } finally {
    adding.value = false
  }
}

const setPrimary = async (row) => {
  try {
    await updateBinding(row.id, {
      channel_type: row.channel_type,
      account_id: row.account_id,
      agent_id: row.agent_id,
      is_primary: true,
      enabled: row.enabled
    })
    ElMessage.success(i18n.global.t('已设为主绑定'))
    await loadBindings()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('设置失败：' + (e.message || '未知错误'))
  }
}

const removeBinding = (row) => {
  ElMessageBox.confirm('确认解绑该智能体吗？', '解绑确认', { type: 'warning' })
    .then(async () => {
      try {
        await deleteBinding(row.id)
        ElMessage.success(i18n.global.t('解绑成功'))
        await loadBindings()
      } catch (e) {
        if (!isUserReportedError(e)) ElMessage.error('解绑失败：' + (e.message || '未知错误'))
      }
    })
    .catch(() => {})
}
</script>

<style scoped>
.mb-16 { margin-bottom: 16px; }
</style>
