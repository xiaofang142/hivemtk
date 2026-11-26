<template>
  <div class="config-page">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <span>{{ $t('系统配置') }}</span>
          <el-button
            type="primary"
            :loading="saving"
            @click="submitForm"
          >
            {{ $t('保存配置') }}
          </el-button>
        </div>
      </template>

      <el-form
        :model="configForm"
        label-width="140px"
      >
        <el-divider content-position="left">
          基础信息
        </el-divider>
        <el-form-item label="站点名称">
          <el-input
            v-model="configForm.site_name"
            placeholder="请输入站点名称"
          />
        </el-form-item>
        <el-form-item label="网站URL">
          <el-input
            v-model="configForm.website_url"
            placeholder="请输入网站URL"
          />
        </el-form-item>
        <el-form-item label="站点Logo URL">
          <el-input
            v-model="configForm.logo_url"
            placeholder="请输入Logo URL"
          />
        </el-form-item>
        <el-form-item label="主题色">
          <el-color-picker v-model="configForm.theme_color" />
        </el-form-item>
        <el-form-item
          v-if="tenantMode && tenantInfo.tenant_code"
          label="租户代码"
        >
          <span class="tenant-code">{{ tenantInfo.tenant_code }}</span>
          <el-button
            link
            type="primary"
            @click="copyTenantCode"
          >
            <el-icon><CopyDocument /></el-icon>
            复制
          </el-button>
          <span class="tenant-hint">登录时填在「租户代码」栏</span>
        </el-form-item>
        <el-form-item
          v-if="tenantMode && tenantInfo.name"
          label="租户名称"
        >
          <span class="tenant-name">{{ tenantInfo.name }}</span>
        </el-form-item>

        <el-divider content-position="left">
          SEO 信息
        </el-divider>
        <el-form-item label="SEO 关键词">
          <el-input
            v-model="configForm.seo_keywords"
            placeholder="多个关键词用英文逗号分隔"
          />
        </el-form-item>
        <el-form-item label="SEO 描述">
          <el-input
            v-model="configForm.seo_description"
            type="textarea"
            :rows="3"
            placeholder="请输入SEO描述"
          />
        </el-form-item>

        <el-divider content-position="left">
          客服与备案
        </el-divider>
        <el-form-item label="客服电话">
          <el-input
            v-model="configForm.service_phone"
            placeholder="请输入客服电话"
          />
        </el-form-item>
        <el-form-item label="客服邮箱">
          <el-input
            v-model="configForm.service_email"
            placeholder="请输入客服邮箱"
          />
        </el-form-item>
        <el-form-item label="ICP 备案号">
          <el-input
            v-model="configForm.icp_record"
            placeholder="如 京ICP备12345678号"
          />
        </el-form-item>
        <el-form-item label="公安备案号">
          <el-input
            v-model="configForm.police_record"
            placeholder="如 京公网安备 11010102000000号"
          />
        </el-form-item>

        <el-divider content-position="left">
          功能开关
        </el-divider>
        <el-form-item label="用户注册">
          <el-switch v-model="configForm.enable_register" />
        </el-form-item>
        <el-form-item label="邮件营销">
          <el-switch v-model="configForm.enable_email_marketing" />
        </el-form-item>
        <el-form-item label="RAG 智能体">
          <el-switch v-model="configForm.enable_rag" />
        </el-form-item>
        <el-form-item label="维护模式">
          <el-switch v-model="configForm.maintenance_mode" />
        </el-form-item>

        <el-divider content-position="left">
          限制
        </el-divider>
        <el-form-item label="用户数">
          <span class="form-tips">{{ tenantMode ? '按订阅套餐限制（以平台账单为准）' : '无限制（私域独立部署不限制用户数）' }}</span>
        </el-form-item>
        <el-form-item label="文件上传大小(MB)">
          <el-input-number
            v-model="configForm.max_upload_size_mb"
            :min="1"
            :max="1024"
          />
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import { SystemApi } from '@/api/system'
import { TenantApi } from '@/api/tenant'

const configForm = ref({
  site_name: '',
  website_url: '',
  logo_url: '',
  theme_color: '#4F46E5',
  seo_keywords: '',
  seo_description: '',
  service_phone: '',
  service_email: '',
  icp_record: '',
  police_record: '',
  enable_register: true,
  enable_email_marketing: true,
  enable_rag: true,
  maintenance_mode: false,
  max_users: 100,
  max_upload_size_mb: 50
});

const loading = ref(false)
const saving = ref(false)

// 租户代码：登录页「租户代码」要填的那串短码。只在 SaaS 多租户形态下展示——
// 私域独立部署登录页没有这个输入框，显示短码只会让人去找一个不存在的概念。
const tenantMode = ref(false)
const tenantInfo = reactive({
  tenant_id: '',
  tenant_code: '',
  name: '',
  status: ''
})

const fetchTenantInfo = async () => {
  tenantMode.value = await TenantApi.isMultiTenant()
  if (!tenantMode.value) return
  try {
    const data = await TenantApi.getInfo()
    if (!data) return
    tenantInfo.tenant_id = data.tenant_id ?? ''
    tenantInfo.tenant_code = data.tenant_code || ''
    tenantInfo.name = data.name || ''
    tenantInfo.status = data.status ?? ''
  } catch {
    // 单租户部署可能没有租户行，属预期，忽略
  }
}

const copyTenantCode = async () => {
  const code = tenantInfo.tenant_code
  if (!code) return
  try {
    await navigator.clipboard.writeText(code)
    ElMessage.success('租户代码已复制：' + code)
  } catch {
    ElMessage.error('复制失败，请手动选中复制')
  }
}

const fetchConfig = async () => {
  loading.value = true
  try {
    const response = await SystemApi.getConfig();
    if (response && typeof response === 'object') {
      configForm.value = { ...configForm.value, ...response }
    }
  } catch (error) {
    console.error('加载配置失败:', error)
    ElMessage.warning(i18n.global.t('配置加载失败,使用默认配置'))
  } finally {
    loading.value = false
  }
};

const saveConfig = async () => {
  saving.value = true
  try {
    const res = await SystemApi.saveConfig(configForm.value)
    if (res !== undefined && res !== null) {
      ElMessage.success(i18n.global.t('保存成功'))
      fetchConfig()
    } else {
      ElMessage.error(i18n.global.t('保存失败'))
    }
  } catch (error) {
    ElMessage.error('保存失败: ' + (error.message || '未知错误'))
  } finally {
    saving.value = false
  }
};

const submitForm = async () => {
  await saveConfig()
}

onMounted(() => {
  fetchConfig()
  fetchTenantInfo()
})
</script>

<style scoped>
.config-page {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.form-tips {
  color: #909399;
  font-size: 13px;
}

/* 租户代码：等宽字体，短码是一串要照抄的字符，等宽才好核对 */
.tenant-code {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 1px;
  color: #303133;
}

.tenant-name {
  color: #606266;
  font-size: 13px;
}

.tenant-hint {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
</style>
