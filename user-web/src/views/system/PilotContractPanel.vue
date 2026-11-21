<template>
  <el-card
    v-loading="busy"
    class="pilot-contract-panel"
    shadow="never"
  >
    <template #header>
      {{ t('pilotContract.title') }}
    </template>
    <el-alert
      :title="t('pilotContract.boundary')"
      type="warning"
      :closable="false"
    />
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      :closable="false"
    />
    <el-button
      v-if="!result"
      :disabled="busy"
      @click="load"
    >
      {{ t('pilotContract.retry') }}
    </el-button>
    <template v-if="result">
      <div class="pilot-contract-identity">
        <p>{{ t('pilotContract.identity') }}: {{ result.installation?.install_id || '—' }}</p>
        <p>{{ t('pilotContract.tenant') }}: {{ result.tenant_id }}</p>
        <p>{{ result.configured ? t('pilotContract.configured') : t('pilotContract.unconfigured') }}</p>
        <p>{{ result.validation_signal }} / {{ result.production_gate }}</p>
      </div>
      <el-empty
        v-if="!result.configured"
        :description="t('pilotContract.empty')"
      />
      <div class="pilot-contract-blockers">
        <p>{{ t('pilotContract.blockers') }}</p>
        <ul>
          <li
            v-for="reason in result.blockers"
            :key="reason"
          >
            {{ reason }}
          </li>
        </ul>
      </div>
      <el-form
        class="pilot-contract-editor"
        label-position="top"
      >
        <el-form-item :label="t('pilotContract.draft')">
          <el-input
            v-model="draft"
            type="textarea"
            :rows="12"
            :maxlength="60000"
            :aria-label="t('pilotContract.draft')"
            :placeholder="t('pilotContract.hint')"
          />
        </el-form-item>
        <p>{{ t('pilotContract.hint') }}</p>
        <el-button
          :disabled="busy"
          @click="submit(false)"
        >
          {{ t('pilotContract.validate') }}
        </el-button>
        <el-button
          :disabled="busy"
          type="primary"
          @click="submit(true)"
        >
          {{ t('pilotContract.save') }}
        </el-button>
        <p role="status">
          {{ notice }}
        </p>
      </el-form>
    </template>
  </el-card>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { pilotContractApi } from '@/api/pilotContract'

const { t } = useI18n()
const result = ref(null)
const draft = ref('')
const busy = ref(false)
const error = ref('')
const notice = ref('')

async function load() {
  busy.value = true
  error.value = ''
  try {
    const data = await pilotContractApi.get()
    if (!data?.contract) throw new Error('unavailable')
    result.value = data
    draft.value = JSON.stringify(data.contract, null, 2)
  } catch {
    error.value = t('pilotContract.loadFailed')
  } finally {
    busy.value = false
  }
}

async function submit(save) {
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const input = JSON.parse(draft.value)
    const data = await (save ? pilotContractApi.save(input) : pilotContractApi.validate(input))
    if (!data?.contract) throw new Error('unavailable')
    result.value = { ...data, configured: save || result.value.configured }
    if (save) draft.value = JSON.stringify(data.contract, null, 2)
    notice.value = save ? t('pilotContract.saved') : t('pilotContract.validated')
  } catch {
    error.value = t('pilotContract.submitFailed')
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.pilot-contract-panel { margin-top: 20px; }
.pilot-contract-identity, .pilot-contract-blockers, .pilot-contract-editor { margin-top: 16px; }
</style>
