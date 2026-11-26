<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          客户 360 视图
        </h2>
        <span class="page-sub">{{ t('businessCopy.crm.profileSubtitle') }}</span>
      </div>
      <el-button
        type="primary"
        :loading="aiLoading"
        @click="analyze"
      >
        生成 AI 建议
      </el-button>
    </div>

    <el-card shadow="never">
      <el-form
        inline
        label-width="60px"
        @submit.prevent
      >
        <el-form-item label="客户ID">
          <el-input
            v-model="customerId"
            placeholder="输入客户 ID"
            style="width: 320px"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="loadView"
          >
            加载 360
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card
      v-if="view"
      shadow="never"
      style="margin-top: 12px"
    >
      <template #header>
        <span>基础信息</span>
      </template>
      <el-descriptions
        :column="3"
        border
      >
        <el-descriptions-item label="客户ID">
          {{ view.customer_id }}
        </el-descriptions-item>
        <el-descriptions-item label="姓名">
          {{ view.profile?.basic_info?.user_name || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="行业">
          {{ view.profile?.basic_info?.industry || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="来源账号">
          {{ view.profile?.basic_info?.source_account_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="联系人">
          {{ (view.contacts || []).length }}
        </el-descriptions-item>
        <el-descriptions-item label="供应商">
          {{ (view.suppliers || []).length }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <el-row
      :gutter="12"
      style="margin-top: 12px"
    >
      <el-col :span="12">
        <el-card shadow="never">
          <template #header>
            <span>AI 建议（每条附证据）</span>
          </template>
          <el-empty
            v-if="!suggestions.length"
            description="尚未生成"
          />
          <div
            v-for="(s, i) in suggestions"
            :key="i"
            class="suggestion"
          >
            <div class="suggestion-head">
              <el-tag
                size="small"
                type="info"
              >
                {{ s.kind }}
              </el-tag>
              <b>{{ s.title }}</b>
              <span class="confidence">置信度 {{ Math.round((s.confidence || 0) * 100) }}%</span>
            </div>
            <p>{{ s.content }}</p>
            <div class="evidence">
              <span
                v-for="(e, j) in s.evidence || []"
                :key="j"
                class="evidence-item"
              >
                {{ e.type }}:{{ e.ref }} = {{ e.snippet }}
              </span>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card shadow="never">
          <template #header>
            <span>合并留痕</span>
          </template>
          <el-table
            :data="view?.merge_history || []"
            border
            size="small"
            empty-text="无合并记录"
          >
            <el-table-column
              prop="merge_id"
              label="合并ID"
              min-width="150"
            />
            <el-table-column
              prop="rule_name"
              label="规则"
              min-width="110"
            />
            <el-table-column
              prop="operator_name"
              label="操作人"
              min-width="100"
            />
            <el-table-column
              prop="created_at"
              label="时间"
              min-width="160"
            />
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <el-card
      shadow="never"
      style="margin-top: 12px"
    >
      <template #header>
        <span>跨账号行业交叉检索</span>
      </template>
      <el-form
        inline
        label-width="60px"
        @submit.prevent
      >
        <el-form-item label="关键词">
          <el-input
            v-model="cross.keyword"
            style="width: 200px"
          />
        </el-form-item>
        <el-form-item label="行业">
          <el-input
            v-model="cross.industry"
            style="width: 160px"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="searchCross"
          >
            检索
          </el-button>
        </el-form-item>
      </el-form>
      <el-table
        :data="crossRows"
        border
        stripe
        size="small"
        empty-text="暂无结果"
      >
        <el-table-column
          prop="name"
          label="姓名"
          min-width="120"
        />
        <el-table-column
          prop="phone"
          label="手机"
          min-width="140"
        />
        <el-table-column
          prop="industry"
          label="行业"
          min-width="110"
        />
        <el-table-column
          prop="source_account_id"
          label="来源账号"
          min-width="140"
        />
      </el-table>
      <div class="cross-tab">
        <el-tag
          v-for="(c, i) in crossTab"
          :key="i"
          size="small"
          style="margin: 2px"
        >
          {{ c.industry || '未分类' }} / {{ c.source_account_id || '无账号' }}: {{ c.count }}
        </el-tag>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { crmApi } from '@/api/crm'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const route = useRoute()
const customerId = ref(String(route.params.id || ''))
const view = ref(null)
const suggestions = ref([])
const aiLoading = ref(false)
const cross = reactive({ keyword: '', industry: '' })
const crossRows = ref([])
const crossTab = ref([])

async function loadView() {
  if (!customerId.value) return
  try {
    view.value = await crmApi.view360(customerId.value.trim())
  } catch (e) {
    ElMessage.error(e?.message || '加载 360 失败')
  }
}

async function analyze() {
  if (!customerId.value) {
    ElMessage.warning('请先填写客户 ID')
    return
  }
  aiLoading.value = true
  try {
    const res = await crmApi.aiAnalyze({ customer_id: customerId.value.trim() })
    suggestions.value = Array.isArray(res?.suggestions) ? res.suggestions : []
  } catch (e) {
    ElMessage.error(e?.message || 'AI 服务暂不可用')
  } finally {
    aiLoading.value = false
  }
}

async function searchCross() {
  try {
    const res = await crmApi.crossIndustry({
      keyword: cross.keyword || undefined,
      industry: cross.industry || undefined,
      page: 1,
      page_size: 50,
    })
    crossRows.value = Array.isArray(res?.items) ? res.items : []
    crossTab.value = Array.isArray(res?.cross_tab) ? res.cross_tab : []
  } catch (e) {
    ElMessage.error(e?.message || '检索失败')
  }
}

onMounted(() => {
  if (customerId.value) loadView()
})
</script>

<style scoped>
.crm-page { padding: 16px; }
.page-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.page-title { font-size: 18px; margin: 0 0 4px; }
.page-sub { font-size: 12px; color: #909399; }
.suggestion { border-bottom: 1px solid #ebeef5; padding: 8px 0; }
.suggestion-head { display: flex; gap: 8px; align-items: center; }
.confidence { margin-left: auto; font-size: 12px; color: #909399; }
.evidence { display: flex; flex-wrap: wrap; gap: 4px; }
.evidence-item { font-size: 11px; color: #606266; background: #f4f4f5; padding: 1px 5px; border-radius: 3px; }
.cross-tab { margin-top: 10px; }
</style>
