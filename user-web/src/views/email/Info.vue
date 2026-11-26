<template>
  <div class="email-providers-container">
    <h2>{{ t('businessCopy.email.providerReferenceTitle') }}</h2>
    <p>{{ t('businessCopy.email.providerReferenceDescription') }}</p>
    <el-table
      :data="filteredProviders"
      border
      stripe
      style="width: 100%"
    >
      <el-table-column
        prop="name"
        :label="$t('名称')"
        width="120"
        fixed
      />
      <el-table-column
        prop="vendor"
        :label="$t('厂家')"
        width="120"
      />
      <el-table-column
        :label="$t('SMTP 服务器')"
        width="180"
      >
        <template #default="{ row }">
          <el-tag>{{ row.smtp }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="端口"
        width="120"
      >
        <template #default="{ row }">
          <div class="port-badges">
            <el-tag
              v-for="port in row.ports"
              :key="port.value"
              :type="getPortType(port.type)"
            >
              {{ port.value }}
            </el-tag>
          </div>
        </template>
      </el-table-column>
      <el-table-column
        label="认证要求"
        min-width="220"
      >
        <template #default="{ row }">
          <div class="auth-notes">
            <el-popover
              placement="top"
              width="300"
              trigger="hover"
            >
              <template #reference>
                <el-icon
                  :size="18"
                  style="margin-right: 8px"
                >
                  <InfoFilled />
                </el-icon>
              </template>
              <div class="popover-content">
                <p
                  v-for="(note, idx) in row.authNotes"
                  :key="idx"
                >
                  {{ note }}
                </p>
              </div>
            </el-popover>
            <span>{{ row.authSummary }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column
        label="注册地址"
        min-width="200"
      >
        <template #default="{ row }">
          <el-link
            type="primary"
            :href="row.registerUrl"
            target="_blank"
          >
            {{ row.registerUrl }}
          </el-link>
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="120"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            type="primary"
            size="small"
            @click="goRegister(row)"
          >
            前往注册
          </el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import _i18n from '@/i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

import { ref, computed } from 'vue'
import { Search as _Search, InfoFilled } from '@element-plus/icons-vue'
import { ElMessage as _ElMessage } from 'element-plus'

const searchText = ref('')

const emailProviders = ref([
  {
    id: 1,
    name: '163邮箱',
    vendor: '网易',
    smtp: 'smtp.163.com',
    ports: [{ value: 465, type: 'ssl' }, { value: 994, type: 'ssl' }],
    authSummary: '网页端开启SMTP服务获取授权码',
    authNotes: [
      '登录 mail.163.com → 设置 → POP3/SMTP/IMAP',
      '开启 IMAP/SMTP 服务',
      '通过手机验证获取授权码'
    ],
    registerUrl: 'https://mail.163.com'
  },
  {
    id: 2,
    name: '126邮箱',
    vendor: '网易',
    smtp: 'smtp.126.com',
    ports: [{ value: 465, type: 'ssl' }],
    authSummary: '独立授权码验证体系',
    authNotes: [
      '登录 mail.126.com → 设置 → 客户端授权密码',
      '启用授权码功能',
      '每个客户端需单独配置'
    ],
    registerUrl: 'https://mail.126.com'
  },
  {
    id: 3,
    name: 'Yeah邮箱',
    vendor: '网易',
    smtp: 'smtp.yeah.net',
    ports: [{ value: 25, type: 'plain' }],
    authSummary: '与163共用认证体系',
    authNotes: [
      '认证流程与163邮箱相同',
      '使用网易统一账号体系'
    ],
    registerUrl: 'https://mail.yeah.net'
  },
  {
    id: 4,
    name: 'QQ邮箱',
    vendor: '腾讯',
    smtp: 'smtp.qq.com',
    ports: [{ value: 465, type: 'ssl' }, { value: 587, type: 'tls' }],
    authSummary: '需生成专属SMTP授权码',
    authNotes: [
      '登录 mail.qq.com → 设置 → 账户',
      '开启 POP3/SMTP 服务',
      '生成16位授权码替代密码'
    ],
    registerUrl: 'https://mail.qq.com'
  },
  {
    id: 5,
    name: '新浪邮箱',
    vendor: '新浪',
    smtp: 'smtp.sina.com',
    ports: [{ value: 25, type: 'plain' }],
    authSummary: '网页端开启SMTP功能',
    authNotes: [
      '登录 mail.sina.com.cn → 设置 → 客户端设置',
      '开启 SMTP 发送功能'
    ],
    registerUrl: 'https://mail.sina.com.cn'
  },
  {
    id: 6,
    name: '139邮箱',
    vendor: '中国移动',
    smtp: 'smtp.139.com',
    ports: [{ value: 465, type: 'ssl' }],
    authSummary: '绑定手机号获取权限',
    authNotes: [
      '需使用移动手机号注册',
      '登录后自动开通SMTP服务',
      '支持手机号免密登录'
    ],
    registerUrl: 'https://mail.10086.cn'
  },
  {
    id: 7,
    name: 'Gmail',
    vendor: 'Google',
    smtp: 'smtp.gmail.com',
    ports: [{ value: 587, type: 'tls' }],
    authSummary: '需启用"低安全性应用"选项',
    authNotes: [
      '登录 Google 账号 → 安全设置',
      '启用"低安全性应用"访问权限',
      '或使用应用专用密码'
    ],
    registerUrl: 'https://mail.google.com'
  },
  {
    id: 8,
    name: 'Outlook',
    vendor: 'Microsoft',
    smtp: 'smtp.office365.com',
    ports: [{ value: 587, type: 'tls' }],
    authSummary: '微软账户双重验证',
    authNotes: [
      '登录 Microsoft 账户 → 安全',
      '启用双重验证',
      '生成应用专用密码'
    ],
    registerUrl: 'https://outlook.live.com'
  },
  {
    id: 9,
    name: 'Yahoo',
    vendor: 'Yahoo',
    smtp: 'smtp.mail.yahoo.com',
    ports: [{ value: 465, type: 'ssl' }],
    authSummary: '生成应用专用密码',
    authNotes: [
      '登录 Yahoo 账户 → 账号安全',
      '生成应用专用密码',
      '替代邮箱密码使用'
    ],
    registerUrl: 'https://mail.yahoo.com'
  },
  {
    id: 10,
    name: 'AOL',
    vendor: 'Verizon',
    smtp: 'smtp.aol.com',
    ports: [{ value: 587, type: 'tls' }],
    authSummary: '国际通用认证',
    authNotes: ['使用标准邮箱密码认证', '可能需要启用安全登录'],
    registerUrl: 'https://mail.aol.com'
  },
  {
    id: 11,
    name: 'GMX',
    vendor: 'United Internet',
    smtp: 'smtp.gmx.com',
    ports: [{ value: 465, type: 'ssl' }],
    authSummary: '德国老牌邮箱服务',
    authNotes: ['使用邮箱完整地址作为用户名', '支持标准SMTP认证'],
    registerUrl: 'https://www.gmx.com'
  }
]);

const filteredProviders = computed(() => {
  if (!searchText.value) return emailProviders.value
  const query = searchText.value.toLowerCase()
  return emailProviders.value.filter(
    provider =>
      provider.name.toLowerCase().includes(query) &&
      provider.vendor.toLowerCase().includes(query) &&
      provider.smtp.toLowerCase().includes(query)
  )
});

const getPortType = type => {
  const types = { ssl: 'success', tls: 'warning', plain: 'info' }
  return types[type] || ''
};

const goRegister = provider => {    
  window.open(provider.registerUrl, '_blank')
};
</script>

<style scoped>
.email-providers-container {
  max-width: 1400px;
  margin: 0 auto;
  padding: 10px;
}

.search-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.search-card {
  margin-bottom: 20px;
}

.port-badges .el-tag {
  margin-right: 5px;
  margin-bottom: 5px;
}

.auth-notes {
  display: flex;
  align-items: center;
}

.notes-section {
  margin-top: 25px;
}

.notes-section ul {
  margin: 10px 0 0 20px;
}

.popover-content p {
  margin: 5px 0;
  line-height: 1.5;
}
</style>
