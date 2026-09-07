<template>
  <div class="reach-pipeline-page">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange" class="rp-tabs">
      
      <el-tab-pane :label="$t('Pipeline 管理')" name="pipelines">
        
        <el-row :gutter="16" class="stats-row">
          <el-col :xs="12" :sm="12" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-blue"><el-icon><Connection /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">总 Pipeline 数</div>
                <div class="stat-value">{{ stats.total | 0 }}</div>
                <div class="stat-sub">活跃 {{ stats.active | 0 }} · 暂停 {{ stats.paused | 0 }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="12" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-green"><el-icon><VideoPlay /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">{{ $t('运行中') }}</div>
                <div class="stat-value">{{ stats.active | 0 }}</div>
                <div class="stat-sub">活跃状态的 Pipeline</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="12" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-orange"><el-icon><VideoPause /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">{{ $t('已暂停') }}</div>
                <div class="stat-value">{{ stats.paused | 0 }}</div>
                <div class="stat-sub">已归档 {{ archivedCount }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="12" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-purple"><el-icon><FolderOpened /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">{{ $t('已归档') }}</div>
                <div class="stat-value">{{ archivedCount }}</div>
                <div class="stat-sub">{{ $t('总数减活跃与暂停') }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="12" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-cyan"><el-icon><List /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">{{ $t('任务总数') }}</div>
                <div class="stat-value">{{ stats.jobs | 0 }}</div>
                <div class="stat-sub">待执行 {{ stats.pending | 0 }} · 运行中 {{ stats.running | 0 }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="12" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-green2"><el-icon><CircleCheck /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">{{ $t('成功数') }}</div>
                <div class="stat-value">{{ stats.success | 0 }}</div>
                <div class="stat-sub">成功率 {{ overallSuccessRate }}%</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="12" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-red"><el-icon><CircleClose /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">{{ $t('失败数') }}</div>
                <div class="stat-value">{{ stats.failed | 0 }}</div>
                <div class="stat-sub">失败率 {{ overallFailureRate }}%</div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-card>
          <div class="toolbar">
            <div class="toolbar-left">
              <el-input
                v-model="pipeFilter.keyword"
                :placeholder="$t('搜索名称')"
                clearable
                style="width: 180px"
                @keyup.enter="onPipeFilterChange"
                @clear="onPipeFilterChange"
              />
              <el-select
                v-model="pipeFilter.channel"
                :placeholder="$t('渠道')"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onPipeFilterChange"
              >
                <el-option v-for="c in CHANNEL_OPTIONS" :key="c.value" :label="c.label" :value="c.value" />
              </el-select>
              <el-select
                v-model="pipeFilter.status"
                :placeholder="$t('状态')"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onPipeFilterChange"
              >
                <el-option :label="$t('活跃')" value="active" />
                <el-option :label="$t('暂停')" value="paused" />
                <el-option :label="$t('归档')" value="archived" />
              </el-select>
              <el-button style="margin-left: 8px" @click="resetPipeFilter">{{ $t('重置') }}</el-button>
              <el-button type="warning" plain @click="resetRateLimit">{{ $t('重置限流') }}</el-button>
            </div>
            <el-button style="margin-left: 8px" @click="$router.push('/reachPipeline/editor')">
              <el-icon><Share /></el-icon>&nbsp;可视化编辑器
            </el-button>
            <el-button type="primary" @click="openCreatePipe">
              <el-icon><Plus /></el-icon>&nbsp;新建 Pipeline
            </el-button>
          </div>

          <el-table :data="filteredPipelines" v-loading="pipeLoading" stripe>
            <el-table-column prop="name" :label="$t('名称')" min-width="150" show-overflow-tooltip />
            <el-table-column :label="$t('描述')" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ row.description | '-' }}</template>
            </el-table-column>
            <el-table-column label="渠道" width="110">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ channelLabel(row.channel) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag :type="pipeStatusType(row.status)" size="small">{{ pipeStatusText(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="版本" prop="version" width="80" align="center" />
            <el-table-column label="运行/成功/失败" width="160" align="center">
              <template #default="{ row }">
                <span>{{ row.total_runs || 0 }} / {{ row.total_success || 0 }} / {{ row.total_failure || 0 }}</span>
              </template>
            </el-table-column>
            <el-table-column label="创建时间" width="170">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="340" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="viewPipe(row)">查看</el-button>
                <el-button v-if="row.status === 'active'" link type="warning" @click="pausePipe(row)">暂停</el-button>
                <el-button v-if="row.status === 'paused'" link type="success" @click="resumePipe(row)">恢复</el-button>
                <el-button v-if="row.status !== 'archived'" link type="info" @click="archivePipe(row)">归档</el-button>
                <el-button link type="danger" @click="deletePipe(row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty><el-empty description="暂无 Pipeline 数据" /></template>
          </el-table>

          <div class="pager">
            <el-pagination
              background
              layout="total, sizes, prev, pager, next, jumper"
              :total="pipePager.total"
              :page-size="pipePager.page_size"
              :current-page="pipePager.page"
              :page-sizes="[10, 20, 50]"
              @size-change="(s) => { pipePager.page_size = s; loadPipelines() }"
              @current-change="(p) => { pipePager.page = p; loadPipelines() }"
            />
          </div>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="任务监控" name="jobs">
        <el-card>
          <div class="toolbar">
            <div class="toolbar-left">
              <el-select
                v-model="jobFilter.pipeline_id"
                placeholder="Pipeline 筛选"
                clearable
                filterable
                style="width: 220px"
              >
                <el-option
                  v-for="p in pipelines"
                  :key="p.id"
                  :label="`${p.name} (${channelLabel(p.channel)})`"
                  :value="p.id"
                />
              </el-select>
              <el-select
                v-model="jobFilter.channel"
                placeholder="渠道"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onJobFilterChange"
              >
                <el-option v-for="c in CHANNEL_OPTIONS" :key="c.value" :label="c.label" :value="c.value" />
              </el-select>
              <el-select
                v-model="jobFilter.state"
                placeholder="状态"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onJobFilterChange"
              >
                <el-option v-for="s in jobStateOptions" :key="s.value" :label="s.label" :value="s.value" />
              </el-select>
              <el-button style="margin-left: 8px" @click="resetJobFilter">重置</el-button>
            </div>
            <el-button type="primary" @click="openEnqueueJob">
              <el-icon><Plus /></el-icon>&nbsp;加入任务
            </el-button>
          </div>

          <el-table :data="filteredJobs" v-loading="jobLoading" stripe>
            <el-table-column prop="id" label="ID" width="80" />
            <el-table-column label="Pipeline" width="160" show-overflow-tooltip>
              <template #default="{ row }">
                <span>{{ pipelineName(row.pipeline_id) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="目标客户" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ row.customer_id }}</template>
            </el-table-column>
            <el-table-column label="渠道" width="110">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ channelLabel(row.channel) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="110">
              <template #default="{ row }">
                <el-tag :type="jobStateType(row.state)" size="small">{{ jobStateText(row.state) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="重试次数" width="100" align="center">
              <template #default="{ row }">{{ row.retry_count || 0 }}/{{ row.max_retry || 0 }}</template>
            </el-table-column>
            <el-table-column label="耗时" width="100" align="center">
              <template #default="{ row }">{{ row.duration_ms ? row.duration_ms + 'ms' : '-' }}</template>
            </el-table-column>
            <el-table-column label="创建时间" width="170">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="260" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="viewJob(row)">查看</el-button>
                <el-button v-if="canExecute(row.state)" link type="success" @click="executeJob(row)">执行</el-button>
                <el-button v-if="canCancel(row.state)" link type="warning" @click="cancelJob(row)">取消</el-button>
                <el-button v-if="row.state === 'failed'" link type="primary" @click="retryJob(row)">重试</el-button>
              </template>
            </el-table-column>
            <template #empty><el-empty description="暂无任务数据" /></template>
          </el-table>

          <div class="pager">
            <el-pagination
              background
              layout="total, sizes, prev, pager, next, jumper"
              :total="jobPager.total"
              :page-size="jobPager.page_size"
              :current-page="jobPager.page"
              :page-sizes="[10, 20, 50]"
              @size-change="(s) => { jobPager.page_size = s; loadJobs() }"
              @current-change="(p) => { jobPager.page = p; loadJobs() }"
            />
          </div>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="统计分析" name="stats">
        <el-row :gutter="16" class="stats-row">
          <el-col :xs="12" :sm="8" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-green"><el-icon><CircleCheck /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">成功率</div>
                <div class="stat-value">{{ overallSuccessRate }}%</div>
                <div class="stat-sub">成功 {{ stats.success || 0 }} / 总计 {{ stats.jobs || 0 }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="8" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-red"><el-icon><CircleClose /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">失败率</div>
                <div class="stat-value">{{ overallFailureRate }}%</div>
                <div class="stat-sub">失败 {{ stats.failed || 0 }} / 总计 {{ stats.jobs || 0 }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="8" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-purple"><el-icon><Timer /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">平均执行时间</div>
                <div class="stat-value">{{ avgDuration }}</div>
                <div class="stat-sub">基于近期任务样本</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="12" :sm="8" :md="6">
            <el-card shadow="hover" class="stat-card">
              <div class="stat-icon stat-icon-orange"><el-icon><Warning /></el-icon></div>
              <div class="stat-body">
                <div class="stat-title">限流次数</div>
                <div class="stat-value">{{ stats.rate_limited || 0 }}</div>
                <div class="stat-sub">已取消 {{ stats.canceled || 0 }}</div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-card>
          <template #header>
            <div class="card-header">
              <span>各渠道任务统计</span>
              <span class="muted">基于近期任务样本（{{ statsSample.length }} 条）</span>
            </div>
          </template>
          <el-table :data="channelStats" v-loading="statsLoading" stripe>
            <el-table-column label="渠道" width="140">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ channelLabel(row.channel) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="total" label="任务数" width="120" align="center" />
            <el-table-column prop="success" label="成功" width="100" align="center">
              <template #default="{ row }">
                <span style="color: #10B981">{{ row.success }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="failed" label="失败" width="100" align="center">
              <template #default="{ row }">
                <span style="color: #EF4444">{{ row.failed }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="running" label="运行中" width="100" align="center" />
            <el-table-column prop="pending" label="待执行" width="100" align="center" />
            <el-table-column label="成功率" width="120" align="center">
              <template #default="{ row }">{{ row.successRate }}%</template>
            </el-table-column>
            <el-table-column label="平均耗时" width="140" align="center">
              <template #default="{ row }">{{ row.avgDuration }}</template>
            </el-table-column>
            <template #empty><el-empty description="暂无任务样本数据" /></template>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    
    <el-dialog
      v-model="pipeDialogVisible"
      :title="pipeDialogTitle"
      width="780px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form :model="pipeForm" :rules="pipeRules" ref="pipeFormRef" label-width="120px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="pipeForm.name" placeholder="请输入 Pipeline 名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="pipeForm.description" type="textarea" :rows="2" placeholder="可选" />
        </el-form-item>
        <el-form-item label="渠道" prop="channel">
          <el-select v-model="pipeForm.channel" style="width: 100%" placeholder="请选择渠道">
            <el-option v-for="c in CHANNEL_OPTIONS" :key="c.value" :label="c.label" :value="c.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="Pipeline 步骤" prop="steps">
          <el-select v-model="pipeForm.steps" multiple style="width: 100%" placeholder="默认 9 步全选，必须包含 send">
            <el-option v-for="s in stepOptions" :key="s.value" :label="s.label" :value="s.value" />
          </el-select>
        </el-form-item>

        <el-divider content-position="left">重试策略</el-divider>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="最大重试次数">
              <el-input-number v-model="pipeForm.retry_policy.max_retries" :min="0" :max="20" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="重试间隔(ms)">
              <el-input-number v-model="pipeForm.retry_policy.interval_ms" :min="0" :step="500" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="退避策略">
              <el-select v-model="pipeForm.retry_policy.backoff" style="width: 100%">
                <el-option label="固定间隔" value="fixed" />
                <el-option label="指数退避" value="exponential" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="退避上限(ms)">
              <el-input-number v-model="pipeForm.retry_policy.max_interval_ms" :min="0" :step="1000" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-divider content-position="left">限流配置</el-divider>
        <el-row :gutter="12">
          <el-col :span="8">
            <el-form-item label="QPS">
              <el-input-number v-model="pipeForm.rate_limit.qps" :min="0" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="突发容量">
              <el-input-number v-model="pipeForm.rate_limit.burst" :min="0" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="每日配额">
              <el-input-number v-model="pipeForm.rate_limit.daily_quota" :min="0" :step="100" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="单用户频次">
              <el-input-number v-model="pipeForm.rate_limit.per_user_limit" :min="0" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="冷却秒数">
              <el-input-number v-model="pipeForm.rate_limit.cooldown_secs" :min="0" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="pipeDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="pipeSubmitting" @click="submitPipe">确定</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="pipeDetailVisible" title="Pipeline 详情" width="820px">
      <el-descriptions v-if="currentPipe" :column="2" border>
        <el-descriptions-item label="ID">{{ currentPipe.id }}</el-descriptions-item>
        <el-descriptions-item label="名称">{{ currentPipe.name }}</el-descriptions-item>
        <el-descriptions-item label="渠道">{{ channelLabel(currentPipe.channel) }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="pipeStatusType(currentPipe.status)" size="small">{{ pipeStatusText(currentPipe.status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="版本">{{ currentPipe.version }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ formatTime(currentPipe.created_at) }}</el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">{{ currentPipe.description || '-' }}</el-descriptions-item>
        <el-descriptions-item label="运行次数">{{ currentPipe.total_runs || 0 }}</el-descriptions-item>
        <el-descriptions-item label="成功次数">{{ currentPipe.total_success || 0 }}</el-descriptions-item>
        <el-descriptions-item label="失败次数">{{ currentPipe.total_failure || 0 }}</el-descriptions-item>
        <el-descriptions-item label="完成率">{{ pipeCompletionRate(currentPipe) }}%</el-descriptions-item>
        <el-descriptions-item label="Pipeline 步骤" :span="2">
          <div class="steps-list">
            <el-tag
              v-for="(s, i) in pipeStepsList(currentPipe)"
              :key="i"
              class="step-tag"
              :type="s === 'send' ? 'success' : 'info'"
              effect="plain"
            >
              {{ i + 1 }}. {{ stepLabel(s) }}
            </el-tag>
            <span v-if="!pipeStepsList(currentPipe).length" class="muted">-</span>
          </div>
        </el-descriptions-item>
        <el-descriptions-item label="重试策略" :span="2">
          <pre class="json-pre">{{ formatJSON(currentPipe.retry_policy) }}</pre>
        </el-descriptions-item>
        <el-descriptions-item label="限流配置" :span="2">
          <pre class="json-pre">{{ formatJSON(currentPipe.rate_limit) }}</pre>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>

    
    <el-dialog
      v-model="jobDialogVisible"
      title="加入任务"
      width="640px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form :model="jobForm" :rules="jobRules" ref="jobFormRef" label-width="120px">
        <el-form-item label="Pipeline" prop="pipeline_id">
          <el-select
            v-model="jobForm.pipeline_id"
            placeholder="选择活跃 Pipeline"
            filterable
            style="width: 100%"
            @change="onJobPipelineChange"
          >
            <el-option
              v-for="p in activePipelines"
              :key="p.id"
              :label="`${p.name} (${channelLabel(p.channel)})`"
              :value="p.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="渠道">
          <el-select v-model="jobForm.channel" placeholder="默认跟随 Pipeline" clearable style="width: 100%">
            <el-option v-for="c in CHANNEL_OPTIONS" :key="c.value" :label="c.label" :value="c.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标客户ID" prop="customer_id">
          <el-input v-model="jobForm.customer_id" placeholder="请输入目标客户 ID" />
        </el-form-item>
        <el-form-item label="账号ID">
          <el-input v-model="jobForm.account_id" placeholder="可选，留空自动选择" />
        </el-form-item>
        <el-form-item label="最大重试">
          <el-input-number v-model="jobForm.max_retry" :min="0" :max="20" />
          <span class="muted" style="margin-left: 8px">0 表示使用 Pipeline 默认策略</span>
        </el-form-item>
        <el-form-item label="Payload" prop="payloadStr">
          <el-input v-model="jobForm.payloadStr" type="textarea" :rows="4" placeholder='例如: {"template":"welcome"}' />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="jobDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="jobSubmitting" @click="submitJob">入队</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="jobDetailVisible" title="任务详情" width="860px">
      <el-descriptions v-if="currentJob" :column="2" border>
        <el-descriptions-item label="ID">{{ currentJob.id }}</el-descriptions-item>
        <el-descriptions-item label="Pipeline">{{ pipelineName(currentJob.pipeline_id) }} (#{{ currentJob.pipeline_id }})</el-descriptions-item>
        <el-descriptions-item label="渠道">{{ channelLabel(currentJob.channel) }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="jobStateType(currentJob.state)" size="small">{{ jobStateText(currentJob.state) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="目标客户">{{ currentJob.customer_id }}</el-descriptions-item>
        <el-descriptions-item label="账号">{{ currentJob.account_id || '-' }}</el-descriptions-item>
        <el-descriptions-item label="重试">{{ currentJob.retry_count || 0 }}/{{ currentJob.max_retry || 0 }}</el-descriptions-item>
        <el-descriptions-item label="耗时">{{ currentJob.duration_ms ? currentJob.duration_ms + 'ms' : '-' }}</el-descriptions-item>
        <el-descriptions-item label="开始时间">{{ formatTime(currentJob.started_at) }}</el-descriptions-item>
        <el-descriptions-item label="完成时间">{{ formatTime(currentJob.completed_at) }}</el-descriptions-item>
        <el-descriptions-item label="下次执行">{{ formatTime(currentJob.next_run_at) }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ formatTime(currentJob.created_at) }}</el-descriptions-item>
        <el-descriptions-item label="错误信息" :span="2">
          <span :style="{ color: currentJob.error_message ? '#EF4444' : '' }">{{ currentJob.error_message || '-' }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="Payload" :span="2">
          <pre class="json-pre">{{ formatJSON(currentJob.payload) }}</pre>
        </el-descriptions-item>
        <el-descriptions-item label="执行日志 / 步骤结果" :span="2">
          <el-table :data="jobStepResults(currentJob)" size="small" border>
            <el-table-column label="#" type="index" width="50" align="center" />
            <el-table-column label="步骤" width="150">
              <template #default="{ row }">{{ stepLabel(row.step) }}</template>
            </el-table-column>
            <el-table-column label="结果" width="90" align="center">
              <template #default="{ row }">
                <el-tag :type="row.success ? 'success' : 'danger'" size="small">{{ row.success ? '成功' : '失败' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="耗时" width="90" align="center">
              <template #default="{ row }">{{ row.duration_ms || 0 }}ms</template>
            </el-table-column>
            <el-table-column label="输出 / 错误" min-width="220">
              <template #default="{ row }">
                <span v-if="row.error" style="color: #EF4444">{{ row.error }}</span>
                <pre v-else class="json-pre small">{{ formatJSON(row.output) }}</pre>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!jobStepResults(currentJob).length" description="暂无步骤结果" :image-size="60" />
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus, Connection, VideoPlay, VideoPause, FolderOpened, List,
  CircleCheck, CircleClose, Timer, Warning, Share
} from '@element-plus/icons-vue'
import { reachPipelineApi } from '@/api/reachPipeline.js'
import { CHANNEL_OPTIONS, getChannelLabel } from '@/constants/channel'

const stepOptions = [
  {
    value: 'audience',
    label: '受众筛选'
  },
  { value: 'content_prepare', label: '内容准备' },
  { value: 'account_select', label: '账号选择' },
  { value: 'rate_limit', label: '限流控制' },
  { value: 'message_gen', label: '文案生成' },
  { value: 'send', label: '发送执行' },
  { value: 'track_result', label: '结果追踪' },
  { value: 'retry', label: '失败重试' },
  { value: 'report', label: '汇总报告' }
];

const jobStateOptions = [
  { value: 'pending', label: '排队' },
  { value: 'running', label: '执行中' },
  { value: 'success', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '取消' },
  { value: 'retrying', label: '重试中' },
  { value: 'rate_limited', label: '已限流' }
]

const channelLabel = getChannelLabel
const stepLabel = (v) => (stepOptions.find((s) => s.value === v) || {}).label || v
const pipeStatusType = (s) => ({ active: 'success', paused: 'warning', archived: 'info' }[s] || 'info')
const pipeStatusText = (s) => ({ active: '活跃', paused: '暂停', archived: '归档' }[s] || s)
const jobStateType = (s) => ({
  pending: 'info',
  running: 'warning',
  success: 'success',
  failed: 'danger',
  canceled: 'info',
  retrying: 'warning',
  rate_limited: 'danger'
}[s] || 'info')
const jobStateText = (s) => ({
  pending: '排队',
  running: '执行中',
  success: '成功',
  failed: '失败',
  canceled: '取消',
  retrying: '重试中',
  rate_limited: '已限流'
}[s] || s)

const formatTime = (t) => {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '-'
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
const formatJSON = (obj) => {
  if (obj === null || obj === undefined || obj === '') return '-'
  try {
    return JSON.stringify(obj, null, 2)
  } catch (e) {
    return String(obj)
  }
}

const stats = ref({});
const loadStats = async () => {
  try {
    const res = await reachPipelineApi.getStats()
    stats.value = res || {}
  } catch (e) {}
}
const archivedCount = computed(() => {
  const total = Number(stats.value.total || 0)
  const active = Number(stats.value.active || 0)
  const paused = Number(stats.value.paused || 0)
  const archived = total - active - paused
  return archived > 0 ? archived : 0
})
const overallSuccessRate = computed(() => {
  const total = Number(stats.value.jobs || 0)
  const succ = Number(stats.value.success || 0)
  if (!total) return 0
  return ((succ / total) * 100).toFixed(1)
})
const overallFailureRate = computed(() => {
  const total = Number(stats.value.jobs || 0)
  const fail = Number(stats.value.failed || 0)
  if (!total) return 0
  return ((fail / total) * 100).toFixed(1)
})

const activeTab = ref('pipelines');
const handleTabChange = (name) => {
  if (name === 'jobs' && !jobs.value.length) loadJobs()
  if (name === 'stats') loadStatsSample()
}

const pipelines = ref([]);
const pipeLoading = ref(false)
const pipeFilter = reactive({ keyword: '', channel: '', status: '' })
const pipePager = reactive({ page: 1, page_size: 20, total: 0 })

const loadPipelines = async () => {
  pipeLoading.value = true
  try {
    const res = await reachPipelineApi.getPipelines({
      page: pipePager.page,
      page_size: pipePager.page_size,
      channel: pipeFilter.channel,
      status: pipeFilter.status
    })
    pipelines.value = res.list || []
    pipePager.total = res.total || 0
  } catch (e) {
    ElMessage.error(e.message || '获取 Pipeline 列表失败')
  } finally {
    pipeLoading.value = false
  }
}
const filteredPipelines = computed(() => {
  const kw = (pipeFilter.keyword || '').trim().toLowerCase()
  if (!kw) return pipelines.value
  return pipelines.value.filter((p) => (p.name || '').toLowerCase().includes(kw))
});
const onPipeFilterChange = () => {
  pipePager.page = 1
  loadPipelines()
}
const resetPipeFilter = () => {
  pipeFilter.keyword = ''
  pipeFilter.channel = ''
  pipeFilter.status = ''
  pipePager.page = 1
  loadPipelines()
}
const activePipelines = computed(() => pipelines.value.filter((p) => p.status === 'active'))
const pipelineName = (id) => {
  const p = pipelines.value.find((x) => x.id === id)
  return p ? p.name : `#${id}`
}

const pipeDialogVisible = ref(false);
const pipeDialogTitle = ref('新建 Pipeline')
const pipeFormRef = ref()
const pipeSubmitting = ref(false)

const defaultPipeForm = () => ({
  name: '',
  description: '',
  channel: 'wecom',
  steps: stepOptions.map((s) => s.value),
  retry_policy: { max_retries: 3, interval_ms: 1000, backoff: 'exponential', max_interval_ms: 60000 },
  rate_limit: { qps: 10, burst: 20, daily_quota: 10000, per_user_limit: 3, cooldown_secs: 60 }
})
const pipeForm = reactive(defaultPipeForm())
const pipeRules = {
  name: [{ required: true, message: i18n.global.t('请输入名称'), trigger: 'blur' }],
  channel: [{ required: true, message: i18n.global.t('请选择渠道'), trigger: 'change' }],
  steps: [{ required: true, type: 'array', min: 1, message: i18n.global.t('至少选择一个步骤'), trigger: 'change' }]
}

const openCreatePipe = () => {
  Object.assign(pipeForm, defaultPipeForm())
  pipeDialogTitle.value = '新建 Pipeline'
  pipeDialogVisible.value = true
}
const submitPipe = async () => {
  if (!pipeFormRef.value) return
  await pipeFormRef.value.validate(async (valid) => {
    if (!valid) return
    if (!pipeForm.steps.includes('send')) {
      ElMessage.warning(i18n.global.t('步骤必须包含 send (发送执行)'))
      return
    }
    pipeSubmitting.value = true
    try {
      const data = {
        name: pipeForm.name,
        description: pipeForm.description,
        channel: pipeForm.channel,
        steps: pipeForm.steps,
        retry_policy: pipeForm.retry_policy,
        rate_limit: pipeForm.rate_limit
      }
      await reachPipelineApi.createPipeline(data)
      ElMessage.success(i18n.global.t('创建成功'))
      pipeDialogVisible.value = false
      loadPipelines()
      loadStats()
    } catch (e) {
      ElMessage.error(e.message || '创建失败')
    } finally {
      pipeSubmitting.value = false
    }
  })
}

const pausePipe = async (row) => {
  try {
    await reachPipelineApi.pausePipeline(row.id)
    ElMessage.success(i18n.global.t('已暂停'))
    loadPipelines()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
}
const resumePipe = async (row) => {
  try {
    await reachPipelineApi.resumePipeline(row.id)
    ElMessage.success(i18n.global.t('已恢复'))
    loadPipelines()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
}
const archivePipe = async (row) => {
  try {
    await ElMessageBox.confirm(`确定归档 "${row.name}"？归档后不可再加入任务`, '确认', { type: 'warning' })
    await reachPipelineApi.archivePipeline(row.id)
    ElMessage.success(i18n.global.t('已归档'))
    loadPipelines()
    loadStats()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
}
const deletePipe = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除 "${row.name}"？此操作不可恢复`, '确认', { type: 'warning' })
    await reachPipelineApi.deletePipeline(row.id)
    ElMessage.success(i18n.global.t('已删除'))
    loadPipelines()
    loadStats()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
}

const resetRateLimit = async () => {
  if (!pipeFilter.channel) {
    ElMessage.warning(i18n.global.t('请先在渠道筛选中选择一个渠道'))
    return
  }
  try {
    await ElMessageBox.confirm(`确定重置渠道 "${channelLabel(pipeFilter.channel)}" 的限流状态？`, '确认', { type: 'warning' })
    await reachPipelineApi.resetRateLimit(pipeFilter.channel)
    ElMessage.success(i18n.global.t('限流已重置'))
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
};

const pipeDetailVisible = ref(false);
const currentPipe = ref(null)
const viewPipe = async (row) => {
  try {
    const res = await reachPipelineApi.getPipeline(row.id)
    currentPipe.value = res
    pipeDetailVisible.value = true
  } catch (e) {
    ElMessage.error(e.message || '获取详情失败')
  }
}
const pipeStepsList = (p) => (Array.isArray(p && p.steps) ? p.steps.map((s) => String(s)) : [])
const pipeCompletionRate = (p) => {
  const runs = Number(p && p.total_runs || 0)
  const succ = Number(p && p.total_success || 0)
  if (!runs) return 0
  return ((succ / runs) * 100).toFixed(1)
}

const jobs = ref([]);
const jobLoading = ref(false)
const jobFilter = reactive({ pipeline_id: null, channel: '', state: '' })
const jobPager = reactive({ page: 1, page_size: 20, total: 0 })

const loadJobs = async () => {
  jobLoading.value = true
  try {
    const res = await reachPipelineApi.getJobs({
      page: jobPager.page,
      page_size: jobPager.page_size,
      channel: jobFilter.channel,
      state: jobFilter.state
    })
    jobs.value = res.list || []
    jobPager.total = res.total || 0
  } catch (e) {
    ElMessage.error(e.message || '获取任务列表失败')
  } finally {
    jobLoading.value = false
  }
}
const filteredJobs = computed(() => {
  if (!jobFilter.pipeline_id) return jobs.value
  return jobs.value.filter((j) => j.pipeline_id === jobFilter.pipeline_id)
});
const onJobFilterChange = () => {
  jobPager.page = 1
  loadJobs()
}
const resetJobFilter = () => {
  jobFilter.pipeline_id = null
  jobFilter.channel = ''
  jobFilter.state = ''
  jobPager.page = 1
  loadJobs()
}

const canExecute = (s) => ['pending', 'retrying', 'rate_limited'].includes(s)
const canCancel = (s) => ['pending', 'running', 'retrying', 'rate_limited'].includes(s)

const executeJob = async (row) => {
  try {
    await reachPipelineApi.executeJob(row.id)
    ElMessage.success(i18n.global.t('执行完成'))
    loadJobs()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || '执行失败')
    loadJobs()
  }
}
const cancelJob = async (row) => {
  try {
    await ElMessageBox.confirm(`确定取消任务 #${row.id}？`, '确认', { type: 'warning' })
    await reachPipelineApi.cancelJob(row.id)
    ElMessage.success(i18n.global.t('已取消'))
    loadJobs()
    loadStats()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
}
const retryJob = async (row) => {
  try {
    await reachPipelineApi.retryJob(row.id)
    ElMessage.success(i18n.global.t('已重新入队'))
    loadJobs()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
}

const jobDialogVisible = ref(false);
const jobFormRef = ref()
const jobSubmitting = ref(false)
const jobForm = reactive({ pipeline_id: null, channel: '', customer_id: '', account_id: '', max_retry: 0, payloadStr: '{}' })
const jobRules = {
  pipeline_id: [{ required: true, message: i18n.global.t('请选择 Pipeline'), trigger: 'change' }],
  customer_id: [{ required: true, message: i18n.global.t('请输入目标客户ID'), trigger: 'blur' }],
  payloadStr: [{ required: true, message: i18n.global.t('请输入 Payload'), trigger: 'blur' }]
}
const openEnqueueJob = () => {
  Object.assign(jobForm, { pipeline_id: null, channel: '', customer_id: '', account_id: '', max_retry: 0, payloadStr: '{}' })
  jobDialogVisible.value = true
}
const onJobPipelineChange = (id) => {
  const p = pipelines.value.find((x) => x.id === id)
  if (p && !jobForm.channel) jobForm.channel = p.channel
}
const submitJob = async () => {
  if (!jobFormRef.value) return
  await jobFormRef.value.validate(async (valid) => {
    if (!valid) return
    let payload
    try {
      payload = JSON.parse(jobForm.payloadStr)
    } catch (e) {
      ElMessage.error(i18n.global.t('Payload 不是有效的 JSON'))
      return
    }
    jobSubmitting.value = true
    try {
      const data = {
        pipeline_id: jobForm.pipeline_id,
        channel: jobForm.channel,
        customer_id: jobForm.customer_id,
        account_id: jobForm.account_id,
        max_retry: jobForm.max_retry,
        payload
      }
      await reachPipelineApi.enqueueJob(data)
      ElMessage.success(i18n.global.t('任务已入队'))
      jobDialogVisible.value = false
      activeTab.value = 'jobs'
      jobPager.page = 1
      loadJobs()
      loadStats()
    } catch (e) {
      ElMessage.error(e.message || '入队失败')
    } finally {
      jobSubmitting.value = false
    }
  })
}

const jobDetailVisible = ref(false);
const currentJob = ref(null)
const viewJob = async (row) => {
  try {
    const res = await reachPipelineApi.getJob(row.id)
    currentJob.value = res
    jobDetailVisible.value = true
  } catch (e) {
    ElMessage.error(e.message || '获取详情失败')
  }
}
const jobStepResults = (j) => (Array.isArray(j && j.step_results) ? j.step_results : [])

const statsSample = ref([]);
const statsLoading = ref(false)
const loadStatsSample = async () => {
  statsLoading.value = true
  try {
    const res = await reachPipelineApi.getJobs({ page: 1, page_size: 200 });
    statsSample.value = res.list || []
  } catch (e) {
    ElMessage.error(e.message || '获取统计样本失败')
  } finally {
    statsLoading.value = false
  }
}
const avgDuration = computed(() => {
  const valid = statsSample.value.filter((j) => Number(j.duration_ms) > 0)
  if (!valid.length) return '-'
  const avg = valid.reduce((s, j) => s + Number(j.duration_ms), 0) / valid.length
  return avg < 1000 ? `${Math.round(avg)}ms` : `${(avg / 1000).toFixed(2)}s`
})
const channelStats = computed(() => {
  const map = {}
  statsSample.value.forEach((j) => {
    const ch = j.channel || 'unknown'
    if (!map[ch]) {
      map[ch] = { channel: ch, total: 0, success: 0, failed: 0, running: 0, pending: 0, durationSum: 0, durationCnt: 0 }
    }
    const item = map[ch]
    item.total++
    if (j.state === 'success') item.success++
    else if (j.state === 'failed') item.failed++
    else if (j.state === 'running') item.running++
    else if (j.state === 'pending') item.pending++
    if (Number(j.duration_ms) > 0) {
      item.durationSum += Number(j.duration_ms)
      item.durationCnt++
    }
  })
  return Object.values(map).map((item) => {
    const rate = item.total ? ((item.success / item.total) * 100).toFixed(1) : '0.0'
    const avg = item.durationCnt ? Math.round(item.durationSum / item.durationCnt) : 0
    return {
      ...item,
      successRate: rate,
      avgDuration: avg < 1000 ? `${avg}ms` : `${(avg / 1000).toFixed(2)}s`
    }
  })
})

onMounted(() => {
  loadStats()
  loadPipelines()
  loadJobs()
});
</script>

<style scoped lang="scss">
.reach-pipeline-page {
  padding: 20px;
}

.rp-tabs {
  :deep(.el-tabs__content) {
    padding-top: 8px;
  }
}

.stats-row {
  margin-bottom: 16px;
}

.stat-card {
  margin-bottom: 12px;
  :deep(.el-card__body) {
    display: flex;
    align-items: center;
    padding: 18px 20px;
  }
  .stat-icon {
    width: 52px;
    height: 52px;
    border-radius: 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-right: 16px;
    font-size: 26px;
    color: #fff;
    flex-shrink: 0;
  }
  .stat-icon-blue { background: linear-gradient(135deg, #4F46E5, #66b1ff); }
  .stat-icon-green { background: linear-gradient(135deg, #10B981, #95d475); }
  .stat-icon-green2 { background: linear-gradient(135deg, #529b2e, #10B981); }
  .stat-icon-orange { background: linear-gradient(135deg, #F59E0B, #f0b87a); }
  .stat-icon-red { background: linear-gradient(135deg, #EF4444, #f89898); }
  .stat-icon-purple { background: linear-gradient(135deg, #9c6cd6, #b589e0); }
  .stat-icon-cyan { background: linear-gradient(135deg, #13c2c2, #5cdbd3); }

  .stat-body {
    flex: 1;
    min-width: 0;
  }
  .stat-title {
    font-size: 13px;
    color: #909399;
    margin-bottom: 6px;
  }
  .stat-value {
    font-size: 26px;
    font-weight: 600;
    color: #303133;
    line-height: 1.2;
  }
  .stat-sub {
    font-size: 12px;
    color: #c0c4cc;
    margin-top: 4px;
  }
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 8px;
}
.toolbar-left {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.steps-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.step-tag {
  margin: 0;
}

.muted {
  color: #c0c4cc;
  font-size: 13px;
}

.json-pre {
  margin: 0;
  padding: 8px 10px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 240px;
  overflow: auto;
}
.json-pre.small {
  padding: 4px 6px;
  max-height: 120px;
}
</style>
