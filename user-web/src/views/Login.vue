<template>
  <div class="login-container">
    <div class="login-aside">
      <div class="aside-inner">
        <div class="brand">
          <span class="brand-mark" />
          <span class="brand-text">{{ t('system.appName') }}</span>
        </div>

        <h1 class="aside-title">
          一个智能体<br>
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

        <div class="aside-footer">
          © 2026 营销智能体套件 · HiveMtk
        </div>
      </div>
    </div>

    
    <div class="login-main">
      <div class="login-box">
        <div class="login-header">
          <h2>{{ t('core.login.welcome') }}</h2>
          <p>{{ t('system.appName') }}</p>
        </div>

        <el-form
          v-if="!mfaMode"
          ref="loginFormRef"
          :model="loginForm"
          :rules="rules"
          @keyup.enter="handleLogin"
        >
          <el-form-item prop="username">
            <el-input
              v-model="loginForm.username"
              :placeholder="t('core.login.username')"
              prefix-icon="User"
              size="large"
            />
          </el-form-item>

          <el-form-item
            v-if="tenantMode"
            ref="tenantCodeItemRef"
            prop="tenant_code"
          >
            <el-input
              v-model="loginForm.tenant_code"
              placeholder="租户代码（如：t42xk，可不填）"
              prefix-icon="OfficeBuilding"
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
              @keyup.enter.stop.prevent="handleLogin"
            />
          </el-form-item>

          <el-form-item>
            <el-button
              type="primary"
              size="large"
              style="width: 100%"
              :loading="loading"
              @click="handleLogin"
            >
              {{ t('core.login.submit') }}
            </el-button>
          </el-form-item>

          <!-- 注册走独立页面（/register）。
               此前内联在本页的注册表单只收 用户名/密码/邮箱，而开户登记还必须收
               公司名称/负责人/电话（监管必需项）。两条提交路径同时存在的结果是
               「从登录页注册的人 100% 被 4304 拒绝，而后端一条错都不报」——
               属于只改后端不改编排入口必然出现的断链。
               现在只有一个注册入口，字段口径与后端校验同源。 -->
          <div class="register-switch">
            <el-button
              v-if="enableRegister"
              link
              type="primary"
              @click="goRegister"
            >
              没有账号？注册商户账号
            </el-button>
          </div>
        </el-form>

        <!-- MFA 第二步：密码校验已通过（后端返回 need_mfa + temp_token），
             凭证表单整体让位给验证码输入；temp_token 5 分钟有效，过期回登录重来 -->
        <el-form
          v-else
          @keyup.enter="handleMfaVerify"
        >
          <el-form-item>
            <el-input
              v-model="mfaCode"
              maxlength="6"
              inputmode="numeric"
              placeholder="请输入 6 位验证码"
              prefix-icon="Key"
              size="large"
            />
          </el-form-item>

          <el-form-item>
            <el-button
              type="primary"
              size="large"
              style="width: 100%"
              :loading="loading"
              @click="handleMfaVerify"
            >
              验证并登录
            </el-button>
          </el-form-item>

          <div class="register-switch">
            <el-button
              link
              type="primary"
              @click="backToLogin"
            >
              返回登录
            </el-button>
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
import { ref, reactive, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
import { usersApi } from '@/api/users'
import i18n from '@/i18n'

const t = i18n.global.t

const router = useRouter()
const route = useRoute()
const userStore = useUserStore()
const permissionStore = usePermissionStore()
const loginFormRef = ref(null)
// 4108（用户名跨租户歧义）时用来把焦点送进租户代码框的锚点
const tenantCodeItemRef = ref(null)
const loading = ref(false)

// MFA 第二步状态机：mfaMode=false 走凭证登录；登录响应带 need_mfa + temp_token 时
// 切到 mfaMode=true，凭证表单隐藏、只留 6 位验证码输入；「返回登录」清态回凭证模式。
const mfaMode = ref(false)
const mfaTempToken = ref('')
const mfaCode = ref('')

// 登录只需账号 + 密码。email 字段随内联注册表单一起去掉了
// （注册已独立成页），留着会让读代码的人以为登录还支持邮箱入口。
const loginForm = reactive({
  username: '',
  password: '',
  tenant_code: ''
});

// 自助注册：开关由公开配置 /api/public/config 提供（未登录可取）
const enableRegister = ref(false)
// 租户模式：为 true 时登录页展示可选的「租户代码」输入（单租户部署下隐藏）
const tenantMode = ref(false)

const goRegister = () => {
  router.push({ path: '/register' })
}

import { http as httpApi } from '@/utils/request'
httpApi.get('/api/public/config').then((res) => {
  const data = res?.data ?? res
  enableRegister.value = !!data?.enable_register
  tenantMode.value = !!data?.tenant_mode
}).catch(() => { enableRegister.value = false })

const rules = {
  username: [
    { required: true, message: t('core.login.pleaseUsername'), trigger: 'blur' }
  ],
  password: [
    { required: true, message: t('core.login.pleasePassword'), trigger: 'blur' }
  ]
};

// finishLogin 登录终态的统一收尾（普通登录与 MFA 第二步共用）：
// 落库用户信息 + 重置菜单缓存 + 跳首页
const finishLogin = async (token, user, resp) => {
  // 租户模式下登录响应会带 tenant_id/tenant_code（顶层；也可能在 user 内），
  // 合并后随用户信息一起落库（setUserInfo 对缺省字段兜底为空串）
  userStore.login({
    ...user,
    tenant_id: resp.tenant_id ?? user.tenant_id,
    tenant_code: resp.tenant_code ?? user.tenant_code
  }, token)
  // RBAC：切换账号时重置旧菜单缓存，避免上一个身份的菜单/路径继续生效（数据与权限隔离）
  permissionStore.resetMenus()
  // 菜单故障由路由守卫处理，不把已完成的凭据校验报成登录失败。
  await permissionStore.ensureMenus(true).catch(() => {})
  if (!userStore.isLoggedIn) return
  localStorage.setItem('system_initialized', 'true')
  ElMessage.success(t('core.login.success'))
  // 登录后回跳：?redirect= 只接受站内绝对路径（防开放重定向）。
  // 市场详情的登录引导、下单拦截都会带 redirect 回来，缺了它用户会被丢去首页。
  const redirect = route.query.redirect
  if (typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')) {
    router.push(redirect)
  } else {
    router.push('/')
  }
}

// 返回登录：退出 MFA 模式，清掉 temp_token 与验证码，回凭证表单
const backToLogin = () => {
  mfaMode.value = false
  mfaTempToken.value = ''
  mfaCode.value = ''
}

// handleMfaVerify MFA 第二步：POST /api/auth/mfa/verify（公开端点，temp_token 换终令牌）
const handleMfaVerify = async () => {
  if (loading.value) return
  const code = String(mfaCode.value || '').trim()
  if (!/^\d{6}$/.test(code)) {
    ElMessage.warning('请输入 6 位数字验证码')
    return
  }
  if (!mfaTempToken.value) {
    // temp_token 丢失（刷新页面等）：无法继续第二步，回登录重来
    backToLogin()
    return
  }

  try {
    loading.value = true
    const response = await usersApi.mfaVerify({ temp_token: mfaTempToken.value, code })
    const token = response?.token || response?.Token
    const user = response?.user || response?.User
    if (!token || !user) {
      console.error('MFA 验证响应数据异常:', response)
      ElMessage.error(t('core.login.failServer'))
      return
    }
    await finishLogin(token, user, response)
  } catch (error) {
    // 验证码错/临时令牌失效是 HTTP 401：未登录时拦截器的 401 分支不弹 toast
    // （没有会话可清），错误信息必须在此提示
    console.error('MFA 验证失败:', error)
    ElMessage.error(error.message || t('core.login.fail'))
  } finally {
    loading.value = false
  }
}

const handleLogin = async () => {
  if (loading.value || !loginFormRef.value) return
  loading.value = true

  // 校验不通过时 validate() 是 reject 一个字段集合，不是 Error：
  // 它和"口令错/服务端挂了"必须分开。此前一起落进下面的 catch，空表单点登录会同时
  // 打出一条 console.error 和一个"登录失败"红条——行内已经写了"请输入用户名"，
  // 再弹一次失败是把用户的漏填报成了系统故障。
  const passed = await loginFormRef.value.validate().catch(() => false)
  if (!passed) {
    loading.value = false
    return
  }

  try {
    // 空租户代码不发给后端：后端把「缺省」视为单租户默认，'' 与未传等价，但省掉字段更干净
    const payload = { ...loginForm }
    if (!payload.tenant_code) delete payload.tenant_code

    // 用户名/邮箱同框：含 @ 视为邮箱通道（email 全局唯一）。跨租户同名登录会被后端
    // 以 4108 拒绝并要求"改用邮箱登录"——同一输入框按内容分流，用户才能照做。
    const account = String(payload.username || '').trim()
    if (account.includes('@')) {
      delete payload.username
      payload.email = account
    } else {
      payload.username = account
    }

    const response = await usersApi.login(payload)

    if (response) {
      // MFA 用户：后端返回 need_mfa + temp_token（无终令牌）——切到验证码模式走第二步
      const needMFA = response.need_mfa ?? response.NeedMFA
      const tempToken = response.temp_token || response.TempToken
      if (needMFA && tempToken) {
        mfaTempToken.value = tempToken
        mfaCode.value = ''
        mfaMode.value = true
        return
      }

      const token = response.token || response.Token
      const user = response.user || response.User

      if (token && user) {
        await finishLogin(token, user, response)
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
    // 4108 CodeTenantAmbiguousUsername：跨租户同名用户无法唯一定位租户，后端要求
    // 改用邮箱或带租户代码——用户体感是「租户代码必选」，这里把文案改成可操作指引，
    // 并把焦点送进租户代码框（不改视觉体系，只 focus）。
    const bizCode = String(error?.bizCode ?? error?.response?.data?.code ?? '')
    if (bizCode === '4108') {
      ElMessage.error('该用户名在多个租户下重复：请输入上方租户代码，或直接用注册邮箱登录')
      if (tenantMode.value) {
        nextTick(() => {
          tenantCodeItemRef.value?.$el?.querySelector('input')?.focus()
        })
      }
    } else {
      ElMessage.error(error.message || t('core.login.fail'))
    }
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
  /* FE-13：#94A3B8 对白底仅约 2.9:1，不达 WCAG AA（4.5:1）；换 slate-500（≈4.76:1） */
  color: #64748B;
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
