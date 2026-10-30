<template>
  <div class="profile-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>{{ $t('个人资料') }}</h2>
          <p class="subtitle">
            {{ $t('查看与维护当前登录账号的基础信息') }}
          </p>
        </div>
        <div class="header-actions">
          <el-button
            :loading="loading"
            @click="loadProfile"
          >
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
        </div>
      </div>
    </el-card>

    <el-row
      v-loading="loading"
      :gutter="20"
    >
      <el-col
        :xs="24"
        :sm="10"
        :md="8"
      >
        <el-card
          class="avatar-card"
          shadow="never"
        >
          <div class="avatar-wrap">
            <el-avatar
              :size="96"
              class="avatar"
            >
              {{ initial }}
            </el-avatar>
            <div class="role-tag">
              <el-tag
                :type="roleTagType"
                effect="dark"
                size="small"
              >
                {{ roleLabel }}
              </el-tag>
            </div>
            <h3 class="username">
              {{ form.username || '-' }}
            </h3>
            <p class="user-id">
              ID: {{ form.id || '-' }}
            </p>
          </div>
        </el-card>
      </el-col>

      <el-col
        :xs="24"
        :sm="14"
        :md="16"
      >
        <el-card shadow="never">
          <template #header>
            <span>{{ $t('基础信息') }}</span>
          </template>
          <el-form
            ref="formRef"
            :model="form"
            :rules="rules"
            label-width="100px"
            label-position="right"
          >
            <el-form-item
              label="登录账号"
              prop="username"
            >
              <el-input
                v-model="form.username"
                disabled
              />
            </el-form-item>
            <el-form-item
              label="真实姓名"
              prop="real_name"
            >
              <el-input
                v-model="form.real_name"
                placeholder="请输入真实姓名"
                maxlength="32"
              />
            </el-form-item>
            <el-form-item
              label="邮箱"
              prop="email"
            >
              <el-input
                v-model="form.email"
                placeholder="请输入邮箱"
                maxlength="64"
              />
            </el-form-item>
            <el-form-item
              label="手机号"
              prop="phone"
            >
              <el-input
                v-model="form.phone"
                placeholder="请输入手机号"
                maxlength="20"
              />
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
              <span class="readonly-hint">下次登录时填在「租户代码」栏即可</span>
            </el-form-item>
            <el-form-item
              v-if="tenantMode && tenantInfo.name"
              label="租户名称"
            >
              <span class="readonly-text">{{ tenantInfo.name }}</span>
            </el-form-item>
            <el-form-item label="角色">
              <el-tag :type="roleTagType">
                {{ roleLabel }}
              </el-tag>
              <span class="readonly-hint">角色由系统根据初始化/分配决定，暂不支持自助修改</span>
            </el-form-item>
            <el-form-item label="状态">
              <el-tag
                v-if="form.status === 1"
                type="success"
              >
                启用
              </el-tag>
              <el-tag
                v-else
                type="info"
              >
                已停用
              </el-tag>
            </el-form-item>
            <el-form-item label="创建时间">
              <span class="readonly-text">{{ form.created_at || '-' }}</span>
            </el-form-item>
            <el-form-item label="最后登录">
              <span class="readonly-text">{{ form.last_login_at || '尚未记录' }}</span>
            </el-form-item>
            <el-form-item>
              <el-button
                type="primary"
                :loading="saving"
                @click="onSave"
              >
                <el-icon><Check /></el-icon>
                保存修改
              </el-button>
              <el-button @click="loadProfile">
                重置
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>

        <el-card
          class="password-card"
          shadow="never"
        >
          <template #header>
            <span>修改密码</span>
          </template>
          <el-alert
            type="info"
            :closable="false"
            show-icon
            style="margin-bottom: 16px"
          >
            修改密码后需要使用新密码重新登录。
          </el-alert>
          <el-form
            ref="pwdFormRef"
            :model="pwdForm"
            :rules="pwdRules"
            label-width="100px"
            label-position="right"
          >
            <el-form-item
              label="当前密码"
              prop="oldPassword"
            >
              <el-input
                v-model="pwdForm.oldPassword"
                type="password"
                show-password
                placeholder="请输入当前密码"
              />
            </el-form-item>
            <el-form-item
              label="新密码"
              prop="newPassword"
            >
              <el-input
                v-model="pwdForm.newPassword"
                type="password"
                show-password
                placeholder="至少 8 位，含大小写字母+数字"
              />
            </el-form-item>
            <el-form-item
              label="确认新密码"
              prop="confirmPassword"
            >
              <el-input
                v-model="pwdForm.confirmPassword"
                type="password"
                show-password
                placeholder="再次输入新密码"
                @keyup.enter="onChangePassword"
              />
            </el-form-item>
            <el-form-item>
              <el-button
                type="warning"
                :loading="pwdSaving"
                @click="onChangePassword"
              >
                <el-icon><Lock /></el-icon>
                修改密码
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, Lock, CopyDocument } from '@element-plus/icons-vue'
import { http } from '@/utils/request'
import { isUserReportedError } from '@/utils/errorReporting'
import { TenantApi } from '@/api/tenant'
import { useUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'

const userStore = useUserStore()
const permissionStore = usePermissionStore()
void Check; void Lock

const loading = ref(false)
const saving = ref(false)
const pwdSaving = ref(false)

const formRef = ref(null)
const pwdFormRef = ref(null)

const form = reactive({
  id: '',
  username: '',
  real_name: '',
  email: '',
  phone: '',
  role: '',
  status: 1,
  created_at: '',
  last_login_at: ''
})

const pwdForm = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

// 租户信息：登录页「租户代码」要填的那串短码。只在 SaaS 多租户形态下展示——
// 私域独立部署登录页没有这个输入框，显示短码只会让人去找一个不存在的概念。
const tenantMode = ref(false)
const tenantInfo = reactive({
  tenant_id: '',
  tenant_code: '',
  name: '',
  status: ''
})

const loadTenantInfo = async () => {
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
    // 单租户/私有化部署没有对应租户行时后端会报「租户不存在」，属预期情况，静默即可
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

const initial = computed(() => (form.username || '?').charAt(0).toUpperCase())

const roleLabel = computed(() => {
  const r = form.role || userStore.role || ''
  // 「商户管理员」是 SaaS 形态的称呼；私域独立部署没有商户概念，同一角色叫「管理员」。
  if (r === 'admin') return tenantMode.value ? '商户管理员' : '管理员'
  if (r === 'manager') return '销售主管'
  if (r === 'sales') return '一线销售'
  if (r === 'viewer') return '数据分析师'
  return r || '未设置'
})

const roleTagType = computed(() => {
  const r = form.role || userStore.role
  if (r === 'admin') return 'danger'
  if (r === 'manager') return 'warning'
  if (r === 'sales') return 'success'
  return 'info'
})

const rules = {
  real_name: [{ max: 32, message: i18n.global.t('长度不能超过 32 字符'), trigger: 'blur' }],
  email: [{
    validator: (_rule, value, cb) => {
      if (!value) return cb()
      if (!/^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(value)) {
        return cb(new Error('邮箱格式不正确'))
      }
      cb()
    },
    trigger: 'blur'
  }],
  phone: [{
    validator: (_rule, value, cb) => {
      if (!value) return cb()
      if (!/^1[3-9]\d{9}$/.test(value)) {
        return cb(new Error('手机号格式不正确'))
      }
      cb()
    },
    trigger: 'blur'
  }]
}

const pwdRules = {
  oldPassword: [{ required: true, message: i18n.global.t('请输入当前密码'), trigger: 'blur' }],
  newPassword: [
    { required: true, message: i18n.global.t('请输入新密码'), trigger: 'blur' },
    { min: 8, max: 64, message: i18n.global.t('密码长度 8-64 位'), trigger: 'blur' },
    {
      validator: (_rule, value, cb) => {
        if (!value) return cb()
        if (!/[a-z]/.test(value)) return cb(new Error('需包含小写字母'))
        if (!/[A-Z]/.test(value)) return cb(new Error('需包含大写字母'))
        if (!/\d/.test(value)) return cb(new Error('需包含数字'))
        cb()
      },
      trigger: 'blur'
    }
  ],
  confirmPassword: [
    { required: true, message: i18n.global.t('请再次输入新密码'), trigger: 'blur' },
    {
      validator: (_rule, value, cb) => {
        if (value !== pwdForm.newPassword) {
          return cb(new Error('两次输入的密码不一致'))
        }
        cb()
      },
      trigger: 'blur'
    }
  ]
}

const loadProfile = async () => {
  loading.value = true
  try {
    const [resp] = await Promise.all([
      http.get('/api/auth/current-user'),
      loadTenantInfo()
    ])
    const data = resp || {}
    Object.assign(form, {
      id: data.id || '',
      username: data.username || '',
      real_name: data.real_name || data.realName || '',
      email: data.email || '',
      phone: data.phone || data.mobile || '',
      role: data.role || '',
      status: data.status ?? 1,
      created_at: data.created_at || data.createdAt || '',
      last_login_at: data.last_login_at || data.lastLoginAt || ''
    })
    userStore.setUserInfo({
      id: data.id,
      username: data.username,
      email: data.email,
      role: data.role
    });
  } catch (err) {
    ElMessage.error('加载个人资料失败：' + (err?.message || err))
  } finally {
    loading.value = false
  }
};

const onSave = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
    saving.value = true
    await http.put(`/api/users/${form.id}`, {
      real_name: form.real_name,
      email: form.email,
      phone: form.phone
    })
    ElMessage.success(i18n.global.t('保存成功'))
  } catch (err) {
    if (err?.message) {
      ElMessage.error('保存失败：' + err.message)
    }
  } finally {
    saving.value = false
  }
};

const onChangePassword = async () => {
  if (!pwdFormRef.value) return
  try {
    await pwdFormRef.value.validate()
    try {
      await ElMessageBox.confirm(
        '修改密码后将退出当前登录，需要使用新密码重新登录。',
        '确认修改密码',
        { type: 'warning', confirmButtonText: '确认修改', cancelButtonText: '取消' }
      )
    } catch {
      return;
    }
    pwdSaving.value = true
    await http.post('/api/auth/change-password', {
      old_password: pwdForm.oldPassword,
      new_password: pwdForm.newPassword
    })
    ElMessage.success(i18n.global.t('密码修改成功，即将退出登录'))
    setTimeout(() => {
      permissionStore.resetMenus()
      userStore.logout()
      window.location.href = '/#/login'
    }, 1200)
  } catch (err) {
    // 拦截器已把后端文案弹给用户（isUserReportedError），这里不再补一条重复 toast
    if (err?.message && !isUserReportedError(err)) {
      ElMessage.error('修改失败：' + err.message)
    }
  } finally {
    pwdSaving.value = false
  }
};

onMounted(() => {
  loadProfile()
})
</script>

<style scoped>
.profile-page {
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
.avatar-card {
  text-align: center;
}
.avatar-wrap {
  padding: 20px 0;
}
.avatar {
  background: linear-gradient(135deg, #667eea, #764ba2);
  color: #fff;
  font-size: 36px;
  font-weight: 600;
}
.role-tag {
  margin: 12px 0 8px;
}
.username {
  margin: 8px 0 4px;
  font-size: 18px;
  color: #303133;
}
.user-id {
  margin: 0;
  color: #909399;
  font-size: 12px;
}
.password-card {
  margin-top: 16px;
}
.readonly-hint {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
.readonly-text {
  color: #606266;
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
</style>
