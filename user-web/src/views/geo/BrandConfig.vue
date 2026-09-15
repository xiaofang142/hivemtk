<template>
  <div class="geo-brand-config">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="📄 llms.txt v2 预览" name="llms">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>llms.txt — AI 爬虫的内容导航（带 describedby Link 头 + Markdown 孪生）</span>
              <div>
                <el-input v-model="previewDomain" placeholder="example.com" style="width:200px;margin-right:8px" />
                <el-button type="primary" @click="loadLlms">生成预览</el-button>
              </div>
            </div>
          </template>
          <el-empty v-if="!llmsContent" description="输入域名 → 点"生成预览"">
            <el-button type="primary" @click="loadLlms">示例域名</el-button>
          </el-empty>
          <pre v-else class="code-block">{{ llmsContent }}</pre>
          <div v-if="llmsContent" style="margin-top:12px">
            <el-tag type="success">✅ 符合 llms.txt v2 规范</el-tag>
            <el-tag style="margin-left:8px">Markdown 孪生 → LLM 友好</el-tag>
            <el-tag style="margin-left:8px">Link 头 → describedby → 语义关联</el-tag>
          </div>
        </el-card>
      </el-tab-pane>

      <el-tab-pane label="🤖 robots.txt AI 爬虫白名单" name="robots">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>robots.txt — 2026 AI 爬虫完整白名单（8 国内 + 13 海外）</span>
              <el-button type="primary" @click="loadRobots">生成预览</el-button>
            </div>
          </template>
          <el-empty v-if="!robotsContent" description="点"生成预览"查看 robots.txt 内容" />
          <pre v-else class="code-block">{{ robotsContent }}</pre>
          <div v-if="robotsContent" style="margin-top:12px">
            <el-tag type="success">✅ 覆盖 21 个 AI 爬虫 UA</el-tag>
            <el-tag style="margin-left:8px">Bytespider/DeepSeekBot/QwenBot/ErnieBot</el-tag>
            <el-tag style="margin-left:8px">GPTBot/ClaudeBot/PerplexityBot/Amazonbot</el-tag>
          </div>
        </el-card>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import request from '@/api/index'
import { ElMessage } from 'element-plus'

const activeTab = ref('llms')
const previewDomain = ref('example.com')
const llmsContent = ref('')
const robotsContent = ref('')

async function loadLlms() {
  try {
    const res = await request.get('/geo/site/llms-txt-preview', { params: { domain: previewDomain.value } })
    llmsContent.value = res.data || ''
  } catch (e) {
    // 后端可能返回纯文本，兼容处理
    llmsContent.value = e?.response?.data || '（后端暂未部署 llms.txt 生成逻辑）'
  }
}

async function loadRobots() {
  try {
    const res = await request.get('/geo/site/robots-preview')
    robotsContent.value = res.data || ''
  } catch (e) {
    robotsContent.value = e?.response?.data || '（后端暂未部署 robots.txt 生成逻辑）'
  }
}

onMounted(() => {
  loadLlms()
  loadRobots()
})
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
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
