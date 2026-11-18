<template>
  <div class="sms-config">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('短信配置') }}</span>
          <el-button
            type="primary"
            :loading="loading"
            @click="handleSave"
          >
            {{ $t('保存配置') }}
          </el-button>
        </div>
      </template>
      
      <el-form
        v-loading="loading"
        :model="smsConfig"
        label-width="120px"
      >
        <el-form-item label="默认短信平台">
          <el-select
            v-model="smsConfig.defaultProvider"
            placeholder="请选择默认短信平台"
          >
            <el-option
              label="阿里云短信"
              value="aliyun"
            />
            <el-option
              label="腾讯云短信"
              value="tencent"
            />
            <el-option
              label="华为云短信"
              value="huawei"
            />
          </el-select>
        </el-form-item>
        
        <el-form-item label="发送频率限制">
          <el-input-number
            v-model="smsConfig.rateLimit"
            :min="1"
            :max="1000"
          />
          <span style="margin-left: 10px;">条/分钟</span>
        </el-form-item>
        
        <el-form-item label="每日发送限制">
          <el-input-number
            v-model="smsConfig.dailyLimit"
            :min="100"
            :max="100000"
          />
          <span style="margin-left: 10px;">条/天</span>
        </el-form-item>
        
        <el-form-item label="失败重试次数">
          <el-input-number
            v-model="smsConfig.retryTimes"
            :min="0"
            :max="5"
          />
        </el-form-item>
        
        <el-divider>阿里云短信配置</el-divider>
        
        <el-form-item label="AccessKey ID">
          <el-input
            v-model="smsConfig.aliyun.accessKeyId"
            placeholder="请输入AccessKey ID"
          />
        </el-form-item>
        
        <el-form-item label="AccessKey Secret">
          <el-input
            v-model="smsConfig.aliyun.accessKeySecret"
            placeholder="请输入AccessKey Secret"
            type="password"
          />
        </el-form-item>
        
        <el-form-item label="短信签名">
          <el-input
            v-model="smsConfig.aliyun.signName"
            placeholder="请输入短信签名"
          />
        </el-form-item>
        
        <el-divider>腾讯云短信配置</el-divider>
        
        <el-form-item label="SecretId">
          <el-input
            v-model="smsConfig.tencent.secretId"
            placeholder="请输入SecretId"
          />
        </el-form-item>
        
        <el-form-item label="SecretKey">
          <el-input
            v-model="smsConfig.tencent.secretKey"
            placeholder="请输入SecretKey"
            type="password"
          />
        </el-form-item>
        
        <el-form-item label="短信应用ID">
          <el-input
            v-model="smsConfig.tencent.appId"
            placeholder="请输入短信应用ID"
          />
        </el-form-item>
        
        <el-form-item label="短信签名">
          <el-input
            v-model="smsConfig.tencent.signName"
            placeholder="请输入短信签名"
          />
        </el-form-item>
        
        <el-divider>华为云短信配置</el-divider>
        
        <el-form-item label="APP Key">
          <el-input
            v-model="smsConfig.huawei.appKey"
            placeholder="请输入APP Key"
          />
        </el-form-item>
        
        <el-form-item label="APP Secret">
          <el-input
            v-model="smsConfig.huawei.appSecret"
            placeholder="请输入APP Secret"
            type="password"
          />
        </el-form-item>
        
        <el-form-item label="通道号">
          <el-input
            v-model="smsConfig.huawei.sender"
            placeholder="请输入通道号"
          />
        </el-form-item>
        
        <el-form-item label="短信签名">
          <el-input
            v-model="smsConfig.huawei.signature"
            placeholder="请输入短信签名"
          />
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import smsApi from '@/api/sms'

const smsConfig = ref({
  defaultProvider: 'aliyun',
  rateLimit: 100,
  dailyLimit: 10000,
  retryTimes: 3,
  aliyun: {
    accessKeyId: '',
    accessKeySecret: '',
    signName: ''
  },
  tencent: {
    secretId: '',
    secretKey: '',
    appId: '',
    signName: ''
  },
  huawei: {
    appKey: '',
    appSecret: '',
    sender: '',
    signature: ''
  }
});

const loading = ref(false)

const getSmsConfig = async () => {
  loading.value = true
  try {
    const response = await smsApi.getConfig()
    smsConfig.value = response || smsConfig.value
  } catch  {
    ElMessage.error(i18n.global.t('获取短信配置失败'))
  } finally {
    loading.value = false
  }
};

const handleSave = async () => {
  loading.value = true
  try {
    await smsApi.saveConfig(smsConfig.value)
    ElMessage.success(i18n.global.t('配置保存成功'))
  } catch  {
    ElMessage.error(i18n.global.t('保存配置失败'))
  } finally {
    loading.value = false
  }
};

onMounted(() => {
  getSmsConfig()
})
</script>

<style scoped>
.sms-config {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.el-divider {
  margin: 30px 0 20px;
}
</style>
