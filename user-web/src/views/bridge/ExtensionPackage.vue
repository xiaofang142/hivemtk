<template>
  <div class="p-4">
    <el-card>
      <template #header>
        <div class="flex items-center justify-between">
          <span>桥接扩展安装包</span>
          <el-button
            size="small"
            :loading="loading"
            @click="load"
          >
            刷新
          </el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        class="mb-3"
        title="扩展不上架 Chrome Web Store：由平台运营上传安装包，租户在此下载后自行安装。"
      />

      <el-alert
        v-if="error"
        type="warning"
        :closable="false"
        show-icon
        class="mb-3"
        :title="error"
      />

      <template v-if="pkg">
        <el-descriptions
          :column="1"
          border
          class="mb-3"
        >
          <el-descriptions-item label="扩展名称">
            {{ pkg.name || 'HiveBridge 蜂桥' }}
          </el-descriptions-item>
          <el-descriptions-item label="版本">
            {{ pkg.version || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="文件大小">
            {{ formatSize(pkg.size_bytes) }}
          </el-descriptions-item>
          <el-descriptions-item label="SHA256">
            <span class="font-mono text-xs break-all">{{ pkg.file_sha256 || '-' }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <el-button
          type="primary"
          :loading="downloading"
          @click="download"
        >
          下载安装包
        </el-button>
      </template>

      <el-empty
        v-else-if="!loading && !error"
        description="暂无安装包信息"
      />
    </el-card>

    <el-card class="mt-4">
      <template #header>
        <span>安装步骤</span>
      </template>
      <ol class="text-sm leading-7 text-gray-600 pl-4">
        <li>下载上方安装包（.zip），解压到任意目录（解压后根目录应直接是 manifest.json）。</li>
        <li>打开 Chrome，地址栏输入 <code>chrome://extensions</code>。</li>
        <li>右上角开启「开发者模式」。</li>
        <li>点「加载已解压的扩展程序」，选择刚才解压出来的目录。</li>
        <li v-if="tenantMode === true">
          点扩展图标，在「服务端地址」填下方任一地址（默认已预填线上，点「线上/本地」可切换），填入「桥接凭证」与租户短码后保存。
        </li>
        <li v-else>
          点扩展图标，在「服务端地址」填下方任一地址（默认已预填线上，点「线上/本地」可切换），填入「桥接凭证」后保存。
        </li>
      </ol>
      <div class="mt-2 text-sm text-gray-600">
        <div class="mb-1 font-medium">服务端地址（点「复制」后粘贴进扩展）：</div>
        <div class="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span>
            线上（默认）：<code class="font-mono text-xs">{{ ONLINE_SERVER_URL }}</code>
            <el-button
              size="small"
              link
              type="primary"
              @click="copyServerUrl(ONLINE_SERVER_URL)"
            >
              复制
            </el-button>
          </span>
          <span>
            本地：<code class="font-mono text-xs">{{ LOCAL_SERVER_URL }}</code>
            <el-button
              size="small"
              link
              type="primary"
              @click="copyServerUrl(LOCAL_SERVER_URL)"
            >
              复制
            </el-button>
          </span>
        </div>
        <div class="mt-1 text-xs text-gray-400">
          线上是云端 SaaS 的 user-server（从本页下载安装包的租户直接用它）；本地是本机自部署 user-server（默认端口 8204）。扩展弹窗里「线上 / 本地」两个按钮可一键填入。
        </div>
      </div>
      <!-- 租户短码只存在于 SaaS 多租户形态：私域独立部署（TENANT_MODE=off）下
           后端 resolveBridgeTenant 直接放行（不绑租户），扩展里没有「租户短码」
           这个字段。此处无条件展示等于让私域用户去填一个不存在的概念。 -->
      <el-alert
        v-if="tenantMode === true"
        type="warning"
        :closable="false"
        show-icon
        title="租户短码必填"
        description="SaaS 多租户部署下，服务端靠租户短码（或账号行自证）确定私信归属；首次上报缺短码会被拒绝。"
      />
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getBridgeExtensionPackage } from '@/api/bridgeExtension.js'
import { getBootstrap } from '@/utils/bootstrapState.js'
import { http } from '@/utils/request'
// 地址单源：直接引扩展源码的 constants，网页端展示的与扩展弹窗里预填的永远一致，
// 不在本页再抄一份硬编码（两处漂移 = 用户照网页填的地址连不上扩展默认连的）。
import { DEFAULT_USER_SERVER, ONLINE_USER_SERVER } from '../../../bridge/src/core/constants.js'

const ONLINE_SERVER_URL = ONLINE_USER_SERVER.baseUrl
const LOCAL_SERVER_URL = DEFAULT_USER_SERVER.baseUrl

const copyServerUrl = async (url) => {
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('已复制：' + url)
  } catch {
    // clipboard API 在非 https / 老浏览器可能不可用，退回 execCommand 兜底
    const ta = document.createElement('textarea')
    ta.value = url
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    ElMessage.success('已复制：' + url)
  }
}

const pkg = ref(null)
const loading = ref(false)
const downloading = ref(false)
const error = ref('')

// 部署形态：SaaS 多租户(on) 才需要租户短码；私域独立部署(off) 没有这个概念。
//
// 三态而不是布尔：null = 还没定。
//   - 引导状态（getBootstrap）在守卫里已拉过 /api/public/config，但它的
//     tenantMode 初值是 false，与「确实不是多租户」同形，分不出「已拉」还是
//     「没拉」⇒ 用 resolved 标志区分。
//   - resolved 时直接用缓存值（不再发请求，也没有闪动）；
//   - 没 resolved（理论上不该发生：守卫对非公开路由会 await ensureBootstrap）
//     就自己拉一次公开配置兜底。
//   - 仍未知时一律先显示**中性文案**（不出现"私域专有"或"租户短码必填"
//     这类形态专属内容），避免在 SaaS 实例上先闪一段私域话术。
const boot = getBootstrap()
const tenantMode = ref(boot.resolved ? !!boot.tenantMode : null)
if (tenantMode.value === null) {
  http.get('/api/public/config', undefined, { _silent: true })
    .then((res) => {
      const cfg = res?.data ?? res
      tenantMode.value = !!cfg?.tenant_mode
    })
    .catch(() => { tenantMode.value = false })
}

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    pkg.value = await getBridgeExtensionPackage() || null
    if (!pkg.value) error.value = '当前部署未接入平台，或平台尚未上架桥接扩展包'
  } catch (e) {
    pkg.value = null
    error.value = (e && (e.message || e.msg)) || '获取安装包信息失败'
  } finally {
    loading.value = false
  }
}

const download = async () => {
  if (!pkg.value || !pkg.value.download_url) return
  downloading.value = true
  try {
    // 直接开新窗口下载：安装包是平台托管的静态文件，不需要带本系统的鉴权。
    // 用 <a download> 而不是 fetch —— 后者要处理二进制流与对象 URL 的释放，
    // 而这里只是把浏览器现成的下载能力接上。
    const a = document.createElement('a')
    a.href = pkg.value.download_url
    a.download = pkg.value.filename || 'hivemtk-bridge.zip'
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    ElMessage.success('已开始下载，下载后解压安装')
  } catch (e) {
    ElMessage.error('下载失败：' + ((e && e.message) || e))
  } finally {
    downloading.value = false
  }
}

const formatSize = (bytes) => {
  const n = Number(bytes)
  if (!Number.isFinite(n) || n <= 0) return '-'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

onMounted(load)
</script>
