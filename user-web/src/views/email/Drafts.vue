<template>
  <div class="email-drafts">
    <div class="drafts-header">
      <h1>{{ $t('邮件草稿箱') }}</h1>
      <div>
        <el-button @click="$router.push('/email/drag-editor')">
          <el-icon-edit /> {{ $t('拖拽编辑器') }}
        </el-button>
        <el-button
          type="primary"
          @click="handleCreateDraft"
        >
          <el-icon-plus /> {{ $t('新建草稿') }}
        </el-button>
      </div>
    </div>
    
    <el-table
      v-if="filteredDrafts.length > 0"
      :data="filteredDrafts"
      style="width: 100%"
      border
      stripe
      @row-dblclick="handleEditDraft"
    >
      <el-table-column
        type="selection"
        width="55"
      />
      <el-table-column
        prop="subject"
        :label="$t('主题')"
        min-width="200"
      />
      <el-table-column
        :label="$t('操作')"
        width="300"
        fixed="right"
      >
        <template #default="scope">
          <el-button
            size="small"
            @click="handleEditDraft(scope.row)"
          >
            {{ $t('编辑') }}
          </el-button>
          <el-button
            size="small"
            type="danger"
            @click="handleDeleteDraft(scope.row.id)"
          >
            {{ $t('删除') }}
          </el-button>
          <el-button
            size="small"
            type="primary"
            @click="handleSend(scope.row)"
          >
            {{ $t('发送') }}
          </el-button> 
        </template>
      </el-table-column>
    </el-table>

    <div
      v-else
      class="empty-drafts"
    >
      <el-empty description="暂无草稿数据" />
    </div>

    
    <el-dialog 
      v-model="dialogVisible"
      :title="dialogType === 'create' ? '新建草稿' : '编辑草稿'"
      width="70%"
      :before-close="handleClose"
    >
      <p>
        支持自定义变量有 {name} {city} {address} {account} 
      </p>
      
      <el-form
        ref="draftFormRef"
        :model="draftForm"
        :rules="formRules"
        label-width="80px"
      >
        <el-form-item
          label="主题"
          prop="subject"
        >
          <el-input
            v-model="draftForm.subject"
            placeholder="请输入邮件主题"
          />
        </el-form-item>

        
        <el-form-item
          label="内容"
          prop="content"
        >
          <SimpleEditor
            v-model="draftForm.content"
            :disabled="false"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button
            type="primary"
            @click="submitDraft"
          >保存草稿</el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog
      v-model="sendDialogVisible"
      title="发送草稿"
      width="560px"
    >
      <el-form
        ref="sendFormRef"
        :model="sendForm"
        :rules="sendRules"
        label-width="90px"
      >
        <el-form-item label="主题">
          <el-input
            :model-value="sendForm.subject"
            disabled
          />
        </el-form-item>
        <el-form-item
          label="收件人"
          prop="to"
        >
          <el-input
            v-model="sendForm.to"
            placeholder="收件人邮箱，多个用逗号分隔"
          />
        </el-form-item>
        <el-form-item
          label="发件账号"
          prop="smtpId"
        >
          <el-select
            v-model="sendForm.smtpId"
            placeholder="请选择邮件代理"
            style="width: 100%"
          >
            <el-option
              v-for="item in smtpList"
              :key="item.id"
              :label="item.name ? `${item.name} (${item.username})` : (item.username || item.server || item.id)"
              :value="String(item.id)"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="sendDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="sending"
          @click="submitSend"
        >
          发送
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import { emailApi } from '@/api/email'
import SimpleEditor from '@/components/SimpleEditor/index.vue'

const drafts = ref([]);
const dialogVisible = ref(false);
const dialogType = ref('create');
const currentDraftId = ref(null);
const draftFormRef = ref(null);
const fileList = ref([]);

const draftForm = reactive({
  id: '',
  subject: '',
  content: '',
  attachments: []
});

const sendDialogVisible = ref(false);
const sendFormRef = ref(null);
const sending = ref(false);
const smtpList = ref([]);
const sendForm = reactive({
  id: '',
  subject: '',
  content: '',
  attachments: [],
  to: '',
  smtpId: ''
});

const validateRecipients = (rule, value, callback) => {
  const list = String(value || '').split(/[,;\s]+/).map(s => s.trim()).filter(Boolean)
  if (!list.length) {
    callback(new Error('请输入收件人'))
    return
  }
  const bad = list.find(addr => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(addr))
  if (bad) {
    callback(new Error(`收件人格式不正确：${bad}`))
    return
  }
  callback()
}

const sendRules = {
  to: [{ required: true, validator: validateRecipients, trigger: 'blur' }],
  smtpId: [{ required: true, message: '请选择发件账号', trigger: 'change' }]
};


const formRules = {
  subject: [
    { required: true, message: i18n.global.t('请输入邮件主题'), trigger: 'blur' },
    { max: 100, message: i18n.global.t('主题长度不能超过100个字符'), trigger: 'blur' }
  ],
  content: [
    { required: true, message: i18n.global.t('请输入邮件内容'), trigger: 'blur' }
  ]
};

const filteredDrafts = computed(() => {
  return drafts.value
});

const handleClose = (done) => {
  dialogVisible.value = false
  if (typeof done === 'function') done()
}

const fetchDrafts = async () => {
  try {
    const response = await emailApi.getDrafts()
    drafts.value = response.list || [];
  } catch (error) {
    console.error('获取草稿列表失败:', error)
    ElMessage.error(i18n.global.t('获取草稿列表失败'))
  }
};


const handleCreateDraft = () => {
  dialogType.value = 'create'
  currentDraftId.value = null
  Object.assign(draftForm, {
    id: '',
    subject: '',
    content: '',
    attachments: []
  });
  fileList.value = []
  dialogVisible.value = true
};

const handleEditDraft = async (row) => {
  dialogType.value = 'edit'
  currentDraftId.value = row.id
  try {
    const response = await emailApi.getDraftDetail(row.id)
    const draft = response;
    Object.assign(draftForm, {
      id: draft.id,
      subject: draft.subject,
      content: draft.content,
      attachments: draft.attachments || []
    })
    dialogVisible.value = true
  } catch (error) {
    console.error('获取草稿详情失败:', error)
    ElMessage.error(i18n.global.t('获取草稿详情失败'))
  }
};

const handleDeleteDraft = (id) => {
  ElMessageBox.confirm(
    '确定要删除此草稿吗？',
    '警告',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(async () => {
    try {
      const response = await emailApi.deleteDraft(id)
      ElMessage.success(i18n.global.t('草稿删除成功'));
      fetchDrafts();
    } catch (error) {
      console.error('删除草稿失败:', error)
      ElMessage.error(i18n.global.t('删除草稿失败'))
    }
  }).catch(() => {})
};

const submitDraft = () => {
  draftFormRef.value.validate(async (valid) => {
    if (valid) {
      try {
        const draftData = {
          id: draftForm.id,
          subject: draftForm.subject,
          content: draftForm.content,
          // 编辑时回填已有附件，避免清空；后端 nil=不更新
          attachments: dialogType.value === 'edit' ? (draftForm.attachments || []) : undefined
        };

        let response
        if (!draftData.id) {
          response = await emailApi.createDraft(draftData);
        } else {
          response = await emailApi.updateDraft(draftData.id, draftData);
        }

        ElMessage.success(dialogType.value === 'create' ? '草稿创建成功' : '草稿更新成功');
        dialogVisible.value = false
        fetchDrafts();
      } catch (error) {
        console.error('保存草稿失败:', error)
        ElMessage.error(i18n.global.t('保存草稿失败'))
      }
    }
  })
};

const handlePreview = (file) => {
  if (file.url) {
    window.open(file.url, '_blank')
  } else {
    ElMessage.warning(i18n.global.t('附件尚未上传，无法预览'))
  }
};

const handleRemove = (file, fileList) => {
  draftForm.attachments = draftForm.attachments.filter(att => att.uid !== file.uid)
};

const beforeRemove = (file, fileList) => {
  return ElMessageBox.confirm(`确定要移除 ${file.name}？`)
};

const handleSend = (draft) => {
  Object.assign(sendForm, {
    id: draft.id,
    subject: draft.subject,
    content: draft.content,
    attachments: draft.attachments || [],
    to: '',
    smtpId: ''
  });
  sendDialogVisible.value = true;
  if (!smtpList.value.length)
    fetchSmtpList();
};

const fetchSmtpList = async () => {
  try {
    const res = await emailApi.getEmailSmtpList()
    smtpList.value = res.list || []
  } catch (error) {
    console.error('获取邮件代理失败:', error)
    ElMessage.error(i18n.global.t('获取邮件代理失败'))
  }
};

const submitSend = async () => {
  if (sendFormRef.value) {
    try {
      await sendFormRef.value.validate()
    } catch {
      return
    }
  }
  sending.value = true
  try {
    await emailApi.sendEmail({
      draftId: sendForm.id,
      to: sendForm.to.split(/[,;\s]+/).map(s => s.trim()).filter(Boolean).join(','),
      subject: sendForm.subject,
      content: sendForm.content,
      attachments: [],
      smtpId: sendForm.smtpId,
      immediateSend: true
    })
    ElMessage.success('已提交发送，投递结果以服务端日志为准');
    sendDialogVisible.value = false
  } catch (error) {
    console.error('发送邮件失败:', error)
    ElMessage.error(i18n.global.t('发送邮件失败'))
  } finally {
    sending.value = false
  }
}

onMounted(() => {
  fetchDrafts()
  fetchSmtpList()
});
</script>

<style lang="scss" scoped>
.email-drafts {
  padding: 20px;

  .drafts-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
  }

  .drafts-search {
    margin-bottom: 20px;
    .el-input {
      width: 400px;
    }
  }

  .empty-drafts {
    padding: 60px 0;
    text-align: center;
  }

  .editor-container {
    border: 1px solid #e5e7eb;
    border-radius: 4px;
    overflow: hidden;
  }

  /* 使用全局样式替代深度选择器 */
}
</style>

<style>
/* 全局样式，不使用scoped */
.email-drafts .el-upload-list__item {
  margin-top: 5px;
}
</style>