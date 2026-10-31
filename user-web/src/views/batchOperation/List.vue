<template>
  <div class="batch-operation-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('批量操作') }}</h2>
        <p class="subtitle">
          {{ $t('批量处理线索、客户、消息等数据') }}
        </p>
      </div>
    </el-card>

    <el-row :gutter="20">
      <el-col
        v-for="tool in tools"
        :key="tool.name"
        :span="8"
      >
        <el-card
          class="tool-card"
          shadow="hover"
          @click="openTool(tool)"
        >
          <div class="tool-icon">
            <el-icon
              :size="40"
              :color="tool.color"
            >
              <component :is="tool.icon" />
            </el-icon>
          </div>
          <h3>{{ tool.label || tool.name }}</h3>
          <p>{{ tool.description }}</p>
          <div class="tool-stats">
            <span>已执行: {{ tool.usedCount }}</span>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card class="history-card">
      <template #header>
        <span>{{ $t('操作历史') }}</span>
      </template>
      <el-table
        v-loading="loading"
        :data="histories"
        stripe
      >
        <el-table-column
          prop="tool"
          label="工具"
          width="150"
        />
        <el-table-column
          prop="target"
          label="目标"
          min-width="200"
        />
        <el-table-column
          prop="totalCount"
          label="总数"
          width="100"
        />
        <el-table-column
          prop="successCount"
          label="成功"
          width="100"
        />
        <el-table-column
          prop="failedCount"
          label="失败"
          width="100"
        />
        <el-table-column
          prop="status"
          label="状态"
          width="120"
        >
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)">
              {{ getStatusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="createdAt"
          label="执行时间"
          width="180"
        />
        <el-table-column
          label="操作"
          width="150"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="viewDetail(row)"
            >
              详情
            </el-button>
            <el-button
              v-if="row.status === 'running'"
              link
              type="danger"
              @click="cancelTask(row)"
            >
              取消
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="currentTool?.name"
      width="800px"
    >
      <el-form
        :model="form"
        label-width="120px"
      >
        <el-form-item label="操作类型">
          <el-input
            :value="currentTool?.name"
            disabled
          />
        </el-form-item>
        <el-form-item label="数据来源">
          <el-radio-group v-model="form.source">
            <el-radio label="all">
              全部
            </el-radio>
            <el-radio label="filter">
              筛选
            </el-radio>
            <el-radio label="import">
              导入
            </el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item
          v-if="form.source === 'filter'"
          label="筛选条件"
        >
          <el-input
            v-model="form.filter"
            type="textarea"
            :rows="3"
            placeholder="{&quot;status&quot;: &quot;active&quot;, &quot;created_at&quot;: &quot;>2024-01-01&quot;}"
          />
        </el-form-item>
        <el-form-item
          v-if="form.source === 'import'"
          label="导入文件"
        >
          <el-upload
            :show-file-list="false"
            :http-request="uploadRequest"
            accept=".csv"
          >
            <el-button>选择文件</el-button>
          </el-upload>
          <span
            v-if="form.file"
            class="form-tip"
          >已选：{{ form.file.name }}</span>
        </el-form-item>
        <el-form-item label="执行参数">
          <el-input
            v-model="form.params"
            type="textarea"
            :rows="4"
            placeholder="{&quot;action&quot;: &quot;send_message&quot;, &quot;template&quot;: &quot;welcome&quot;}"
          />
        </el-form-item>
        <el-form-item label="是否预览">
          <el-switch v-model="form.dryRun" />
          <span class="form-tip">预览模式不会真正执行</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button @click="preview">
          预览
        </el-button>
        <el-button
          type="primary"
          @click="execute"
        >
          执行
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="previewVisible"
      title="批量操作预览"
      width="900px"
      :close-on-click-modal="false"
    >
      <div v-loading="previewLoading">
        <div
          v-if="!previewLoading"
          class="preview-summary"
        >
          <el-row :gutter="20">
            <el-col :span="8">
              <el-statistic
                title="总影响条数"
                :value="previewSummary.total"
              />
            </el-col>
            <el-col :span="8">
              <el-statistic
                title="预计成功"
                :value="previewSummary.success"
              >
                <template #suffix>
                  <el-icon color="#10B981">
                    <component :is="'Check'" />
                  </el-icon>
                </template>
              </el-statistic>
            </el-col>
            <el-col :span="8">
              <el-statistic
                title="预计失败"
                :value="previewSummary.failed"
              >
                <template #suffix>
                  <el-icon color="#EF4444">
                    <component :is="'Close'" />
                  </el-icon>
                </template>
              </el-statistic>
            </el-col>
          </el-row>
        </div>

        <div
          v-if="previewSummary.errors.length > 0"
          class="preview-errors"
        >
          <h4>警告信息</h4>
          <el-alert
            v-for="(err, idx) in previewSummary.errors"
            :key="idx"
            :title="err"
            type="warning"
            show-icon
            :closable="false"
            style="margin-bottom: 8px"
          />
        </div>

        <el-table
          v-if="previewData.length > 0"
          :data="previewData"
          style="margin-top: 20px"
        >
          <el-table-column
            type="index"
            label="序号"
            width="60"
          />
          <el-table-column
            prop="target"
            label="目标"
            min-width="200"
          />
          <el-table-column
            prop="action"
            label="操作"
            width="150"
          />
          <el-table-column
            prop="status"
            label="预览状态"
            width="120"
          >
            <template #default="{ row }">
              <el-tag
                :type="getPreviewStatusTagType(row.status)"
                size="small"
              >
                {{ getPreviewStatusLabel(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            prop="message"
            label="备注"
            show-overflow-tooltip
            min-width="200"
          />
        </el-table>

        <el-empty
          v-if="!previewLoading && previewData.length === 0"
          description="暂无预览数据"
        />
      </div>

      <template #footer>
        <el-button @click="previewVisible = false">
          关闭
        </el-button>
        <el-button
          type="primary"
          @click="execute"
        >
          确认执行
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="detailDialogVisible"
      title="批量任务执行详情"
      width="800px"
    >
      <div v-loading="detailLoading">
        <template v-if="taskDetail">
          <el-descriptions
            :column="2"
            border
          >
            <el-descriptions-item label="任务ID">
              {{ taskDetail.id }}
            </el-descriptions-item>
            <el-descriptions-item label="工具名称">
              {{ taskDetail.tool }}
            </el-descriptions-item>
            <el-descriptions-item label="目标">
              {{ taskDetail.target }}
            </el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="getStatusType(taskDetail.status)">
                {{ getStatusText(taskDetail.status) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="执行时间">
              {{ taskDetail.createdAt }}
            </el-descriptions-item>
            <el-descriptions-item label="完成时间">
              {{ taskDetail.finishedAt }}
            </el-descriptions-item>
            <el-descriptions-item label="总数量">
              {{ taskDetail.totalCount }}
            </el-descriptions-item>
            <el-descriptions-item label="成功数量">
              <span style="color: #10B981">{{ taskDetail.successCount }}</span>
            </el-descriptions-item>
            <el-descriptions-item label="失败数量">
              <span style="color: #EF4444">{{ taskDetail.failedCount }}</span>
            </el-descriptions-item>
            <el-descriptions-item label="跳过数量">
              {{ taskDetail.skippedCount || 0 }}
            </el-descriptions-item>
          </el-descriptions>

          <div
            v-if="taskDetail.errorMessage"
            style="margin-top: 20px"
          >
            <h4>错误信息</h4>
            <el-alert
              :title="taskDetail.errorMessage"
              type="error"
              show-icon
              :closable="false"
            />
          </div>

          <div
            v-if="taskDetail.errorDetails && taskDetail.errorDetails.length > 0"
            style="margin-top: 20px"
          >
            <h4>错误详情</h4>
            <el-table
              :data="taskDetail.errorDetails"
              stripe
              max-height="300"
            >
              <el-table-column
                type="index"
                label="序号"
                width="60"
              />
              <el-table-column
                prop="target"
                label="目标"
                min-width="150"
              />
              <el-table-column
                prop="error"
                label="错误原因"
                min-width="250"
                show-overflow-tooltip
              />
              <el-table-column
                prop="time"
                label="时间"
                width="170"
              />
            </el-table>
          </div>
        </template>
        <el-empty
          v-if="!detailLoading && !taskDetail"
          description="暂无任务详情"
        />
      </div>
      <template #footer>
        <el-button @click="detailDialogVisible = false">
          关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, User, ChatLineRound, Promotion, Money, Document, Bell, ShoppingCart } from '@element-plus/icons-vue'
import { getBatchTools, runBatch, getBatchHistories, cancelBatch, previewBatch, getBatchDetail, batchImportFile } from '@/api/batchOperation.js'
import { toList } from '@/utils/list.js'
import { isUserReportedError } from '@/utils/errorReporting'
import { PASS_FAIL_STATUS, getStatusLabel, getStatusTagType } from '@/constants/status';

const getPreviewStatusLabel = (s) => getStatusLabel(s, PASS_FAIL_STATUS) === '成功' ? '可通过' : (getStatusLabel(s, PASS_FAIL_STATUS) || s || '-');
const getPreviewStatusTagType = (s) => getStatusTagType(s, PASS_FAIL_STATUS)

const loading = ref(false)
const tools = ref([])
const histories = ref([])
const dialogVisible = ref(false)
const currentTool = ref(null)
const form = ref({ source: 'all', filter: '', params: '', dryRun: true, file: null })

const detailDialogVisible = ref(false)
const detailLoading = ref(false)
const taskDetail = ref(null)

const previewVisible = ref(false);
const previewLoading = ref(false)
const previewData = ref([])
const previewSummary = ref({ total: 0, success: 0, failed: 0, errors: [] })

const getStatusType = (s) => ({ running: 'warning', success: 'success', failed: 'danger', partial: 'warning' }[s])
const getStatusText = (s) => ({ running: '执行中', success: '完成', failed: '失败', partial: '部分成功' }[s] || s)

const TOOL_PRESENTATION = {
  import: { icon: 'Upload', color: '#4F46E5' },
  export: { icon: 'Download', color: '#10B981' },
  delete: { icon: 'Delete', color: '#EF4444' },
  update: { icon: 'Edit', color: '#F59E0B' }
}

const loadTools = async () => {
  // 后端 /api/batch/tools 返回的是 {list:[{name,label,description}], total}，
  // 不是裸数组。旧实现 `tools.value = res || [...]` 把对象塞进 v-for，
  // 卡片区整块渲染不出来（页面只剩操作历史），兜底数组也永远用不上。
  const res = await getBatchTools()
  const list = Array.isArray(res) ? res : (res?.list || [])
  const mapped = list.map((t) => ({
    name: t.name,
    label: t.label || t.name,
    description: t.description || '',
    usedCount: t.usedCount || 0,
    ...(TOOL_PRESENTATION[t.name] || { icon: 'Operation', color: '#909399' })
  }))
  tools.value = mapped.length ? mapped : [
    { name: 'import', label: '批量导入', description: '通过 CSV 文件批量导入数据', icon: 'Upload', color: '#4F46E5', usedCount: 0 },
    { name: 'export', label: '批量导出', description: '导出数据为 CSV 文件', icon: 'Download', color: '#10B981', usedCount: 0 },
    { name: 'update', label: '批量更新', description: '批量更新选中记录的字段', icon: 'Edit', color: '#F59E0B', usedCount: 0 },
    { name: 'delete', label: '批量删除', description: '批量删除选中的记录', icon: 'Delete', color: '#EF4444', usedCount: 0 }
  ]
}

const loadHistories = async () => {
  loading.value = true
  try {
    const res = await getBatchHistories()
    histories.value = toList(res)
  } finally {
    loading.value = false
  }
}

const openTool = (tool) => {
  currentTool.value = tool
  form.value = { source: 'all', filter: '', params: '', dryRun: true, file: null }
  dialogVisible.value = true
}

// uploadRequest：走 api 层的上传（http-request 自定义），而不是 el-upload 的
// action= 直连。
//
// action= 直连是 el-upload 发的裸 XHR：**不带 Authorization 头** ⇒ 后端回
// 401「未提供认证令牌」，而且失败信息不会进全局错误处理，页面上看不出原因。
// 走 api 层则由 axios 拦截器统一带上令牌、统一处理业务码。
const uploadRequest = async (options) => {
  try {
    const res = await batchImportFile(options.file, 'clue')
    form.value.file = options.file
    ElMessage.success(i18n.global.t('batchOperation.importDone'))
    loadHistories()
    if (options.onSuccess) options.onSuccess(res)
  } catch (err) {
    ElMessage.error(i18n.global.t('batchOperation.importFailed') + '：' + (err?.message || err))
    if (options.onError) options.onError(err)
  }
}

const preview = async () => {
  previewLoading.value = true
  previewVisible.value = true
  previewData.value = []
  previewSummary.value = { total: 0, success: 0, failed: 0, errors: [] }

  try {
    const res = await previewBatch({ tool: currentTool.value.name, ...form.value, dryRun: true })
    const data = res;

    // 后端 Preview 的响应是 { operation_type, data_type, selected_count, preview }
    previewData.value = data.items || data.records || [];
    previewSummary.value = {
      total: data.selected_count ?? data.total ?? data.totalCount ?? previewData.value.length,
      success: data.successCount || data.success || 0,
      failed: data.failedCount || data.failed || 0,
      errors: data.errors || []
    }
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error('预览失败：' + (error?.message || error))
    previewVisible.value = false
  } finally {
    previewLoading.value = false
  }
}

// 导出走文件流：把 Blob 落成一次浏览器下载，而不是把 CSV 文本当 JSON 解析
const saveBlob = (blob, filename) => {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // 立刻 revoke 会让部分浏览器取消下载，延后一拍释放
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

const execute = async () => {
  const loading = ElMessage({ message: i18n.global.t('batchOperation.running'), type: 'info', duration: 0 })
  try {
    const result = await runBatch({ tool: currentTool.value.name, ...form.value })
    loading.close()
    if (currentTool.value.name === 'export' && result instanceof Blob) {
      const stamp = new Date().toISOString().slice(0, 10)
      saveBlob(result, `batch-export-${stamp}.csv`)
      ElMessage.success('导出文件已开始下载')
    } else {
      ElMessage.success(i18n.global.t('batchOperation.taskSubmitted'))
    }
    dialogVisible.value = false
    previewVisible.value = false
    loadHistories()
  } catch (error) {
    loading.close()
    // 这里必须把后端/契约层的原话透出来：批量操作的失败原因（ids 超限、
    // 筛选条件不是合法 JSON、没选文件……）全在 message 里，换成一句
    // 「执行失败」等于把唯一有用的信息吃掉。
    // 前缀仍走 i18n（词条「执行失败」在 9 个语言包里都有），别又写死中文。
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('batchOperation.executeFailed') + '：' + (error?.message || error))
  }
}

const viewDetail = async (row) => {
  detailDialogVisible.value = true
  detailLoading.value = true
  try {
    const res = await getBatchDetail(row.id)
    taskDetail.value = res || row
  } catch {
    taskDetail.value = row
  } finally {
    detailLoading.value = false
  }
}

const cancelTask = async (row) => {
  try {
    await ElMessageBox.confirm('确定取消该任务？', '确认', { type: 'warning' })
    await cancelBatch(row.id)
    ElMessage.success(i18n.global.t('已取消'))
    loadHistories()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('batchOperation.cancelFailed'))
  }
}

onMounted(() => {
  loadTools()
  loadHistories()
})
</script>

<style scoped lang="scss">
.batch-operation-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.tool-card {
  cursor: pointer;
  text-align: center;
  margin-bottom: 20px;
  transition: transform 0.3s;
  &:hover { transform: translateY(-5px); }
  .tool-icon { margin-bottom: 15px; }
  h3 { margin: 0 0 10px 0; }
  p { color: #909399; font-size: 13px; }
  .tool-stats { color: #4F46E5; font-size: 12px; margin-top: 10px; }
}
.history-card { margin-top: 20px; }
.form-tip { color: #909399; font-size: 12px; margin-left: 10px; }
</style>
