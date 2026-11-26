<template>
  <div class="leads-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          线索归档
        </h2>
        <span class="page-sub">写线索归档字段 + 对接 Phase 3 客户关联与负责人推送（T041/T042）</span>
      </div>
    </div>

    <el-card shadow="never">
      <el-form
        label-width="120px"
        @submit.prevent
      >
        <el-form-item
          label="线索ID"
          required
        >
          <el-input
            v-model="form.clue_id"
            placeholder="待归档线索ID"
          />
        </el-form-item>
        <el-form-item label="客户ID">
          <el-input
            v-model="form.customer_id"
            placeholder="已有客户时填写；否则用身份标识识别"
          />
        </el-form-item>
        <el-form-item label="身份标识">
          <el-input
            v-model="form.identifiers"
            type="textarea"
            :rows="2"
            placeholder="JSON，如 {&quot;phone&quot;:&quot;138...&quot;,&quot;email&quot;:&quot;a@b.com&quot;}"
          />
        </el-form-item>
        <el-form-item label="用户信息">
          <el-input
            v-model="form.user_info"
            type="textarea"
            :rows="2"
            placeholder="JSON 对象：昵称/头像/来源等"
          />
        </el-form-item>
        <el-form-item label="来源渠道">
          <ChannelSelect v-model="form.source_channel" />
        </el-form-item>
        <el-form-item label="来源账号">
          <ChannelAccountSelect
            v-model="form.source_account_id"
            :channel="form.source_channel"
          />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.role">
            <el-option
              label="需求方"
              value="demand"
            />
            <el-option
              label="供货方"
              value="supply"
            />
            <el-option
              label="资源方"
              value="resource"
            />
            <el-option
              label="中介"
              value="broker"
            />
            <el-option
              label="待识别"
              value="unknown"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="证据ID">
          <el-input
            v-model="form.source_hit_ids"
            placeholder="多个用逗号分隔"
          />
        </el-form-item>
        <el-form-item label="摘要">
          <el-input
            v-model="form.summary"
            type="textarea"
            :rows="3"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="saving"
            @click="submit"
          >
            归档并关联客户
          </el-button>
        </el-form-item>
      </el-form>

      <el-alert
        v-if="result"
        title="归档结果"
        type="success"
        :closable="false"
        show-icon
      >
        <template #default>
          <span>客户ID：{{ result.customer_id }} ｜ 已关联：{{ result.linked ? '是' : '否' }} ｜ 已通知：{{ result.notified ? '是' : '否' }}</span>
        </template>
      </el-alert>
    </el-card>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { leadsApi } from '@/api/leads'
import ChannelSelect from '@/components/ChannelSelect.vue'
import ChannelAccountSelect from '@/components/ChannelAccountSelect.vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const catalog = useChannelCatalogStore()
catalog.ensure()

const saving = ref(false)
const result = ref(null)
const form = reactive({
  clue_id: '', customer_id: '', identifiers: '', user_info: '',
  source_channel: '', source_account_id: '', role: 'unknown',
  source_hit_ids: '', summary: ''
})

function parseJson(text, fallback) {
  if (!text || !text.trim()) return fallback
  try {
    return JSON.parse(text)
  } catch (e) {
    throw new Error('JSON 格式错误：' + e.message, { cause: e })
  }
}

function split(text) {
  return String(text || '').split(/[,，]/).map((s) => s.trim()).filter(Boolean)
}

async function submit() {
  if (!form.clue_id.trim()) {
    ElMessage.warning('请输入线索ID')
    return
  }
  saving.value = true
  result.value = null
  try {
    const res = await leadsApi.archiveClue({
      clue_id: form.clue_id.trim(),
      customer_id: form.customer_id.trim(),
      identifiers: parseJson(form.identifiers, {}),
      user_info: parseJson(form.user_info, {}),
      source_channel: form.source_channel,
      source_account_id: form.source_account_id,
      role: form.role,
      source_hit_ids: split(form.source_hit_ids),
      summary: form.summary
    })
    result.value = res
    ElMessage.success('归档成功')
  } catch (e) {
    ElMessage.error(e?.message || '归档失败')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.leads-page { padding: 16px; }
.page-head { margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
.el-alert { margin-top: 16px; }
</style>
