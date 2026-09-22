<template>
  <div class="init-setup-container">
    <div class="setup-header">
      <h1>{{ $t('系统初始化向导') }}</h1>
      <p>{{ $t('请按照以下步骤完成系统首次配置') }}</p>
    </div>

    <div class="progress-container">
      <el-steps :active="currentStep" finish-status="success" align-center>
        <el-step :title="$t('阅读协议')" description="阅读使用声明" />
        <el-step :title="$t('创建超管')" description="配置管理员账号" />
        <el-step :title="$t('完成')" description="进入登录" />
      </el-steps>
    </div>

    <div class="step-content">
      
      <div v-if="currentStep === 0" class="step-item">
        <h2>{{ $t('软件使用声明') }}</h2>
        <div class="agreement-content">
          <p>{{ $t('本软件已全面开源，您可自由使用、部署与二次开发。') }}</p>
          <ul>
            <li>{{ $t('本软件仅用于合法用途，不得用于从事任何违法活动') }}</li>
            <li>{{ $t('用户需自行承担使用本软件所产生的所有法律责任') }}</li>
            <li>{{ $t('默认完全本地运行：仅当显式开启平台集成（环境变量 PLATFORM_ENABLED=true）时，安装信息（域名/IP/设备指纹/联系方式）才会上报') }}</li>
          </ul>
          <el-checkbox v-model="agreed" style="margin-top: 20px">
            {{ $t('我已阅读并同意以上使用条款') }}
          </el-checkbox>
        </div>
        <div class="step-actions">
          <el-button type="primary" :disabled="!agreed" @click="nextStep">
            {{ $t('下一步') }}
          </el-button>
        </div>
      </div>

      
      <div v-if="currentStep === 1" class="step-item">
        <h2>{{ $t('创建超级管理员') }}</h2>
        <p class="step-tip">
          {{ $t('首次进入系统的用户将被设置为超管，请设置安全的用户名与强密码') }}
        </p>
        <el-form
          ref="adminFormRef"
          :model="adminForm"
          :rules="adminRules"
          label-width="110px"
        >
          <el-form-item :label="$t('超管账号')" prop="username">
            <el-input
              v-model="adminForm.username"
              :placeholder="$t('3-20 位字母数字下划线')"
              size="large"
              clearable
            />
          </el-form-item>
          <el-form-item :label="$t('超管密码')" prop="password">
            <el-input
              v-model="adminForm.password"
              type="password"
              show-password
              :placeholder="$t('至少 8 位，含大小写字母+数字')"
              size="large"
            />
            <div class="field-hint">
              {{ $t('密码至少 8 位，必须同时包含大写字母、小写字母和数字') }}
            </div>
          </el-form-item>
          <el-form-item :label="$t('确认密码')" prop="confirmPassword">
            <el-input
              v-model="adminForm.confirmPassword"
              type="password"
              show-password
              :placeholder="$t('请再次输入密码')"
              size="large"
            />
          </el-form-item>

          <el-divider content-position="left">{{ $t('联系信息（选填，用于系统通知与问题回访）') }}</el-divider>

          <el-form-item :label="$t('手机号')" prop="contact_phone">
            <el-input
              v-model="adminForm.contact_phone"
              :placeholder="$t('选填，用于问题回访')"
              size="large"
              clearable
            />
          </el-form-item>
          <el-form-item :label="$t('邮箱')" prop="email">
            <el-input
              v-model="adminForm.email"
              :placeholder="$t('选填，用于接收系统通知')"
              size="large"
              clearable
            />
          </el-form-item>
          <el-form-item :label="$t('姓名')" prop="real_name">
            <el-input
              v-model="adminForm.real_name"
              :placeholder="$t('选填')"
              size="large"
              clearable
            />
          </el-form-item>
        </el-form>
        <div class="step-actions">
          <el-button @click="prevStep">{{ $t('上一步') }}</el-button>
          <el-button type="primary" :loading="creatingAdmin" @click="handleCreateAdmin">
            {{ $t('创建并完成初始化') }}
          </el-button>
        </div>
      </div>

      
      <div v-if="currentStep === 2" class="step-item">
        <el-result
          icon="success"
          :title="$t('系统初始化已完成')"
          :sub-title="$t('超管账号已创建，现在可以使用超管账号登录系统')"
        >
          <template #extra>
            <el-button type="primary" size="large" @click="goToLogin">
              {{ $t('前往登录') }}
            </el-button>
          </template>
        </el-result>
        <el-descriptions :column="1" border style="margin-top: 20px">
          <el-descriptions-item :label="$t('超管账号')">
            {{ adminForm.username }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('提示')">
            <el-text type="info">
              {{ $t('如忘记密码，可在登录后于「个人中心 - 修改密码」重置') }}
            </el-text>
          </el-descriptions-item>
        </el-descriptions>
      </div>
    </div>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { http } from '@/utils/request'
import { markInitializationComplete } from '@/utils/initHelper'

const router = useRouter()

const currentStep = ref(0)
const agreed = ref(false)

const adminFormRef = ref(null);
const adminForm = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  contact_phone: '',
  email: '',
  real_name: ''
})
const adminRules = {
  username: [
    { required: true, message: i18n.global.t('请输入用户名'), trigger: 'blur' },
    {
      pattern: /^[A-Za-z0-9_]{3,20}$/,
      message: i18n.global.t('用户名必须为 3-20 位字母数字下划线'),
      trigger: 'blur'
    }
  ],
  password: [
    { required: true, message: i18n.global.t('请输入密码'), trigger: 'blur' },
    { min: 8, max: 64, message: i18n.global.t('密码长度需 8-64 位'), trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        if (!value) return callback()
        if (!/[a-z]/.test(value)) return callback(new Error('密码必须包含小写字母'))
        if (!/[A-Z]/.test(value)) return callback(new Error('密码必须包含大写字母'))
        if (!/\d/.test(value)) return callback(new Error('密码必须包含数字'))
        callback()
      },
      trigger: 'blur'
    }
  ],
  confirmPassword: [
    { required: true, message: i18n.global.t('请再次输入密码'), trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        if (value !== adminForm.password) {
          return callback(new Error('两次输入的密码不一致'))
        }
        callback()
      },
      trigger: 'blur'
    }
  ],
  email: [
    { type: 'email', message: i18n.global.t('请输入有效的邮箱地址'), trigger: 'blur' }
  ]
}

const creatingAdmin = ref(false)

const nextStep = () => {
  if (currentStep.value < 2) currentStep.value++
}
const prevStep = () => {
  if (currentStep.value > 0) currentStep.value--
}

const handleCreateAdmin = async () => {
  if (!adminFormRef.value) return
  try {
    await adminFormRef.value.validate()
    creatingAdmin.value = true
    const resp = await http.post('/api/system/init-admin', {
      username: adminForm.username,
      password: adminForm.password,
      contact_phone: adminForm.contact_phone,
      email: adminForm.email,
      real_name: adminForm.real_name
    }, { _silent: true });
    if (resp) {
      ElMessage.success(resp.message || '超管创建成功')
      await markInitializationComplete();
      nextStep()
    }
  } catch (error) {
    console.error('创建超管失败:', error)
    ElMessage.error(error?.response?.data?.message || error?.message || '超管创建失败，请检查后重试')
  } finally {
    creatingAdmin.value = false
  }
}

const goToLogin = () => {
  router.push('/login')
}

onMounted(async () => {
  try {
    const status = await http.get('/api/system/init-status')
    if (status && status.state) {
      switch (status.state) {
        case 'HAS_ADMIN':
        case 'INITIALIZED':
          await markInitializationComplete()
          ElMessage.info(i18n.global.t('系统已初始化，请直接登录'))
          router.push('/login')
          break
        default:
          currentStep.value = 0
      }
    }
  } catch (e) {
    console.warn('查询初始化状态失败:', e)
  }
})
</script>

<style scoped>
.init-setup-container {
  max-width: 900px;
  margin: 0 auto;
  padding: 40px 20px;
}

.setup-header {
  text-align: center;
  margin-bottom: 40px;
}

.setup-header h1 {
  font-size: 28px;
  color: #303133;
  margin-bottom: 10px;
}

.setup-header p {
  font-size: 16px;
  color: #909399;
}

.progress-container {
  margin-bottom: 40px;
}

.step-content {
  min-height: 400px;
}

.step-item {
  background-color: #fff;
  padding: 30px;
  border-radius: 8px;
  box-shadow: 0 2px 12px 0 rgba(0, 0, 0, 0.1);
}

.step-item h2 {
  font-size: 20px;
  color: #303133;
  margin-bottom: 10px;
  padding-bottom: 10px;
  border-bottom: 1px solid #ebeef5;
}

.step-tip {
  color: #909399;
  font-size: 14px;
  margin-bottom: 24px;
}

.agreement-content ul {
  padding-left: 20px;
  margin-bottom: 20px;
}

.agreement-content li {
  margin-bottom: 8px;
  line-height: 1.6;
  color: #606266;
}

.step-actions {
  margin-top: 30px;
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}

.field-hint {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: #909399;
}
</style>
