<template>
  <div class="login-container">
    
    <div class="login-aside">
      <div class="aside-inner">
        <div class="brand">
          <span class="brand-mark"></span>
          <span class="brand-text">{{ t('system.appName') }}</span>
        </div>

        <h1 class="aside-title">
          一个智能体<br />
          全域<span class="grad">获客 · 转化 · 复购</span>
        </h1>
        <p class="aside-desc">
          私有化部署的营销智能体中台：多渠道统一接入、本地 RAG 知识库、智能体客服与销售协同，让增长在一条闭环里自动跑起来。
        </p>

        <ul class="feature-list">
          <li><el-icon><Check /></el-icon>私有化部署，客户数据 100% 归你</li>
          <li><el-icon><Check /></el-icon>企微 / 飞书 / 抖音 / 网页等全渠道接入</li>
          <li><el-icon><Check /></el-icon>基于私有知识库的本地 RAG 检索</li>
          <li><el-icon><Check /></el-icon>7×24 智能体在线承接，夜间不丢单</li>
        </ul>

        <div class="aside-footer">© 2026 营销智能体套件 · HiveMtk</div>
      </div>
    </div>

    
    <div class="login-main">
      <div class="login-box">
        <div class="login-header">
          <h2>{{ t('core.login.welcome') }}</h2>
          <p>{{ t('system.appName') }}</p>
        </div>

        <el-form :model="loginForm" :rules="rules" ref="loginFormRef" @keyup.enter="handleLogin">
          <el-form-item prop="username">
            <el-input
              v-model="loginForm.username"
              :placeholder="t('core.login.username')"
              prefix-icon="User"
              size="large"
            />
          </el-form-item>

          <el-form-item prop="password">
            <el-input
              v-model="loginForm.password"
              type="password"
              :placeholder="t('core.login.password')"
              prefix-icon="Lock"
              size="large"
              show-password
              @keyup.enter="handleLogin"
            />
          </el-form-item>

          <el-form-item>
            <el-button
              type="primary"
              size="large"
              style="width: 100%"
              :loading="loading"
              @click="mode === 'login' ? handleLogin() : handleRegister()"
            >
              {{ mode === 'login' ? t('core.login.submit') : '注 册' }}
            </el-button>
          </el-form-item>

          <div v-if="mode === 'register'">
            <el-form-item prop="email">
              <el-input v-model="loginForm.email" placeholder="邮箱" prefix-icon="Message" size="large" />
            </el-form-item>
          </div>

          <div class="register-switch">
            <template v-if="mode === 'login'">
              <el-button v-if="enableRegister" link type="primary" @click="switchMode('register')">没有账号？注册新账号</el-button>
            </template>
            <template v-else>
              <el-button link type="primary" @click="switchMode('login')">已有账号？返回登录</el-button>
            </template>
          </div>

        </el-form>

        <p class="login-disclaimer">
          {{ t('disclaimer.login') }}
        </p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { usersApi } from '@/api/users'
import i18n from '@/i18n'

const t = i18n.global.t

const router = useRouter()
const userStore = useUserStore()
const loginFormRef = ref(null)
const loading = ref(false)

const loginForm = reactive({
  username: '',
  password: '',
  email: ''
});

// 自助注册：开关由公开配置 /api/public/config 提供（未登录可取）
const mode = ref('login')
const enableRegister = ref(false)
const switchMode = (m) => { mode.value = m }

const handleRegister = async () => {
  if (!loginFormRef.value) return
  if (!loginForm.username || !loginForm.password || !loginForm.email) {
    ElMessage.warning('请填写用户名、邮箱和密码')
    return
  }
  loading.value = true
  try {
    const { http } = await import('@/utils/request')
    await http.post('/api/auth/register', {
      username: loginForm.username,
      password: loginForm.password,
      email: loginForm.email
    })
    ElMessage.success('注册成功，请登录')
    mode.value = 'login'
  } catch (e) {
    const msg = e?.response?.data?.message || e?.message || '注册失败'
    ElMessage.error(msg)
  } finally {
    loading.value = false
  }
}

import { http as httpApi } from '@/utils/request'
httpApi.get('/api/public/config').then((res) => {
  const data = res?.data ?? res
  enableRegister.value = !!data?.enable_register
}).catch(() => { enableRegister.value = false })

const rules = {
  username: [
    { required: true, message: t('core.login.pleaseUsername'), trigger: 'blur' }
  ],
  password: [
    { required: true, message: t('core.login.pleasePassword'), trigger: 'blur' }
  ]
};

const handleLogin = async () => {
  if (!loginFormRef.value) return

  // 校验不通过时 validate() 是 reject 一个字段集合，不是 Error：
  // 它和"口令错/服务端挂了"必须分开。此前一起落进下面的 catch，空表单点登录会同时
  // 打出一条 console.error 和一个"登录失败"红条——行内已经写了"请输入用户名"，
  // 再弹一次失败是把用户的漏填报成了系统故障。
  const passed = await loginFormRef.value.validate().catch(() => false)
  if (!passed) return

  try {
    loading.value = true

    const response = await usersApi.login(loginForm)

    if (response) {
      const token = response.token || response.Token
      const user = response.user || response.User

      if (token && user) {
        userStore.login(user, token)
        localStorage.setItem('system_initialized', 'true')
        ElMessage.success(t('core.login.success'))
        router.push('/')
      } else {
        console.error('登录响应数据异常:', response)
        ElMessage.error(t('core.login.failServer'))
      }
    } else {
      console.error('登录响应数据异常:', response)
      ElMessage.error(t('core.login.failServer'))
    }
    } catch (error) {
    console.error('登录失败:', error)
    ElMessage.error(error.message || t('core.login.fail'))
  } finally {
    loading.value = false
  }
};
</script>

<style scoped>
.login-container {
  display: flex;
  min-height: 100vh;
  background: #F8FAFC;
}

/* ===== 左侧品牌面板 ===== */
.login-aside {
  flex: 1;
  background: linear-gradient(150deg, #1E1B4B 0%, #312E81 55%, #4F46E5 100%);
  position: relative;
  overflow: hidden;
  display: flex;
  align-items: center;
  padding: 48px;
}
.login-aside::before {
  content: '';
  position: absolute;
  width: 460px;
  height: 460px;
  right: -120px;
  top: -120px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(129, 140, 248, 0.35) 0%, transparent 70%);
}
.login-aside::after {
  content: '';
  position: absolute;
  width: 360px;
  height: 360px;
  left: -100px;
  bottom: -100px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(99, 102, 241, 0.25) 0%, transparent 70%);
}
.aside-inner {
  position: relative;
  z-index: 2;
  max-width: 460px;
  color: #fff;
}
.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 56px;
}
.brand-mark {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.16);
  border: 1.5px solid rgba(255, 255, 255, 0.5);
  position: relative;
}
.brand-mark::after {
  content: '';
  position: absolute;
  inset: 9px;
  border: 2px solid #fff;
  border-radius: 4px;
  border-bottom-color: transparent;
  border-left-color: transparent;
}
.brand-text {
  font-size: 19px;
  font-weight: 700;
  letter-spacing: 0.4px;
}
.aside-title {
  font-size: 38px;
  line-height: 1.25;
  font-weight: 800;
  margin: 0 0 20px;
}
.grad {
  background: linear-gradient(90deg, #A5B4FC, #C7D2FE);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}
.aside-desc {
  font-size: 15px;
  line-height: 1.8;
  color: rgba(226, 232, 240, 0.82);
  margin: 0 0 32px;
}
.feature-list {
  list-style: none;
  padding: 0;
  margin: 0 0 48px;
  display: grid;
  gap: 14px;
}
.feature-list li {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14.5px;
  color: rgba(241, 245, 249, 0.92);
}
.feature-list .el-icon {
  color: #34D399;
  font-size: 18px;
  flex-shrink: 0;
}
.aside-footer {
  font-size: 12.5px;
  color: rgba(203, 213, 225, 0.6);
}

/* ===== 右侧表单 ===== */
.login-main {
  width: 480px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px;
}
.login-box {
  width: 100%;
  max-width: 380px;
}
.login-header {
  margin-bottom: 32px;
}
.login-header h2 {
  font-size: 26px;
  font-weight: 700;
  color: #0F172A;
  margin: 0 0 8px;
}
.login-header p {
  font-size: 14px;
  color: #64748B;
  margin: 0;
}

.register-switch {
  text-align: center;
  margin: -6px 0 10px;
  font-size: 13px;
}
.register-switch a {
  color: var(--el-color-primary);
  cursor: pointer;
}
.login-disclaimer {
  margin-top: 28px;
  padding-top: 18px;
  border-top: 1px solid #E2E8F0;
  font-size: 11.5px;
  line-height: 1.7;
  color: #94A3B8;
  text-align: justify;
}

.forgot-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}
.forgot-row .el-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

:deep(.el-form-item) {
  margin-bottom: 22px;
}
:deep(.el-input__wrapper) {
  border-radius: 10px;
  padding: 4px 12px;
}
:deep(.el-input--large .el-input__wrapper) {
  min-height: 46px;
}

/* ===== 响应式 ===== */
@media (max-width: 900px) {
  .login-aside {
    display: none;
  }
  .login-main {
    width: 100%;
  }
}
</style>
