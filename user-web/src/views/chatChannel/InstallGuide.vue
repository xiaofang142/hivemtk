<template>
  <div class="chat-channel-install-page">
    <el-card class="header-card" shadow="never">
      <div class="header-content">
        <div>
          <h2>客服 Widget 安装引导</h2>
          <p class="subtitle">将客服浮标嵌入到第三方网站 · 复制代码粘贴到目标网站 &lt;/body&gt; 之前</p>
        </div>
        <div class="header-actions">
          <el-button @click="$router.back()">
            <el-icon><Back /></el-icon>
            {{ $t('返回') }}
          </el-button>
          <el-button @click="loadChannels" :loading="loading">
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
        </div>
      </div>
    </el-card>

    <el-row :gutter="20" v-loading="loading">
      
      <el-col :xs="24" :md="14">
        <el-card shadow="never">
          <template #header>
            <span>第 1 步：选择渠道</span>
          </template>
          <el-empty v-if="channels.length === 0" description="暂无可用渠道，请先在 [客服渠道] 创建">
            <el-button type="primary" @click="$router.push({ name: 'ChatChannelCreate' })">
              前往创建
            </el-button>
          </el-empty>
          <el-radio-group v-else v-model="selectedId" class="channel-list" @change="onChannelChange">
            <el-radio
              v-for="ch in channels"
              :key="ch.channel_id"
              :value="ch.channel_id"
              border
              class="channel-radio"
            >
              <div class="channel-info">
                <div class="channel-name">
                  {{ ch.channel_name }}
                  <el-tag v-if="ch.status === 'active'" type="success" size="small">启用</el-tag>
                  <el-tag v-else type="info" size="small">禁用</el-tag>
                </div>
                <div class="channel-meta">
                  AppKey: <code>{{ ch.app_key }}</code>
                </div>
              </div>
            </el-radio>
          </el-radio-group>
        </el-card>

        <el-card v-if="selected" shadow="never" class="code-card">
          <template #header>
            <div class="code-header">
              <span>第 2 步：复制嵌入代码</span>
              <el-button link type="primary" @click="copyEmbed">
                <el-icon><CopyDocument /></el-icon>
                复制全部
              </el-button>
            </div>
          </template>
          <el-alert type="info" :closable="false" show-icon style="margin-bottom: 12px">
            将下方代码粘贴到目标网站 <code>&lt;/body&gt;</code> 之前即可生效。
            已开启 AppSecret 鉴权 + 限流 + XSS 过滤。
          </el-alert>
          <pre class="code-block"><code>{{ embedSnippet }}</code></pre>
        </el-card>

        <el-card v-if="selected" shadow="never" class="code-card">
          <template #header>
            <span>第 3 步：白名单 Origin 配置</span>
          </template>
          <el-form label-width="120px">
            <el-form-item label="允许的来源">
              <el-input
                v-model="originInput"
                type="textarea"
                :rows="4"
                placeholder="每行一个 origin，例如：https://www.example.com"
              />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="originSaving" @click="onSaveOrigins">
                保存白名单
              </el-button>
              <el-button @click="originInput = originsToText(selected?.allowed_origins)">
                重置
              </el-button>
              <span class="hint">只有白名单内的来源才能加载 Widget，防滥用</span>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      
      <el-col :xs="24" :md="10">
        <el-card shadow="never" class="preview-card">
          <template #header>
            <span>实时预览</span>
          </template>
          <div class="preview-frame">
            <div class="preview-mock-browser">
              <div class="browser-bar">
                <span class="dot dot-red"></span>
                <span class="dot dot-yellow"></span>
                <span class="dot dot-green"></span>
                <span class="url">https://your-website.com</span>
              </div>
              <div class="browser-content">
                <p class="mock-line">您的网站内容</p>
                <p class="mock-line">访客在此浏览...</p>
                <p class="mock-line">点击右下角浮标发起对话</p>
                
                <div
                  v-if="selected"
                  class="widget-button"
                  :class="['pos-' + (selected.widget_position || 'bottom-right')]"
                  :style="{ backgroundColor: selected.widget_color || '#4F46E5' }"
                  @click="previewOpen = !previewOpen"
                >
                  <el-icon :size="24"><ChatDotRound /></el-icon>
                </div>
                
                <div v-if="previewOpen && selected" class="widget-window" :class="['pos-' + (selected.widget_position || 'bottom-right')]">
                  <div class="window-header" :style="{ backgroundColor: selected.widget_color || '#4F46E5' }">
                    <span>{{ selected.welcome_message || '在线客服' }}</span>
                    <el-icon class="close-btn" @click="previewOpen = false"><Close /></el-icon>
                  </div>
                  <div class="window-body">
                    <p>您好，请问有什么可以帮您？</p>
                  </div>
                  <div class="window-input">
                    <el-input v-model="previewInput" placeholder="输入消息..." size="small" />
                  </div>
                </div>
              </div>
            </div>
          </div>
        </el-card>

        <el-card shadow="never" class="test-card">
          <template #header>
            <span>第 4 步：测试连接</span>
          </template>
          <el-button type="primary" :loading="testing" @click="onTestConnection">
            <el-icon><Connection /></el-icon>
            测试 API 连通性
          </el-button>
          <div v-if="testResult" class="test-result" :class="{ ok: testResult.ok, fail: !testResult.ok }">
            <el-icon><CircleCheck v-if="testResult.ok" /><CircleClose v-else /></el-icon>
            <span>{{ testResult.message }}</span>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Back, Refresh, CopyDocument, ChatDotRound, Close,
  Connection, CircleCheck, CircleClose
} from '@element-plus/icons-vue'
import { listChannels, getChannel, updateChannel } from '@/api/chatChannel'
import { http } from '@/utils/request'

const router = useRouter()
void Back; void Refresh; void CopyDocument; void ChatDotRound
void Close; void Connection; void CircleCheck; void CircleClose

const loading = ref(false)
const channels = ref([])
const selectedId = ref('')
const selected = ref(null)

const originsToText = (v) =>
  Array.isArray(v) ? v.join('\n') : String(v || '').split(',').map(s => s.trim()).filter(Boolean).join('\n');
const originInput = ref('')
const originSaving = ref(false)
const previewOpen = ref(false)
const previewInput = ref('')
const testing = ref(false)
const testResult = ref(null)

const embedSnippet = computed(() => {
  if (!selected.value) return ''
  const appKey = selected.value.app_key
  const host = window.location.origin
  return `<!-- 客服 Widget 浮标 - 私域部署 -->
<script>
(function() {
  var APP_KEY = '${appKey}';
  var HOST = '${host}';
  // 创建浮标按钮
  var btn = document.createElement('div');
  btn.style.cssText = 'position:fixed;${selected.value.widget_position?.includes('left') ? 'left:20px;' : 'right:20px;'}bottom:20px;width:56px;height:56px;border-radius:50%;background:${selected.value.widget_color || '#4F46E5'};color:#fff;display:flex;align-items:center;justify-content:center;cursor:pointer;z-index:9999;box-shadow:0 4px 12px rgba(0,0,0,0.15);';
  btn.innerHTML = '<svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg>';
  btn.onclick = function() {
    // 打开 iframe
    var w = 380, h = 600;
    var left = window.screen.width - w - 40;
    var top = window.screen.height - h - 80;
    window.open(HOST + '/chat/embed/' + APP_KEY, 'chat_' + APP_KEY, 'width=' + w + ',height=' + h + ',left=' + left + ',top=' + top);
  };
  document.body.appendChild(btn);
})();
<${'/script'}>`;
})

const loadChannels = async () => {
  loading.value = true
  try {
    const res = await listChannels({ page: 1, page_size: 100 })
    channels.value = res?.list || []
    if (channels.value.length > 0 && !selectedId.value) {
      onChannelChange(channels.value[0].channel_id)
    }
  } catch (err) {
    ElMessage.error('加载失败：' + (err?.message || err))
  } finally {
    loading.value = false
  }
}

const onChannelChange = async (channelId) => {
  selectedId.value = channelId
  try {
    const res = await getChannel(channelId)
    selected.value = res || null
    originInput.value = originsToText(res?.allowed_origins)
    testResult.value = null
  } catch (err) {
    ElMessage.error('加载渠道失败：' + (err?.message || err))
  }
}

const copyEmbed = async () => {
  if (!embedSnippet.value) return
  try {
    await navigator.clipboard.writeText(embedSnippet.value)
    ElMessage.success(i18n.global.t('已复制到剪贴板'))
  } catch {
    ElMessage.error(i18n.global.t('复制失败，请手动复制'))
  }
}

const onSaveOrigins = async () => {
  if (!selected.value) return
  const origins = originInput.value
    .split('\n')
    .map(s => s.trim())
    .filter(s => s.length > 0)
  try {
    await ElMessageBox.confirm(
      `将白名单更新为 ${origins.length} 个 origin？`,
      '保存白名单',
      { type: 'warning' }
    )
    originSaving.value = true
    await updateChannel(selected.value.channel_id, { allowed_origins: origins })
    ElMessage.success(i18n.global.t('保存成功'))
    await onChannelChange(selected.value.channel_id)
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error('保存失败：' + (err?.message || err))
    }
  } finally {
    originSaving.value = false
  }
}

const onTestConnection = async () => {
  if (!selected.value) return
  testing.value = true
  testResult.value = null
  try {
    const resp = await http.get(`/api/chat/public/channel/${selected.value.app_key}/info`);
    if (resp && resp.channel_id) {
      testResult.value = { ok: true, message: `连接成功 · ${resp.channel_name || ''}` }
    } else {
      testResult.value = { ok: false, message: i18n.global.t('连接成功但返回数据异常') }
    }
  } catch (err) {
    testResult.value = { ok: false, message: i18n.global.t('连接失败：') + (err?.message || err) }
  } finally {
    testing.value = false
  }
}

onMounted(() => {
  loadChannels()
})
</script>

<style scoped>
.chat-channel-install-page {
  padding: 0;
}
.header-card {
  margin-bottom: 16px;
}
.header-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-content h2 {
  margin: 0 0 4px;
  font-size: 20px;
}
.subtitle {
  color: #909399;
  font-size: 13px;
  margin: 0;
}
.channel-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
}
.channel-radio {
  width: 100%;
  margin-right: 0;
  padding: 12px 16px;
}
.channel-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.channel-name {
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 8px;
}
.channel-meta {
  font-size: 12px;
  color: #909399;
}
.channel-meta code {
  background: #f5f7fa;
  padding: 1px 6px;
  border-radius: 3px;
}
.code-card {
  margin-top: 16px;
}
.code-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.code-block {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 16px;
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
  font-family: 'SF Mono', Monaco, Consolas, monospace;
  max-height: 320px;
  overflow-y: auto;
}
.hint {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
.preview-card {
  position: sticky;
  top: 20px;
}
.preview-frame {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  overflow: hidden;
}
.preview-mock-browser {
  background: #fff;
}
.browser-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  background: #f5f5f5;
  border-bottom: 1px solid #ebeef5;
}
.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
}
.dot-red { background: #ff5f57; }
.dot-yellow { background: #ffbd2e; }
.dot-green { background: #28ca42; }
.url {
  margin-left: 8px;
  flex: 1;
  background: #fff;
  padding: 4px 8px;
  border-radius: 3px;
  font-size: 12px;
  color: #606266;
}
.browser-content {
  height: 360px;
  position: relative;
  background: #fafafa;
  padding: 16px;
}
.mock-line {
  color: #c0c4cc;
  font-size: 13px;
  margin: 8px 0;
}
.widget-button {
  position: absolute;
  width: 56px;
  height: 56px;
  border-radius: 50%;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  z-index: 10;
  box-shadow: 0 4px 12px rgba(0,0,0,0.15);
  transition: transform 0.2s;
}
.widget-button:hover {
  transform: scale(1.05);
}
.widget-button.pos-bottom-right { right: 16px; bottom: 16px; }
.widget-button.pos-bottom-left { left: 16px; bottom: 16px; }
.widget-window {
  position: absolute;
  width: 300px;
  height: 380px;
  background: #fff;
  border-radius: 8px;
  box-shadow: 0 4px 20px rgba(0,0,0,0.15);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  z-index: 11;
}
.widget-window.pos-bottom-right { right: 16px; bottom: 80px; }
.widget-window.pos-bottom-left { left: 16px; bottom: 80px; }
.window-header {
  color: #fff;
  padding: 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 14px;
}
.close-btn {
  cursor: pointer;
}
.window-body {
  flex: 1;
  padding: 12px;
  overflow-y: auto;
  font-size: 13px;
  color: #606266;
}
.window-input {
  padding: 8px;
  border-top: 1px solid #ebeef5;
}
.test-card {
  margin-top: 16px;
}
.test-result {
  margin-top: 12px;
  padding: 12px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.test-result.ok {
  background: #f0f9ff;
  color: #10B981;
}
.test-result.fail {
  background: #fef0f0;
  color: #EF4444;
}
</style>
