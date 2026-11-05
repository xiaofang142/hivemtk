<template>
  <div class="geo-page p-4">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <div>
            <span class="title">承接链接</span>
            <span class="muted">
              从文章/引用流量接一个公开落地页：访客可先问知识库，再留联系方式；线索进统一线索池，按链接统计首响。
            </span>
          </div>
          <div class="actions">
            <el-input
              v-model="keyword"
              placeholder="搜承接码 / 标题 / 关键词"
              clearable
              style="width: 240px"
              @keyup.enter="reload"
              @clear="reload"
            />
            <el-button @click="reload">
              查询
            </el-button>
            <el-button
              type="primary"
              @click="openCreate"
            >
              新建承接链接
            </el-button>
          </div>
        </div>
      </template>

      <el-table
        v-loading="loading"
        :data="list"
        border
        size="small"
      >
        <el-table-column
          prop="code"
          label="承接码"
          width="140"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              effect="plain"
            >
              {{ row.code }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="title"
          label="标题 / 关键词"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <div>{{ row.title }}</div>
            <div class="muted sub">
              {{ row.keyword || '未设关键词' }}
            </div>
          </template>
        </el-table-column>
        <el-table-column
          label="落地页"
          min-width="220"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <div class="mono">
              {{ row.landing_path || ('/api/public/geo/l/' + row.code) }}
            </div>
            <div
              v-if="row.short_url"
              class="muted mono sub"
            >
              {{ row.short_url }}
            </div>
          </template>
        </el-table-column>
        <el-table-column
          prop="clicks"
          label="点击"
          width="80"
        />
        <el-table-column
          prop="leads"
          label="线索"
          width="80"
        />
        <el-table-column
          label="创建时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="170"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openDetail(row)"
            >
              统计
            </el-button>
            <el-button
              link
              @click="copy(row.landing_url || row.landing_path)"
            >
              复制链接
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

      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          :page-size="limit"
          :total="total"
          layout="total, prev, pager, next"
          @current-change="load"
        />
      </div>
    </el-card>

    <el-dialog
      v-model="createVisible"
      title="新建承接链接"
      width="620px"
    >
      <el-form
        :model="form"
        label-width="92px"
      >
        <el-form-item label="目标链接">
          <el-input
            v-model="form.target_url"
            placeholder="https://example.com/geo（访客点“查看详情”去这里）"
          />
        </el-form-item>
        <el-form-item label="标题">
          <el-input
            v-model="form.title"
            maxlength="255"
            placeholder="留空则用关键词"
          />
        </el-form-item>
        <el-form-item label="关键词">
          <el-input
            v-model="form.keyword"
            maxlength="255"
            placeholder="这条链接对应的 GEO 关键词（选填）"
          />
        </el-form-item>
        <el-form-item label="承接码">
          <el-input
            v-model="form.code"
            maxlength="32"
            placeholder="留空自动生成；4-32 位字母/数字/-/_"
          />
        </el-form-item>
        <el-form-item label="关联文章">
          <el-select
            v-model="form.article_id"
            filterable
            clearable
            placeholder="可选：绑定一篇文章（UUID）"
            style="width: 100%"
          >
            <el-option
              v-for="a in articles"
              :key="a.id"
              :label="a.title || a.keyword || a.id"
              :value="a.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="归因链 ID">
          <el-input
            v-model="form.chain_id"
            placeholder="选填；留空按 geo_link:<承接码> 归因"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          @click="submitCreate"
        >
          创建
        </el-button>
      </template>
    </el-dialog>

    <el-drawer
      v-model="detailVisible"
      title="承接统计"
      size="520px"
    >
      <template v-if="detail">
        <el-descriptions
          :column="2"
          border
          size="small"
        >
          <el-descriptions-item label="承接码">
            {{ detail.code }}
          </el-descriptions-item>
          <el-descriptions-item label="标题">
            {{ detail.title }}
          </el-descriptions-item>
          <el-descriptions-item label="点击">
            {{ detail.clicks }}
          </el-descriptions-item>
          <el-descriptions-item label="线索">
            {{ detail.leads }}
          </el-descriptions-item>
          <el-descriptions-item
            label="落地页"
            :span="2"
          >
            <span class="mono">{{ detail.landing_url || detail.landing_path }}</span>
          </el-descriptions-item>
          <el-descriptions-item
            v-if="detail.short_url"
            label="短链"
            :span="2"
          >
            <span class="mono">{{ detail.short_url }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <el-card
          shadow="never"
          class="stat-card"
        >
          <template #header>
            <span>首响</span>
          </template>
          <template v-if="detail.connected">
            <el-row :gutter="12">
              <el-col :span="8">
                <el-statistic
                  title="已关联会话"
                  :value="detail.responses?.matched || 0"
                />
              </el-col>
              <el-col :span="8">
                <el-statistic
                  title="已人工首响"
                  :value="detail.responses?.replied || 0"
                />
              </el-col>
              <el-col :span="8">
                <el-statistic
                  title="平均首响"
                  :value="Math.round(detail.responses?.avg_seconds || 0)"
                  suffix="秒"
                />
              </el-col>
            </el-row>
          </template>
          <el-empty
            v-else
            :image-size="60"
            description="未接入：还没有线索能通过 OneID 关联到客服会话，不展示零延迟假数据"
          />
        </el-card>

        <div class="muted">
          公开页：访客可提问（每日 {{ 200 }} 次）或留资（每日 {{ 100 }} 条）；
          线索写入统一线索池的类型 18，有 OneID 时回填归因链。
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { geoApi } from '@/api/geo'
import {
  createCaptureLink,
  deleteCaptureLink,
  getCaptureLink,
  listCaptureLinks
} from '@/api/geoCapture'
import { isUserReportedError } from '@/utils/errorReporting'

const list = ref([])
const total = ref(0)
const page = ref(1)
const limit = ref(20)
const keyword = ref('')
const loading = ref(false)

const createVisible = ref(false)
const creating = ref(false)
const articles = ref([])
const form = reactive({
  target_url: '',
  title: '',
  keyword: '',
  code: '',
  article_id: '',
  chain_id: ''
})

const detailVisible = ref(false)
const detail = ref(null)

function formatTime (v) {
  if (!v) return '-'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
}

async function load () {
  loading.value = true
  try {
    const res = await listCaptureLinks({ page: page.value, limit: limit.value, keyword: keyword.value })
    list.value = res?.list || []
    total.value = res?.total || 0
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '加载承接链接失败')
  } finally {
    loading.value = false
  }
}

function reload () {
  page.value = 1
  load()
}

function openCreate () {
  Object.assign(form, { target_url: '', title: '', keyword: '', code: '', article_id: '', chain_id: '' })
  createVisible.value = true
}

async function loadArticles () {
  try {
    const res = await geoApi.getArticleList({ page: 1, limit: 100 })
    articles.value = res?.list || res?.items || (Array.isArray(res) ? res : [])
  } catch {
    articles.value = []
  }
}

async function submitCreate () {
  if (!form.target_url.trim()) {
    ElMessage.warning('请填写目标链接')
    return
  }
  creating.value = true
  try {
    const res = await createCaptureLink({ ...form })
    ElMessage.success('已创建：' + (res?.landing_url || res?.landing_path || ''))
    createVisible.value = false
    await load()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '创建失败')
  } finally {
    creating.value = false
  }
}

async function openDetail (row) {
  try {
    detail.value = await getCaptureLink(row.id)
    detailVisible.value = true
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '读取统计失败')
  }
}

async function remove (row) {
  try {
    await ElMessageBox.confirm(`删除承接链接 ${row.code}？公开页会立即下线。`, '删除确认', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteCaptureLink(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '删除失败')
  }
}

async function copy (text) {
  if (!text) return
  const url = text.startsWith('http') ? text : window.location.origin + text
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('链接已复制')
  } catch {
    ElMessage.warning(url)
  }
}

onMounted(() => {
  load()
  loadArticles()
})
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.title {
  font-size: 16px;
  font-weight: 600;
}
.muted {
  color: #6b7280;
  font-size: 12px;
  margin-left: 8px;
}
.sub {
  margin-left: 0;
  margin-top: 2px;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  word-break: break-all;
}
.actions {
  display: flex;
  gap: 8px;
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
.stat-card {
  margin-top: 16px;
}
</style>
