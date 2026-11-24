<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          客户去重与合并
        </h2>
        <span class="page-sub">手机号 / 邮箱 / 外部 ID 走既有 OneID 规则，名称相似度仅作人工复核候选</span>
      </div>
      <el-button
        type="primary"
        :loading="scanning"
        @click="scan"
      >
        扫描重复
      </el-button>
    </div>

    <el-card shadow="never">
      <el-form
        inline
        label-width="90px"
        @submit.prevent
      >
        <el-form-item label="最多扫描">
          <el-input-number
            v-model="options.max_customers"
            :min="100"
            :max="50000"
            :step="1000"
          />
        </el-form-item>
        <el-form-item label="名称阈值">
          <el-input-number
            v-model="options.name_threshold"
            :min="50"
            :max="100"
          />
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="options.same_account_only">
            仅同账号内比对
          </el-checkbox>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="scanning"
        :data="rows"
        border
        stripe
        empty-text="点击「扫描重复」开始"
      >
        <el-table-column
          prop="left_id"
          label="客户A"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="left_name"
          label="名称A"
          min-width="120"
        />
        <el-table-column
          prop="right_id"
          label="客户B"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="right_name"
          label="名称B"
          min-width="120"
        />
        <el-table-column
          prop="rule"
          label="命中规则"
          min-width="130"
        />
        <el-table-column
          prop="score"
          label="得分"
          width="80"
        />
        <el-table-column
          prop="action"
          label="动作"
          width="90"
        />
        <el-table-column
          prop="reason"
          label="依据"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          label="操作"
          width="110"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              :disabled="row.action === 'review'"
              @click="openMerge(row)"
            >
              合并
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card
      shadow="never"
      style="margin-top: 16px"
    >
      <template #header>
        <span>合并审计</span>
      </template>
      <el-input
        v-model="auditCustomerId"
        placeholder="输入客户 ID 查看合并留痕"
        style="width: 320px"
      />
      <el-button
        style="margin-left: 8px"
        @click="loadAudit"
      >
        查询
      </el-button>
      <el-table
        :data="auditRows"
        border
        stripe
        style="margin-top: 12px"
        empty-text="暂无合并记录"
      >
        <el-table-column
          prop="merge_id"
          label="合并ID"
          min-width="160"
        />
        <el-table-column
          prop="primary_id"
          label="主档"
          min-width="140"
        />
        <el-table-column
          prop="secondary_id"
          label="被合并"
          min-width="140"
        />
        <el-table-column
          prop="rule_name"
          label="规则"
          min-width="120"
        />
        <el-table-column
          prop="operator_name"
          label="操作人"
          min-width="110"
        />
        <el-table-column
          prop="created_at"
          label="时间"
          min-width="170"
        />
      </el-table>
    </el-card>

    <el-dialog
      v-model="mergeVisible"
      title="确认合并"
      width="480px"
    >
      <p>主档：<b>{{ mergeForm.primary_id }}</b></p>
      <p>被合并：<b>{{ mergeForm.secondary_id }}</b></p>
      <el-form label-width="70px">
        <el-form-item label="原因">
          <el-input
            v-model="mergeForm.reason"
            type="textarea"
          />
        </el-form-item>
        <el-form-item label="来源">
          <el-input
            v-model="mergeForm.source"
            placeholder="crm/manual"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="mergeVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="merging"
          @click="confirmMerge"
        >
          确认合并
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { crmApi } from '@/api/crm'

const scanning = ref(false)
const merging = ref(false)
const rows = ref([])
const auditRows = ref([])
const auditCustomerId = ref('')
const mergeVisible = ref(false)
const options = reactive({ max_customers: 5000, name_threshold: 85, same_account_only: true })
const mergeForm = reactive({ primary_id: '', secondary_id: '', reason: '', source: 'crm/manual' })

async function scan() {
  scanning.value = true
  try {
    const res = await crmApi.scanDedupe({ ...options })
    rows.value = Array.isArray(res?.list) ? res.list : []
  } catch (e) {
    ElMessage.error(e?.message || '扫描失败')
  } finally {
    scanning.value = false
  }
}

function openMerge(row) {
  mergeForm.primary_id = row.left_id
  mergeForm.secondary_id = row.right_id
  mergeForm.reason = `命中规则：${row.rule}（${row.score}）`
  mergeVisible.value = true
}

async function confirmMerge() {
  merging.value = true
  try {
    await crmApi.merge({ ...mergeForm })
    ElMessage.success('合并成功')
    mergeVisible.value = false
    auditCustomerId.value = mergeForm.primary_id
    await Promise.all([loadAudit(), scan()])
  } catch (e) {
    ElMessage.error(e?.message || '合并失败')
  } finally {
    merging.value = false
  }
}

async function loadAudit() {
  if (!auditCustomerId.value) return
  try {
    const res = await crmApi.mergeAudit(auditCustomerId.value.trim())
    auditRows.value = Array.isArray(res?.list) ? res.list : []
  } catch (e) {
    ElMessage.error(e?.message || '查询审计失败')
  }
}

onMounted(() => {})
</script>

<style scoped>
.crm-page { padding: 16px; }
.page-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.page-title { font-size: 18px; margin: 0 0 4px; }
.page-sub { font-size: 12px; color: #909399; }
</style>
