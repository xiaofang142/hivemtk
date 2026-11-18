<template>
  <div class="email-settings">
    <div class="toolbar">
      <el-button
        type="primary"
        @click="handleAdd"
      >
        <el-icon-plus /> {{ $t('新增邮件代理') }}
      </el-button>
    </div>

    
    <el-table
      :data="tableData"
      border
      stripe
      style="width: 100%"
    >
      <el-table-column
        prop="name"
        :label="$t('代理名称')"
        width="300"
      />
      <el-table-column
        prop="server"
        :label="$t('服务器地址')"
        width="200"
      />
      <el-table-column
        prop="port"
        :label="$t('端口')"
        width="100"
      />
      <el-table-column
        prop="limit"
        :label="$t('代理日限制')"
        width="100"
      />
      <el-table-column
        prop="password"
        :label="$t('密码')"
      />
      <el-table-column
        prop="username"
        :label="$t('用户名')"
      />
      <el-table-column
        :label="$t('操作')"
        width="200"
        align="center"
      >
        <template #default="scope">
          <el-button
            size="small"
            @click="handleEdit(scope.row)"
          >
            {{ $t('编辑') }}
          </el-button>
          <el-button
            size="small"
            type="danger"
            @click="handleDelete(scope.row.id)"
          >
            {{ $t('删除') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="600px"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="120px"
      >
        <el-form-item
          label="代理名称"
          prop="name"
        >
          <el-input
            v-model="form.name"
            placeholder="请输入代理名称"
          />
        </el-form-item>
        <el-form-item
          label="服务器地址"
          prop="server"
        >
          <el-input
            v-model="form.server"
            placeholder="请输入SMTP服务器地址"
          />
        </el-form-item>
        <el-form-item
          label="端口"
          prop="port"
        >
          <el-input
            v-model.number="form.port"
            placeholder="请输入端口号"
            type="number"
          />
        </el-form-item>
        <el-form-item
          label="用户名"
          prop="username"
        >
          <el-input
            v-model="form.username"
            placeholder="请输入用户名"
          />
        </el-form-item>
        <el-form-item
          label="密码"
          prop="password"
        >
          <el-input
            v-model="form.password"
            placeholder="请输入密码"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item
          label="代理日限制"
          prop="limit"
        >
          <el-input
            v-model="form.limit"
            placeholder="请输入代理日限制"
            type="number"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitForm"
        >
          确定
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { Plus as _Plus } from '@element-plus/icons-vue';

import { emailApi } from '@/api/email';

const tableData = ref([]);

const fetchEmailSmtp = async () => {
  const res = await emailApi.getEmailSmtpList()
  tableData.value = res.list || [];
};

onMounted(() => {
  fetchEmailSmtp()
})


const dialogVisible = ref(false);
const dialogTitle = ref('新增邮件代理');
const formRef = ref(null);

const form = reactive({
  id: '',
  name: '',
  server: '',
  port: '',
  username: '',
  password: '',
  limit: 50
});

const rules = {
  name: [{ required: true, message: i18n.global.t('请输入代理名称'), trigger: 'blur' }],
  server: [{ required: true, message: i18n.global.t('请输入服务器地址'), trigger: 'blur' }],
  port: [
    { required: true, message: i18n.global.t('请输入端口号'), trigger: 'blur' },
    { type: 'number', message: i18n.global.t('端口号必须为数字'), trigger: 'blur' }
  ],
  username: [{ required: true, message: i18n.global.t('请输入用户名'), trigger: 'blur' }],
  password: [{ required: true, message: i18n.global.t('请输入密码'), trigger: 'blur' }],
  limit: [{ required: true, message: i18n.global.t('请输入代理日限制'), trigger: 'blur' }],
};

const handleAdd = () => {
  dialogTitle.value = '新增邮件代理';
  Object.assign(form, { id: '', name: '', server: '', port: '', username: '', password: '', limit: 50 });
  dialogVisible.value = true;
};

const handleEdit = (row) => {
  dialogTitle.value = '编辑邮件代理';
  Object.assign(form, { ...row });
  dialogVisible.value = true;
};

const handleDelete = (id) => {
  ElMessageBox.confirm('确定要删除该邮件代理吗？', '警告', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  }).then( async () => {
      try {
        const _response = await emailApi.deleteEmailSmtp(id);
        ElMessage({
          type: 'success',
          message: i18n.global.t('删除成功'),
        });
        fetchEmailSmtp()
      } catch (error) {
        console.error('删除账号失败:', error)
        ElMessage.error(i18n.global.t('删除账号失败'))
      }
  }).catch((e) => {
    if (e !== 'cancel' && e !== 'close')
      throw e;
  });
};

const submitForm = () => {
  form.limit = Number(form.limit)
  formRef.value.validate(async (valid) => {
    if (valid) {
      const isEdit = !!form.id
      try {
        if (isEdit) {
          await emailApi.updateEmailSmtp(form.id, form)
        } else {
          await emailApi.addEmailSmtp(form)
        }
      } catch {
        // 失败原因 request.js 已经弹过；validate() 的回调是异步的，这里的拒绝
        // 不会回到任何调用方，逸出去就是控制台一条未捕获错误 + 一次假"没反应"。
        return
      }
      ElMessage.success(isEdit ? i18n.global.t('更新成功') : i18n.global.t('新增成功'))
      dialogVisible.value = false
      fetchEmailSmtp()
    }
  });
};
</script>

<style scoped>
.toolbar {
  margin-bottom: 16px;
  display: flex;
  justify-content: flex-end;
}

.email-settings {
  padding: 10px;
}
</style>
