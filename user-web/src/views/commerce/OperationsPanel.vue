<template>
  <div class="commerce-operations-page">
    <el-alert
      :title="t('commerce.boundary')"
      type="info"
      show-icon
      :closable="false"
    />
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
    />
    <el-card
      class="commerce-target"
      shadow="never"
    >
      <template #header>
        {{ t('commerce.title') }}
      </template>
      <el-form
        :inline="true"
        @submit.prevent
      >
        <el-form-item :label="t('commerce.profile')">
          <el-select
            v-model="target.profile"
            :placeholder="t('commerce.chooseProfile')"
            @change="selectProfile"
          >
            <el-option
              v-for="profile in profiles"
              :key="profile.id"
              :label="`${profile.product} · ${profile.region} · ${profile.id}`"
              :value="profile.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('commerce.shop')">
          <el-input
            v-model.trim="target.shop_id"
            :placeholder="t('commerce.shopHint')"
            maxlength="160"
          />
        </el-form-item>
        <el-form-item :label="t('commerce.order')">
          <el-input-number
            v-model="target.order_id"
            :min="1"
            :precision="0"
          />
        </el-form-item>
        <el-button
          :loading="loading"
          :disabled="!target.profile || !target.shop_id"
          @click="loadOperations"
        >
          {{ t('commerce.refresh') }}
        </el-button>
      </el-form>
      <template v-if="selectedProfile">
        <el-tag>{{ selectedProfile.mode }}</el-tag>
        <el-tag type="warning">
          {{ selectedProfile.live_status }}
        </el-tag>
        <p>{{ t('commerce.role') }}: {{ selectedProfile.role }}</p>
        <p v-if="!selectedProfile.provider_configured">
          {{ t('commerce.providerMissing') }}
        </p>
        <ol>
          <li
            v-for="step in selectedProfile.steps"
            :key="step"
          >
            {{ step }}
          </li>
        </ol>
      </template>
    </el-card>

    <el-card
      class="commerce-create"
      shadow="never"
    >
      <template #header>
        {{ t('commerce.newOperation') }}
      </template>
      <el-form
        label-position="top"
        @submit.prevent="create"
      >
        <div class="commerce-form-grid">
          <el-form-item :label="t('commerce.action')">
            <el-select
              v-model="draft.action"
              :placeholder="t('commerce.chooseAction')"
            >
              <el-option
                v-for="action in selectedProfile?.actions || []"
                :key="action.action"
                :label="action.action"
                :value="action.action"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('commerce.sourceVersion')">
            <el-input
              v-model.trim="draft.expected_version"
              :placeholder="t('commerce.sourceVersionHint')"
            />
          </el-form-item>
          <el-form-item :label="t('commerce.intent')">
            <el-input
              v-model.trim="draft.intent_id"
              :placeholder="t('commerce.intentHint')"
              maxlength="128"
            />
          </el-form-item>
          <el-form-item :label="t('commerce.revision')">
            <el-input-number
              v-model="draft.revision"
              :min="1"
              :precision="0"
            />
          </el-form-item>
        </div>
        <el-alert
          v-if="selectedAction"
          :title="selectedAction.postcondition"
          type="info"
          :closable="false"
        />
        <p v-if="selectedAction?.required_fields?.length">
          {{ t('commerce.required') }}: {{ selectedAction.required_fields.join(', ') }}
        </p>
        <div class="commerce-form-grid">
          <el-form-item
            v-for="field in parameterFields"
            :key="field"
            :label="t(`commerce.fields.${field}`)"
          >
            <el-input
              v-model.trim="parameters[field]"
              :placeholder="t('commerce.parameterHint')"
              maxlength="1000"
              :type="field === 'phone' ? 'tel' : 'text'"
            />
          </el-form-item>
        </div>
        <el-form-item
          v-if="selectedAction?.obligations?.length"
          :label="t('commerce.obligations')"
        >
          <el-checkbox-group v-model="parameters.acknowledgements">
            <el-checkbox
              v-for="item in selectedAction.obligations"
              :key="item"
              :value="item"
            >
              {{ item }}
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-button
          native-type="submit"
          type="primary"
          :loading="acting === 'create'"
          :disabled="!canCreate"
        >
          {{ t('commerce.createDraft') }}
        </el-button>
      </el-form>
    </el-card>

    <el-card
      class="commerce-list"
      shadow="never"
    >
      <template #header>
        {{ t('commerce.operations') }} · {{ total }}
      </template>
      <el-table
        v-loading="loading"
        :data="operations"
        row-key="id"
        highlight-current-row
        @current-change="selectOperation"
      >
        <el-table-column
          prop="id"
          :label="t('commerce.operationId')"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          prop="action"
          :label="t('commerce.action')"
          min-width="160"
        />
        <el-table-column
          prop="state"
          :label="t('commerce.state')"
          min-width="160"
        />
        <el-table-column
          prop="mode"
          :label="t('commerce.mode')"
          min-width="140"
        />
        <el-table-column
          prop="evidence_level"
          :label="t('commerce.evidenceLevel')"
          min-width="160"
        />
        <el-table-column
          prop="funds_state"
          :label="t('commerce.funds')"
          min-width="120"
        />
        <template #empty>
          <el-empty :description="t('commerce.empty')" />
        </template>
      </el-table>
    </el-card>

    <el-card
      class="commerce-detail"
      shadow="never"
    >
      <template #header>
        {{ t('commerce.details') }}
      </template>
      <el-empty
        v-if="!selected"
        :description="t('commerce.selectOperation')"
      />
      <template v-else>
        <el-alert
          v-if="selected.state === 'outcome_unknown' || selected.state === 'reconciling'"
          :title="t('commerce.unknownHint')"
          type="warning"
          show-icon
          :closable="false"
        />
        <el-descriptions
          :column="2"
          border
        >
          <el-descriptions-item
            v-for="field in detailFields"
            :key="field"
            :label="t(`commerce.detail.${field}`)"
          >
            {{ selected[field] ?? '—' }}
          </el-descriptions-item>
        </el-descriptions>
        <p v-if="selected.missing_fields?.length">
          {{ t('commerce.required') }}: {{ selected.missing_fields.join(', ') }}
        </p>
        <ul>
          <li
            v-for="item in selected.obligations || []"
            :key="item"
          >
            {{ item }}
          </li>
        </ul>
        <div class="commerce-actions">
          <el-button
            v-for="action in availableTransitions"
            :key="action"
            :loading="acting === action"
            :disabled="!!acting"
            @click="transition(action)"
          >
            {{ t(`commerce.transitions.${action}`) }}
          </el-button>
        </div>
        <el-divider />
        <el-form
          label-position="top"
          @submit.prevent="saveEvidence"
        >
          <el-alert
            :title="t('commerce.manualHint')"
            type="info"
            :closable="false"
          />
          <div class="commerce-form-grid">
            <el-form-item :label="t('commerce.evidenceReference')">
              <el-input
                v-model.trim="evidence.reference"
                :placeholder="t('commerce.evidenceHint')"
                maxlength="512"
              />
            </el-form-item>
            <el-form-item :label="t('commerce.nativeState')">
              <el-input
                v-model.trim="evidence.native_state"
                maxlength="128"
              />
            </el-form-item>
            <el-form-item :label="t('commerce.funds')">
              <el-select v-model="evidence.funds_state">
                <el-option
                  v-for="state in ['', 'pending', 'succeeded', 'failed', 'unknown']"
                  :key="state"
                  :label="state || '—'"
                  :value="state"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('commerce.outcome')">
              <el-select v-model="evidence.outcome">
                <el-option
                  v-for="state in ['processing', 'succeeded', 'failed', 'outcome_unknown']"
                  :key="state"
                  :label="state"
                  :value="state"
                />
              </el-select>
            </el-form-item>
          </div>
          <el-button
            native-type="submit"
            :disabled="!evidence.reference || !!acting"
            :loading="acting === 'evidence'"
          >
            {{ t('commerce.appendEvidence') }}
          </el-button>
        </el-form>
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import * as api from '@/api/commerce'

const { t } = useI18n()
const profiles = ref([])
const operations = ref([])
const selected = ref(null)
const total = ref(0)
const loading = ref(false)
const acting = ref('')
const error = ref('')
const target = reactive({ profile: '', shop_id: '', order_id: 1 })
const draft = reactive({ action: '', intent_id: '', revision: 1, expected_version: '' })
const parameters = reactive({ acknowledgements: [] })
const evidence = reactive({ reference: '', native_state: '', funds_state: '', outcome: 'processing' })
const selectedProfile = computed(() => profiles.value.find((item) => item.id === target.profile))
const selectedAction = computed(() => selectedProfile.value?.actions?.find((item) => item.action === draft.action))
const fields = ['recipient', 'phone', 'address', 'province', 'city', 'district', 'amount_minor', 'currency', 'quantity', 'line_id', 'case_ref', 'reason', 'sku', 'logistics_ref', 'warehouse_ref', 'payment_path']
const parameterFields = computed(() => {
  const required = selectedAction.value?.required_fields || []
  return fields.filter((field) => required.includes(field) || field === 'reason' || ((draft.action.includes('address')) && ['recipient', 'phone', 'address', 'province', 'city', 'district'].includes(field)) || ((draft.action.includes('refund')) && ['amount_minor', 'currency', 'payment_path', 'case_ref', 'line_id', 'quantity'].includes(field)))
})
const canCreate = computed(() => target.profile && target.shop_id && target.order_id && draft.action && draft.intent_id && draft.expected_version)
const detailFields = ['id', 'state', 'native_state', 'mode', 'write_owner', 'approver_id', 'owner_id', 'evidence_level', 'evidence_ref', 'external_ref', 'funds_state', 'manual_state', 'amount_minor', 'currency', 'actual_attempts', 'unknown_since', 'due_at', 'expires_at', 'failure_reason', 'result_summary', 'live_status', 'version', 'preflight_hash']
const availableTransitions = computed(() => {
  if (!selected.value) return []
  const state = selected.value.state
  const result = ['observe']
  if (['draft', 'needs_information', 'prechecking', 'awaiting_approval', 'ready'].includes(state)) result.push('preflight')
  if (state === 'awaiting_approval') result.push('approve')
  if (state === 'ready') result.push('submit')
  if (['draft', 'needs_information', 'awaiting_approval', 'ready', 'queued'].includes(state)) result.push('cancel')
  if (selected.value.mode === 'official_ui') result.push('claim')
  return result
})

function selectProfile() {
  draft.action = ''
  selected.value = null
  operations.value = []
  Object.keys(parameters).forEach((key) => delete parameters[key])
  parameters.acknowledgements = []
}

function selectOperation(row) { selected.value = row || null }

async function loadOperations() {
  loading.value = true
  error.value = ''
  try {
    const data = await api.listCommerceOperations({ profile: target.profile, shop_id: target.shop_id, order_id: target.order_id })
    operations.value = Array.isArray(data?.list) ? data.list : []
    total.value = data?.total ?? operations.value.length
    if (selected.value) selected.value = operations.value.find((item) => item.id === selected.value.id) || null
  } catch { error.value = t('commerce.loadError') } finally { loading.value = false }
}

async function create() {
  if (!canCreate.value) return
  acting.value = 'create'
  error.value = ''
  try {
    const values = {}
    for (const field of parameterFields.value) {
      if (!parameters[field]) continue
      if (['amount_minor', 'quantity'].includes(field)) {
        if (!/^[0-9]+$/.test(parameters[field]) || !Number.isSafeInteger(Number(parameters[field]))) throw new Error('invalid_integer')
        values[field] = Number(parameters[field])
      } else values[field] = parameters[field]
    }
    values.acknowledgements = [...parameters.acknowledgements]
    const created = await api.createCommerceOperation({ ...target, ...draft, parameters: values })
    await loadOperations()
    selected.value = created || null
    for (const field of ['recipient', 'phone', 'address']) parameters[field] = ''
  } catch { error.value = t('commerce.createError') } finally { acting.value = '' }
}

async function transition(action) {
  if (!selected.value) return
  if (['approve', 'submit', 'cancel'].includes(action)) {
    try { await ElMessageBox.confirm(t('commerce.confirmTransition'), t(`commerce.transitions.${action}`), { type: 'warning' }) } catch { return }
  }
  const actions = { preflight: api.preflightCommerceOperation, approve: api.approveCommerceOperation, submit: api.submitCommerceOperation, observe: api.observeCommerceOperation, cancel: api.cancelCommerceOperation, claim: api.claimCommerceOperation }
  acting.value = action
  error.value = ''
  try { selected.value = await actions[action](selected.value.id, { expected_version: selected.value.version, preflight_hash: selected.value.preflight_hash }); await loadOperations() } catch { error.value = t('commerce.actionError') } finally { acting.value = '' }
}

async function saveEvidence() {
  if (!selected.value || !evidence.reference) return
  acting.value = 'evidence'
  error.value = ''
  try { selected.value = await api.appendCommerceEvidence(selected.value.id, { ...evidence, expected_version: selected.value.version }); evidence.reference = ''; await loadOperations() } catch { error.value = t('commerce.actionError') } finally { acting.value = '' }
}

onMounted(async () => {
  try { const data = await api.listCommerceProfiles(); profiles.value = Array.isArray(data) ? data : data?.list || [] } catch { error.value = t('commerce.loadError') }
})
</script>

<style scoped>
.commerce-operations-page { display: grid; gap: 16px; }
.commerce-form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 0 20px; }
.commerce-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
.el-select { min-width: 220px; }
</style>
