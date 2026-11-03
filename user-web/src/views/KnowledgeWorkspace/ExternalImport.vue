<template>
  <div class="external-import-page">
    <el-alert
      :title="$t('外部系统文档接入')"
      type="info"
      :closable="false"
      show-icon
    >
      <template #default>
        通过 API Token 鉴权,接入外部系统(飞书 / Notion / 钉钉 / 自有 CRM)的文档数据。
        支持两种模式:1) <strong>{{ $t('异步任务') }}</strong> 提交后返回 job_no,2) <strong>{{ $t('同步模式') }}</strong>(sync=true)立即返回处理结果。
      </template>
    </el-alert>

    <el-row
      :gutter="16"
      style="margin-top: 16px"
    >
      <el-col :span="14">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>导入测试</span>
              <el-tag
                :type="testMode ? 'warning' : 'success'"
                size="small"
              >
                {{ testMode ? '同步模式' : '异步模式' }}
              </el-tag>
            </div>
          </template>
          <el-form
            :model="form"
            label-width="100px"
          >
            <el-form-item label="数据源">
              <el-radio-group
                v-model="form.source"
                @change="handleSourceChange"
              >
                <el-radio-button label="custom">
                  通用 JSON
                </el-radio-button>
                <el-radio-button label="feishu">
                  飞书
                </el-radio-button>
                <el-radio-button label="notion">
                  Notion
                </el-radio-button>
                <el-radio-button
                  label="dingtalk"
                  disabled
                >
                  钉钉（未接入）
                </el-radio-button>
              </el-radio-group>
              <!-- 钉钉没有与飞书/Notion 对等的「按 docID 拉正文」接口（需应用凭证 + 文档权限审批），
                   后端未接入；保留入口但禁用，避免用户填完 DocID 才收到失败。 -->
              <div class="source-hint">
                钉钉文档自动抓取尚未接入：请改用「通用 JSON」粘贴内容，或由钉钉侧调用 OpenAPI 推送 items。
              </div>
            </el-form-item>
            <el-form-item
              label="产品"
              required
            >
              <el-select
                v-model="form.product_id"
                placeholder="选择产品"
                style="width: 100%"
              >
                <el-option
                  v-for="p in productList"
                  :key="p.id"
                  :label="productOptionLabel(p)"
                  :value="p.id"
                />
              </el-select>
            </el-form-item>
            <el-form-item
              v-if="form.source === 'feishu'"
              label="飞书 DocID"
            >
              <el-input
                v-model="form.feishu_doc_id"
                placeholder="如:doccnXYZ123"
              />
            </el-form-item>
            <el-form-item
              v-if="form.source === 'notion'"
              label="Notion PageID"
            >
              <el-input
                v-model="form.notion_page_id"
                placeholder="Notion 页面 ID"
              />
            </el-form-item>
            <el-form-item
              v-if="form.source === 'dingtalk'"
              label="钉钉 DocID"
            >
              <el-input
                v-model="form.dingtalk_doc_id"
                placeholder="钉钉文档 ID"
              />
            </el-form-item>
            <el-form-item
              v-if="form.source === 'custom'"
              label="文档数据"
              required
            >
              <el-input
                v-model="form.itemsJson"
                type="textarea"
                :rows="10"
                placeholder="JSON 数组,例如:&#10;[{&quot;title&quot;: &quot;FAQ1&quot;, &quot;content&quot;: &quot;...&quot;, &quot;category&quot;: &quot;售后&quot;, &quot;tags&quot;: [&quot;退款&quot;]}]"
              />
            </el-form-item>
            <el-form-item>
              <el-checkbox v-model="testMode">
                同步返回(否则异步)
              </el-checkbox>
              <el-button
                :icon="Document"
                style="margin-left: 8px"
                @click="loadTemplate"
              >
                载入模板
              </el-button>
            </el-form-item>
            <el-form-item>
              <el-button
                type="primary"
                :icon="Promotion"
                :loading="importing"
                :disabled="!canSubmit"
                style="width: 100%"
                @click="handleImport"
              >
                提交导入
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>

        
        <el-card
          v-if="lastResponse"
          style="margin-top: 16px"
        >
          <template #header>
            <div class="card-header">
              <span>提交结果</span>
              <el-button
                link
                @click="lastResponse = null"
              >
                关闭
              </el-button>
            </div>
          </template>
          <el-descriptions
            :column="2"
            border
            size="small"
          >
            <el-descriptions-item label="任务号">
              {{ lastResponse.job_no }}
            </el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="statusType(lastResponse.status)">
                {{ statusLabel(lastResponse.status) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="总数">
              {{ lastResponse.total }}
            </el-descriptions-item>
            <el-descriptions-item label="成功">
              {{ lastResponse.accepted }}
            </el-descriptions-item>
            <el-descriptions-item label="失败">
              {{ lastResponse.failed_items }}
            </el-descriptions-item>
            <el-descriptions-item label="异步">
              <el-tag
                :type="lastResponse.async ? 'warning' : 'success'"
                size="small"
              >
                {{ lastResponse.async ? '异步处理中' : '同步完成' }}
              </el-tag>
            </el-descriptions-item>
          </el-descriptions>
          <div
            v-if="lastResponse.document_ids && lastResponse.document_ids.length"
            style="margin-top: 12px"
          >
            <el-text size="small">
              成功文档 ID:
            </el-text>
            <el-tag
              v-for="id in lastResponse.document_ids"
              :key="id"
              size="small"
              style="margin-left: 4px"
            >
              #{{ id }}
            </el-tag>
          </div>
          <el-alert
            v-if="lastResponse.errors && lastResponse.errors.length"
            type="error"
            :closable="false"
            style="margin-top: 12px"
          >
            <template #title>
              失败明细 ({{ lastResponse.errors.length }})
            </template>
            <ul style="margin: 0; padding-left: 20px">
              <li
                v-for="(e, idx) in lastResponse.errors"
                :key="idx"
              >
                {{ e }}
              </li>
            </ul>
          </el-alert>
        </el-card>
      </el-col>

      
      <el-col :span="10">
        <el-card>
          <template #header>
            <span>API Token</span>
          </template>
          <el-form label-width="80px">
            <el-form-item label="Token">
              <el-input
                v-model="apiToken"
                type="password"
                placeholder="请先到「API Token 管理」创建"
                show-password
              />
            </el-form-item>
            <el-form-item>
              <el-text
                size="small"
                type="info"
              >
                外部系统通过 <code>X-Knowledge-Token</code> Header 鉴权
              </el-text>
            </el-form-item>
          </el-form>
        </el-card>

        <el-card style="margin-top: 16px">
          <template #header>
            <div class="card-header">
              <span>历史任务</span>
              <el-button
                link
                :icon="Refresh"
                @click="loadJobs"
              >
                刷新
              </el-button>
            </div>
          </template>
          <el-select
            v-model="jobFilterProduct"
            placeholder="按产品筛选"
            clearable
            style="width: 100%; margin-bottom: 8px"
            @change="loadJobs"
          >
            <el-option
              v-for="p in productList"
              :key="p.id"
              :label="productOptionLabel(p)"
              :value="p.id"
            />
          </el-select>
          <el-table
            v-loading="jobsLoading"
            :data="jobs"
            max-height="380"
            size="small"
          >
            <el-table-column
              prop="job_no"
              label="任务号"
              width="160"
            />
            <el-table-column
              prop="source"
              label="来源"
              width="80"
            >
              <template #default="{ row }">
                <el-tag size="small">
                  {{ row.source }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="状态"
              width="90"
            >
              <template #default="{ row }">
                <el-tag
                  :type="statusType(row.status)"
                  size="small"
                >
                  {{ statusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="total_items"
              label="总数"
              width="60"
            />
            <el-table-column
              label="成功/失败"
              width="100"
            >
              <template #default="{ row }">
                <span style="color: #10B981">{{ row.done_items || 0 }}</span>
                /
                <span style="color: #EF4444">{{ row.failed_items || 0 }}</span>
              </template>
            </el-table-column>
            <el-table-column
              prop="created_at"
              label="时间"
              width="150"
            >
              <template #default="{ row }">
                {{ formatDate(row.created_at) }}
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Document, Promotion } from '@element-plus/icons-vue'
import { knowledgeMerchantAPI } from '@/api/knowledgeMerchant'
import { isUserReportedError } from '@/utils/errorReporting'
import { ragProductConfigAPI } from '@/api/ragProductConfig'

const importing = ref(false)
const jobsLoading = ref(false)
const productList = ref([])
const apiToken = ref('')
const jobFilterProduct = ref('')
const jobs = ref([])
const lastResponse = ref(null)

const form = reactive({
  source: 'custom',
  product_id: '',
  feishu_doc_id: '',
  notion_page_id: '',
  itemsJson: ''
})

const testMode = ref(false)

// 产品列表里存在同名产品（如「通用电商客服知识库」有 seed 与正式两条），
// 下拉只显示名字时无法区分，重名时补上产品 ID。
const duplicatedProductNames = computed(() => {
  const seen = new Map()
  productList.value.forEach((p) => seen.set(p.name, (seen.get(p.name) || 0) + 1))
  return new Set([...seen.entries()].filter(([, n]) => n > 1).map(([name]) => name))
})
const productOptionLabel = (p) => (duplicatedProductNames.value.has(p.name) ? `${p.name}（${p.id}）` : p.name)

const canSubmit = computed(() => {
  if (!form.product_id) return false
  if (form.source === 'custom') return form.itemsJson.trim().length > 0
  if (form.source === 'feishu') return !!form.feishu_doc_id
  if (form.source === 'dingtalk') return !!form.dingtalk_doc_id
  if (form.source === 'notion') return !!form.notion_page_id
  return false
})

const loadProducts = async () => {
  try {
    const res = await ragProductConfigAPI.listProducts()
    if (Array.isArray(res)) productList.value = res
    else if (res?.items) productList.value = res.items
  } catch (e) {
    console.error('加载产品失败:', e)
  }
}

const handleSourceChange = () => {
  form.feishu_doc_id = ''
  form.dingtalk_doc_id = ''
  form.notion_page_id = ''
  form.itemsJson = ''
}

const loadTemplate = () => {
  form.itemsJson = JSON.stringify([
    { title: i18n.global.t('退款流程'), content: i18n.global.t('用户可在订单页面提交退款申请,审核通过后 3-5 个工作日原路退回。'), category: '售后', tags: ['退款', '售后'] },
    { title: i18n.global.t('保修政策'), content: i18n.global.t('产品自购买之日起 12 个月内提供免费保修服务。'), category: '售后', tags: ['保修'] }
  ], null, 2)
}

const handleImport = async () => {
  if (!canSubmit.value) {
    ElMessage.warning(i18n.global.t('请填写完整信息'))
    return
  }
  if (!apiToken.value) {
    ElMessage.warning(i18n.global.t('请填写 API Token'))
    return
  }
  let items
  if (form.source === 'custom') {
    try {
      items = JSON.parse(form.itemsJson)
      if (!Array.isArray(items)) throw new Error('items 必须是数组')
    } catch (e) {
      ElMessage.error('JSON 格式错误: ' + e.message)
      return
    }
  }
  importing.value = true
  try {
    const payload = {
      source: form.source,
      product_id: form.product_id,
      sync: testMode.value
    }
    if (form.source === 'feishu') {
      payload.feishu_doc_id = form.feishu_doc_id
    } else if (form.source === 'dingtalk') {
      payload.dingtalk_doc_id = form.dingtalk_doc_id
    } else if (form.source === 'notion') {
      payload.notion_page_id = form.notion_page_id
    } else {
      payload.items = items
    }
    const res = await knowledgeMerchantAPI.externalImport(payload, apiToken.value)
    if (res) {
      lastResponse.value = res
      ElMessage.success(res.async ? '任务已提交,异步处理中' : '导入完成')
      loadJobs()
    }
  } catch (e) {
    // 拦截器已经弹过后端可读文案（如「飞书抓取未配置凭证 …」），这里再弹一次会重复
    if (!isUserReportedError(e)) ElMessage.error('提交失败: ' + (e.message || ''))
  } finally {
    importing.value = false
  }
}

const loadJobs = async () => {
  jobsLoading.value = true
  try {
    const res = await knowledgeMerchantAPI.listExternalJobs({
      product_id: jobFilterProduct.value,
      page: 1,
      page_size: 20
    })
    jobs.value = res?.items || []
  } catch (e) {
    console.error('加载任务失败:', e)
  } finally {
    jobsLoading.value = false
  }
}

const statusType = (s) => ({ pending: 'info', running: 'warning', completed: 'success', failed: 'danger' }[s] || '')
const statusLabel = (s) => ({ pending: '等待中', running: '处理中', completed: '已完成', failed: '失败' }[s] || s)
const formatDate = (d) => d ? new Date(d).toLocaleString('zh-CN') : '-'

onMounted(async () => {
  await loadProducts()
  await loadJobs()
  apiToken.value = localStorage.getItem('knowledge_merchant_token') || '';
})

import { watch } from 'vue';
watch(apiToken, (val) => {
  if (val) localStorage.setItem('knowledge_merchant_token', val)
})
</script>

<style scoped lang="scss">
.source-hint {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
}

.external-import-page {
  padding: 0;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
