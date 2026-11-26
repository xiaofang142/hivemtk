<template>
  <div class="reach-pipeline-page">
    <el-tabs
      v-model="activeTab"
      class="rp-tabs"
      @tab-change="handleTabChange"
    >
      <el-tab-pane
        :label="$t('reachPipeline.tabPipelines')"
        name="pipelines"
      >
        <el-row
          :gutter="16"
          class="stats-row"
        >
          <el-col
            :xs="12"
            :sm="12"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-blue">
                <el-icon><Connection /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.statTotalPipelines') }}
                </div>
                <div class="stat-value">
                  {{ stats.total ?? 0 }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statActiveSub', { active: stats.active ?? 0, paused: stats.paused ?? 0 }) }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="12"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-green">
                <el-icon><VideoPlay /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.running') }}
                </div>
                <div class="stat-value">
                  {{ stats.active ?? 0 }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statActiveDesc') }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="12"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-orange">
                <el-icon><VideoPause /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.paused') }}
                </div>
                <div class="stat-value">
                  {{ stats.paused ?? 0 }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statArchivedSub', { count: archivedCount }) }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="12"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-purple">
                <el-icon><FolderOpened /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.archived') }}
                </div>
                <div class="stat-value">
                  {{ archivedCount }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statArchivedHint') }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="12"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-cyan">
                <el-icon><List /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.statJobsTotal') }}
                </div>
                <div class="stat-value">
                  {{ stats.jobs ?? 0 }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statJobsSub', { pending: stats.pending ?? 0, running: stats.running ?? 0 }) }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="12"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-green2">
                <el-icon><CircleCheck /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.statSuccessCount') }}
                </div>
                <div class="stat-value">
                  {{ stats.success ?? 0 }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statSuccessSub', { rate: overallSuccessRate }) }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="12"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-red">
                <el-icon><CircleClose /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.statFailCount') }}
                </div>
                <div class="stat-value">
                  {{ stats.failed ?? 0 }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statFailSub', { rate: overallFailureRate }) }}
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-card>
          <div class="toolbar">
            <div class="toolbar-left">
              <el-input
                v-model="pipeFilter.keyword"
                :placeholder="$t('reachPipeline.searchName')"
                clearable
                style="width: 180px"
                @keyup.enter="onPipeFilterChange"
                @clear="onPipeFilterChange"
              />
              <ChannelSelect
                v-model="pipeFilter.channel"
                :placeholder="$t('reachPipeline.channel')"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onPipeFilterChange"
              />
              <el-select
                v-model="pipeFilter.status"
                :placeholder="$t('reachPipeline.status')"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onPipeFilterChange"
              >
                <el-option
                  :label="$t('reachPipeline.active')"
                  value="active"
                />
                <el-option
                  :label="$t('reachPipeline.pausedAction')"
                  value="paused"
                />
                <el-option
                  :label="$t('reachPipeline.archiveAction')"
                  value="archived"
                />
              </el-select>
              <el-button
                style="margin-left: 8px"
                @click="resetPipeFilter"
              >
                {{ $t('reachPipeline.reset') }}
              </el-button>
              <el-button
                type="warning"
                plain
                @click="resetRateLimit"
              >
                {{ $t('reachPipeline.resetRateLimit') }}
              </el-button>
            </div>
            <el-button
              style="margin-left: 8px"
              @click="$router.push('/reachPipeline/editor')"
            >
              <el-icon><Share /></el-icon>&nbsp;{{ $t('reachPipeline.openEditor') }}
            </el-button>
            <el-button
              type="primary"
              @click="openCreatePipe"
            >
              <el-icon><Plus /></el-icon>&nbsp;{{ $t('reachPipeline.newPipeline') }}
            </el-button>
          </div>

          <el-table
            v-loading="pipeLoading"
            :data="filteredPipelines"
            stripe
          >
            <el-table-column
              prop="name"
              :label="$t('reachPipeline.name')"
              min-width="150"
              show-overflow-tooltip
            />
            <el-table-column
              :label="$t('reachPipeline.description')"
              min-width="180"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ row.description ?? '-' }}
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.channel')"
              width="110"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  effect="plain"
                >
                  {{ channelLabel(row.channel) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.status')"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  :type="pipeStatusType(row.status)"
                  size="small"
                >
                  {{ pipeStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.version')"
              prop="version"
              width="80"
              align="center"
            />
            <el-table-column
              :label="$t('reachPipeline.runsCol')"
              width="160"
              align="center"
            >
              <template #default="{ row }">
                <span>{{ row.total_runs || 0 }} / {{ row.total_success || 0 }} / {{ row.total_failure || 0 }}</span>
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.createdAt')"
              width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.created_at) }}
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.operation')"
              width="340"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  @click="viewPipe(row)"
                >
                  {{ $t('reachPipeline.view') }}
                </el-button>
                <el-button
                  v-if="row.status === 'active'"
                  link
                  type="warning"
                  @click="pausePipe(row)"
                >
                  {{ $t('reachPipeline.pausedAction') }}
                </el-button>
                <el-button
                  v-if="row.status === 'paused'"
                  link
                  type="success"
                  @click="resumePipe(row)"
                >
                  {{ $t('reachPipeline.resume') }}
                </el-button>
                <el-button
                  v-if="row.status !== 'archived'"
                  link
                  type="info"
                  @click="archivePipe(row)"
                >
                  {{ $t('reachPipeline.archiveAction') }}
                </el-button>
                <el-button
                  link
                  type="danger"
                  @click="deletePipe(row)"
                >
                  {{ $t('reachPipeline.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="$t('reachPipeline.emptyPipelines')" />
            </template>
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

      
      <el-tab-pane
        :label="$t('reachPipeline.tabJobs')"
        name="jobs"
      >
        <el-card>
          <div class="toolbar">
            <div class="toolbar-left">
              <el-select
                v-model="jobFilter.pipeline_id"
                :placeholder="$t('reachPipeline.filterPipeline')"
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
              <ChannelSelect
                v-model="jobFilter.channel"
                :placeholder="$t('reachPipeline.channel')"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onJobFilterChange"
              />
              <el-select
                v-model="jobFilter.state"
                :placeholder="$t('reachPipeline.status')"
                clearable
                style="width: 140px; margin-left: 8px"
                @change="onJobFilterChange"
              >
                <el-option
                  v-for="s in jobStateOptions"
                  :key="s.value"
                  :label="s.label"
                  :value="s.value"
                />
              </el-select>
              <el-button
                style="margin-left: 8px"
                @click="resetJobFilter"
              >
                {{ $t('reachPipeline.reset') }}
              </el-button>
            </div>
            <el-button
              type="primary"
              @click="openEnqueueJob"
            >
              <el-icon><Plus /></el-icon>&nbsp;{{ $t('reachPipeline.addJob') }}
            </el-button>
          </div>

          <el-table
            v-loading="jobLoading"
            :data="filteredJobs"
            stripe
          >
            <el-table-column
              prop="id"
              label="ID"
              width="80"
            />
            <el-table-column
              label="Pipeline"
              width="160"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span>{{ pipelineName(row.pipeline_id) }}</span>
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.targetCustomer')"
              min-width="140"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ row.customer_id }}
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.channel')"
              width="110"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  effect="plain"
                >
                  {{ channelLabel(row.channel) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.status')"
              width="110"
            >
              <template #default="{ row }">
                <el-tag
                  :type="jobStateType(row.state)"
                  size="small"
                >
                  {{ jobStateText(row.state) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.retryCount')"
              width="100"
              align="center"
            >
              <template #default="{ row }">
                {{ row.retry_count || 0 }}/{{ row.max_retry || 0 }}
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.durationCol')"
              width="100"
              align="center"
            >
              <template #default="{ row }">
                {{ row.duration_ms ? row.duration_ms + 'ms' : '-' }}
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.createdAt')"
              width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.created_at) }}
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.operation')"
              width="260"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  @click="viewJob(row)"
                >
                  {{ $t('reachPipeline.view') }}
                </el-button>
                <el-button
                  v-if="canExecute(row.state)"
                  link
                  type="success"
                  @click="executeJob(row)"
                >
                  {{ $t('reachPipeline.execute') }}
                </el-button>
                <el-button
                  v-if="canCancel(row.state)"
                  link
                  type="warning"
                  @click="cancelJob(row)"
                >
                  {{ $t('reachPipeline.cancel') }}
                </el-button>
                <el-button
                  v-if="row.state === 'failed'"
                  link
                  type="primary"
                  @click="retryJob(row)"
                >
                  {{ $t('reachPipeline.retry') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="$t('reachPipeline.emptyJobs')" />
            </template>
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

      
      <el-tab-pane
        :label="$t('reachPipeline.tabStats')"
        name="stats"
      >
        <el-row
          :gutter="16"
          class="stats-row"
        >
          <el-col
            :xs="12"
            :sm="8"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-green">
                <el-icon><CircleCheck /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.successRate') }}
                </div>
                <div class="stat-value">
                  {{ overallSuccessRate }}%
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statSuccessOfTotal', { ok: stats.success || 0, total: stats.jobs || 0 }) }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="8"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-red">
                <el-icon><CircleClose /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.failRate') }}
                </div>
                <div class="stat-value">
                  {{ overallFailureRate }}%
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.statFailOfTotal', { fail: stats.failed || 0, total: stats.jobs || 0 }) }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="8"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-purple">
                <el-icon><Timer /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.avgExecTime') }}
                </div>
                <div class="stat-value">
                  {{ avgDuration }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.avgExecHint') }}
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col
            :xs="12"
            :sm="8"
            :md="6"
          >
            <el-card
              shadow="hover"
              class="stat-card"
            >
              <div class="stat-icon stat-icon-orange">
                <el-icon><Warning /></el-icon>
              </div>
              <div class="stat-body">
                <div class="stat-title">
                  {{ $t('reachPipeline.rateLimitedCount') }}
                </div>
                <div class="stat-value">
                  {{ stats.rate_limited || 0 }}
                </div>
                <div class="stat-sub">
                  {{ $t('reachPipeline.canceledSub', { count: stats.canceled || 0 }) }}
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('reachPipeline.channelStats') }}</span>
              <span class="muted">{{ $t('reachPipeline.channelStatsHint', { count: statsSample.length }) }}</span>
            </div>
          </template>
          <el-table
            v-loading="statsLoading"
            :data="channelStats"
            stripe
          >
            <el-table-column
              :label="$t('reachPipeline.channel')"
              width="140"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  effect="plain"
                >
                  {{ channelLabel(row.channel) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="total"
              :label="$t('reachPipeline.jobCount')"
              width="120"
              align="center"
            />
            <el-table-column
              prop="success"
              :label="$t('reachPipeline.success')"
              width="100"
              align="center"
            >
              <template #default="{ row }">
                <span style="color: #10B981">{{ row.success }}</span>
              </template>
            </el-table-column>
            <el-table-column
              prop="failed"
              :label="$t('reachPipeline.failed')"
              width="100"
              align="center"
            >
              <template #default="{ row }">
                <span style="color: #EF4444">{{ row.failed }}</span>
              </template>
            </el-table-column>
            <el-table-column
              prop="running"
              :label="$t('reachPipeline.running')"
              width="100"
              align="center"
            />
            <el-table-column
              prop="pending"
              :label="$t('reachPipeline.pending')"
              width="100"
              align="center"
            />
            <el-table-column
              :label="$t('reachPipeline.successRate')"
              width="120"
              align="center"
            >
              <template #default="{ row }">
                {{ row.successRate }}%
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.avgDuration')"
              width="140"
              align="center"
            >
              <template #default="{ row }">
                {{ row.avgDuration }}
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="$t('reachPipeline.emptyStats')" />
            </template>
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
      <el-form
        ref="pipeFormRef"
        :model="pipeForm"
        :rules="pipeRules"
        label-width="120px"
      >
        <el-form-item
          :label="$t('reachPipeline.name')"
          prop="name"
        >
          <el-input
            v-model="pipeForm.name"
            :placeholder="$t('reachPipeline.namePlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="$t('reachPipeline.description')">
          <el-input
            v-model="pipeForm.description"
            type="textarea"
            :rows="2"
            :placeholder="$t('reachPipeline.optional')"
          />
        </el-form-item>
        <el-form-item
          :label="$t('reachPipeline.channel')"
          prop="channel"
        >
          <ChannelSelect
            v-model="pipeForm.channel"
            style="width: 100%"
            :placeholder="$t('reachPipeline.channelPlaceholder')"
          />
        </el-form-item>
        <el-form-item
          :label="$t('reachPipeline.steps')"
          prop="steps"
        >
          <el-select
            v-model="pipeForm.steps"
            multiple
            style="width: 100%"
            :placeholder="$t('reachPipeline.stepsPlaceholder')"
          >
            <el-option
              v-for="s in stepOptions"
              :key="s.value"
              :label="s.label"
              :value="s.value"
            />
          </el-select>
        </el-form-item>

        <el-divider content-position="left">
          {{ $t('reachPipeline.retryPolicy') }}
        </el-divider>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item :label="$t('reachPipeline.maxRetries')">
              <el-input-number
                v-model="pipeForm.retry_policy.max_retries"
                :min="0"
                :max="20"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('reachPipeline.retryIntervalMs')">
              <el-input-number
                v-model="pipeForm.retry_policy.interval_ms"
                :min="0"
                :step="500"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('reachPipeline.backoff')">
              <el-select
                v-model="pipeForm.retry_policy.backoff"
                style="width: 100%"
              >
                <el-option
                  :label="$t('reachPipeline.backoffFixed')"
                  value="fixed"
                />
                <el-option
                  :label="$t('reachPipeline.backoffExponential')"
                  value="exponential"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('reachPipeline.backoffMaxMs')">
              <el-input-number
                v-model="pipeForm.retry_policy.max_interval_ms"
                :min="0"
                :step="1000"
              />
            </el-form-item>
          </el-col>
        </el-row>

        <el-divider content-position="left">
          {{ $t('reachPipeline.rateLimitConfig') }}
        </el-divider>
        <el-row :gutter="12">
          <el-col :span="8">
            <el-form-item label="QPS">
              <el-input-number
                v-model="pipeForm.rate_limit.qps"
                :min="0"
              />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="$t('reachPipeline.burst')">
              <el-input-number
                v-model="pipeForm.rate_limit.burst"
                :min="0"
              />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="$t('reachPipeline.dailyQuota')">
              <el-input-number
                v-model="pipeForm.rate_limit.daily_quota"
                :min="0"
                :step="100"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('reachPipeline.perUserLimit')">
              <el-input-number
                v-model="pipeForm.rate_limit.per_user_limit"
                :min="0"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('reachPipeline.cooldownSecs')">
              <el-input-number
                v-model="pipeForm.rate_limit.cooldown_secs"
                :min="0"
              />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="pipeDialogVisible = false">
          {{ $t('reachPipeline.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="pipeSubmitting"
          @click="submitPipe"
        >
          {{ $t('reachPipeline.confirm') }}
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="pipeDetailVisible"
      :title="$t('reachPipeline.pipelineDetail')"
      width="820px"
    >
      <el-descriptions
        v-if="currentPipe"
        :column="2"
        border
      >
        <el-descriptions-item label="ID">
          {{ currentPipe.id }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.name')">
          {{ currentPipe.name }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.channel')">
          {{ channelLabel(currentPipe.channel) }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.status')">
          <el-tag
            :type="pipeStatusType(currentPipe.status)"
            size="small"
          >
            {{ pipeStatusText(currentPipe.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.version')">
          {{ currentPipe.version }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.createdAt')">
          {{ formatTime(currentPipe.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item
          :label="$t('reachPipeline.description')"
          :span="2"
        >
          {{ currentPipe.description || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.runCount')">
          {{ currentPipe.total_runs || 0 }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.successTotal')">
          {{ currentPipe.total_success || 0 }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.failureTotal')">
          {{ currentPipe.total_failure || 0 }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.completionRate')">
          {{ pipeCompletionRate(currentPipe) }}%
        </el-descriptions-item>
        <el-descriptions-item
          :label="$t('reachPipeline.steps')"
          :span="2"
        >
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
            <span
              v-if="!pipeStepsList(currentPipe).length"
              class="muted"
            >-</span>
          </div>
        </el-descriptions-item>
        <el-descriptions-item
          :label="$t('reachPipeline.retryPolicy')"
          :span="2"
        >
          <pre class="json-pre">{{ formatJSON(currentPipe.retry_policy) }}</pre>
        </el-descriptions-item>
        <el-descriptions-item
          :label="$t('reachPipeline.rateLimitConfig')"
          :span="2"
        >
          <pre class="json-pre">{{ formatJSON(currentPipe.rate_limit) }}</pre>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>

    
    <el-dialog
      v-model="jobDialogVisible"
      :title="$t('reachPipeline.addJob')"
      width="640px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form
        ref="jobFormRef"
        :model="jobForm"
        :rules="jobRules"
        label-width="120px"
      >
        <el-form-item
          label="Pipeline"
          prop="pipeline_id"
        >
          <el-select
            v-model="jobForm.pipeline_id"
            :placeholder="$t('reachPipeline.selectActivePipeline')"
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
        <el-form-item :label="$t('reachPipeline.channel')">
          <ChannelSelect
            v-model="jobForm.channel"
            :placeholder="$t('reachPipeline.followPipeline')"
            clearable
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item
          :label="$t('reachPipeline.targetCustomerId')"
          prop="customer_id"
        >
          <el-input
            v-model="jobForm.customer_id"
            :placeholder="$t('reachPipeline.inputCustomerId')"
          />
        </el-form-item>
        <el-form-item :label="$t('reachPipeline.accountId')">
          <el-input
            v-model="jobForm.account_id"
            :placeholder="$t('reachPipeline.accountOptional')"
          />
        </el-form-item>
        <el-form-item :label="$t('reachPipeline.maxRetry')">
          <el-input-number
            v-model="jobForm.max_retry"
            :min="0"
            :max="20"
          />
          <span
            class="muted"
            style="margin-left: 8px"
          >{{ $t('reachPipeline.maxRetryHint') }}</span>
        </el-form-item>
        <el-form-item
          label="Payload"
          prop="payloadStr"
        >
          <el-input
            v-model="jobForm.payloadStr"
            type="textarea"
            :rows="4"
            :placeholder="$t('reachPipeline.payloadPlaceholder', { example: '{&quot;template&quot;:&quot;welcome&quot;}' })"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="jobDialogVisible = false">
          {{ $t('reachPipeline.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="jobSubmitting"
          @click="submitJob"
        >
          {{ $t('reachPipeline.enqueue') }}
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="jobDetailVisible"
      :title="$t('reachPipeline.jobDetail')"
      width="860px"
    >
      <el-descriptions
        v-if="currentJob"
        :column="2"
        border
      >
        <el-descriptions-item label="ID">
          {{ currentJob.id }}
        </el-descriptions-item>
        <el-descriptions-item label="Pipeline">
          {{ pipelineName(currentJob.pipeline_id) }} (#{{ currentJob.pipeline_id }})
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.channel')">
          {{ channelLabel(currentJob.channel) }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.status')">
          <el-tag
            :type="jobStateType(currentJob.state)"
            size="small"
          >
            {{ jobStateText(currentJob.state) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.targetCustomer')">
          {{ currentJob.customer_id }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.account')">
          {{ currentJob.account_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.retry')">
          {{ currentJob.retry_count || 0 }}/{{ currentJob.max_retry || 0 }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.durationCol')">
          {{ currentJob.duration_ms ? currentJob.duration_ms + 'ms' : '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.startedAt')">
          {{ formatTime(currentJob.started_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.completedAt')">
          {{ formatTime(currentJob.completed_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.nextRunAt')">
          {{ formatTime(currentJob.next_run_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('reachPipeline.createdAt')">
          {{ formatTime(currentJob.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item
          :label="$t('reachPipeline.errorMessage')"
          :span="2"
        >
          <span :style="{ color: currentJob.error_message ? '#EF4444' : '' }">{{ currentJob.error_message || '-' }}</span>
        </el-descriptions-item>
        <el-descriptions-item
          label="Payload"
          :span="2"
        >
          <pre class="json-pre">{{ formatJSON(currentJob.payload) }}</pre>
        </el-descriptions-item>
        <el-descriptions-item
          :label="$t('reachPipeline.execLog')"
          :span="2"
        >
          <el-table
            :data="jobStepResults(currentJob)"
            size="small"
            border
          >
            <el-table-column
              label="#"
              type="index"
              width="50"
              align="center"
            />
            <el-table-column
              :label="$t('reachPipeline.step')"
              width="150"
            >
              <template #default="{ row }">
                {{ stepLabel(row.step) }}
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.result')"
              width="90"
              align="center"
            >
              <template #default="{ row }">
                <el-tag
                  :type="row.success ? 'success' : 'danger'"
                  size="small"
                >
                  {{ row.success ? $t('reachPipeline.success') : $t('reachPipeline.failed') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.durationCol')"
              width="90"
              align="center"
            >
              <template #default="{ row }">
                {{ row.duration_ms || 0 }}ms
              </template>
            </el-table-column>
            <el-table-column
              :label="$t('reachPipeline.outputError')"
              min-width="220"
            >
              <template #default="{ row }">
                <span
                  v-if="row.error"
                  style="color: #EF4444"
                >{{ row.error }}</span>
                <pre
                  v-else
                  class="json-pre small"
                >{{ formatJSON(row.output) }}</pre>
              </template>
            </el-table-column>
          </el-table>
          <el-empty
            v-if="!jobStepResults(currentJob).length"
            :description="$t('reachPipeline.emptyStepResults')"
            :image-size="60"
          />
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
// 本视图分批替换为 $t()（词条 reachPipeline.*）。
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus, Connection, VideoPlay, VideoPause, FolderOpened, List,
  CircleCheck, CircleClose, Timer, Warning, Share
} from '@element-plus/icons-vue'
import { reachPipelineApi } from '@/api/reachPipeline.js'
import ChannelSelect from '@/components/ChannelSelect.vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const catalog = useChannelCatalogStore()
const channelLabel = (v) => catalog.channelLabel(v)

const { t } = useI18n()

const stepOptions = computed(() => [
  { value: 'audience', label: t('reachPipeline.stepAudience') },
  { value: 'content_prepare', label: t('reachPipeline.stepContentPrepare') },
  { value: 'account_select', label: t('reachPipeline.stepAccountSelect') },
  { value: 'rate_limit', label: t('reachPipeline.stepRateLimit') },
  { value: 'message_gen', label: t('reachPipeline.stepMessageGen') },
  { value: 'send', label: t('reachPipeline.stepSend') },
  { value: 'track_result', label: t('reachPipeline.stepTrackResult') },
  { value: 'retry', label: t('reachPipeline.stepRetry') },
  { value: 'report', label: t('reachPipeline.stepReport') }
])

const jobStateOptions = computed(() => [
  { value: 'pending', label: t('reachPipeline.statePending') },
  { value: 'running', label: t('reachPipeline.stateRunning') },
  { value: 'success', label: t('reachPipeline.success') },
  { value: 'failed', label: t('reachPipeline.failed') },
  { value: 'canceled', label: t('reachPipeline.stateCanceled') },
  { value: 'retrying', label: t('reachPipeline.stateRetrying') },
  { value: 'rate_limited', label: t('reachPipeline.stateRateLimited') }
])

const stepLabel = (v) => (stepOptions.value.find((s) => s.value === v) || {}).label || v
const pipeStatusType = (s) => ({ active: 'success', paused: 'warning', archived: 'info' }[s] || 'info')
const pipeStatusText = (s) => ({
  active: t('reachPipeline.active'),
  paused: t('reachPipeline.pausedAction'),
  archived: t('reachPipeline.archiveAction')
}[s] || s)
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
  pending: t('reachPipeline.statePending'),
  running: t('reachPipeline.stateRunning'),
  success: t('reachPipeline.success'),
  failed: t('reachPipeline.failed'),
  canceled: t('reachPipeline.stateCanceled'),
  retrying: t('reachPipeline.stateRetrying'),
  rate_limited: t('reachPipeline.stateRateLimited')
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
  } catch  {
    return String(obj)
  }
}

const stats = ref({});
const loadStats = async () => {
  try {
    const res = await reachPipelineApi.getStats()
    stats.value = res || {}
  } catch  {
    // 忽略：清理/存储/恢复类 best-effort 操作
  }
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
    ElMessage.error(e.message || t('reachPipeline.loadPipelinesFailed'))
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
const pipeDialogTitle = ref('')
const pipeFormRef = ref()
const pipeSubmitting = ref(false)

const defaultPipeForm = () => ({
  name: '',
  description: '',
  channel: 'wecom',
  steps: stepOptions.value.map((s) => s.value),
  retry_policy: { max_retries: 3, interval_ms: 1000, backoff: 'exponential', max_interval_ms: 60000 },
  rate_limit: { qps: 10, burst: 20, daily_quota: 10000, per_user_limit: 3, cooldown_secs: 60 }
})
const pipeForm = reactive(defaultPipeForm())
const pipeRules = {
  name: [{ required: true, message: t('reachPipeline.nameRequired'), trigger: 'blur' }],
  channel: [{ required: true, message: t('reachPipeline.channelPlaceholder'), trigger: 'change' }],
  steps: [{ required: true, type: 'array', min: 1, message: t('reachPipeline.stepRequired'), trigger: 'change' }]
}

const openCreatePipe = () => {
  Object.assign(pipeForm, defaultPipeForm())
  pipeDialogTitle.value = t('reachPipeline.newPipeline')
  pipeDialogVisible.value = true
}
const submitPipe = async () => {
  if (!pipeFormRef.value) return
  await pipeFormRef.value.validate(async (valid) => {
    if (!valid) return
    if (!pipeForm.steps.includes('send')) {
      ElMessage.warning(t('reachPipeline.stepMustIncludeSend'))
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
      ElMessage.success(t('reachPipeline.createSuccess'))
      pipeDialogVisible.value = false
      loadPipelines()
      loadStats()
    } catch (e) {
      ElMessage.error(e.message || t('reachPipeline.createFailed'))
    } finally {
      pipeSubmitting.value = false
    }
  })
}

const pausePipe = async (row) => {
  try {
    await reachPipelineApi.pausePipeline(row.id)
    ElMessage.success(t('reachPipeline.paused'))
    loadPipelines()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || t('reachPipeline.operationFailed'))
  }
}
const resumePipe = async (row) => {
  try {
    await reachPipelineApi.resumePipeline(row.id)
    ElMessage.success(t('reachPipeline.resumeSuccess'))
    loadPipelines()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || t('reachPipeline.operationFailed'))
  }
}
const archivePipe = async (row) => {
  try {
    await ElMessageBox.confirm(
      t('reachPipeline.confirmArchive', { name: row.name }),
      t('reachPipeline.confirmTitle'),
      { type: 'warning' }
    )
    await reachPipelineApi.archivePipeline(row.id)
    ElMessage.success(t('reachPipeline.archived'))
    loadPipelines()
    loadStats()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || t('reachPipeline.operationFailed'))
  }
}
const deletePipe = async (row) => {
  try {
    await ElMessageBox.confirm(
      t('reachPipeline.confirmDelete', { name: row.name }),
      t('reachPipeline.confirmTitle'),
      { type: 'warning' }
    )
    await reachPipelineApi.deletePipeline(row.id)
    ElMessage.success(t('reachPipeline.deleteSuccess'))
    loadPipelines()
    loadStats()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || t('reachPipeline.operationFailed'))
  }
}

const resetRateLimit = async () => {
  if (!pipeFilter.channel) {
    ElMessage.warning(t('reachPipeline.resetRateLimitNeedChannel'))
    return
  }
  try {
    await ElMessageBox.confirm(
      t('reachPipeline.confirmResetRateLimit', { name: channelLabel(pipeFilter.channel) }),
      t('reachPipeline.confirmTitle'),
      { type: 'warning' }
    )
    await reachPipelineApi.resetRateLimit(pipeFilter.channel)
    ElMessage.success(t('reachPipeline.resetRateLimitDone'))
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || t('reachPipeline.operationFailed'))
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
    ElMessage.error(e.message || t('reachPipeline.detailLoadFailed'))
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
    ElMessage.error(e.message || t('reachPipeline.loadJobsFailed'))
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
    ElMessage.success(t('reachPipeline.executeDone'))
    loadJobs()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || t('reachPipeline.executeFailed'))
    loadJobs()
  }
}
const cancelJob = async (row) => {
  try {
    await ElMessageBox.confirm(
      t('reachPipeline.confirmCancelJob', { id: row.id }),
      t('reachPipeline.confirmTitle'),
      { type: 'warning' }
    )
    await reachPipelineApi.cancelJob(row.id)
    ElMessage.success(t('reachPipeline.canceledOk'))
    loadJobs()
    loadStats()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.message || t('reachPipeline.operationFailed'))
  }
}
const retryJob = async (row) => {
  try {
    await reachPipelineApi.retryJob(row.id)
    ElMessage.success(t('reachPipeline.requeued'))
    loadJobs()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message || t('reachPipeline.operationFailed'))
  }
}

const jobDialogVisible = ref(false);
const jobFormRef = ref()
const jobSubmitting = ref(false)
const jobForm = reactive({ pipeline_id: null, channel: '', customer_id: '', account_id: '', max_retry: 0, payloadStr: '{}' })
const jobRules = {
  pipeline_id: [{ required: true, message: t('reachPipeline.pipelineRequired'), trigger: 'change' }],
  customer_id: [{ required: true, message: t('reachPipeline.customerIdRequired'), trigger: 'blur' }],
  payloadStr: [{ required: true, message: t('reachPipeline.payloadRequired'), trigger: 'blur' }]
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
    } catch  {
      ElMessage.error(t('reachPipeline.payloadInvalid'))
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
      ElMessage.success(t('reachPipeline.enqueued'))
      jobDialogVisible.value = false
      activeTab.value = 'jobs'
      jobPager.page = 1
      loadJobs()
      loadStats()
    } catch (e) {
      ElMessage.error(e.message || t('reachPipeline.enqueueFailed'))
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
    ElMessage.error(e.message || t('reachPipeline.detailLoadFailed'))
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
    ElMessage.error(e.message || t('reachPipeline.loadStatsFailed'))
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
  catalog.ensure()
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
