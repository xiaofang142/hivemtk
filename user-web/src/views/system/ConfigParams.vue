<template>
  <div class="config-params-page">
    <el-row
      :gutter="16"
      class="stat-row"
    >
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          shadow="hover"
          class="stat-card"
        >
          <div class="stat-inner">
            <div class="stat-icon stat-icon--total">
              <el-icon :size="24">
                <Setting />
              </el-icon>
            </div>
            <div class="stat-body">
              <div class="stat-value">
                {{ stats.total }}
              </div>
              <div class="stat-label">
                总参数
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          shadow="hover"
          class="stat-card"
        >
          <div class="stat-inner">
            <div class="stat-icon stat-icon--group">
              <el-icon :size="24">
                <Files />
              </el-icon>
            </div>
            <div class="stat-body">
              <div class="stat-value">
                {{ stats.groupCount }}
              </div>
              <div class="stat-label">
                分组数
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          shadow="hover"
          class="stat-card"
        >
          <div class="stat-inner">
            <div class="stat-icon stat-icon--modified">
              <el-icon :size="24">
                <Warning />
              </el-icon>
            </div>
            <div class="stat-body">
              <div class="stat-value">
                {{ stats.modified }}
              </div>
              <div class="stat-label">
                已修改
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          shadow="hover"
          class="stat-card"
          :class="{ 'stat-card--danger': stats.danger > 0 }"
        >
          <div class="stat-inner">
            <div class="stat-icon stat-icon--danger">
              <el-icon :size="24">
                <WarningFilled />
              </el-icon>
            </div>
            <div class="stat-body">
              <div class="stat-value">
                {{ stats.danger }}
              </div>
              <div class="stat-label">
                高危参数
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>


    <el-row
      :gutter="16"
      class="main-row"
    >
      <el-col
        :xs="24"
        :md="5"
        :lg="4"
      >
        <el-card
          shadow="hover"
          class="group-card"
        >
          <template #header>
            <div class="card-header">
              <span>参数分组</span>
              <el-button
                text
                size="small"
                @click="loadAll"
              >
                <el-icon><Refresh /></el-icon>
              </el-button>
            </div>
          </template>
          <div class="group-list">
            <div
              v-for="g in groups"
              :key="g.key"
              class="group-item"
              role="button"
              tabindex="0"
              :class="{ active: currentGroup === g.key }"
              @click="switchGroup(g.key)"
              @keydown.enter.prevent="switchGroup(g.key)"
              @keydown.space.prevent="switchGroup(g.key)"
            >
              <div class="group-item__main">
                <el-icon class="group-icon">
                  <Collection />
                </el-icon>
                <div class="group-name">
                  {{ g.label }}
                </div>
              </div>
              <div class="group-item__count">
                <span class="group-count">{{ g.count }}</span>
                <span
                  v-if="getGroupModified(g.key) > 0"
                  class="group-mod"
                >
                  {{ getGroupModified(g.key) }}
                </span>
              </div>
            </div>
          </div>
          <div
            v-if="groups.length === 0"
            class="group-empty"
          >
            后端未返回任何分组元数据
          </div>
        </el-card>
      </el-col>


      <el-col
        :xs="24"
        :md="19"
        :lg="20"
      >
        <el-card
          shadow="hover"
          class="table-card"
        >
          <template #header>
            <div class="card-header">
              <div class="header-title">
                <el-tag
                  type="info"
                  effect="plain"
                  class="group-tag"
                >
                  {{ currentGroupLabel }}
                </el-tag>
                <span class="header-count">共 {{ filteredList.length }} 条</span>
                <el-tooltip
                  v-if="currentGroupDesc"
                  :content="currentGroupDesc"
                  placement="top"
                >
                  <el-icon class="header-help">
                    <Document />
                  </el-icon>
                </el-tooltip>
              </div>
              <div class="header-actions">
                <el-input
                  v-model="keyword"
                  placeholder="搜索 key / 名称 / 描述"
                  clearable
                  size="default"
                  style="width: 240px"
                >
                  <template #prefix>
                    <el-icon><Search /></el-icon>
                  </template>
                </el-input>
                <el-radio-group
                  v-model="searchScope"
                  size="default"
                >
                  <el-radio-button value="group">
                    本组
                  </el-radio-button>
                  <el-radio-button value="all">
                    全部组
                  </el-radio-button>
                </el-radio-group>
                <el-tooltip
                  content="默认不列出已下架参数：那些旋钮在代码里没有消费点，改了不生效"
                  placement="top"
                >
                  <el-switch
                    v-model="showRetired"
                    active-text="显示已下架"
                  />
                </el-tooltip>
                <el-button
                  :disabled="groupModifiedCount === 0"
                  :loading="resettingGroup"
                  @click="handleBulkReset"
                >
                  <el-icon><RefreshLeft /></el-icon>
                  重置整组
                  <span
                    v-if="groupModifiedCount > 0"
                    class="reset-badge"
                  >{{ groupModifiedCount }}</span>
                </el-button>
                <el-button @click="openAuditDrawer">
                  <el-icon><Document /></el-icon>
                  审计日志
                </el-button>
              </div>
            </div>
          </template>

          <el-alert
            v-if="isGlobalSearch"
            type="info"
            :closable="false"
            show-icon
            class="scope-hint"
            :title="`正在跨全部 ${groups.length} 个分组搜索「${keyword.trim()}」，命中 ${filteredList.length} 条`"
          />

          <!-- 平台端地址专用卡片：只在 platform 分组显示。
               为什么不用下面表格里的通用编辑器：那条路在 SaaS 模式下会落进
               租户覆盖行（后端 UpdateValue 见到租户 ctx 就写 tenant_config_params），
               而平台地址是部署级事实 —— 覆盖行对它不生效，页面会回「保存成功」
               而地址没变。专用端点直接写平台默认行。 -->
          <div
            v-if="showPlatformHostCard"
            class="platform-host-card"
          >
            <el-alert
              type="warning"
              :closable="false"
              show-icon
              class="platform-host-hint"
              title="平台端地址是部署级参数：修改后影响本实例的全部租户平台能力（商户注册/心跳/资产市场/模块订阅/订单/LLM 钱包/站内信）。"
            />
            <el-alert
              v-if="platformHostForbidden"
              type="info"
              :closable="false"
              show-icon
              class="platform-host-hint"
              title="当前账号不是平台运营方，只能查看平台端地址；连通性测试与保存需使用平台运营方账号。"
            />
            <div class="platform-host-row">
              <el-input
                v-model="platformHostDraft"
                placeholder="https://platform.example.com"
                clearable
                :disabled="platformHostForbidden"
                class="platform-host-input"
              >
                <template #prefix>
                  <el-icon><Link /></el-icon>
                </template>
              </el-input>
              <el-button
                :loading="platformHostProbing"
                :disabled="platformHostForbidden"
                @click="probePlatformHost"
              >
                <el-icon><Connection /></el-icon>
                测试连通性
              </el-button>
              <el-button
                type="primary"
                :loading="platformHostSaving"
                :disabled="!platformHostDirty || platformHostForbidden"
                @click="savePlatformHost"
              >
                保存
              </el-button>
            </div>
            <div
              v-if="platformHostProbe"
              class="platform-host-probe"
              :class="`platform-host-probe--${platformHostProbe.status}`"
            >
              <el-tag
                :type="platformHostProbeTagType"
                size="small"
                effect="dark"
              >
                {{ platformHostProbeLabel }}
              </el-tag>
              <span class="platform-host-probe-msg">{{ platformHostProbe.message }}</span>
            </div>
          </div>

          <el-table
            v-loading="loading"
            :data="filteredList"
            stripe
            border
            empty-text="没有匹配的参数"
            style="width: 100%"
            :header-cell-style="{ background: '#fafafa' }"
            :row-class-name="rowClassName"
          >
            <el-table-column
              v-if="isGlobalSearch"
              prop="group"
              label="分组"
              width="120"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  effect="plain"
                  type="info"
                >
                  {{ groupLabel(row.group) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="key"
              label="Key"
              width="210"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <code class="param-key">{{ row.key }}</code>
              </template>
            </el-table-column>
            <el-table-column
              prop="name"
              label="名称"
              min-width="150"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <div class="name-cell">
                  <span class="name-text">{{ stripSuffix(row.name) }}</span>
                  <el-tooltip
                    v-if="row.deprecated"
                    :content="retiredTip(row)"
                    placement="top"
                  >
                    <el-tag
                      type="danger"
                      size="small"
                      effect="dark"
                    >
                      已下架
                    </el-tag>
                  </el-tooltip>
                  <el-tooltip
                    v-else-if="row.risk === 'danger'"
                    content="高危：填错可能直接让功能不可用，保存前会二次确认"
                    placement="top"
                  >
                    <el-tag
                      type="danger"
                      size="small"
                      effect="plain"
                    >
                      高危
                    </el-tag>
                  </el-tooltip>
                  <el-tooltip
                    v-else-if="row.risk === 'warning'"
                    content="敏感：填错不报错，但会静默改变业务行为"
                    placement="top"
                  >
                    <el-tag
                      type="warning"
                      size="small"
                      effect="plain"
                    >
                      敏感
                    </el-tag>
                  </el-tooltip>
                  <el-tooltip
                    v-if="row.restart"
                    content="装配期读取：改完需要重启服务才对已建立的连接/账号生效"
                    placement="top"
                  >
                    <el-tag
                      type="warning"
                      size="small"
                      effect="plain"
                    >
                      需重启
                    </el-tag>
                  </el-tooltip>
                  <el-tag
                    v-if="row.read_only"
                    type="info"
                    size="small"
                    effect="plain"
                  >
                    只读
                  </el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column
              label="描述"
              min-width="200"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span v-if="!row.deprecated">{{ row.description || '-' }}</span>
                <span
                  v-else
                  class="retired-reason"
                >{{ row.deprecated_reason || '未记录下架原因' }}</span>
              </template>
            </el-table-column>
            <el-table-column
              label="类型"
              width="86"
              align="center"
            >
              <template #default="{ row }">
                <el-tag
                  :type="typeTagColor(row.value_type)"
                  size="small"
                >
                  {{ row.value_type }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="当前值"
              min-width="170"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <div class="val-cell">
                  <span
                    class="val-current"
                    :class="{ 'val-changed': !isDefault(row) }"
                  >
                    {{ formatValue(row, row.value) }}
                  </span>
                  <span
                    v-if="unitSuffix(row)"
                    class="unit-inline"
                  >{{ unitSuffix(row) }}</span>
                  <el-tag
                    v-if="!isDefault(row)"
                    type="warning"
                    size="small"
                    effect="plain"
                    class="changed-badge"
                  >
                    已修改
                  </el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column
              label="默认值"
              min-width="130"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span class="val-default">{{ formatValue(row, row.default_value) }}</span>
              </template>
            </el-table-column>
            <el-table-column
              label="范围"
              width="150"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span
                  v-if="hasRange(row)"
                  class="range-cell"
                >{{ rangeText(row) }}</span>
                <span
                  v-else
                  class="muted"
                >-</span>
              </template>
            </el-table-column>
            <el-table-column
              label="操作"
              width="150"
              fixed="right"
              align="center"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  size="small"
                  :disabled="row.read_only || row.deprecated"
                  @click="openEdit(row)"
                >
                  <el-icon><Edit /></el-icon>编辑
                </el-button>
                <!-- 只读项服务端对 ResetToDefault 同样回 ErrParamReadOnly（见 repository.ErrParamReadOnly），
                     编辑按钮与对话框里的「恢复默认值」早就禁了，这一列是最后一处漏网的入口。 -->
                <el-button
                  link
                  type="warning"
                  size="small"
                  :disabled="isDefault(row) || row.deprecated || row.read_only"
                  :loading="row._resetting"
                  @click="handleResetOne(row)"
                >
                  <el-icon><RefreshLeft /></el-icon>重置
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>


    <el-dialog
      v-model="editDialogVisible"
      :title="`编辑参数 · ${editing?.key ?? ''}`"
      width="560px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <div
        v-if="editing"
        class="edit-body"
      >
        <el-alert
          v-if="editing.deprecated"
          type="error"
          show-icon
          :closable="false"
          :title="editing.deprecated_reason || '该参数已下架'"
        />
        <el-alert
          v-else-if="editing.restart"
          type="warning"
          show-icon
          :closable="false"
          title="该参数在服务装配期读取，保存后需重启服务，才会对已建立的连接 / 已加载的账号生效。"
        />
        <el-alert
          v-else-if="editing.risk === 'danger'"
          type="error"
          show-icon
          :closable="false"
          title="高危参数：填错可能让功能直接不可用（例如锁 TTL 填 0 会变成永不过期）。保存时会再确认一次。"
        />
        <el-alert
          v-else-if="editing.risk === 'warning'"
          type="warning"
          show-icon
          :closable="false"
          title="敏感参数：填错不会报错，但会静默改变业务行为。"
        />

        <el-descriptions
          :column="2"
          border
          size="small"
          class="edit-desc"
        >
          <el-descriptions-item label="分组">
            {{ groupLabel(editing.group) }}
          </el-descriptions-item>
          <el-descriptions-item label="类型">
            <el-tag
              :type="typeTagColor(editing.value_type)"
              size="small"
            >
              {{ editing.value_type }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item
            label="名称"
            :span="2"
          >
            {{ stripSuffix(editing.name) }}
          </el-descriptions-item>
          <el-descriptions-item
            label="描述"
            :span="2"
          >
            {{ editing.description || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="默认值">
            {{ formatValue(editing, editing.default_value) }}
          </el-descriptions-item>
          <el-descriptions-item label="只读">
            <el-tag
              :type="editing.read_only ? 'info' : 'success'"
              size="small"
            >
              {{ editing.read_only ? '是' : '否' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item
            v-if="hasRange(editing)"
            label="可填范围"
            :span="2"
          >
            {{ rangeText(editing) }}
          </el-descriptions-item>
        </el-descriptions>

        <div class="edit-field">
          <div class="edit-field__label">
            新值
            <span
              v-if="isDefault(editing)"
              class="muted"
            >（与默认值一致）</span>
          </div>

          <template v-if="isBoolType(editing.value_type)">
            <el-switch
              v-model="editValueBool"
              :active-value="true"
              :inactive-value="false"
            />
            <span class="muted switch-hint">{{ editValueBool ? 'true' : 'false' }}</span>
          </template>

          <template v-else-if="isNumericType(editing.value_type)">
            <div
              v-if="hasSlider(editing)"
              class="slider-wrap"
            >
              <el-slider
                v-model="editValueNum"
                :min="numBound(editing.min) ?? 0"
                :max="numBound(editing.max) ?? 100"
                :step="numBound(editing.step) ?? defaultStep(editing)"
                :show-tooltip="false"
                style="flex:1; margin-right:12px"
              />
              <el-input-number
                v-model="editValueNum"
                :min="numBound(editing.min)"
                :max="numBound(editing.max)"
                :step="numBound(editing.step) ?? defaultStep(editing)"
                :precision="precisionOf(editing)"
                controls-position="right"
                size="default"
                style="width:140px"
              />
              <span
                v-if="unitSuffix(editing)"
                class="unit-label"
              >{{ unitSuffix(editing) }}</span>
            </div>
            <el-input-number
              v-else
              v-model="editValueNum"
              :min="numBound(editing.min)"
              :max="numBound(editing.max)"
              :step="numBound(editing.step) ?? defaultStep(editing)"
              :precision="precisionOf(editing)"
              controls-position="right"
              size="default"
              style="width:100%"
            />
            <div
              v-if="editing.value_type === 'duration'"
              class="edit-hint"
            >
              单位是秒。填 {{ editValueNum }} 秒 ≈ <strong>{{ formatDuration(editValueNum) }}</strong>
            </div>
            <div
              v-else-if="unitSuffix(editing)"
              class="edit-hint"
            >
              单位：{{ unitSuffix(editing) }}
            </div>
          </template>

          <template v-else>
            <el-input
              v-model="editValueStr"
              :type="isTextareaType(editing) ? 'textarea' : 'text'"
              :rows="isTextareaType(editing) ? 6 : 3"
              :placeholder="`默认值：${editing.default_value ?? ''}`"
            />
            <div
              v-if="looksLikeJson(editing)"
              class="json-tools"
            >
              <el-button
                size="small"
                @click="formatEditJson"
              >
                <el-icon><Document /></el-icon>格式化 JSON
              </el-button>
              <span class="muted">默认值是一段 JSON，保存前会校验语法</span>
            </div>
          </template>
        </div>

        <div
          v-if="editError"
          class="edit-error"
        >
          <el-icon><WarningFilled /></el-icon> {{ editError }}
        </div>
      </div>

      <template #footer>
        <el-button @click="editDialogVisible = false">
          取消
        </el-button>
        <el-button
          v-if="editing && !isDefault(editing) && !editing.read_only && !editing.deprecated"
          :loading="resettingOne"
          @click="resetInDialog"
        >
          <el-icon><RefreshLeft /></el-icon>恢复默认值
        </el-button>
        <el-button
          :type="editing?.risk === 'danger' ? 'danger' : 'primary'"
          :disabled="editValueUnchanged || !!editError || !editing || editing.deprecated || editing.read_only"
          :loading="saving"
          @click="confirmEdit"
        >
          保存修改
        </el-button>
      </template>
    </el-dialog>


    <el-drawer
      v-model="auditDrawerVisible"
      title="变更审计日志"
      size="640px"
    >
      <template v-if="auditLoading">
        <div class="drawer-loading">
          <el-icon
            class="is-loading"
            :size="28"
          >
            <Loading />
          </el-icon>
        </div>
      </template>
      <template v-else>
        <div
          v-if="auditList.length === 0"
          class="drawer-empty"
        >
          暂无变更记录
        </div>
        <el-timeline
          v-else
          class="audit-timeline"
        >
          <el-timeline-item
            v-for="(item, idx) in auditList"
            :key="idx"
            :timestamp="formatTs(item.updated_at || item.created_at)"
            placement="top"
          >
            <div class="audit-item">
              <div class="audit-item__head">
                <el-tag
                  size="small"
                  effect="plain"
                >
                  {{ item.param_group || '-' }}
                </el-tag>
                <code class="audit-key">{{ item.param_key }}</code>
                <span class="audit-user">{{ item.actor_id ? `用户 #${item.actor_id}` : '-' }}</span>
              </div>
              <div class="audit-item__diff">
                <span class="val-default">{{ String(item.old_value ?? '') || '(空)' }}</span>
                <el-icon class="audit-arrow">
                  <ArrowRight />
                </el-icon>
                <span class="val-current val-changed">{{ String(item.new_value ?? '') || '(空)' }}</span>
              </div>
            </div>
          </el-timeline-item>
        </el-timeline>
      </template>
      <template #footer>
        <el-button
          :loading="auditLoading"
          @click="loadAuditLogs"
        >
          <el-icon><Refresh /></el-icon>刷新
        </el-button>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as configParamsApi from '@/api/configParams'

const allParams = ref([])

// ── 平台端地址（platform.api_host）专用编辑区 ──
//
// 这条参数与其它参数不同：它是**部署级**事实（这台部署连哪个平台），
// 且在 SaaS 模式下通用编辑器的写入口会落进租户覆盖行 —— 对它无效。
// 所以给它一条独立的读写通道 + 一个连通性探针。
const platformHostGroup = 'platform'
const platformHostKey = 'api_host'
const platformHostDraft = ref('')
const platformHostSaving = ref(false)
const platformHostProbing = ref(false)
const platformHostProbe = ref(null)
// 403 不是"平台端不可达"：地址没填错、网络也没问题，是当前租户管理员
// 没有平台运营方权限。混成 unavailable 会让人去查地址/防火墙，白查一轮。
const platformHostForbidden = ref(false)

// 通用编辑器里那条 platform.api_host 的当前值（用于判脏与初始化草稿）
const platformHostCurrent = computed(() => {
  const p = allParams.value.find(
    (x) => x && x.group === platformHostGroup && x.key === platformHostKey
  )
  return p ? String(p.value || '') : ''
})

const showPlatformHostCard = computed(() => currentGroup.value === platformHostGroup)

const platformHostDirty = computed(
  () => platformHostDraft.value.trim().replace(/\/+$/, '') !== platformHostCurrent.value.trim().replace(/\/+$/, '')
)

const platformHostProbeLabel = computed(() => {
  const s = platformHostProbe.value && platformHostProbe.value.status
  if (s === 'ok') return '可达'
  if (s === 'unreachable') return '不可达'
  if (s === 'forbidden') return '无权限'
  return '未配置'
})

const platformHostProbeTagType = computed(() => {
  const s = platformHostProbe.value && platformHostProbe.value.status
  if (s === 'ok') return 'success'
  if (s === 'unreachable') return 'danger'
  if (s === 'forbidden') return 'warning'
  return 'info'
})

function isForbiddenError(e) {
  return Number((e && (e.status || (e.response && e.response.status))) || 0) === 403
}

async function probePlatformHost() {
  platformHostProbing.value = true
  platformHostProbe.value = null
  try {
    const res = await configParamsApi.testPlatformConnectivity(platformHostDraft.value.trim())
    const data = res && res.data ? res.data : null
    platformHostProbe.value = data || { status: 'not_configured', message: res && res.message ? res.message : '无响应' }
  } catch (e) {
    if (isForbiddenError(e)) {
      platformHostForbidden.value = true
      platformHostProbe.value = { status: 'forbidden', message: (e && e.message) || '仅平台运营方可执行该操作' }
    } else {
      platformHostProbe.value = { status: 'unreachable', message: (e && e.message) || '探测请求失败' }
    }
  } finally {
    platformHostProbing.value = false
  }
}

async function savePlatformHost() {
  const address = platformHostDraft.value.trim().replace(/\/+$/, '')
  if (!address) {
    ElMessage.warning('平台地址不能为空')
    return
  }
  // 二次确认：这一改影响全实例的平台能力，且 60s 缓存过期后才生效，
  // 改错了不是立刻能看出来的。
  try {
    await ElMessageBox.confirm(
      `确认把平台端地址改成 ${address} 吗？\n\n这会影响本实例全部租户的平台能力（资产市场/模块订阅/订单/LLM 钱包/站内信）。改完约 60 秒内生效。`,
      '确认修改平台端地址',
      { type: 'warning', confirmButtonText: '确认修改', cancelButtonText: '取消' }
    )
  } catch  {
    return
  }
  platformHostSaving.value = true
  try {
    await configParamsApi.setPlatformHost(address)
    ElMessage.success('平台端地址已更新，约 60 秒内生效（无需重启）')
    platformHostProbe.value = null
    await loadAll()
  } catch (e) {
    if (isForbiddenError(e)) {
      platformHostForbidden.value = true
      ElMessage.warning((e && e.message) || '仅平台运营方可修改平台端地址')
    } else {
      ElMessage.error((e && e.message) || '保存失败')
    }
  } finally {
    platformHostSaving.value = false
  }
}

// 分组元数据（中文名 / 说明 / 排序）来自后端 config_param_groups——
// 事实源只保留一份（前端写死 GROUP_LABELS 表会导致后端加新组后静默回落英文 key），
// 这里只负责渲染；后端没给元数据时回落英文 key 而不是崩溃。
const groupMeta = ref([])
const groupMetaMap = computed(() => {
  const m = new Map()
  for (const g of groupMeta.value) {
    if (g && g.group) m.set(g.group, g)
  }
  return m
})

function groupLabel(key) {
  const g = groupMetaMap.value.get(key)
  return g && g.label ? g.label : (key || '-')
}

function groupDesc(key) {
  const g = groupMetaMap.value.get(key)
  return g && g.description ? g.description : ''
}

const groups = computed(() => {
  // 计数一律从 allParams 推导，口径必须和 filteredList 的分组过滤完全一致，
  // 否则侧栏数字与表头「共 N 条」又会各说各话（历史上就这么打脸过）。
  const counts = new Map()
  for (const p of visibleParams.value) {
    if (!p.group) continue
    counts.set(p.group, (counts.get(p.group) || 0) + 1)
  }
  // 元数据里的组也一并列出：某组参数全部处于「已下架」且当前未勾选显示时，
  // 它的计数是 0，但从侧栏进得去、看得见有这一组，而不是凭空消失。
  for (const g of groupMeta.value) {
    if (g && g.group && !counts.has(g.group)) counts.set(g.group, 0)
  }
  const meta = groupMetaMap.value
  return [...counts.entries()]
    .map(([key, count]) => ({ key, count, label: groupLabel(key), meta: meta.get(key) }))
    .sort((a, b) => {
      const sa = a.meta ? Number(a.meta.sort_order || 0) : 0
      const sb = b.meta ? Number(b.meta.sort_order || 0) : 0
      // sort_order=0 是「没排过序」的哨兵（后端 ORDER BY 已把它排到最后），
      // 前端再排一次是为了在元数据缺失时也能有个稳定顺序。
      if (sa !== sb) {
        if (sa === 0) return 1
        if (sb === 0) return -1
        return sa - sb
      }
      return a.key.localeCompare(b.key)
    })
})

const currentGroup = ref('')
const loading = ref(false)
const saving = ref(false)
const resettingOne = ref(false)
const resettingGroup = ref(false)
const keyword = ref('')
const searchScope = ref('all')
const showRetired = ref(false)

// visibleParams 是真正参与渲染的集合。后端 list(include_deprecated=0) 本来就过滤了
// 已下架项，这里再滤一次是**有意的冗余**：这一页唯一不能出的错就是「页面上挂着一个
// 改了不生效的旋钮」。后端过滤一旦回归（老后端配新前端、或某次改动漏传参数），
// 用户看到的仍然是一个能编辑、能保存、但行为不变的行——而且审计里还留下一条 update，
// 排查时最难的那种。后端过滤是性能与职责边界，前端过滤是最后一道防线。
const visibleParams = computed(() => {
  if (showRetired.value) return allParams.value
  return allParams.value.filter((p) => !p.deprecated)
})

const isGlobalSearch = computed(() => keyword.value.trim() !== '' && searchScope.value === 'all')

const stats = computed(() => {
  const list = visibleParams.value
  return {
    total: list.length,
    groupCount: groups.value.length,
    modified: list.filter((p) => !isDefault(p)).length,
    danger: list.filter((p) => p.risk === 'danger' && !p.deprecated).length
  }
})

const currentGroupLabel = computed(() => {
  const g = groups.value.find((x) => x.key === currentGroup.value)
  return g ? `${g.label} / ${g.key}` : (currentGroup.value || '全部')
})

const currentGroupDesc = computed(() => groupDesc(currentGroup.value))

// 草稿随分组切换同步：进 platform 组时把当前生效值灌进草稿框。
//
// 位置说明：这段 watch 依赖 currentGroup，而 currentGroup 在下面才声明 ——
// 放在声明之前会踩 **暂时性死区**（watch 的 getter 在 { immediate: true } 下
// 立即执行 ⇒ 读到一个还没初始化的 const ⇒ ReferenceError:
// "Cannot access 'currentGroup' before initialization" ⇒ 整页白屏）。
// 所以它必须待在 currentGroup 声明之后。
watch(
  () => currentGroup.value,
  (g) => {
    if (g === platformHostGroup) {
      platformHostDraft.value = platformHostCurrent.value
      platformHostProbe.value = null
      platformHostForbidden.value = false
    }
  },
  { immediate: true }
)

const filteredList = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  // 没有搜索词时永远只看当前组：左侧分组栏是这页的主导航，
  // 一进页面就把 217 条全铺在表里会让人不知道该从哪看起。
  // 一旦输入搜索词，才按 searchScope 决定搜本组还是搜全部组。
  if (!kw) return visibleParams.value.filter((p) => p.group === currentGroup.value)
  const base = searchScope.value === 'group'
    ? visibleParams.value.filter((p) => p.group === currentGroup.value)
    : visibleParams.value
  return base.filter((p) => {
    const hay = `${p.group} ${p.key} ${p.name} ${p.description || ''} ${p.deprecated_reason || ''}`.toLowerCase()
    return hay.includes(kw)
  })
})

const groupModifiedCount = computed(() => {
  // 只读项不进计数：后端 BulkResetGroup 对 read_only 和 deprecated 都是「跳过」而不是
  // 「整组失败」，前端把它们算进去就是骗用户「重置了 2 条」而实际只改了 1 条，
  // 且那条只读项压根改不动（knowledge.embedding_dimension 是唯一一条）。
  return filteredList.value.filter((p) => !p.deprecated && !p.read_only && !isDefault(p)).length
})

async function loadAll() {
  loading.value = true
  try {
    const [paramsRes, groupsRes] = await Promise.all([
      configParamsApi.list(undefined, showRetired.value),
      configParamsApi.listGroups()
    ])
    const list = Array.isArray(paramsRes) ? paramsRes : (paramsRes && Array.isArray(paramsRes.list) ? paramsRes.list : [])
    allParams.value = list
    const meta = Array.isArray(groupsRes) ? groupsRes : (groupsRes && Array.isArray(groupsRes.list) ? groupsRes.list : [])
    groupMeta.value = meta
    // 默认选中的组必须在真实返回的分组里，否则进页面就是一张没有任何解释的空表。
    if (!groups.value.some((g) => g.key === currentGroup.value)) {
      currentGroup.value = groups.value.length ? groups.value[0].key : ''
    }
  } catch (e) {
    ElMessage.error('加载参数失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

function switchGroup(key) {
  currentGroup.value = key
}

// 已下架的旋钮不参与「重置整组」的计数：它们本来就不在默认值语义之外，
// 而且后端 BulkResetGroup 会跳过它们，前端报一个含它们的条数就是骗人。
watch(showRetired, () => {
  loadAll()
})

onMounted(() => {
  loadAll()
})

function getGroupModified(groupKey) {
  // 左侧栏的「已修改 N」是在指「这一组有本页能处理的偏离」，所以只读项不进计数
  // （它的编辑/重置按钮都是禁的，批量重置也会被后端跳过）；
  // 行内的「已修改」徽标（见 changed-badge）照常显示，状态本身并没有被藏起来。
  return visibleParams.value.filter((p) => p.group === groupKey && !p.deprecated && !p.read_only && !isDefault(p)).length
}

function isDefault(p) {
  if (!p) return true
  if (p.value === undefined || p.value === null) return true
  if (p.default_value === undefined || p.default_value === null) return false
  if (typeof p.value === 'number' || (typeof p.value === 'string' && /^-?\d+(\.\d+)?$/.test(p.value))) {
    const a = Number(p.value)
    const b = Number(p.default_value)
    if (Number.isFinite(a) && Number.isFinite(b)) return a === b
  }
  return String(p.value) === String(p.default_value)
}

// 种子里的 Name/Description 带的是状态标记（「（未接线）」/「【已下架：…】」），
// 那是给扫描器和审计看的，不该出现在给用户看的表格里。
const STATUS_SUFFIX_RE = /[（(]\s*(未接线|已下架|只读)\s*[）)]\s*$/;
const STATUS_PREFIX_RE = /^【[^】]*】\s*/;

function stripSuffix(text) {
  if (!text) return ''
  return String(text).replace(STATUS_SUFFIX_RE, '').replace(STATUS_PREFIX_RE, '').trim()
}

function formatNum(n) {
  if (!Number.isFinite(n)) return String(n)
  return Number.isInteger(n) ? String(n) : String(Math.round(n * 1000) / 1000)
}

// duration 一律显示「N 秒」在参数中心里是反人类：86400 秒没人能一眼读成一天。
// 这里给人读，但只有「整除」时才升单位——90 秒显示成「1.5 分钟」不如显示「90 秒」。
function formatDuration(seconds) {
  const n = Number(seconds)
  if (!Number.isFinite(n)) return String(seconds)
  const sign = n < 0 ? '-' : ''
  const s = Math.abs(n)
  if (s < 60) return `${sign}${formatNum(s)} 秒`
  if (s < 3600) {
    const m = s / 60
    return Number.isInteger(m) ? `${sign}${m} 分钟` : `${sign}${formatNum(s)} 秒`
  }
  if (s < 86400) {
    const h = s / 3600
    return Number.isInteger(h) ? `${sign}${h} 小时` : `${sign}${formatNum(s)} 秒`
  }
  const d = Math.floor(s / 86400)
  const restH = Math.round((s - d * 86400) / 3600)
  if (restH === 0) return `${sign}${d} 天`
  return `${sign}${d} 天 ${restH} 小时`
}

// duration 的单位恒为秒，bool 没有单位——这两种 Unit 留空是对的，
// 不能因为「单位」听起来是必填就把所有 duration 都挂上「秒」。
function unitSuffix(row) {
  if (!row) return ''
  const t = String(row.value_type || '').toLowerCase()
  if (t === 'duration' || t === 'bool' || t === 'boolean') return ''
  return row.unit || ''
}

function formatValue(row, v) {
  if (v === undefined || v === null) return ''
  if (row && String(row.value_type || '').toLowerCase() === 'duration') {
    const n = Number(v)
    if (Number.isFinite(n)) return formatDuration(n)
  }
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  if (typeof v === 'object') {
    try { return JSON.stringify(v) } catch { return String(v) }
  }
  return String(v)
}

function typeTagColor(type) {
  if (!type) return 'info'
  switch (type.toLowerCase()) {
    case 'bool':
    case 'boolean': return 'success'
    case 'int':
    case 'integer': return 'primary'
    case 'float': return 'warning'
    case 'duration': return 'danger'
    // el-tag 的 type 只接受 primary/success/info/warning/danger（2.14.5 的 values 里
    // 没有空串），返回 '' 会为每一条 string 类型的参数行触发一次 Vue prop 校验告警，
    // 并回落到 default='primary'。中性色用 'info'。
    case 'string':
    case 'text': return 'info'
    default: return 'info'
  }
}

function isBoolType(t) {
  return typeof t === 'string' && ['bool', 'boolean'].includes(t.toLowerCase())
}
function isNumericType(t) {
  return typeof t === 'string' && ['int', 'integer', 'float', 'duration', 'number'].includes(t.toLowerCase())
}
// 现在按「这一条参数实际是不是一段 JSON」判断，而不是按 value_type 白名单：
// 4 条 string 参数里 3 条装的是 JSON blob，type 却写 string，
// 按类型名判断会让它们全部落进单行输入框，JSON 语法错了也看不出来。
function isTextareaType(row) {
  const t = String(row && row.value_type ? row.value_type : row || '').toLowerCase()
  if (['text', 'json', 'yaml', 'string'].includes(t)) return looksLikeJson(row)
  return false
}
function looksLikeJson(row) {
  if (!row) return false
  const t = String(row.value_type || '').toLowerCase()
  if (t === 'json') return true
  if (t !== 'string' && t !== 'text') return false
  if (/_json$/.test(String(row.key || ''))) return true
  for (const probe of [row.default_value, row.value]) {
    const s = String(probe ?? '').trim()
    if (s.startsWith('{') || s.startsWith('[')) return true
  }
  return false
}
function hasRange(row) {
  return !!(row && (row.min !== undefined || row.max !== undefined || row.step !== undefined))
}
function rangeText(row) {
  const parts = []
  if (row.min !== undefined && row.max !== undefined) parts.push(`[${row.min}, ${row.max}]`)
  else if (row.min !== undefined) parts.push(`≥ ${row.min}`)
  else if (row.max !== undefined) parts.push(`≤ ${row.max}`)
  const step = Number(row.step)
  if (Number.isFinite(step) && step !== 0 && step !== 1) parts.push(`步长 ${row.step}`)
  return parts.join(' ')
}
function defaultStep(row) {
  const t = String(row && row.value_type ? row.value_type : '').toLowerCase()
  return (t === 'float' || t === 'duration') ? 0.1 : 1
}
function precisionOf(row) {
  const t = String(row && row.value_type ? row.value_type : '').toLowerCase()
  return (t === 'float' || t === 'duration') ? 3 : 0
}
function hasSlider(row) {
  const hasMinMax = row.min !== undefined && row.max !== undefined && Number(row.min) !== Number(row.max)
  return hasMinMax && isNumericType(row.value_type)
}
function retiredTip(row) {
  const when = row.deprecated_at ? formatTs(row.deprecated_at) : '时间未记录'
  return `${row.deprecated_reason || '未记录下架原因'}（下架于 ${when}）`
}
function rowClassName({ row }) {
  return row.deprecated ? 'row-retired' : ''
}

// 接口把 min/max/step 存成字符串（"1"、"0.1"），而 el-slider / el-input-number 的这三个
// prop 声明为 Number ⇒ 直接绑字符串会让每个数值参数弹一次 Vue prop 校验告警。
// 缺省或非法值返回 undefined，交给调用点的 ?? 兜底。
function numBound(v) {
  if (v === undefined || v === null || v === '') return undefined
  const n = Number(v)
  return Number.isFinite(n) ? n : undefined
}

const editDialogVisible = ref(false);
const editing = ref(null)
const editValueBool = ref(false)
const editValueNum = ref(0)
const editValueStr = ref('')
const editError = ref('')

const editValueUnchanged = computed(() => {
  if (!editing.value) return true
  const p = editing.value
  if (isBoolType(p.value_type)) return editValueBool.value === (p.value === true || p.value === 'true')
  if (isNumericType(p.value_type)) {
    if (p.value === undefined || p.value === null) return editValueNum.value === 0
    const a = Number(editValueNum.value)
    const b = Number(p.value)
    return a === b
  }
  return editValueStr.value === String(p.value ?? '')
})

function openEdit(row) {
  if (row.deprecated) {
    ElMessage.warning('该参数已下架：' + (row.deprecated_reason || '代码里没有消费点，改了不生效'))
    return
  }
  if (row.read_only) {
    ElMessage.warning('该参数为只读，不可编辑')
    return
  }
  editing.value = { ...row }
  editError.value = ''
  if (isBoolType(row.value_type)) {
    const v = row.value === true || row.value === 'true' || row.value === 1
    editValueBool.value = v
  } else if (isNumericType(row.value_type)) {
    const n = Number(row.value)
    editValueNum.value = Number.isFinite(n) ? n : Number(row.default_value || 0)
  } else {
    editValueStr.value = row.value === undefined || row.value === null ? '' : String(row.value)
  }
  editDialogVisible.value = true
}

function validateEdit() {
  editError.value = ''
  const p = editing.value
  if (!p) return false
  if (isNumericType(p.value_type)) {
    const n = Number(editValueNum.value)
    if (!Number.isFinite(n)) { editError.value = '必须是合法数字'; return false }
    if (p.min !== undefined && n < Number(p.min)) { editError.value = `不能小于最小值 ${p.min}`; return false }
    if (p.max !== undefined && n > Number(p.max)) { editError.value = `不能大于最大值 ${p.max}`; return false }
    if (n === 0 && looksBlockingZero(p)) {
      editError.value = '0 会让这个参数失效（锁不会过期 / 缓冲不存任何东西 / 上限清零），确认要填 0 吗？'
      return false
    }
  }
  if (looksLikeJson(p)) {
    // 整份 JSON 的旋钮不许置空。服务端现在按 JSON 语法校验写侧，空串会被拒；
    // 这一句把它挡在人已经按下「保存」之前，而不是让人收到一条后端红字。
    // 早一版两侧都不判，坏 JSON 一路写进库，消费方 Unmarshal 失败后静默沿用内置默认——
    // 页面显示「保存成功」，策略却一个字没变。
    const raw = String(editValueStr.value).trim()
    if (!raw) {
      editError.value = '这份参数是整份 JSON，不能置空（要停用请把它写成合法 JSON，例如关掉对应开关字段）'
      return false
    }
    try {
      JSON.parse(raw)
    } catch (e) {
      editError.value = 'JSON 语法错误：' + (e.message || '解析失败')
      return false
    }
  }
  return true
}

// duration 型的 0 是「零秒」还是「不过期」取决于读取侧有没有回落兜底，
// 前端不可能知道；但把 0 一律拦下来比事后排查便宜——锁 TTL、缓冲区大小、上传上限
// 这几类填 0 的后果都是静默的（不是报错，是功能悄悄不工作）。
function looksBlockingZero(p) {
  if (p.value_type !== 'duration') return true
  return /ttl|buffer|size|cap|limit|timeout/i.test(String(p.key || ''))
}

watch([editValueBool, editValueNum, editValueStr], () => {
  if (editing.value) validateEdit()
})

function formatEditJson() {
  try {
    editValueStr.value = JSON.stringify(JSON.parse(editValueStr.value), null, 2)
    editError.value = ''
  } catch (e) {
    editError.value = 'JSON 解析失败：' + (e.message || '语法错误')
  }
}

async function confirmEdit() {
  if (!validateEdit()) return
  const p = editing.value
  let submitValue
  if (isBoolType(p.value_type)) submitValue = editValueBool.value
  else if (isNumericType(p.value_type)) {
    if (p.value_type === 'int' || p.value_type === 'integer') submitValue = Math.round(Number(editValueNum.value))
    else submitValue = Number(editValueNum.value)
  } else submitValue = editValueStr.value

  // 高危参数二次确认：不是所有 danger 都值得拦一次，但代价只是多按一下回车，
  // 而漏按的那一次可能是「锁 TTL 改成 0」这种不报错、只是服务行为悄悄变掉的事故。
  if (p.risk === 'danger') {
    try {
      await ElMessageBox.confirm(
        `「${p.key}」是高危参数，改错可能让功能直接不可用。\n当前值：${formatValue(p, p.value)}\n将改为：${formatValue(p, submitValue)}\n\n确定继续吗？`,
        '高危参数二次确认',
        { confirmButtonText: '我确认，继续保存', cancelButtonText: '再想想', type: 'error' }
      )
    } catch  {
      return
    }
  }

  saving.value = true
  try {
    await configParamsApi.update(p.group, p.key, submitValue)
    const target = visibleParams.value.find((x) => x.group === p.group && x.key === p.key);
    if (target) target.value = submitValue
    editDialogVisible.value = false
    ElMessage.success(p.restart ? '参数已更新（需重启服务后才对已有连接生效）' : '参数已更新')
  } catch (e) {
    ElMessage.error('保存失败：' + (e.message || '未知错误'))
  } finally {
    saving.value = false
  }
}

async function resetInDialog() {
  const p = editing.value
  if (!p) return
  resettingOne.value = true
  try {
    await configParamsApi.reset(p.group, p.key)
    const def = p.default_value ?? ''
    if (isBoolType(p.value_type)) editValueBool.value = def === true || def === 'true'
    else if (isNumericType(p.value_type)) {
      const n = Number(def)
      editValueNum.value = Number.isFinite(n) ? n : 0
    } else editValueStr.value = String(def)
    const target = visibleParams.value.find((x) => x.group === p.group && x.key === p.key)
    if (target) target.value = def
    editing.value = { ...p, value: def }
    ElMessage.success('已恢复默认值，记得点「保存修改」提交')
  } catch (e) {
    ElMessage.error('恢复默认值失败：' + (e.message || '未知错误'))
  } finally {
    resettingOne.value = false
  }
}

async function handleResetOne(row) {
  if (isDefault(row)) { ElMessage.info('已是默认值'); return }
  try {
    await ElMessageBox.confirm(
      `确定要将「${row.key}」重置为默认值吗？`,
      '确认重置',
      { confirmButtonText: '重置', cancelButtonText: '取消', type: 'warning' }
    )
    row._resetting = true
    await configParamsApi.reset(row.group, row.key)
    row.value = row.default_value
    ElMessage.success('已重置为默认值')
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('重置失败：' + (e.message || '未知错误'))
  } finally {
    row._resetting = false
  }
}

async function handleBulkReset() {
  const count = groupModifiedCount.value
  if (count === 0) { ElMessage.info('当前分组没有已修改的参数'); return }
  try {
    await ElMessageBox.confirm(
      `即将重置分组「${currentGroupLabel.value}」下 ${count} 条已修改的参数，全部恢复为默认值。此操作不可撤销。`,
      `重置整组（${count} 条）`,
      { confirmButtonText: '全部重置', cancelButtonText: '取消', type: 'warning' }
    )
    resettingGroup.value = true
    await configParamsApi.bulkReset(currentGroup.value)
    await loadAll();
    ElMessage.success(`已重置 ${count} 条参数`)
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('整组重置失败：' + (e.message || '未知错误'))
  } finally {
    resettingGroup.value = false
  }
}

const auditDrawerVisible = ref(false);
const auditLoading = ref(false)
const auditList = ref([])

function openAuditDrawer() {
  auditDrawerVisible.value = true
  loadAuditLogs()
}

async function loadAuditLogs() {
  auditLoading.value = true
  try {
    const res = await configParamsApi.auditLogs(80)
    auditList.value = Array.isArray(res) ? res : (res && Array.isArray(res.list) ? res.list : [])
  } catch  {
    auditList.value = []
    ElMessage.error('加载审计日志失败')
  } finally {
    auditLoading.value = false
  }
}

function formatTs(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return String(ts)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
</script>

<style scoped>
.config-params-page {
  padding: 16px;
}
.stat-row { margin-bottom: 16px; }
.stat-card { border-radius: 10px; }
.stat-card--danger { border-color: #fca5a5; }
.stat-inner { display: flex; align-items: center; gap: 14px; }
.stat-icon {
  width: 44px; height: 44px;
  border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  color: #fff;
}
.stat-icon--total    { background: linear-gradient(135deg, #6366f1, #8b5cf6); }
.stat-icon--group   { background: linear-gradient(135deg, #06b6d4, #0ea5e9); }
.stat-icon--modified{ background: linear-gradient(135deg, #f59e0b, #f97316); }
.stat-icon--danger  { background: linear-gradient(135deg, #ef4444, #dc2626); }
.stat-body { flex: 1; }
.stat-value { font-size: 24px; font-weight: 600; color: #1f2937; line-height: 1.1; }
.stat-label { font-size: 13px; color: #6b7280; margin-top: 4px; }

.main-row { margin-bottom: 16px; }

.card-header {
  display: flex; justify-content: space-between; align-items: center;
}
.group-card { height: 100%; }
.group-list {
  display: flex; flex-direction: column; gap: 4px;
  max-height: calc(100vh - 260px); overflow-y: auto;
}
.group-empty { font-size: 12px; color: #9ca3af; text-align: center; padding: 12px 0; }
.group-item {
  display: flex; align-items: center; justify-content: space-between;
  padding: 9px 10px; border-radius: 8px; cursor: pointer;
  transition: background .15s;
}
.group-item:hover { background: #f3f4f6; }
.group-item.active { background: #eef2ff; color: #4338ca; }
.group-item__main { display: flex; align-items: center; gap: 8px; min-width: 0; }
.group-icon { font-size: 15px; flex-shrink: 0; opacity: .75; }
.group-name { font-size: 13px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.group-item__count { display: flex; gap: 4px; flex-shrink: 0; }
.group-count {
  min-width: 22px; text-align: center; padding: 0 6px;
  background: #f3f4f6; border-radius: 10px; font-size: 11px; color: #6b7280; line-height: 18px;
}
.group-item.active .group-count { background: #e0e7ff; color: #4338ca; }
.group-mod {
  min-width: 18px; text-align: center; padding: 0 5px;
  background: #fef3c7; color: #b45309; border-radius: 10px; font-size: 11px; line-height: 18px;
}

.table-card .card-header { gap: 12px; flex-wrap: wrap; }
.header-title { display: flex; align-items: center; gap: 10px; }
.group-tag { font-weight: 600; }
.header-count { font-size: 13px; color: #6b7280; }
.header-help { color: #9ca3af; cursor: help; }
.header-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.scope-hint { margin-bottom: 12px; }

/* 平台端地址专用编辑区 */
.platform-host-card {
  margin-bottom: 12px;
  padding: 12px;
  border: 1px solid #f0d9a8;
  border-radius: 6px;
  background: #fffbf3;
}
.platform-host-hint { margin-bottom: 10px; }
.platform-host-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.platform-host-input { flex: 1 1 320px; min-width: 240px; }
.platform-host-probe {
  display: flex; gap: 8px; align-items: center;
  margin-top: 10px; font-size: 12px; line-height: 18px;
}
.platform-host-probe-msg { color: #4b5563; }
.platform-host-probe--unreachable .platform-host-probe-msg { color: #b91c1c; }
.platform-host-probe--ok .platform-host-probe-msg { color: #15803d; }
.platform-host-probe--forbidden .platform-host-probe-msg { color: #b45309; }

.reset-badge {
  display: inline-block; margin-left: 4px; padding: 0 6px;
  background: #f59e0b; color: #fff; border-radius: 10px; font-size: 11px; line-height: 16px;
}

.param-key {
  background: #f3f4f6; padding: 2px 6px; border-radius: 4px;
  font-size: 12px; color: #4338ca;
}
.name-cell { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.name-text { font-size: 13px; color: #1f2937; }
.retired-reason { font-size: 12px; color: #b91c1c; }

.val-cell { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.val-current { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; color: #1f2937; }
.val-changed { color: #b45309; font-weight: 500; }
.val-default { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; color: #6b7280; }
.changed-badge { margin-left: 4px; }
.unit-inline { font-size: 11px; color: #9ca3af; }
.range-cell { font-size: 12px; color: #374151; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.muted { color: #9ca3af; font-size: 12px; }

:deep(.row-retired) { background: #fef2f2; opacity: .85; }

/* 编辑对话框 */
.edit-body { display: flex; flex-direction: column; gap: 12px; }
.edit-desc { margin-bottom: 4px; }
.edit-field { display: flex; flex-direction: column; gap: 8px; }
.edit-field__label { font-size: 13px; color: #374151; font-weight: 600; }
.slider-wrap { display: flex; align-items: center; }
.unit-label { font-size: 12px; color: #6b7280; margin-left: 8px; white-space: nowrap; }
.switch-hint { margin-left: 8px; }
.edit-hint { font-size: 12px; color: #6b7280; }
.json-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.edit-error {
  display: flex; align-items: center; gap: 6px;
  color: #dc2626; font-size: 13px; background: #fef2f2;
  border: 1px solid #fecaca; padding: 8px 10px; border-radius: 6px;
}

/* 审计抽屉 */
.drawer-loading, .drawer-empty {
  display: flex; justify-content: center; align-items: center;
  min-height: 120px; color: #6b7280;
}
.audit-timeline { padding: 4px 0; }
.audit-item { padding: 2px 0 10px; }
.audit-item__head { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; flex-wrap: wrap; }
.audit-key {
  background: #f3f4f6; padding: 1px 6px; border-radius: 4px;
  font-size: 12px; color: #4338ca;
}
.audit-user { font-size: 12px; color: #6b7280; margin-left: auto; }
.audit-item__diff {
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  background: #fafafa; border-radius: 6px; padding: 8px 10px;
}
.audit-arrow { color: #9ca3af; font-size: 16px; }

/* 响应式 */
@media (max-width: 768px) {
  .config-params-page { padding: 10px; }
  .header-actions { width: 100%; justify-content: flex-start; }
}
</style>
