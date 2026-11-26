<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          客户导入导出
        </h2>
        <span class="page-sub">导入逐行按账号/行业/负责人权限过滤；导出按角色脱敏并写审计</span>
      </div>
    </div>

    <el-row :gutter="16">
      <el-col :span="12">
        <el-card shadow="never">
          <template #header>
            <span>导入客户</span>
          </template>
          <el-alert
            title="复用导入向导：上传 CSV/真实 xlsx → 预检（映射、非法行、重复、越权原因）→ 后台作业执行 → 下载完整失败报告"
            type="info"
            :closable="false"
            show-icon
          />
          <el-button
            type="primary"
            style="margin-top: 12px"
            @click="goImportWizard"
          >
            前往导入向导
          </el-button>
          <div
            class="muted"
            style="margin-top: 8px"
          >
            支持 CSV（BOM/引号/换行）与真正的 xlsx；字段映射含行业/负责人/来源/公司，越权行在预检中逐行说明原因。
          </div>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card shadow="never">
          <template #header>
            <span>导出客户</span>
          </template>
          <el-form
            inline
            label-width="70px"
            @submit.prevent
          >
            <el-form-item label="关键词">
              <el-input
                v-model="exportFilters.keyword"
                style="width: 160px"
              />
            </el-form-item>
            <el-form-item label="行业">
              <el-input
                v-model="exportFilters.industry"
                style="width: 140px"
              />
            </el-form-item>
            <el-form-item label="来源账号">
              <ChannelAccountSelect
                v-model="exportFilters.source_account_id"
                style="width: 180px"
              />
            </el-form-item>
            <el-form-item label="负责人">
              <EntitySelect
                v-model="exportFilters.owner_user_id"
                entity="user"
                placeholder="全部负责人"
                style="width: 180px"
              />
            </el-form-item>
            <el-form-item label="实体">
              <el-select
                v-model="exportFilters.entity_type"
                style="width: 150px"
              >
                <el-option
                  label="客户"
                  value="customer"
                />
                <el-option
                  label="联系人"
                  value="contact"
                />
                <el-option
                  label="公司"
                  value="company"
                />
                <el-option
                  label="供应商"
                  value="supplier"
                />
                <el-option
                  label="服务商"
                  value="service_provider"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="字段">
              <el-select
                v-model="exportFilters.fields"
                multiple
                collapse-tags
                style="width: 200px"
              >
                <el-option
                  v-for="f in exportFieldOptions"
                  :key="f"
                  :label="f"
                  :value="f"
                />
              </el-select>
            </el-form-item>
          </el-form>
          <el-button
            :loading="exporting"
            @click="doExport('json')"
          >
            导出 JSON
          </el-button>
          <el-button
            :loading="exporting"
            @click="doExport('csv')"
          >
            导出 CSV
          </el-button>
          <el-button
            :loading="exporting"
            @click="doExport('xlsx')"
          >
            导出 Excel
          </el-button>
          <div
            v-if="exportResult"
            class="result"
          >
            <el-tag>导出 {{ exportResult.total }} 条</el-tag>
            <el-tag
              v-if="exportResult.sensitive_masked"
              type="warning"
            >
              敏感字段已脱敏
            </el-tag>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { crmApi } from '@/api/crm'
import ChannelAccountSelect from '@/components/ChannelAccountSelect.vue'
import EntitySelect from '@/components/EntitySelect.vue'

const router = useRouter()

function goImportWizard() {
  router.push('/customerImportJob/list')
}

const exporting = ref(false)
const exportResult = ref(null)
const exportFilters = reactive({
  keyword: '', industry: '', source_account_id: '', owner_user_id: 0,
  entity_type: 'customer', fields: [],
})
const exportFieldOptions = [
  'id', 'name', 'phone', 'email', 'industry', 'source_account_id', 'owner_user_id', 'tags',
]

function exportParams(format) {
  const params = {
    keyword: exportFilters.keyword || undefined,
    industry: exportFilters.industry || undefined,
    source_account_id: exportFilters.source_account_id || undefined,
    owner_user_id: exportFilters.owner_user_id || undefined,
    entity_type: exportFilters.entity_type || 'customer',
    fields: exportFilters.fields?.length ? exportFilters.fields.join(',') : undefined,
  }
  if (format) params.format = format
  return params
}

async function doExport(format) {
  exporting.value = true
  try {
    if (format === 'csv' || format === 'xlsx') {
      const res = await crmApi.exportRows(exportParams(format))
      const blob = (typeof Blob !== 'undefined' && res instanceof Blob)
        ? res
        : new Blob([typeof res === 'string' ? res : String(res?.data || '')], {
          type: format === 'xlsx'
            ? 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
            : 'text/csv;charset=utf-8',
        })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = format === 'xlsx' ? 'crm-customers.xlsx' : 'crm-customers.csv'
      a.click()
      URL.revokeObjectURL(url)
    } else {
      exportResult.value = await crmApi.exportRows(exportParams())
    }
  } catch (e) {
    ElMessage.error(e?.message || '导出失败')
  } finally {
    exporting.value = false
  }
}
</script>

<style scoped>
.crm-page { padding: 16px; }
.page-head { margin-bottom: 12px; }
.page-title { font-size: 18px; margin: 0 0 4px; }
.page-sub { font-size: 12px; color: #909399; }
.result { margin-top: 12px; display: flex; gap: 6px; flex-wrap: wrap; }
.errors { font-size: 12px; color: #f56c6c; max-height: 140px; overflow: auto; }
</style>
