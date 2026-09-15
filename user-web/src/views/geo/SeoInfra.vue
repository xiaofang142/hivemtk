<template>
  <div class="seo-infra">
    <el-tabs v-model="tab">
      <el-tab-pane label="llms.txt v2" name="llms">
        <div style="display:flex;gap:16px">
          <div style="flex:1">
            <h4>预览</h4>
            <pre class="code">{{ llmsPreview }}</pre>
          </div>
          <div style="flex:1">
            <h4>v2 规范检查清单</h4>
            <ul>
              <li>✅ H1 站点名</li>
              <li>✅ blockquote 简介</li>
              <li>✅ ## 板块分组</li>
              <li>✅ markdown 链接 + 说明</li>
              <li>✅ rel="describedby" Link 头（需 Cloudflare Pages _headers 配置）</li>
              <li>✅ 页面 Markdown 孪生（需 Hugo content/*.md）</li>
            </ul>
          </div>
        </div>
      </el-tab-pane>
      <el-tab-pane label="robots.txt" name="robots">
        <pre class="code">{{ robotsPreview }}</pre>
      </el-tab-pane>
      <el-tab-pane label="sitemap.xml" name="sitemap">
        <el-button @click="genSitemap" style="margin-bottom:8px">生成</el-button>
        <pre class="code">{{ sitemapPreview }}</pre>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { geoApi } from '@/api/geo'
const tab = ref('llms')
const llmsPreview = ref('')
const robotsPreview = ref('')
const sitemapPreview = ref('')
async function load() {
  try {
    const [l, r] = await Promise.all([
      geoApi.getLlmsTxtPreview(),
      geoApi.getRobotsTxtPreview(),
    ])
    llmsPreview.value = l?.content || ''
    robotsPreview.value = r?.content || ''
  } catch (e) {}
}
async function genSitemap() {
  try {
    const r = await geoApi.getSitemapPreview()
    sitemapPreview.value = r?.content || ''
  } catch (e) {}
}
onMounted(load)
</script>

<style scoped>
.code { background:#1e1e1e; color:#d4d4d4; padding:16px; border-radius:6px; max-height:400px; overflow:auto; font-size:12px; }
</style>
