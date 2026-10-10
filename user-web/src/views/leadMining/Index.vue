<template>
  <div class="lead-mining-page">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="发掘设置" name="setting">
        <el-card shadow="never">
          <el-form :model="form" label-width="120px" class="setting-form">
            <el-form-item label="启用发掘">
              <el-switch v-model="form.enabled" />
              <span class="hint">关闭后不再对消息做线索判断（已发掘线索保留）</span>
            </el-form-item>

            <el-form-item label="监测渠道">
              <el-checkbox-group v-model="form.channels">
                <el-checkbox v-for="c in channelOptions" :key="c.value" :label="c.value">{{ c.label }}</el-checkbox>
              </el-checkbox-group>
              <div class="hint">不勾选任何渠道 = 监测全部渠道（含 TG 群聊 / 抖音群聊的每条消息）</div>
            </el-form-item>

            <el-form-item label="关键词">
              <el-input
                v-model="keywordsText"
                type="textarea"
                :rows="3"
                placeholder="多个关键词用逗号或换行分隔，例如：报价, 购买, 代理, 合作"
              />
              <div class="hint">命中任一关键词将提升线索意向分（仅供 LLM 参考，非唯一标准）</div>
            </el-form-item>

            <el-form-item label="线索标签">
              <el-input
                v-model="tagsText"
                type="textarea"
                :rows="2"
                placeholder="命中线索后打给客户的标签，逗号分隔，例如：高意向, 待跟进"
              />
            </el-form-item>

            <el-form-item label="线索要求">
              <el-input
                v-model="form.requirement"
                type="textarea"
                :rows="4"
                placeholder="用自然语言描述「什么算线索」，例如：客户明确表达购买意向、询问价格/代理政策、留下联系方式等"
              />
            </el-form-item>

            <el-form-item label="意向分阈值">
              <el-input-number v-model="form.min_intent_score" :min="0" :max="100" :step="5" />
              <span class="hint">LLM 给出的意向分 ≥ 该值才记入线索库存</span>
            </el-form-item>

            <el-form-item label="指定模型">
              <el-input v-model="form.model" placeholder="留空使用默认推理模型" style="max-width: 320px" />
            </el-form-item>

            <el-form-item>
              <el-button type="primary" :loading="saving" @click="onSave">保存设置</el-button>
              <el-button @click="onReset">重置</el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-tab-pane>

      <el-tab-pane label="线索库" name="leads">
        <el-card shadow="never">
          <div class="toolbar">
            <el-input
              v-model="kw"
              placeholder="搜索摘要/名称"
              clearable
              style="width: 240px"
              @input="loadLeads"
            />
            <el-button type="primary" @click="loadLeads">刷新</el-button>
          </div>
          <el-table :data="leads" v-loading="loading" stripe style="margin-top: 12px">
            <el-table-column prop="name" label="名称" min-width="160" />
            <el-table-column prop="desc" label="线索摘要" min-width="320" show-overflow-tooltip />
            <el-table-column label="意向分" width="100">
              <template #default="{ row }">
                <el-tag :type="row.intent_score >= 70 ? 'danger' : (row.intent_score >= 40 ? 'warning' : 'info')">
                  {{ row.intent_score }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="商机" width="90">
              <template #default="{ row }">
                <el-tag v-if="row.is_opportunity === 1 || row.is_opportunity === true" type="success">高意向</el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column prop="one_id" label="客户标识" min-width="160" show-overflow-tooltip />
            <el-table-column label="发现时间" width="170">
              <template #default="{ row }">{{ formatTime(row.create_time) }}</template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-if="total > pageSize"
            layout="prev, pager, next"
            :total="total"
            :page-size="pageSize"
            @current-change="onPage"
            style="margin-top: 12px"
          />
        </el-card>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getLeadMiningConfig, saveLeadMiningConfig } from '@/api/leadMining'
import { clueApi } from '@/api/clue'

const activeTab = ref('setting')
const saving = ref(false)
const keywordsText = ref('')
const tagsText = ref('')
const form = reactive({
  enabled: false,
  channels: [],
  requirement: '',
  min_intent_score: 50,
  model: ''
})

const channelOptions = [
  { value: 'telegram', label: 'Telegram' },
  { value: 'whatsapp', label: 'WhatsApp' },
  { value: 'douyin', label: '抖音' },
  { value: 'xiaohongshu', label: '小红书' },
  { value: 'wecom', label: '企业微信' },
  { value: 'wechat', label: '微信' },
  { value: 'xianyu', label: '闲鱼' },
  { value: 'kuaishou', label: '快手' },
  { value: 'tiktok', label: 'TikTok' },
  { value: 'instagram', label: 'Instagram' }
]

function parseList(text) {
  return (text || '')
    .split(/[\n,，、]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

async function loadConfig() {
  try {
    const res = await getLeadMiningConfig()
    // 拦截器已拆包：config 直接回配置对象，按信封取 .data 只会读到 undefined
    const data = res?.data?.data || res?.data || res || {}
    form.enabled = !!data.enabled
    form.channels = data.channels || []
    form.requirement = data.requirement || ''
    form.min_intent_score = data.min_intent_score || 50
    form.model = data.model || ''
    keywordsText.value = (data.keywords || []).join(', ')
    tagsText.value = (data.tags || []).join(', ')
  } catch (e) {
    ElMessage.error('加载配置失败：' + (e.message || e))
  }
}

async function onSave() {
  saving.value = true
  const payload = {
    enabled: form.enabled,
    channels: form.channels,
    requirement: form.requirement,
    min_intent_score: form.min_intent_score,
    model: form.model,
    keywords: parseList(keywordsText.value),
    tags: parseList(tagsText.value)
  }
  try {
    await saveLeadMiningConfig(payload)
    ElMessage.success('保存成功')
  } catch (e) {
    ElMessage.error('保存失败：' + (e.message || e))
  } finally {
    saving.value = false
  }
}

function onReset() {
  loadConfig()
}

const leads = ref([]);
const loading = ref(false)
const total = ref(0)
const pageSize = ref(20)
const page = ref(1)
const kw = ref('')

function formatTime(t) {
  if (!t) return '-'
  const d = new Date(t * 1000)
  if (isNaN(d.getTime())) return String(t)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function loadLeads() {
  loading.value = true
  try {
    const res = await clueApi.list(page.value, pageSize.value)
    const envelope = res?.data ?? res
    const data = envelope?.data ?? envelope
    const list = data?.list ?? (Array.isArray(data) ? data : [])
    let filtered = list.filter((c) => Number(c.type) === 8);
    if (kw.value) {
      const k = kw.value.toLowerCase()
      filtered = filtered.filter(
        (c) => (c.name || '').toLowerCase().includes(k) || (c.desc || '').toLowerCase().includes(k)
      )
    }
    leads.value = filtered
    total.value = data?.total ?? list.length
  } catch (e) {
    ElMessage.error('加载线索失败：' + (e.message || e))
  } finally {
    loading.value = false
  }
}

function onPage(p) {
  page.value = p
  loadLeads()
}

onMounted(() => {
  loadConfig()
  loadLeads()
})
</script>

<style scoped>
.lead-mining-page {
  padding: 16px;
}
.setting-form {
  max-width: 760px;
}
.hint {
  color: #909399;
  font-size: 12px;
  margin-left: 10px;
}
.toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
}
</style>
