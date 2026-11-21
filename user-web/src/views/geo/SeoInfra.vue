<template>
  <div class="seo-infra">
    <el-tabs v-model="tab">
      <el-tab-pane
        label="llms.txt v2"
        name="llms"
      >
        <div style="display:flex;gap:16px">
          <div
            v-loading="previews.llms.loading"
            style="flex:1"
          >
            <h4>预览</h4>
            <el-alert
              v-if="previews.llms.error"
              :title="previews.llms.error"
              type="error"
              :closable="false"
              show-icon
            />
            <el-button
              v-if="previews.llms.error"
              @click="loadPreview('llms')"
            >
              {{ $t('重试') }}
            </el-button>
            <pre
              v-if="previews.llms.content"
              class="code"
            >{{ previews.llms.content }}</pre>
            <el-empty
              v-else-if="!previews.llms.loading && !previews.llms.error"
              :description="$t('暂无数据')"
            />
          </div>
          <div style="flex:1">
            <h4>v2 规范检查清单</h4>
            <ul>
              <li>H1 站点名</li>
              <li>blockquote 简介</li>
              <li>## 板块分组</li>
              <li>markdown 链接 + 说明</li>
              <li>rel="describedby" Link 头（需实际验证 Cloudflare Pages _headers 配置）</li>
              <li>页面 Markdown 孪生（需实际验证 Hugo content/*.md）</li>
            </ul>
          </div>
        </div>
      </el-tab-pane>
      <el-tab-pane
        label="robots.txt"
        name="robots"
      >
        <div v-loading="previews.robots.loading">
          <el-alert
            v-if="previews.robots.error"
            :title="previews.robots.error"
            type="error"
            :closable="false"
            show-icon
          />
          <el-button
            v-if="previews.robots.error"
            @click="loadPreview('robots')"
          >
            {{ $t('重试') }}
          </el-button>
          <pre
            v-if="previews.robots.content"
            class="code"
          >{{ previews.robots.content }}</pre>
          <el-empty
            v-else-if="!previews.robots.loading && !previews.robots.error"
            :description="$t('暂无数据')"
          />
        </div>
      </el-tab-pane>
      <el-tab-pane
        label="sitemap.xml"
        name="sitemap"
      >
        <el-button
          :loading="previews.sitemap.loading"
          style="margin-bottom:8px"
          @click="loadPreview('sitemap')"
        >
          生成
        </el-button>
        <el-alert
          v-if="previews.sitemap.error"
          :title="previews.sitemap.error"
          type="error"
          :closable="false"
          show-icon
        />
        <pre
          v-if="previews.sitemap.content"
          class="code"
        >{{ previews.sitemap.content }}</pre>
        <el-empty
          v-else-if="previews.sitemap.loaded && !previews.sitemap.loading && !previews.sitemap.error"
          :description="$t('暂无数据')"
        />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { geoApi } from '@/api/geo'
import i18n from '@/i18n'
const tab = ref('llms')
const state = () => ({ content: '', loading: false, error: '', loaded: false })
const previews = reactive({ llms: state(), robots: state(), sitemap: state() })
const readers = {
  llms: () => geoApi.getLlmsTxtPreview(),
  robots: () => geoApi.getRobotsTxtPreview(),
  sitemap: () => geoApi.getSitemapPreview(),
}
let disposed = false
async function loadPreview(key) {
  const preview = previews[key]
  if (!preview || preview.loading || disposed) return
  preview.loading = true
  preview.error = ''
  preview.content = ''
  try {
    const result = await readers[key]()
    if (typeof result?.content !== 'string') throw new Error(i18n.global.t('加载失败'))
    if (!disposed) {
      preview.content = result.content
      preview.loaded = true
    }
  } catch (error) {
    if (!disposed) preview.error = error?.message || i18n.global.t('加载失败')
  } finally {
    if (!disposed) preview.loading = false
  }
}
onMounted(() => { loadPreview('llms'); loadPreview('robots') })
onUnmounted(() => { disposed = true })
</script>

<style scoped>
.code { background:#1e1e1e; color:#d4d4d4; padding:16px; border-radius:6px; max-height:400px; overflow:auto; font-size:12px; }
</style>
