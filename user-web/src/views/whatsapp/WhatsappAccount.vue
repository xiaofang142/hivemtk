<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="card-header">
          WhatsApp 账号管理
        </div>
      </template>
      <el-alert
        class="provider-alert"
        type="warning"
        :closable="false"
        show-icon
        title="个人版扫码登录已下线（底层 gowa 已停止维护）"
        description="新账号请使用 WhatsApp Cloud API（PhoneID + Token）接入；存量个人版会话仅保留只读记录。"
      >
        <el-button
          size="small"
          type="primary"
          link
          @click="$router.push('/whatsapp-cloud')"
        >
          前往 WhatsApp Cloud
        </el-button>
      </el-alert>
      <el-form
        :inline="true"
        @submit.prevent
      >
        <el-form-item label="名称">
          <el-input
            v-model="form.name"
            placeholder="账号名称"
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.remark"
            placeholder="备注"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="createAccount"
          >
            添加账号
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="accounts"
        style="width:100%"
      >
        <el-table-column
          prop="name"
          label="名称"
        />
        <el-table-column
          prop="remark"
          label="备注"
        />
        <el-table-column
          prop="status"
          label="状态"
        />
        <el-table-column
          label="操作"
          width="340"
        >
          <template #default="{ row }">
            <el-button
              size="small"
              @click="refreshStatus(row)"
            >
              状态
            </el-button>
            <el-button
              size="small"
              type="primary"
              @click="openBindingDialog(row)"
            >
              绑定AI
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <AgentBindingDialog
      v-model="bindingDialogVisible"
      channel-type="whatsapp"
      :account-id="bindingAccountId"
      :account-label="bindingAccountLabel"
      :account-enabled="true"
    />

    <el-dialog
      v-model="qrDialog"
      title="扫码登录"
      width="360px"
    >
      <div
        v-if="qrCode"
        class="qr-box"
      >
        <img
          :src="qrImageSrc"
          alt="QR"
          width="300"
          height="300"
        >
      </div>
      <div v-else>
        等待二维码生成...
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import api from '@/api/whatsapp'
import { ElMessage } from 'element-plus'
import { toList } from '@/utils/list'
import { computed } from 'vue'
import { isUserReportedError } from '@/utils/errorReporting'

const accounts = ref([])
const loading = ref(false)
const form = ref({ name: '', remark: '' })
const qrDialog = ref(false)
const qrCode = ref('')
const qrImageSrc = computed(() => qrCode.value ? `https://api.qrserver.com/v1/create-qr-code/?size=300x300&data=${encodeURIComponent(qrCode.value)}` : '')

onMounted(() => { loadAccounts() })

function loadAccounts() {
  loading.value = true
  api.listAccounts().then(res => {
    accounts.value = toList(res)
  }).finally(() => loading.value = false)
}

function createAccount() {
  if (!form.value.name) { ElMessage.error(i18n.global.t('请输入名称')); return }
  api.createAccount(form.value).then(() => {
    ElMessage.success(i18n.global.t('创建成功'))
    form.value = { name: '', remark: '' }
    loadAccounts()
  })
}

function refreshStatus(row) {
  api.loginStatus(row.id).then(res => {
    if (res.logged_in) {
      ElMessage.success(i18n.global.t('登录成功'))
      loadAccounts()
    } else {
      qrCode.value = res.qr || ''
      if (qrCode.value) qrDialog.value = true
      else ElMessage.info('未登录：个人版扫码已下线，请到「WhatsApp Cloud」配置 PhoneID + Token')
    }
  }).catch((e) => {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '获取登录状态失败')
  })
}

const bindingDialogVisible = ref(false);
const bindingAccountId = ref(0)
const bindingAccountLabel = ref('')
function openBindingDialog(row) {
  bindingAccountId.value = row.id
  bindingAccountLabel.value = row.name || row.remark || `#${row.id}`
  bindingDialogVisible.value = true
}
</script>

<style scoped>
.page { padding: 20px; }
.card-header { font-weight: 600; }
.provider-alert { margin-bottom: 16px; }
.qr-box { display:flex; justify-content:center; }
</style>
