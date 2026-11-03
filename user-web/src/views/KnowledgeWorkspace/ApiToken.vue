<template>
  <div class="api-token-page">
    <el-alert
      :title="$t('API Token 管理')"
      type="info"
      :closable="false"
      show-icon
    >
      <template #default>
        为外部系统(自有 CRM/ERP/Helpdesk)创建 API Token,可通过 <code>/api/knowledge-merchant/external/import</code> 推送文档。
        创建时明文仅显示一次,请立即保存;之后系统只保存哈希。
      </template>
    </el-alert>

    <el-row
      :gutter="16"
      style="margin-top: 16px"
    >
      <el-col :span="16">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>Token 列表</span>
              <div>
                <el-select
                  v-model="filterProductId"
                  placeholder="按产品筛选"
                  clearable
                  style="width: 180px"
                  @change="loadTokens"
                >
                  <el-option
                    v-for="p in productList"
                    :key="p.id"
                    :label="productOptionLabel(p)"
                    :value="p.id"
                  />
                </el-select>
                <el-button
                  :icon="Refresh"
                  style="margin-left: 8px"
                  @click="loadTokens"
                >
                  刷新
                </el-button>
              </div>
            </div>
          </template>
          <el-table
            v-loading="loading"
            :data="tokens"
            stripe
          >
            <el-table-column
              prop="id"
              label="ID"
              width="80"
            />
            <el-table-column
              prop="name"
              label="名称"
              min-width="140"
              show-overflow-tooltip
            />
            <el-table-column
              label="产品"
              width="160"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ getProductName(row.product_id) }}
              </template>
            </el-table-column>
            <el-table-column
              label="权限"
              width="160"
            >
              <template #default="{ row }">
                <el-tag
                  v-for="s in parseScopes(row.scopes)"
                  :key="s"
                  size="small"
                  style="margin-right: 4px"
                >
                  {{ scopeLabel(s) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="状态"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  v-if="row.enabled === 1"
                  type="success"
                >
                  启用
                </el-tag>
                <el-tag
                  v-else
                  type="danger"
                >
                  已吊销
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="使用统计"
              width="140"
            >
              <template #default="{ row }">
                <div>调用 {{ row.use_count || 0 }} 次</div>
                <el-text
                  v-if="row.last_used_at"
                  size="small"
                  type="info"
                >
                  最后: {{ formatDate(row.last_used_at) }}
                </el-text>
              </template>
            </el-table-column>
            <el-table-column
              label="过期时间"
              width="170"
            >
              <template #default="{ row }">
                <span v-if="row.expires_at">{{ formatDate(row.expires_at) }}</span>
                <span
                  v-else
                  style="color: #909399"
                >永不过期</span>
              </template>
            </el-table-column>
            <el-table-column
              prop="created_by"
              label="创建人"
              width="100"
            />
            <el-table-column
              label="操作"
              width="140"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  v-if="row.enabled === 1"
                  link
                  type="danger"
                  size="small"
                  @click="handleRevoke(row)"
                >
                  吊销
                </el-button>
                <el-text
                  v-else
                  type="info"
                  size="small"
                >
                  已吊销
                </el-text>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      
      <el-col :span="8">
        <el-card>
          <template #header>
            <span>创建 Token</span>
          </template>
          <el-form
            ref="formRef"
            :model="form"
            :rules="rules"
            label-width="100px"
          >
            <el-form-item
              label="名称"
              prop="name"
            >
              <el-input
                v-model="form.name"
                placeholder="如:CRM 推送"
              />
            </el-form-item>
            <el-form-item
              label="产品"
              prop="product_id"
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
            <el-form-item label="权限">
              <el-checkbox-group v-model="form.scopes">
                <el-checkbox label="read">
                  只读
                </el-checkbox>
                <el-checkbox label="write">
                  可写
                </el-checkbox>
              </el-checkbox-group>
              <div class="form-hint">
                不勾选则默认只读；需要写入请勾选「可写」
              </div>
            </el-form-item>
            <el-form-item label="过期时间">
              <el-date-picker
                v-model="form.expires_at"
                type="datetime"
                placeholder="不填则永不过期"
                style="width: 100%"
                value-format="YYYY-MM-DDTHH:mm:ssZ"
                format="YYYY-MM-DD HH:mm"
              />
            </el-form-item>
            <el-form-item>
              <el-button
                type="primary"
                :loading="creating"
                style="width: 100%"
                @click="handleCreate"
              >
                创建
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>

        
        <el-card style="margin-top: 16px">
          <template #header>
            <span>外部系统调用示例</span>
          </template>
          <pre class="code-block">
# 1. 推送文档(JSON)
curl -X POST \
  'http://your-domain/api/knowledge-merchant/external/import' \
  -H 'X-Knowledge-Token: kbg_xxxx' \
  -H 'Content-Type: application/json' \
  -d '{
    "source": "custom",
    "product_id": "your-product",
    "items": [
      {"title": "FAQ1", "content": "..."}
    ]
  }'

# 2. 同步返回
curl -X POST \
  'http://your-domain/api/knowledge-merchant/external/import?sync=true' \
  -H 'X-Knowledge-Token: kbg_xxxx' \
  -H 'Content-Type: application/json' \
  -d '{"source":"custom","product_id":"...","items":[]}'</pre>
        </el-card>
      </el-col>
    </el-row>

    
    <el-dialog
      v-model="showTokenDialog"
      title="Token 已创建"
      width="640px"
      :close-on-click-modal="false"
    >
      <el-alert
        type="success"
        :closable="false"
        show-icon
      >
        <template #default>
          请立即复制并保存明文 Token,关闭后无法再次查看。
        </template>
      </el-alert>
      <el-form
        label-width="80px"
        style="margin-top: 16px"
      >
        <el-form-item label="名称">
          <el-text>{{ createdToken.name }}</el-text>
        </el-form-item>
        <el-form-item label="产品">
          <el-text>{{ getProductName(createdToken.product_id) }}</el-text>
        </el-form-item>
        <el-form-item label="Token">
          <el-input
            v-model="createdToken.token_plain"
            readonly
          >
            <template #append>
              <el-button
                :icon="CopyDocument"
                @click="copyToken"
              >
                复制
              </el-button>
            </template>
          </el-input>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button
          type="primary"
          @click="showTokenDialog = false"
        >
          我已保存,关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, CopyDocument } from '@element-plus/icons-vue'
import { knowledgeMerchantAPI } from '@/api/knowledgeMerchant'
import { copyToClipboard } from '@/utils/clipboard'
import { ragProductConfigAPI } from '@/api/ragProductConfig'
import { isUserReportedError } from '@/utils/errorReporting'
import { fmtTime } from '@/utils/time'

const loading = ref(false)
const creating = ref(false)
const tokens = ref([])
const productList = ref([])
const filterProductId = ref('')

const formRef = ref(null)
const form = reactive({
  name: '',
  product_id: '',
  scopes: ['read', 'write'],
  expires_at: null
})

const rules = {
  name: [{ required: true, message: i18n.global.t('请输入名称'), trigger: 'blur' }],
  product_id: [{ required: true, message: i18n.global.t('请选择产品'), trigger: 'change' }]
}

const showTokenDialog = ref(false)
const createdToken = ref({ name: '', product_id: 0, token_plain: '' })

const loadProducts = async () => {
  try {
    const res = await ragProductConfigAPI.listProducts()
    if (Array.isArray(res)) productList.value = res
    else if (res?.items) productList.value = res.items
  } catch (e) {
    console.error('加载产品失败:', e)
  }
}

const loadTokens = async () => {
  loading.value = true
  try {
    const res = await knowledgeMerchantAPI.listTokens({
      product_id: filterProductId.value
    })
    tokens.value = res?.items || res || []
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('加载 Token 失败: ' + (e.message || ''))
  } finally {
    loading.value = false
  }
}

// 产品下拉里存在重名（如两条「通用电商客服知识库」），重名时补 ID 以便区分
const duplicatedProductNames = computed(() => {
  const names = productList.value.map((p) => p.name)
  return new Set(names.filter((n, i) => names.indexOf(n) !== i))
})
const productOptionLabel = (p) =>
  duplicatedProductNames.value.has(p.name) ? `${p.name}（${p.id}）` : p.name

const getProductName = (id) => {
  const p = productList.value.find(p => p.id === id)
  return p ? p.name : `#${id}`
}

const parseScopes = (scopes) => {
  try {
    return JSON.parse(scopes)
  } catch {
    return []
  }
}

const scopeLabel = (s) => {
  return { read: '读', write: '写', '*': '全部' }[s] || s
}

const handleCreate = async () => {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  creating.value = true
  try {
    const res = await knowledgeMerchantAPI.createToken({
      name: form.name,
      product_id: form.product_id,
      scopes: form.scopes,
      expires_at: form.expires_at || null
    })
    if (res) {
      createdToken.value = res
      showTokenDialog.value = true
      ElMessage.success(i18n.global.t('Token 已创建'))
      formRef.value.resetFields()
      form.scopes = ['read', 'write']
      form.expires_at = null
      loadTokens()
    }
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('创建失败: ' + (e.message || ''))
  } finally {
    creating.value = false
  }
}

const handleRevoke = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确认吊销 Token「${row.name}」吗?吊销后外部系统将无法继续使用。`,
      '吊销 Token',
      { type: 'warning' }
    )
    await knowledgeMerchantAPI.revokeToken(row.id)
    ElMessage.success(i18n.global.t('已吊销'))
    loadTokens()
  } catch (e) {
    // 拦截器已弹过后端文案，取消/关闭确认框按已报处理
    if (!isUserReportedError(e)) ElMessage.error('吊销失败: ' + (e.message || ''))
  }
}

const copyToken = async () => {
  if (await copyToClipboard(createdToken.value.token_plain)) {
    ElMessage.success(i18n.global.t('已复制到剪贴板'))
  } else {
    ElMessage.warning(i18n.global.t('复制失败,请手动选择'))
  }
}

// 统一时间口径：零值/非法时间显示 '-'，其余按当前语言格式化
const formatDate = (d) => fmtTime(d, undefined, '-')

onMounted(async () => {
  await loadProducts()
  await loadTokens()
})
</script>

<style scoped lang="scss">
.api-token-page {
  padding: 0;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.form-hint {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}

.code-block {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 12px;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
