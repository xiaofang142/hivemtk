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
            title="CSV 列顺序：name,phone,email,industry,source_account_id,source_channel,owner_user_id,tags"
            type="info"
            :closable="false"
            show-icon
          />
          <el-input
            v-model="csvText"
            type="textarea"
            :rows="10"
            placeholder="可粘贴 CSV 内容（首行可为表头）"
            style="margin-top: 12px"
          />
          <el-button
            type="primary"
            :loading="importing"
            style="margin-top: 10px"
            @click="doImport"
          >
            导入
          </el-button>
          <div
            v-if="importResult"
            class="result"
          >
            <el-tag>总计 {{ importResult.total }}</el-tag>
            <el-tag type="success">
              新建 {{ importResult.created }}
            </el-tag>
            <el-tag type="warning">
              更新 {{ importResult.updated }}
            </el-tag>
            <el-tag type="danger">
              越权 {{ importResult.denied }}
            </el-tag>
          </div>
          <ul
            v-if="importResult?.errors?.length"
            class="errors"
          >
            <li
              v-for="(e, i) in importResult.errors"
              :key="i"
            >
              {{ e }}
            </li>
          </ul>
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
import { ElMessage } from 'element-plus'
import { crmApi } from '@/api/crm'

const csvText = ref('')
const importing = ref(false)
const exporting = ref(false)
const importResult = ref(null)
const exportResult = ref(null)
const exportFilters = reactive({ keyword: '', industry: '' })

function parseCsvLine(line) {
  const out = []
  let cur = ''
  let quoted = false
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (ch === '"') {
      if (quoted && line[i + 1] === '"') { cur += '"'; i++ }
      else quoted = !quoted
    } else if (ch === ',' && !quoted) {
      out.push(cur.trim()); cur = ''
    } else {
      cur += ch
    }
  }
  out.push(cur.trim())
  return out
}

function buildRows(text) {
  const lines = text.split(/\r?\n/).filter((l) => l.trim())
  if (!lines.length) return []
  const start = /^name/i.test(lines[0].split(',')[0] || '') ? 1 : 0
  const keys = ['name', 'phone', 'email', 'industry', 'source_account_id', 'source_channel', 'owner_user_id', 'tags']
  return lines.slice(start).map((line) => {
    const cells = parseCsvLine(line)
    const row = {}
    keys.forEach((k, i) => { row[k] = cells[i] || '' })
    row.owner_user_id = Number(row.owner_user_id || 0)
    return row
  })
}

async function doImport() {
  const rows = buildRows(csvText.value)
  if (!rows.length) {
    ElMessage.warning('没有可导入的数据')
    return
  }
  importing.value = true
  try {
    importResult.value = await crmApi.importRows({ rows, file_name: 'paste.csv' })
    ElMessage.success('导入完成')
  } catch (e) {
    ElMessage.error(e?.message || '导入失败')
  } finally {
    importing.value = false
  }
}

async function doExport(format) {
  exporting.value = true
  try {
    if (format === 'csv') {
      const res = await crmApi.exportRows({
        keyword: exportFilters.keyword || undefined,
        industry: exportFilters.industry || undefined,
        format: 'csv',
      })
      const blob = (typeof Blob !== 'undefined' && res instanceof Blob)
        ? res
        : new Blob([typeof res === 'string' ? res : String(res?.data || '')], { type: 'text/csv;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'crm-customers.csv'
      a.click()
      URL.revokeObjectURL(url)
    } else {
      exportResult.value = await crmApi.exportRows({
        keyword: exportFilters.keyword || undefined,
        industry: exportFilters.industry || undefined,
      })
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
