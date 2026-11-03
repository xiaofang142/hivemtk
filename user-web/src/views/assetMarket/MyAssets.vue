<template>
  <div class="my-assets">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>我的资产</span>
          <div>
            <el-button
              type="primary"
              @click="$router.push('/asset-market')"
            >
              去市场浏览
            </el-button>
            <el-button @click="openCreate">
              自建资产
            </el-button>
            <el-button @click="$router.push('/asset-market/sync-log')">
              同步日志
            </el-button>
          </div>
        </div>
      </template>

      <el-form
        inline
        class="mb-12"
      >
        <el-form-item label="类型">
          <el-select
            v-model="filter.asset_type"
            clearable
            placeholder="全部"
            @change="fetchList"
          >
            <el-option
              label="智能体角色"
              value="agent_persona"
            />
            <el-option
              label="销冠话术"
              value="sales_script"
            />
            <el-option
              label="AB 测试"
              value="ab_test_plan"
            />
            <el-option
              label="工作流"
              value="marketing_workflow"
            />
            <el-option
              label="行业 SOP"
              value="industry_sop"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="来源">
          <el-select
            v-model="filter.source"
            clearable
            placeholder="全部"
            @change="fetchList"
          >
            <el-option
              label="平台购买"
              value="purchased"
            />
            <el-option
              label="自建"
              value="manual"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-input
            v-model="filter.keyword"
            placeholder="搜索名称"
            clearable
            @keyup.enter="fetchList"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="fetchList"
          >
            查询
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="list"
        stripe
      >
        <el-table-column
          prop="name"
          label="名称"
          min-width="160"
        />
        <el-table-column
          prop="asset_type"
          label="类型"
          width="140"
        />
        <el-table-column
          prop="industry"
          label="行业"
          width="90"
        />
        <el-table-column
          prop="version"
          label="版本"
          width="90"
        />
        <el-table-column
          prop="source"
          label="来源"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="getSourceTagType(row.source)"
            >
              {{ getSourceLabel(row.source) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="90"
        >
          <template #default="{ row }">
            <el-tag
              :type="getEnabledTagType(row.is_active ? 1 : 0)"
              size="small"
            >
              {{ row.is_active ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="use_count"
          label="使用次数"
          width="100"
        />
        <el-table-column
          prop="synced_at"
          label="同步时间"
          width="170">
          <template #default="{ row }">
            {{ fmtTime(row.synced_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="280"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.source === 'purchased' || row.source === 'synced'"
              link
              type="primary"
              @click="handleSync(row)"
            >
              同步
            </el-button>
            <el-button
              link
              type="primary"
              @click="viewDetail(row)"
            >
              查看
            </el-button>
            <el-button
              v-if="row.source === 'purchased' || row.source === 'synced'"
              link
              type="warning"
              @click="handleReport(row)"
            >
              上报使用
            </el-button>
            <el-button
              link
              @click="toggle(row)"
            >
              {{ row.is_active ? '停用' : '启用' }}
            </el-button>
            <el-button
              link
              type="danger"
              @click="remove(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="filter.page"
        class="mt-12"
        :page-size="filter.size"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="fetchList"
      />
    </el-card>

    <el-dialog
      v-model="createVisible"
      title="自建资产"
      width="720px"
    >
      <el-form label-width="100px">
        <el-form-item
          label="名称"
          required
        >
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item
          label="类型"
          required
        >
          <el-select
            v-model="form.asset_type"
            style="width: 100%"
          >
            <el-option
              label="智能体角色"
              value="agent_persona"
            />
            <el-option
              label="销冠话术"
              value="sales_script"
            />
            <el-option
              label="AB 测试"
              value="ab_test_plan"
            />
            <el-option
              label="工作流"
              value="marketing_workflow"
            />
            <el-option
              label="行业 SOP"
              value="industry_sop"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="行业"
          required
        >
          <el-select
            v-model="form.industry"
            style="width: 100%"
          >
            <el-option
              v-for="i in industries"
              :key="i"
              :label="i"
              :value="i"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="JSON 数据"
          required
        >
          <el-input
            v-model="form.dataText"
            type="textarea"
            :rows="12"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitCreate"
        >
          创建
        </el-button>
      </template>
    </el-dialog>

    <el-drawer
      v-model="detailVisible"
      title="资产详情"
      size="50%"
    >
      <pre class="json-preview">{{ detailText }}</pre>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import {
  listLocalAssets,
  getLocalAsset,
  createLocalAsset,
  deleteLocalAsset,
  toggleLocalAsset,
  syncAsset,
  reportUsage
} from '@/api/assetMarket'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getSourceLabel, getSourceTagType } from '@/constants/source';
import { getEnabledTagType } from '@/constants/enabled'
import { INDUSTRY_VALUES } from '@/constants/industry'
import { fmtTime } from '@/utils/time'

const industries = INDUSTRY_VALUES
const loading = ref(false)
const list = ref([])
const total = ref(0)
const filter = ref({ asset_type: '', source: '', keyword: '', page: 1, size: 20 })
const createVisible = ref(false)
const detailVisible = ref(false)
const detailText = ref('')
const form = ref({ name: '', asset_type: 'agent_persona', industry: '美妆', dataText: '' })

const sourceLabel = (s) => getSourceLabel(s)

const fetchList = async () => {
  loading.value = true
  try {
    const resp = await listLocalAssets(filter.value)
    const data = resp?.data || resp || {}
    list.value = Array.isArray(data?.list) ? data.list : (Array.isArray(data) ? data : [])
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

const handleSync = async (row) => {
  await syncAsset({ asset_id: row.asset_id })
  ElMessage.success('同步成功')
  fetchList()
}

const handleReport = async (row) => {
  await reportUsage({ asset_id: row.asset_id })
  ElMessage.success('已上报平台使用次数')
  fetchList()
}

const viewDetail = async (row) => {
  const resp = await getLocalAsset(row.id)
  const data = resp?.data || resp
  detailText.value = JSON.stringify(data, null, 2)
  detailVisible.value = true
}

const toggle = async (row) => {
  await toggleLocalAsset(row.id, !row.is_active)
  ElMessage.success('操作成功')
  fetchList()
}

const remove = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除「${row.name}」？`, '删除', { type: 'warning' })
  } catch {
    return
  }
  await deleteLocalAsset(row.id)
  ElMessage.success('已删除')
  fetchList()
}

const openCreate = () => {
  form.value = {
    name: '',
    asset_type: 'agent_persona',
    industry: '美妆',
    dataText: JSON.stringify(
      {
        schema_version: '1.0',
        asset_type: 'agent_persona',
        name: '自定义助手',
        industry: '美妆',
        version: '1.0.0',
        system_prompt: '你是一位专业助手...',
        persona: { tone: '专业', expertise: ['咨询'] }
      },
      null,
      2
    )
  }
  createVisible.value = true
}

const submitCreate = async () => {
  let data
  try {
    data = JSON.parse(form.value.dataText)
  } catch {
    ElMessage.error('JSON 格式错误')
    return
  }
  data.asset_type = form.value.asset_type
  data.industry = form.value.industry
  data.name = form.value.name || data.name
  await createLocalAsset({
    name: form.value.name,
    asset_type: form.value.asset_type,
    industry: form.value.industry,
    data
  })
  ElMessage.success('创建成功')
  createVisible.value = false
  fetchList()
}

onMounted(fetchList)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.mb-12 {
  margin-bottom: 12px;
}
.mt-12 {
  margin-top: 12px;
}
.json-preview {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 6px;
  font-size: 12px;
  white-space: pre-wrap;
}
</style>
