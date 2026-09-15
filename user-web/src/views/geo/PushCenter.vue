<template>
  <div class="push-center">
    <el-row :gutter="16">
      <el-col :span="8" v-for="(status, plat) in quota" :key="plat">
        <el-card shadow="hover" class="quota-card">
          <div class="plat-name">{{ plat }}</div>
          <el-progress
            v-if="status.remain >= 0"
            :percentage="Math.max(0, 100 - (status.remain / (status.remain + 1) * 100))"
            :status="status.remain > 100 ? 'success' : status.remain > 20 ? 'warning' : 'exception'"
          />
          <div class="plat-detail">
            <el-tag v-if="status.registered" type="success">已注册</el-tag>
            <el-tag v-else type="info">未注册</el-tag>
            <span v-if="status.remain >= 0">剩余 {{ status.remain }}</span>
            <span v-else>∞ 无限额</span>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" style="margin-top:16px">
      <template #header>
        <div class="card-header">
          <span>⚡ 手动推送</span>
        </div>
      </template>
      <el-input
        v-model="urlsText"
        type="textarea"
        :rows="4"
        placeholder="每行一个 URL，粘贴要推送的文章地址"
      />
      <div style="margin-top:8px">
        <el-checkbox-group v-model="selectedPlatforms">
          <el-checkbox value="baidu">百度</el-checkbox>
          <el-checkbox value="indexnow">IndexNow (Bing/Yandex/Naver)</el-checkbox>
          <el-checkbox value="google">Google</el-checkbox>
        </el-checkbox-group>
        <el-button type="primary" @click="push" style="margin-left:12px">🚀 推送</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { geoApi } from '@/api/geo'
import { ElMessage } from 'element-plus'
const quota = ref({})
const urlsText = ref('')
const selectedPlatforms = ref(['indexnow', 'baidu'])
async function loadQuota() {
  try {
    quota.value = (await geoApi.getPushQuota()) || {}
  } catch (e) {}
}
async function push() {
  const urls = urlsText.value.split('\n').map(s => s.trim()).filter(Boolean)
  if (!urls.length) return
  try {
    await geoApi.pushUrls({ urls, platforms: selectedPlatforms.value })
    ElMessage.success('推送成功')
    urlsText.value = ''
    loadQuota()
  } catch (e) { ElMessage.error('推送失败') }
}
onMounted(loadQuota)
</script>

<style scoped>
.quota-card { text-align:center; }
.plat-name { font-size:16px; font-weight:bold; margin-bottom:8px; }
.plat-detail { margin-top:8px; font-size:12px; color:#666; }
</style>
