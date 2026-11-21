<template>
  <section
    class="merchant-connection-panel"
    aria-labelledby="merchant-title"
  >
    <header>
      <h2 id="merchant-title">
        {{ t('merchantConnection.title') }}
      </h2>
      <p>{{ t('merchantConnection.intro') }}</p>
    </header>
    <p
      v-if="!authorized"
      role="alert"
    >
      {{ t('merchantConnection.adminOnly') }}
    </p>
    <template v-else>
      <div class="merchant-connection-boundary">
        <strong>{{ t('merchantConnection.actionGate') }}: blocked</strong>
        <p>{{ t('merchantConnection.actionBoundary') }}</p>
      </div>
      <ol
        class="merchant-connection-steps"
        :aria-label="t('merchantConnection.stepsLabel')"
      >
        <li :aria-current="phase === 'idle' ? 'step' : undefined">
          {{ t('merchantConnection.guideTarget') }}
        </li>
        <li :aria-current="phase === 'prepared' ? 'step' : undefined">
          {{ t('merchantConnection.guideAuthorize') }}
        </li>
        <li :aria-current="phase === 'opened' ? 'step' : undefined">
          {{ t('merchantConnection.guideComplete') }}
        </li>
      </ol>
      <el-button
        data-test="guide"
        @click="showGuide"
      >
        {{ t('merchantConnection.helpButton') }}
      </el-button>
      <details
        class="merchant-connection-guide"
        data-test="guidance"
      >
        <summary>{{ t('merchantConnection.guideTitle') }}</summary>
        <p>{{ t('merchantConnection.privacy') }}</p>
      </details>
      <el-form
        class="merchant-connection-target"
        label-position="top"
        @submit.prevent
      >
        <el-form-item
          :label="t('merchantConnection.provider')"
          for="merchant-provider"
        >
          <el-input
            id="merchant-provider"
            v-model="provider"
            :maxlength="64"
            autocomplete="off"
            aria-describedby="merchant-target-help"
          />
        </el-form-item>
        <el-form-item
          :label="t('merchantConnection.shop')"
          for="merchant-shop"
        >
          <el-input
            id="merchant-shop"
            v-model="shop"
            :maxlength="160"
            autocomplete="off"
            aria-describedby="merchant-target-help"
          />
        </el-form-item>
        <p id="merchant-target-help">
          {{ t('merchantConnection.targetHelp') }}
        </p>
        <el-button
          data-test="check"
          :disabled="!targetValid || !!busy || phase !== 'idle'"
          :loading="busy === 'status'"
          @click="check"
        >
          {{ t('merchantConnection.check') }}
        </el-button>
      </el-form>
      <div
        class="merchant-connection-status"
        role="status"
        aria-live="polite"
        :aria-busy="!!busy"
      >
        <strong>{{ t('merchantConnection.connectionStatus') }}: {{ stateLabel }}</strong>
        <p v-if="blocked">
          {{ t('merchantConnection.pendingValidation') }} <code>pending_live_validation</code>
        </p>
        <p v-if="status?.reason === 'provider_revocation_unconfirmed'">
          {{ t('merchantConnection.revokedNotice') }} <code>provider_revocation_unconfirmed</code>
        </p>
        <p v-if="status && !fresh && phase === 'idle' && !busy">
          {{ t('merchantConnection.staleStatus') }}
        </p>
        <p v-if="!status && !busy">
          {{ t('merchantConnection.empty') }}
        </p>
      </div>
      <p
        v-if="error"
        id="merchant-error"
        role="alert"
      >
        {{ errorLabel }}
      </p>
      <div class="merchant-connection-authorize">
        <el-button
          data-test="start"
          :disabled="!canStart"
          :loading="busy === 'start'"
          @click="start"
        >
          {{ t('merchantConnection.start') }}
        </el-button>
        <el-button
          v-if="phase === 'prepared'"
          data-test="open"
          @click="openAuthorization"
        >
          {{ t('merchantConnection.open') }}
        </el-button>
        <el-button
          v-if="phase !== 'idle'"
          @click="invalidate"
        >
          {{ t('merchantConnection.discard') }}
        </el-button>
        <p v-if="phase === 'prepared'">
          {{ t('merchantConnection.openHelp') }}
        </p>
        <el-form
          v-if="phase === 'opened'"
          class="merchant-connection-complete"
          data-private="true"
          autocomplete="off"
          label-position="top"
          @submit.prevent="complete"
        >
          <p id="merchant-handoff-help">
            {{ t('merchantConnection.handoffHelp') }}
          </p>
          <el-form-item
            :label="t('merchantConnection.code')"
            for="merchant-code"
          >
            <el-input
              id="merchant-code"
              v-model="code"
              type="password"
              :maxlength="4096"
              autocomplete="off"
              :spellcheck="false"
              aria-describedby="merchant-handoff-help"
            />
          </el-form-item>
          <el-form-item
            :label="t('merchantConnection.state')"
            for="merchant-state"
          >
            <el-input
              id="merchant-state"
              v-model="returnedState"
              type="password"
              :maxlength="43"
              autocomplete="off"
              :spellcheck="false"
              aria-describedby="merchant-handoff-help"
            />
          </el-form-item>
          <el-button
            data-test="complete"
            native-type="submit"
            :disabled="!canComplete"
          >
            {{ t('merchantConnection.complete') }}
          </el-button>
        </el-form>
      </div>
      <div class="merchant-connection-actions">
        <el-button
          data-test="refresh"
          :disabled="!canRefresh"
          :loading="busy === 'refresh'"
          @click="refresh"
        >
          {{ t('merchantConnection.refresh') }}
        </el-button>
        <p>{{ t('merchantConnection.refreshHelp') }}</p>
        <p id="merchant-revoke-help">
          {{ t('merchantConnection.revokeHelp') }}
        </p>
        <el-checkbox
          v-model="confirmed"
          data-test="revoke-confirm"
          :disabled="!fresh || !!busy || pending || phase !== 'idle' || !status?.version"
          aria-describedby="merchant-revoke-help"
        >
          {{ t('merchantConnection.confirmRevoke') }}
        </el-checkbox>
        <el-button
          data-test="revoke"
          type="danger"
          :disabled="!canRevoke"
          :loading="busy === 'revoke'"
          @click="revoke"
        >
          {{ t('merchantConnection.revoke') }}
        </el-button>
      </div>
    </template>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMerchantConnection } from '@/composables/useMerchantConnection'
import { useOnboarding } from '@/composables/useOnboarding'

const { t } = useI18n()
const { startPageTour } = useOnboarding()
const showGuide = () => startPageTour('/integration/list', true)
const { provider, shop, code, returnedState, status, fresh, busy, phase, error, confirmed, authorized, targetValid, pending, blocked,
  canStart, canComplete, canRefresh, canRevoke, check, start, openAuthorization, complete, invalidate, refresh, revoke } = useMerchantConnection()
const stateLabel = computed(() => ({
  unconfigured: t('merchantConnection.states.unconfigured'), connecting: t('merchantConnection.states.connecting'), ready: t('merchantConnection.states.ready'),
  reauth: t('merchantConnection.states.reauth'), refreshing: t('merchantConnection.states.refreshing'), revoked: t('merchantConnection.states.revoked')
})[status.value?.state] || t('merchantConnection.states.unknown'))
const errorLabel = computed(() => ({
  statusFailed: t('merchantConnection.errors.statusFailed'), startFailed: t('merchantConnection.errors.startFailed'), completeFailed: t('merchantConnection.errors.completeFailed'),
  refreshFailed: t('merchantConnection.errors.refreshFailed'), revokeFailed: t('merchantConnection.errors.revokeFailed'),
  requestExpired: t('merchantConnection.errors.requestExpired'), attemptExpired: t('merchantConnection.errors.attemptExpired')
})[error.value] || t('merchantConnection.errors.statusFailed'))
</script>

<style scoped>
.merchant-connection-panel { margin-block-end: 20px; padding: 20px; background: var(--el-bg-color); border: 1px solid var(--el-border-color-light); border-radius: var(--el-border-radius-base); }
h2 { margin-block: 0 12px; font-size: var(--el-font-size-large); }
p, li { max-inline-size: 72ch; line-height: 1.6; overflow-wrap: anywhere; }
.merchant-connection-boundary, .merchant-connection-guide, .merchant-connection-status, .merchant-connection-authorize, .merchant-connection-actions { margin-block: 20px; }
.merchant-connection-target, .merchant-connection-complete { max-inline-size: 640px; }
.merchant-connection-actions { border-block-start: 1px solid var(--el-border-color-light); padding-block-start: 20px; }
[role="alert"] { color: var(--el-color-danger); }
summary { cursor: pointer; }
summary:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 4px; }
@media (max-width: 600px) { .merchant-connection-panel { padding: 16px; } .merchant-connection-actions :deep(.el-checkbox) { white-space: normal; height: auto; margin-block-end: 12px; } }
</style>
