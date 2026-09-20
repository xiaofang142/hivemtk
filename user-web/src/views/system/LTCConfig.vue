<template>
  <div class="ltc-config-page">
    <el-alert
      v-if="cfg.degraded"
      class="degraded-alert"
      type="error"
      :closable="false"
      show-icon
      title="ltc.config 当前读不动，页面显示的是「默认全关」那一份"
    >
      <div class="alert-body">
        <p>
          底层原因：{{ cfg.degrade_reason || '（后端未给出）' }}
        </p>
        <p class="alert-note">
          这不是"运营把开关关了"。存储读不动时闸门按 fail-closed 拦着所有 LTC 路由，
          先把存储修好再回来判读这份表单。保存一次读不到内容的表单会把库里那一份盖掉。
        </p>
      </div>
    </el-alert>

    <el-row :gutter="16">
      <el-col
        :xs="24"
        :md="8"
      >
        <el-card
          shadow="never"
          class="master-card"
        >
          <template #header>
            <div class="card-head">
              <span>总开关</span>
              <el-tag
                :type="cfg.source === 'kv' ? 'success' : 'info'"
                size="small"
              >
                来源：{{ sourceText }}
              </el-tag>
            </div>
          </template>
          <div class="master-body">
            <el-switch
              v-model="cfg.enabled"
              active-text="开"
              inactive-text="关"
            />
            <p class="hint">
              关掉时六个阶段全部拦在外面，无论各自那一项配的是什么。
              这一档与分阶段档是两道独立的锁。
            </p>
          </div>
        </el-card>
      </el-col>

      <el-col
        :xs="24"
        :md="16"
      >
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>分阶段开关</span>
              <span class="head-sub">归管路由合计 {{ cfg.guarded_total }} 条</span>
            </div>
          </template>
          <el-table
            :data="stageRows"
            size="small"
          >
            <el-table-column
              label="阶段"
              width="120"
            >
              <template #default="{ row }">
                {{ stageLabel(row.stage) }}
              </template>
            </el-table-column>
            <el-table-column
              label="开关"
              width="90"
            >
              <template #default="{ row }">
                <el-switch v-model="cfg.stages_enabled[row.stage]" />
              </template>
            </el-table-column>
            <el-table-column
              label="当前是否放行"
              width="150"
            >
              <template #default="{ row }">
                <el-tag
                  :type="row.active ? 'success' : 'danger'"
                  size="small"
                >
                  {{ reasonText(row.reason) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="归管路由"
              width="110"
            >
              <template #default="{ row }">
                {{ row.guarded }} 条
              </template>
            </el-table-column>
            <el-table-column label="键名">
              <template #default="{ row }">
                <code>{{ row.stage }}</code>
              </template>
            </el-table-column>
          </el-table>
          <p class="hint">
            打开某一项只影响它自己那一段。归管路由为 0 表示这个阶段还没有任何业务路由
            挂到闸门上 —— 此刻这一项配成开或配成关，现网行为一模一样。
          </p>
        </el-card>
      </el-col>
    </el-row>

    <el-card
      v-if="boundsReady"
      shadow="never"
      class="threshold-card"
    >
      <template #header>
        <div class="card-head">
          <span>阈值</span>
          <span class="head-sub">lead_score 与 confidence 是两道独立闸门，不可合并成一个分</span>
        </div>
      </template>
      <el-form
        label-width="150px"
        label-position="left"
      >
        <el-row :gutter="16">
          <el-col
            v-for="key in thresholdKeys"
            :key="key"
            :xs="24"
            :md="12"
          >
            <el-form-item :label="thresholdLabel(key)">
              <el-input-number
                v-model="cfg.thresholds[key]"
                :min="inputMin(key)"
                :max="maxOf(key)"
                :step="stepOf(key)"
                :precision="key === 'lead_score' || key === 'discount_percent' ? 0 : 3"
                controls-position="right"
              />
              <div class="bound-text">
                <span>{{ bounds[key].desc }}</span>
                <span
                  v-if="!bounds[key].zero_legal"
                  class="zero-bad"
                >给 0 会被拒（0 等于拆掉这道闸门）</span>
                <span
                  v-else
                  class="zero-ok"
                >0 合法：含义是「任何折扣都要走审批」</span>
              </div>
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
    </el-card>

    <el-card
      v-else
      shadow="never"
      class="threshold-card"
    >
      <template #header>
        <div class="card-head">
          <span>阈值</span>
        </div>
      </template>
      <p class="hint">
        阈值输入框要等后端的取值边界到位才能渲染（边界与校验同源，写在页面里就是第二份事实）。
        点「重新读取」再试一次；一直读不到就是存储或权限的问题，别凭记忆填。
      </p>
    </el-card>

    <el-card
      v-if="cfg.reading_hints && cfg.reading_hints.length"
      shadow="never"
      class="hints-card"
    >
      <template #header>
        <div class="card-head">
          <span>这份读数怎么讲</span>
        </div>
      </template>
      <ul class="hints">
        <li
          v-for="(h, i) in cfg.reading_hints"
          :key="i"
        >
          {{ h }}
        </li>
      </ul>
    </el-card>

    <el-alert
      v-if="saveError"
      class="save-error"
      type="error"
      :closable="false"
      show-icon
      :title="saveError"
    />
    <el-alert
      v-if="saveNotice.audit_error"
      class="save-error"
      type="warning"
      :closable="false"
      show-icon
      :title="'开关已改，但这次变更没进审计：' + saveNotice.audit_error"
    />

    <div class="actions">
      <el-button
        type="primary"
        :loading="saving"
        @click="onSave"
      >
        保存整份策略
      </el-button>
      <el-button
        :loading="loading"
        @click="load"
      >
        重新读取
      </el-button>
      <span class="actions-meta">
        KV 键 <code>{{ cfg.kv_key }}</code> · 上限 {{ cfg.max_bytes }} 字节 ·
        缓存 TTL {{ cfg.cache_ttl_seconds }}s（保存后立即失效，不必等）
      </span>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getLTCConfig, saveLTCConfig } from '@/api/ltcConfig'

// 阶段键与中文名：顺序照后端 LTCKnownStages（销售流程顺序），
// 键名照 ltc.config 的 JSON 字面量 —— 这两处错一处，保存就会被后端拒收。
const STAGES = [
  { stage: 'opportunity', label: '商机' },
  { stage: 'outreach', label: '外联触达' },
  { stage: 'quote', label: '报价' },
  { stage: 'bill', label: '账单' },
  { stage: 'payment', label: '回款' },
  { stage: 'collection', label: '催收' }
]

const THRESHOLDS = [
  { key: 'lead_score', label: '线索分下限 lead_score' },
  { key: 'confidence', label: '置信度下限 confidence' },
  { key: 'discount_percent', label: '折扣审批线 discount_percent' },
  { key: 'win_probability', label: '赢率门槛 win_probability' }
]

const REASON_TEXT = {
  active: '放行',
  master_off: '总开关关着',
  stage_off: '本阶段未开',
  degraded: '配置读不动',
  unknown_stage: '阶段名未注册'
}

const emptyCfg = () => ({
  enabled: false,
  stages_enabled: { opportunity: false, outreach: false, quote: false, bill: false, payment: false, collection: false },
  thresholds: { lead_score: 70, confidence: 0.8, discount_percent: 15, win_probability: 0.5 },
  stage_status: STAGES.map((s) => ({ stage: s.stage, active: false, reason: 'master_off' })),
  guarded_routes: {},
  guarded_total: 0,
  reading_hints: [],
  source: 'default',
  degraded: false,
  degrade_reason: '',
  kv_key: 'ltc.config',
  max_bytes: 8192,
  cache_ttl_seconds: 60,
  threshold_ranges: {},
  threshold_bounds: {}
})

const cfg = reactive(emptyCfg())
const bounds = ref({})
const thresholdKeys = THRESHOLDS.map((t) => t.key)
const loading = ref(false)
const saving = ref(false)
const saveError = ref('')
const saveNotice = ref({})

const sourceText = computed(() => (cfg.source === 'kv' ? '库里那份' : '内置默认（库里还没有值）'))

// 边界没到位之前不渲染阈值区：首帧 bounds 还是空对象，直接取 .max 会让整页白屏。
const boundsReady = computed(() => thresholdKeys.every((k) => !!bounds.value[k]))

const stageRows = computed(() =>
  (cfg.stage_status || []).map((row) => ({ ...row, guarded: cfg.guarded_routes?.[row.stage] ?? 0 }))
)

const stageLabel = (s) => STAGES.find((x) => x.stage === s)?.label || s
const reasonText = (r) => REASON_TEXT[r] || r
const thresholdLabel = (k) => THRESHOLDS.find((t) => t.key === k)?.label || k

// 输入边界取自后端同一份表：写在视图里的第二份范围，迟早有一天和校验各说各话。
const inputMin = (k) => (bounds.value[k]?.zero_legal ? 0 : bounds.value[k]?.min)
const maxOf = (k) => bounds.value[k]?.max
const stepOf = (k) => (k === 'lead_score' || k === 'discount_percent' ? 1 : 0.01)

async function load() {
  loading.value = true
  saveError.value = ''
  try {
    const data = await getLTCConfig()
    Object.assign(cfg, emptyCfg(), data)
    bounds.value = data.threshold_bounds || {}
  } catch (err) {
    saveError.value = '读取失败：' + (err?.message || '未知错误')
  } finally {
    loading.value = false
  }
}

function payload() {
  const stages = {}
  for (const s of STAGES) stages[s.stage] = !!cfg.stages_enabled[s.stage]
  const thresholds = {}
  for (const k of thresholdKeys) thresholds[k] = cfg.thresholds[k]
  return { enabled: !!cfg.enabled, stages_enabled: stages, thresholds }
}

async function onSave() {
  saving.value = true
  saveError.value = ''
  saveNotice.value = {}
  try {
    const res = await saveLTCConfig(payload())
    saveNotice.value = res || {}
    if (res?.effective) {
      Object.assign(cfg, emptyCfg(), res.effective)
      bounds.value = res.effective.threshold_bounds || {}
    }
    if (res?.persisted && !res?.audit_error) {
      ElMessage.success(`已保存（${res.stored_bytes} 字节，开启阶段：${(res.stages_on || []).join('、') || '无'}）`)
    }
  } catch (err) {
    // 后端把拒绝原因拼到了句子级（「阶段名 qoute 不认识」「lead_score 给 0 等于拆闸」…），
    // 原样铺出来，不要换成一句"参数错误"让运营在六个阶段名里猜。
    saveError.value = err?.message || '保存失败'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.ltc-config-page {
  padding: 16px;
}
.ltc-config-page > .el-row,
.ltc-config-page > .el-card,
.ltc-config-page > .el-alert {
  margin-bottom: 16px;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.head-sub {
  font-size: 12px;
  color: #909399;
}
.master-body .hint,
.hint {
  margin: 12px 0 0;
  font-size: 12px;
  line-height: 1.7;
  color: #909399;
}
.bound-text {
  display: flex;
  flex-direction: column;
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
.zero-bad {
  color: #f56c6c;
}
.zero-ok {
  color: #67c23a;
}
.hints {
  margin: 0;
  padding-left: 18px;
  font-size: 13px;
  line-height: 1.9;
}
.actions {
  display: flex;
  align-items: center;
  gap: 12px;
}
.actions-meta {
  font-size: 12px;
  color: #909399;
}
.alert-body p {
  margin: 4px 0;
}
.alert-note {
  font-size: 12px;
  color: #606266;
}
code {
  background: #f5f7fa;
  padding: 0 4px;
  border-radius: 3px;
}
</style>
