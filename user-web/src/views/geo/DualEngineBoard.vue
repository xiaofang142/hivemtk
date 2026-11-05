<template>
  <div class="geo-page p-4">
    <ProbeModeBanner />

    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <div>
            <span class="title">双端监测</span>
            <span class="muted">
              AI 引用（探针）与 Google 点击/展现（GSC 导入）分列展示，不混算成单一分数。
            </span>
          </div>
          <div class="actions">
            <el-select
              v-model="days"
              style="width: 120px"
              @change="load"
            >
              <el-option
                label="近 7 天"
                :value="7"
              />
              <el-option
                label="近 30 天"
                :value="30"
              />
              <el-option
                label="近 90 天"
                :value="90"
              />
            </el-select>
            <el-button
              type="primary"
              @click="importVisible = true"
            >
              导入 GSC 数据
            </el-button>
            <el-button @click="load">
              刷新
            </el-button>
          </div>
        </div>
      </template>

      <el-row
        :gutter="12"
        class="mb-4"
      >
        <el-col :span="6">
          <el-card shadow="hover">
            <el-statistic
              title="AI 探针次数"
              :value="report.ai.probe_runs || 0"
            />
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover">
            <el-statistic
              title="品牌提及次数"
              :value="report.ai.brand_mentions || 0"
            />
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover">
            <el-statistic
              title="引用链接数"
              :value="report.ai.citation_count || 0"
            />
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover">
            <el-statistic
              title="Google 点击"
              :value="report.google.clicks || 0"
            />
            <div class="muted">
              展现 {{ report.google.impressions || 0 }} · 平均排名 {{ (report.google.position || 0).toFixed(1) }}
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-row :gutter="12">
        <el-col :span="10">
          <el-card
            shadow="never"
            class="inner-card"
          >
            <template #header>
              <span>AI 引擎明细</span>
            </template>
            <el-table
              :data="report.ai.engines || []"
              size="small"
              border
            >
              <el-table-column
                prop="engine"
                label="引擎"
                min-width="130"
              />
              <el-table-column
                label="来源"
                width="90"
              >
                <template #default="{ row }">
                  <el-tag
                    size="small"
                    :type="row.mode === 'real' ? 'success' : row.mode === 'manual' ? 'info' : 'warning'"
                  >
                    {{ modeText(row.mode) }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column
                prop="runs"
                label="次数"
                width="70"
              />
              <el-table-column
                prop="brand_mentions"
                label="提及"
                width="70"
              />
              <el-table-column
                prop="citation_count"
                label="引用"
                width="70"
              />
            </el-table>
            <el-empty
              v-if="!(report.ai.engines || []).length"
              description="窗口内还没有探针数据"
            />
          </el-card>
        </el-col>
        <el-col :span="14">
          <el-card
            shadow="never"
            class="inner-card"
          >
            <template #header>
              <span>Google 查询明细（GSC 导入）</span>
            </template>
            <el-table
              :data="report.google.top_queries || []"
              size="small"
              border
              max-height="420"
            >
              <el-table-column
                prop="query"
                label="查询"
                min-width="220"
                show-overflow-tooltip
              />
              <el-table-column
                prop="clicks"
                label="点击"
                width="80"
              />
              <el-table-column
                prop="impressions"
                label="展现"
                width="80"
              />
              <el-table-column
                label="CTR"
                width="80"
              >
                <template #default="{ row }">
                  {{ ((row.ctr || 0) * 100).toFixed(1) }}%
                </template>
              </el-table-column>
              <el-table-column
                label="排名"
                width="70"
              >
                <template #default="{ row }">
                  {{ (row.position || 0).toFixed(1) }}
                </template>
              </el-table-column>
            </el-table>
            <el-empty
              v-if="!(report.google.top_queries || []).length"
              description="还没有 GSC 数据，点右上角导入"
            />
            <div class="muted footer-note">
              区间 {{ report.google.from }} ~ {{ report.google.to }} ·
              推送成功 {{ report.push.google_push_success || 0 }} / 失败 {{ report.push.google_push_fail || 0 }}
            </div>
          </el-card>
        </el-col>
      </el-row>
    </el-card>

    <el-dialog
      v-model="importVisible"
      title="导入 Google Search Console 性能数据"
      width="760px"
    >
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="支持 GSC 导出的 CSV（Top queries / Top pages）。没有日期列时按导入当天记账并标注来源，报告不会把它当成精确日粒度。"
        style="margin-bottom: 10px"
      />
      <el-input
        v-model="importText"
        type="textarea"
        :rows="14"
        placeholder="Top queries,Clicks,Impressions,CTR,Position&#10;GEO 优化,120,3400,3.5%,8.2&#10;AI 客服,80,2100,3.8%,6.1%"
      />
      <template #footer>
        <el-button @click="importVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="importing"
          @click="submitImport"
        >
          导入
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getDualEngineReport, importSearchConsole } from '@/api/geoSearchConsole'
import { isUserReportedError } from '@/utils/errorReporting'
import ProbeModeBanner from './ProbeModeBanner.vue'

const days = ref(30)
const report = ref({ ai: {}, google: {}, push: {} })
const importVisible = ref(false)
const importText = ref('')
const importing = ref(false)

const modeText = (mode) => {
  if (mode === 'real') return '真实'
  if (mode === 'manual') return '人工导入'
  return '模拟'
}

async function load () {
  try {
    report.value = (await getDualEngineReport(days.value)) || { ai: {}, google: {}, push: {} }
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '加载双端报表失败')
  }
}

async function submitImport () {
  if (!importText.value.trim()) {
    ElMessage.warning('请粘贴 GSC CSV 内容')
    return
  }
  importing.value = true
  try {
    const res = await importSearchConsole({ csv: importText.value })
    ElMessage.success(`导入完成：成功 ${res?.imported || 0}，跳过 ${res?.skipped || 0}`)
    importVisible.value = false
    importText.value = ''
    await load()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '导入失败')
  } finally {
    importing.value = false
  }
}

onMounted(load)
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
.actions {
  display: flex;
  gap: 8px;
}
.inner-card {
  min-height: 360px;
}
.footer-note {
  margin-top: 10px;
  margin-left: 0;
}
</style>
