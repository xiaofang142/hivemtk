<template>
  <el-card
    class="capability-admin-panel"
    shadow="never"
  >
    <template #header>
      {{ t('capabilityAdmin.title') }}
    </template>
    <p class="capability-admin-help">
      {{ t('capabilityAdmin.help') }}
    </p>
    <el-form
      label-position="top"
      @submit.prevent="resolve"
    >
      <div class="capability-admin-fields">
        <el-form-item :label="t('capabilityAdmin.tuple')">
          <el-select
            v-model="selectedKey"
            :loading="loadingOptions"
            :placeholder="t('capabilityAdmin.choose')"
            filterable
          >
            <el-option
              v-for="cell in options"
              :key="cellKey(cell)"
              :label="[cell.product, cell.region, cell.action, cell.carrier].join(' / ')"
              :value="cellKey(cell)"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('capabilityAdmin.shop')">
          <el-input
            v-model="shop"
            data-test="capability-shop"
            :maxlength="128"
            :placeholder="t('capabilityAdmin.shopHint')"
          />
        </el-form-item>
      </div>
      <div class="capability-admin-actions">
        <el-button
          type="primary"
          data-test="capability-resolve"
          :disabled="!query"
          :loading="resolving"
          @click="resolve"
        >
          {{ t('capabilityAdmin.resolve') }}
        </el-button>
        <el-button
          data-test="capability-export"
          :disabled="!summary"
          @click="exportSummary"
        >
          {{ t('capabilityAdmin.export') }}
        </el-button>
        <el-button
          v-if="optionsFailed"
          @click="loadOptions"
        >
          {{ t('capabilityAdmin.retryOptions') }}
        </el-button>
      </div>
    </el-form>
    <el-alert
      v-if="optionsFailed || resolveFailed"
      :title="optionsFailed ? t('capabilityAdmin.optionsFailed') : t('capabilityAdmin.resolveFailed')"
      type="error"
      :closable="false"
      show-icon
    />
    <el-alert
      v-else-if="summaryExpired"
      :title="t('capabilityAdmin.expired')"
      type="warning"
      :closable="false"
      show-icon
    />
    <el-descriptions
      v-if="summary"
      data-test="capability-result"
      :title="summary.shop_id"
      :column="1"
      border
      class="capability-admin-result"
    >
      <el-descriptions-item
        v-for="field in fields"
        :key="field.label"
        :label="field.label"
      >
        {{ field.value }}
      </el-descriptions-item>
    </el-descriptions>
    <p
      v-else-if="!optionsFailed && !resolveFailed && !resolving && !summaryExpired"
      class="capability-admin-result"
    >
      {{ t('capabilityAdmin.empty') }}
    </p>
  </el-card>
</template>

<script setup>
import { computed, onMounted, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCapabilityCell, listCapabilityCells } from '@/api/capabilityAdmin'

const { t } = useI18n()
const cellKey = ({ product, region, action, carrier }) => [product, region, action, carrier].join('/')
const options = shallowRef([])
const selectedKey = ref('')
const shop = ref('')
const summary = shallowRef(null)
const loadingOptions = ref(false)
const optionsFailed = ref(false)
const resolving = ref(false)
const resolveFailed = ref(false)
const summaryExpired = ref(false)
let generation = 0
let expiryTimer

const query = computed(() => {
  const cell = options.value.find(option => cellKey(option) === selectedKey.value)
  const shopID = shop.value.trim()
  return cell && shopID ? { ...cell, shop_id: shopID } : null
})

function invalidateResolve() {
  generation++
  clearTimeout(expiryTimer)
  summary.value = null
  resolving.value = false
  resolveFailed.value = false
  summaryExpired.value = false
}

watch([selectedKey, shop], invalidateResolve, { flush: 'sync' })
onScopeDispose(invalidateResolve)

async function loadOptions() {
  loadingOptions.value = true
  optionsFailed.value = false
  try {
    const list = await listCapabilityCells()
    if (!Array.isArray(list)) throw new Error('capability cells response is not a list')
    options.value = list
  } catch {
    optionsFailed.value = true
  } finally {
    loadingOptions.value = false
  }
}

async function resolve() {
  if (!query.value || resolving.value) return
  const selection = query.value
  invalidateResolve()
  const request = generation
  resolving.value = true
  try {
    const result = await getCapabilityCell(selection)
    if (request !== generation) return
    if (!result || Object.entries(selection).some(([key, value]) => result[key] !== value)) {
      resolveFailed.value = true
      return
    }
    const remaining = Date.parse(result.valid_until) - Date.now()
    if (!Number.isFinite(remaining)) {
      resolveFailed.value = true
      return
    }
    if (remaining <= 0) {
      expireSummary()
      return
    }
    summary.value = result
    expiryTimer = setTimeout(expireSummary, remaining)
  } catch {
    if (request === generation) resolveFailed.value = true
  } finally {
    if (request === generation) resolving.value = false
  }
}

function expireSummary() {
  summary.value = null
  summaryExpired.value = true
}

function exportSummary() {
  if (!summary.value) return
  if (Date.parse(summary.value.valid_until) <= Date.now()) {
    expireSummary()
    return
  }
  const blob = new Blob([JSON.stringify(summary.value, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'capability-summary.json'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

const fields = computed(() => {
  if (!summary.value) return []
  const result = summary.value
  const modes = {
    bridge: t('capabilityAdmin.bridge'), api_adapter: t('capabilityAdmin.adapter'),
    official_ui: t('capabilityAdmin.workbench'), platform_managed: t('capabilityAdmin.managed')
  }
  const connections = {
    unconfigured: t('capabilityAdmin.unconfigured'), connecting: t('capabilityAdmin.connecting'), ready: t('capabilityAdmin.ready'),
    reauth: t('capabilityAdmin.reauth'), disabled: t('capabilityAdmin.disabled')
  }
  const statuses = { ready: t('capabilityAdmin.ready'), blocked: t('capabilityAdmin.blocked'), simulated: t('capabilityAdmin.simulated') }
  const owners = {
    tenant_admin: t('capabilityAdmin.admin'), merchant_operator: t('capabilityAdmin.operator'),
    platform: t('capabilityAdmin.platform'), system: t('capabilityAdmin.system')
  }
  const steps = {
    configure_connection: t('capabilityAdmin.configure'), complete_connection: t('capabilityAdmin.complete'),
    reconnect: t('capabilityAdmin.reconnect'), review_disabled_connection: t('capabilityAdmin.reviewDisabled'),
    review_permissions: t('capabilityAdmin.reviewPermissions'), review_action_state: t('capabilityAdmin.reviewAction'),
    verify_live_evidence: t('capabilityAdmin.verifyEvidence'), run_preflight: t('capabilityAdmin.preflight'),
    use_official_workbench: t('capabilityAdmin.useWorkbench'), observe_platform_result: t('capabilityAdmin.observePlatform')
  }
  return [
    { label: t('capabilityAdmin.tenant'), value: result.tenant_id },
    { label: t('capabilityAdmin.tuple'), value: [result.product, result.region, result.action, result.carrier].join(' / ') },
    { label: t('capabilityAdmin.mode'), value: modes[result.execution_mode] },
    { label: t('capabilityAdmin.connection'), value: connections[result.connection_state] },
    { label: t('capabilityAdmin.readiness'), value: statuses[result.status] },
    { label: t('capabilityAdmin.submit'), value: result.production_allowed ? t('capabilityAdmin.yes') : t('capabilityAdmin.no') },
    { label: t('capabilityAdmin.owner'), value: owners[result.owner] },
    { label: t('capabilityAdmin.next'), value: steps[result.next_step] },
    { label: t('capabilityAdmin.requiredScopes'), value: result.required_scopes?.join(', ') },
    { label: t('capabilityAdmin.observedScopes'), value: result.observed_scopes?.join(', ') },
    { label: t('capabilityAdmin.contract'), value: result.contract_version },
    { label: t('capabilityAdmin.observedContract'), value: result.observed_version },
    { label: t('capabilityAdmin.docs'), value: result.documentation_version },
    { label: t('capabilityAdmin.observedDocs'), value: result.observed_documentation_version },
    { label: t('capabilityAdmin.limits'), value: result.state_limits?.join(', ') },
    { label: t('capabilityAdmin.reason'), value: result.reason_code },
    { label: t('capabilityAdmin.validUntil'), value: result.valid_until }
  ].map(field => ({ ...field, value: field.value || '—' }))
})

onMounted(loadOptions)
</script>

<style scoped>
.capability-admin-panel { margin-top: 16px; }
.capability-admin-help { margin-top: 0; color: var(--el-text-color-secondary); }
.capability-admin-fields { display: grid; gap: 0 16px; grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr)); }
.capability-admin-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.capability-admin-actions :deep(.el-button) { margin-left: 0; }
.capability-admin-result { margin-top: 16px; overflow-wrap: anywhere; }
</style>
