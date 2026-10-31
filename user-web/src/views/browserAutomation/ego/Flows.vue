<template>
  <div class="page">
    <div class="toolbar">
      <el-button
        type="primary"
        icon="Plus"
        @click="openEditor()"
      >
        新建流程
      </el-button>
      <el-button
        :loading="loading"
        @click="load"
      >
        刷新
      </el-button>
      <span class="hint">
        流程是「你自己写的 ego 浏览器脚本」。保存即静态扫描（block 级命中不许试跑/发布）；
        提交审核后由管理员在「资产发布台」审核上架，全租户可用。
      </span>
    </div>

    <el-table
      v-loading="loading"
      :data="list"
      border
      @row-click="openEditor"
    >
      <el-table-column
        prop="id"
        label="ID"
        width="70"
      />
      <el-table-column
        prop="name"
        label="名称"
        min-width="180"
      />
      <el-table-column
        prop="version"
        label="版本"
        width="80"
      />
      <el-table-column
        label="状态"
        width="110"
      >
        <template #default="{ row }">
          <el-tag :type="statusTag(row.status)">
            {{ statusText(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="扫描"
        width="110"
      >
        <template #default="{ row }">
          <el-tag :type="row.scan_passed ? 'success' : 'danger'">
            {{ row.scan_passed ? '通过' : '有 block' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="域名"
        min-width="160"
      >
        <template #default="{ row }">
          {{ domainsText(row.domains) }}
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="330"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            size="small"
            @click.stop="openEditor(row)"
          >
            编辑
          </el-button>
          <el-button
            size="small"
            @click.stop="onRescan(row)"
          >
            重扫
          </el-button>
          <el-button
            size="small"
            :disabled="!row.scan_passed"
            @click.stop="onDryRun(row)"
          >
            试跑
          </el-button>
          <el-button
            size="small"
            type="primary"
            :disabled="!row.scan_passed"
            @click.stop="onPublish(row)"
          >
            发布
          </el-button>
          <el-button
            size="small"
            :disabled="row.status !== 'published'"
            @click.stop="onSubmit(row)"
          >
            提交审核
          </el-button>
          <el-button
            size="small"
            type="danger"
            @click.stop="onDelete(row)"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 编辑器 -->
    <el-dialog
      v-model="editorVisible"
      :title="form.id ? `编辑流程 #${form.id}` : '新建流程'"
      width="900px"
      top="6vh"
    >
      <el-form
        :model="form"
        label-width="110px"
      >
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="脚本契约（runner 注入，直接可用）"
        >
          <div>常量：<code>__PARAMS__</code>（调用方参数对象）、<code>__SPACE_NAME__</code>（同名 space 续跑）、<code>__EXEC__</code>、<code>__DOMAINS__</code></div>
          <div>写提交点前调用：<code>if (!(await __awaitConfirm__("动作名")))&#123; /* 跳过写，打印 ok:false 审计行 */ &#125;</code>——false=用户拒绝或确认超时（<b>不要 process.exit</b>，扫描门禁止）；域内导航建议 <code>__gotoChecked__(page, url)</code> / <code>__fetchChecked__(page, url, opts)</code>（越域即抛错）</div>
          <div>脚本应至少打印一行 <code>console.log(JSON.stringify({...}))</code> 事件，否则事件流为空、审计只剩退出码（扫描门 warn）</div>
        </el-alert>
        <el-form-item
          label="名称"
          required
        >
          <el-input
            v-model="form.name"
            placeholder="例如：GitHub 仓库检索（名称会 slug 化成资产编码/目录名）"
          />
        </el-form-item>
        <el-form-item
          label="域名白名单"
          required
        >
          <el-input
            v-model="form.domainsText"
            placeholder="github.com, api.github.com（逗号或换行分隔；用户流程不允许 * 整站通配）"
          />
        </el-form-item>
        <el-form-item label="写动作声明">
          <el-input
            v-model="form.writesText"
            placeholder="例如：comment, submit（留空=纯读流程；非空则执行时强制逐步确认）"
          />
        </el-form-item>
        <el-form-item label="定时触发">
          <el-input
            v-model="form.cronText"
            placeholder="可选；5 段 cron（如 0 9 * * * 每日 9 点）；仅纯读流程可挂（writes 非空会被拒绝）"
          />
        </el-form-item>
        <el-form-item label="参数 Schema">
          <el-input
            v-model="form.paramsSchemaText"
            type="textarea"
            :rows="3"
            placeholder="可选 JSON，例如 {&quot;keyword&quot;:&quot;string&quot;}"
          />
        </el-form-item>
        <el-form-item
          label="脚本"
          required
        >
          <el-input
            v-model="form.script"
            type="textarea"
            :rows="16"
            class="mono"
            placeholder="ego-browser ES module 脚本（taskSpace/page 原语；发布时快照并复扫）"
          />
        </el-form-item>
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="脚本契约（runner 组装时注入；ego-browser 不继承 env，只能内联）"
        >
          <div class="contract">
            <code>__PARAMS__</code> 参数对象 · <code>__SPACE_NAME__</code> space 名（同名续跑）·
            <code>__EXEC__</code> 执行键
          </div>
          <div class="contract">
            写操作打印 <code>{"type":"confirm","action":"..."}</code> 请求裁决；其余输出按
            <code>type</code>（action/snapshot/artifact/handoff/extract）归事件
          </div>
        </el-alert>
      </el-form>

      <el-alert
        v-if="scanReport"
        :type="scanReport.passed ? 'success' : 'error'"
        :closable="false"
        show-icon
        :title="scanReport.passed ? '扫描通过' : `扫描未通过：block ${scanReport.blocked?.length || 0} 条`"
      >
        <div
          v-for="v in (scanReport.blocked || [])"
          :key="`b${v.rule_id}${v.line}`"
          class="verdict"
        >
          [{{ v.rule_id }}] 第 {{ v.line }} 行 {{ v.desc }} —— <code>{{ v.excerpt }}</code>
        </div>
        <div
          v-for="v in (scanReport.warnings || [])"
          :key="`w${v.rule_id}${v.line}`"
          class="verdict warn"
        >
          [{{ v.rule_id }}] 第 {{ v.line }} 行 {{ v.desc }} —— <code>{{ v.excerpt }}</code>
        </div>
      </el-alert>

      <template #footer>
        <el-button @click="editorVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="onSave"
        >
          保存（并扫描）
        </el-button>
      </template>
    </el-dialog>

    <!-- 试跑 -->
    <el-dialog
      v-model="dryRunVisible"
      title="试跑（下发到你的 ego-runner）"
      width="560px"
    >
      <el-form label-width="100px">
        <el-form-item
          v-if="!dryRunHasSchema"
          label="参数 JSON"
        >
          <el-input
            v-model="dryRunForm.paramsText"
            type="textarea"
            :rows="4"
            placeholder="可选；将作为 params 透传给脚本"
          />
        </el-form-item>
        <ParamsForm
          v-else
          v-model="dryRunForm.params"
          :schema="dryRunForm.paramsSchema"
          is-flow
        />
        <el-form-item label="space 名">
          <el-input
            v-model="dryRunForm.spaceName"
            placeholder="可选；同名 space 会续跑（ego taskSpace 语义）"
          />
        </el-form-item>
        <el-form-item label="超时（秒）">
          <el-input-number
            v-model="dryRunForm.timeoutSec"
            :min="30"
            :max="3600"
            :step="30"
          />
        </el-form-item>
      </el-form>
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="runner 不在线时执行会留在 created 态（等 runner 上线后重发）"
      />
      <template #footer>
        <el-button @click="dryRunVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="dryRunLoading"
          @click="doDryRun"
        >
          下发试跑
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { isUserReportedError } from '@/utils/errorReporting'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  deleteEgoFlow,
  getEgoFlowCron,
  upsertEgoFlowCron,
  deleteEgoFlowCron,
  dryRunEgoFlow,
  listEgoFlows,
  publishEgoFlow,
  rescanEgoFlow,
  saveEgoFlow,
  submitEgoFlow
} from '@/api/egolite'
import ParamsForm from './ParamsForm.vue'

const loading = ref(false)
const saving = ref(false)
const list = ref([])
const editorVisible = ref(false)
const scanReport = ref(null)
const dryRunVisible = ref(false)
const dryRunLoading = ref(false)

const emptyForm = () => ({
  id: 0,
  name: '',
  domainsText: '',
  writesText: '',
  paramsSchemaText: '',
  cronText: '',
  script: ''
})
const form = ref(emptyForm())
const dryRunForm = ref({ flowId: 0, paramsText: '', params: {}, paramsSchema: null, spaceName: '', timeoutSec: 600 })

// 流程登记了 params_schema ⇒ 表单模式（与资产运行同口径）；否则保持 JSON 文本域
const dryRunHasSchema = computed(() => {
  const s = dryRunForm.value.paramsSchema
  if (!s) return false
  try {
    const obj = typeof s === 'string' ? JSON.parse(s) : s
    return !!(obj && obj.properties && Object.keys(obj.properties).length)
  } catch {
    return false
  }
})

const statusText = (s) =>
  ({ draft: '草稿', published: '已发布', submitted: '审核中' }[s] || s || '-')

const statusTag = (s) =>
  ({ draft: 'info', published: 'success', submitted: 'warning' }[s] || 'info')

const toArray = (text) =>
  String(text || '')
    .split(/[,，\n]/)
    .map((x) => x.trim())
    .filter(Boolean)

const domainsText = (domains) => {
  if (!domains) return '-'
  if (Array.isArray(domains)) return domains.join(', ')
  try {
    return JSON.parse(domains).join(', ')
  } catch {
    return '-'
  }
}

// 回填编辑器用：空 → ''（不能用 domainsText + replace('-','') 凑，
// 域名里本来就有 '-'，会被顺手吃掉一个字符）
const joinList = (v) => {
  if (!v) return ''
  if (Array.isArray(v)) return v.join(', ')
  try {
    const arr = JSON.parse(v)
    return Array.isArray(arr) ? arr.join(', ') : ''
  } catch {
    return ''
  }
}

const load = async () => {
  loading.value = true
  try {
    const data = await listEgoFlows({ page: 1, page_size: 100 })
    list.value = data?.list || []
  } finally {
    loading.value = false
  }
}

const openEditor = (row) => {
  scanReport.value = null
  if (!row) {
    form.value = emptyForm()
  } else {
    form.value = {
      id: row.id,
      name: row.name,
      domainsText: joinList(row.domains),
      writesText: joinList(row.writes),
      paramsSchemaText: row.params_schema ? JSON.stringify(row.params_schema, null, 2) : '',
      cronText: '',
      script: row.script || ''
    }
    // 已挂载的定时触发器回填（specs/016：404=未挂载，静默留空）
    getEgoFlowCron(row.id)
      .then((tr) => { form.value.cronText = tr?.cron_expr || '' })
      .catch(() => {})
  }
  editorVisible.value = true
}

const parseJsonOrUndefined = (text) => {
  if (!String(text || '').trim()) return undefined
  try {
    return JSON.parse(text)
  } catch {
    throw new Error('参数 Schema 不是合法 JSON')
  }
}

const onSave = async () => {
  // 后端 SaveRequest 只有服务层校验（返回「flow 名称必填」），空名提交会白打一次 400
  if (!String(form.value.name || '').trim()) {
    ElMessage.warning('请输入流程名称')
    return
  }
  saving.value = true
  try {
    const payload = {
      name: form.value.name,
      domains: toArray(form.value.domainsText),
      writes: toArray(form.value.writesText),
      script: form.value.script
    }
    const schema = parseJsonOrUndefined(form.value.paramsSchemaText)
    if (schema !== undefined) payload.params_schema = schema
    const data = await saveEgoFlow(form.value.id, payload)
    scanReport.value = data?.scan_report || null
    form.value.id = data?.flow?.id || form.value.id
    // 定时触发器随保存同步（specs/016 S10）：填了表达式挂载/更新，清空则摘除
    const cronExpr = String(form.value.cronText || '').trim()
    try {
      if (cronExpr) {
        await upsertEgoFlowCron(form.value.id, { cron_expr: cronExpr })
      } else {
        await deleteEgoFlowCron(form.value.id).catch(() => {})
      }
    } catch (e) {
      ElMessage.warning(e?.message || '定时触发器保存失败（流程本体已保存）')
    }
    ElMessage.success(data?.scan_report?.passed ? '已保存，扫描通过' : '已保存，但扫描有 block 命中')
    await load()
  } catch (e) {
    // 拦截器已经弹过后端文案，这里再补一条就是同一个失败的第二次记账
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

const onRescan = async (row) => {
  const report = await rescanEgoFlow(row.id)
  ElMessage[report?.passed ? 'success' : 'warning'](
    report?.passed ? '扫描通过' : `扫描未通过：block ${report?.blocked?.length || 0} 条`
  )
  await load()
}

const onPublish = async (row) => {
  await publishEgoFlow(row.id)
  ElMessage.success('已发布（版本自增）')
  await load()
}

const onSubmit = async (row) => {
  await submitEgoFlow(row.id)
  ElMessage.success('已提交审核，等管理员在资产发布台处理')
  await load()
}

const onDelete = async (row) => {
  await ElMessageBox.confirm(`确认删除流程「${row.name}」？已发布的资产快照不受影响。`, '删除', {
    type: 'warning'
  })
  await deleteEgoFlow(row.id)
  ElMessage.success('已删除')
  await load()
}

const onDryRun = (row) => {
  dryRunForm.value = {
    flowId: row.id,
    paramsText: '',
    params: {},
    paramsSchema: row.params_schema || null,
    spaceName: '',
    timeoutSec: 600
  }
  dryRunVisible.value = true
}

const doDryRun = async () => {
  dryRunLoading.value = true
  try {
    const payload = {
      space_name: dryRunForm.value.spaceName || undefined,
      timeout_sec: dryRunForm.value.timeoutSec
    }
    if (dryRunHasSchema.value) {
      payload.params = { ...dryRunForm.value.params }
    } else {
      const params = parseJsonOrUndefined(dryRunForm.value.paramsText)
      if (params !== undefined) payload.params = params
    }
    const exec = await dryRunEgoFlow(dryRunForm.value.flowId, payload)
    dryRunVisible.value = false
    ElMessage.success(`已下发：执行 #${exec?.id}（状态 ${exec?.state}）→ 去「执行监控」看事件时间线${exec?.state === 'created' ? '（runner 未在线，上线后在监控页点「重发」）' : ''}`)
  } catch (e) {
    ElMessage.error(e?.message || '试跑失败（runner 可能不在线）')
  } finally {
    dryRunLoading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page { padding: 12px; }
.toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.hint { color: #909399; font-size: 12px; }
.mono :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
.verdict { font-size: 12px; line-height: 1.7; }
.verdict.warn { color: #b88230; }
.contract { font-size: 12px; line-height: 1.8; }
</style>
