<template>
  <div class="geo-brand-config">
    <el-alert
      :title="t('navigationContent.brandConfig.previewNotice')"
      type="info"
      :closable="false"
      show-icon
      class="preview-notice"
    />
    <el-tabs v-model="activeTab">
      <el-tab-pane
        :label="t('navigationContent.brandConfig.llmsTab')"
        name="llms"
      >
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>{{ t('navigationContent.brandConfig.llmsTitle') }}</span>
              <div class="preview-actions">
                <el-input
                  v-model="previewDomain"
                  placeholder="example.com"
                  :aria-label="t('navigationContent.brandConfig.domain')"
                  :disabled="previews.llms.status === 'loading'"
                  class="domain-input"
                />
                <el-button
                  type="primary"
                  :loading="previews.llms.status === 'loading'"
                  @click="loadLlms"
                >
                  {{ t('navigationContent.brandConfig.generate') }}
                </el-button>
              </div>
            </div>
          </template>
          <p
            v-if="previews.llms.status === 'loading'"
            role="status"
          >
            {{ t('navigationContent.brandConfig.loading') }}
          </p>
          <div v-else-if="previews.llms.status === 'error'">
            <el-alert
              :title="t('navigationContent.brandConfig.loadError')"
              type="error"
              :closable="false"
              show-icon
            />
            <el-button
              class="retry-button"
              @click="loadLlms"
            >
              {{ t('navigationContent.brandConfig.retry') }}
            </el-button>
          </div>
          <div v-else-if="previews.llms.status === 'preview'">
            <pre class="code-block">{{ previews.llms.content }}</pre>
            <el-tag type="info">
              {{ t('navigationContent.brandConfig.previewStatus') }}
            </el-tag>
          </div>
          <el-empty
            v-else
            :description="t(previews.llms.status === 'empty' ? 'navigationContent.brandConfig.empty' : 'navigationContent.brandConfig.idle')"
          />
        </el-card>
      </el-tab-pane>

      <el-tab-pane
        :label="t('navigationContent.brandConfig.robotsTab')"
        name="robots"
      >
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>{{ t('navigationContent.brandConfig.robotsTitle') }}</span>
              <el-button
                type="primary"
                :loading="previews.robots.status === 'loading'"
                @click="loadRobots"
              >
                {{ t('navigationContent.brandConfig.generate') }}
              </el-button>
            </div>
          </template>
          <p
            v-if="previews.robots.status === 'loading'"
            role="status"
          >
            {{ t('navigationContent.brandConfig.loading') }}
          </p>
          <div v-else-if="previews.robots.status === 'error'">
            <el-alert
              :title="t('navigationContent.brandConfig.loadError')"
              type="error"
              :closable="false"
              show-icon
            />
            <el-button
              class="retry-button"
              @click="loadRobots"
            >
              {{ t('navigationContent.brandConfig.retry') }}
            </el-button>
          </div>
          <div v-else-if="previews.robots.status === 'preview'">
            <pre class="code-block">{{ previews.robots.content }}</pre>
            <el-tag type="info">
              {{ t('navigationContent.brandConfig.previewStatus') }}
            </el-tag>
          </div>
          <el-empty
            v-else
            :description="t(previews.robots.status === 'empty' ? 'navigationContent.brandConfig.empty' : 'navigationContent.brandConfig.idle')"
          />
        </el-card>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { geoApi } from '@/api/geo'

const { t } = useI18n()
const activeTab = ref('llms')
const previewDomain = ref('example.com')
const previewState = () => ({ status: 'idle', content: '' })
const previews = reactive({ llms: previewState(), robots: previewState() })
let disposed = false

async function loadPreview(preview, read) {
  if (disposed || preview.status === 'loading') return
  preview.status = 'loading'
  preview.content = ''
  try {
    const res = await read()
    if (disposed) return
    if (typeof res?.content !== 'string') {
      preview.status = 'error'
      return
    }
    preview.content = res.content
    preview.status = res.content.trim() ? 'preview' : 'empty'
  } catch {
    if (!disposed) preview.status = 'error'
  }
}

const loadLlms = () => loadPreview(previews.llms, () => geoApi.getLlmsTxtPreview(previewDomain.value))
const loadRobots = () => loadPreview(previews.robots, () => geoApi.getRobotsTxtPreview())

onMounted(() => {
  loadLlms()
  loadRobots()
})
onUnmounted(() => { disposed = true })
</script>

<style scoped>
.preview-notice { margin-bottom: 16px; }
.card-header { display: flex; flex-wrap: wrap; gap: 12px; justify-content: space-between; align-items: center; }
.preview-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.domain-input { width: 200px; max-width: 100%; }
.retry-button { margin-top: 12px; }
.code-block {
  background: #1e1e2e;
  color: #cdd6f4;
  padding: 16px;
  border-radius: 8px;
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 400px;
  overflow-y: auto;
}
</style>
