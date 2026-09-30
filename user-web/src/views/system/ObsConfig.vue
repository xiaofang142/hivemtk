<template>
  <div class="obs-config">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('云存储配置') }}</span>
          <el-button type="primary" @click="handleCreate">{{ $t('新增配置') }}</el-button>
        </div>
      </template>

      <el-table :data="configList" v-loading="loading" border>
        <el-table-column prop="name" label="配置名称" width="150" />
        <el-table-column prop="provider" label="服务商" width="120">
          <template #default="{ row }">
            <el-tag :type="getProviderType(row.provider)">
              {{ getProviderLabel(row.provider) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="存储位置" min-width="240">
          <template #default="{ row }">
            <span v-if="row.provider === 'local'">
              {{ row.endpoint || './uploads' }}
            </span>
            <span v-else>
              {{ row.bucket || '-' }}<span v-if="row.endpoint"> @ {{ row.endpoint }}</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="domain" label="访问前缀" min-width="180">
          <template #default="{ row }">
            <span v-if="row.domain">{{ row.domain }}</span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="is_default" label="默认" width="80">
          <template #default="{ row }">
            <el-tag v-if="row.is_default" type="success">是</el-tag>
            <span v-else>否</span>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusTagType(row.status)">
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link @click="handleTest(row)">测试</el-button>
            <el-button type="primary" link @click="handleEdit(row)">编辑</el-button>
            <el-button
              type="success"
              link
              @click="handleSetDefault(row)"
              v-if="!row.is_default"
            >
              设为默认
            </el-button>
            <el-button type="danger" link @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-dialog
      :title="dialogTitle"
      v-model="dialogVisible"
      width="640px"
      @close="handleDialogClose"
    >
      <el-form
        ref="formRef"
        :model="formData"
        :rules="dynamicRules"
        label-width="130px"
      >
        <el-form-item label="配置名称" prop="name">
          <el-input v-model="formData.name" placeholder="请输入配置名称" />
        </el-form-item>

        <el-form-item label="服务商" prop="provider">
          <el-select v-model="formData.provider" placeholder="请选择服务商" @change="handleProviderChange" style="width:100%">
            <el-option label="本地存储" value="local" />
            <el-option label="阿里云OSS" value="aliyun" />
            <el-option label="七牛云" value="qiniu" />
            <el-option label="腾讯云COS" value="tencent" />
            <el-option label="AWS S3" value="aws" />
          </el-select>
        </el-form-item>

        
        <template v-if="formData.provider !== 'local'">
          <el-form-item label="Access Key" prop="access_key">
            <el-input v-model="formData.access_key" placeholder="请输入 Access Key" show-password />
          </el-form-item>

          <el-form-item label="Secret Key" prop="secret_key">
            <el-input v-model="formData.secret_key" type="password" placeholder="请输入 Secret Key" show-password />
          </el-form-item>

          <el-form-item label="存储桶" prop="bucket">
            <el-input v-model="formData.bucket" placeholder="请输入存储桶名称" />
          </el-form-item>

          <el-form-item label="存储区域" prop="region" v-if="formData.provider === 'aws'">
            <el-input v-model="formData.region" placeholder="如：ap-southeast-1" />
          </el-form-item>
        </template>

        
        <el-form-item :label="endpointLabel" prop="endpoint">
          <el-input v-model="formData.endpoint" :placeholder="endpointPlaceholder" />
          <div class="form-tip" v-if="formData.provider === 'local'">
            本地存储目录，默认 ./uploads（相对程序运行目录）
          </div>
          <div class="form-tip" v-else-if="formData.provider === 'aliyun'">
            https://xxx.oss-cn-hangzhou.aliyuncs.com
          </div>
          <div class="form-tip" v-else-if="formData.provider === 'qiniu'">
            http://xxx.bkt.clouddn.com
          </div>
          <div class="form-tip" v-else-if="formData.provider === 'tencent'">
            https://xxx.cos.ap-guangzhou.myqcloud.com
          </div>
          <div class="form-tip" v-else-if="formData.provider === 'aws'">
            https://s3.ap-southeast-1.amazonaws.com
          </div>
        </el-form-item>

        
        <el-form-item :label="domainLabel">
          <el-input v-model="formData.domain" :placeholder="domainPlaceholder" />
          <div class="form-tip" v-if="formData.provider === 'local'">
            本地文件对外访问的 URL 前缀，默认 /files
          </div>
          <div class="form-tip" v-else>
            可选：自定义 CDN 加速域名（留空则使用 endpoint 访问）
          </div>
        </el-form-item>

        <el-form-item label="路径前缀">
          <el-input v-model="formData.path_prefix" placeholder="可选：统一添加的路径前缀" />
        </el-form-item>

        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="单文件上限">
              <el-input-number v-model="formData.max_size" :min="10240" :step="10485760" />
              <div class="form-tip">字节，默认 100MB</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="文件数量上限">
              <el-input-number v-model="formData.max_count" :min="1" :step="100" />
              <div class="form-tip">默认 1000 个</div>
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="状态">
          <el-radio-group v-model="formData.status">
            <el-radio label="active">启用</el-radio>
            <el-radio label="inactive">禁用</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getObsConfigList, createObsConfig, updateObsConfig, deleteObsConfig, testObsConnection, setDefaultObsConfig } from '@/api/obs'

const OBS_STATUS = { active: '正常', disabled: '禁用', inactive: '禁用', error: '错误' };
const OBS_STATUS_TAG = { active: 'success', disabled: 'danger', inactive: 'danger', error: 'warning' }
const getStatusLabel = (s) => OBS_STATUS[s] || s || '-'
const getStatusTagType = (s) => OBS_STATUS_TAG[s] || ''

const loading = ref(false)
const configList = ref([])
const dialogVisible = ref(false)
const formRef = ref(null)
const currentId = ref(null)

const formData = reactive({
  name: '',
  provider: '',
  access_key: '',
  secret_key: '',
  bucket: '',
  region: '',
  endpoint: '',
  domain: '',
  path_prefix: '',
  max_size: 104857600,
  max_count: 1000,
  status: 'active'
});

const dynamicRules = computed(() => {
  const rules = {
    name: [{ required: true, message: i18n.global.t('请输入配置名称'), trigger: 'blur' }],
    provider: [{ required: true, message: i18n.global.t('请选择服务商'), trigger: 'change' }]
  }
  if (formData.provider !== 'local') {
    rules.access_key = [{ required: true, message: i18n.global.t('请输入 Access Key'), trigger: 'blur' }]
    rules.secret_key = [{ required: true, message: i18n.global.t('请输入 Secret Key'), trigger: 'blur' }]
    rules.bucket = [{ required: true, message: i18n.global.t('请输入存储桶名称'), trigger: 'blur' }]
  }
  if (formData.provider === 'aws') {
    rules.region = [{ required: true, message: i18n.global.t('请输入 AWS 存储区域'), trigger: 'blur' }]
  }
  return rules
});

const dialogTitle = computed(() => (currentId.value ? '编辑配置' : '新增配置'))

const endpointLabel = computed(() =>
  formData.provider === 'local' ? '本地存储目录' : '节点域名 / Endpoint'
)
const domainLabel = computed(() =>
  formData.provider === 'local' ? '公开访问前缀' : 'CDN 域名（可选）'
)
const endpointPlaceholder = computed(() => {
  if (!formData.provider) return '请先选择服务商'
  if (formData.provider === 'local') return './uploads'
  const map = {
    aliyun: 'https://xxx.oss-cn-hangzhou.aliyuncs.com',
    qiniu: 'http://xxx.bkt.clouddn.com',
    tencent: 'https://xxx.cos.ap-guangzhou.myqcloud.com',
    aws: 'https://s3.ap-southeast-1.amazonaws.com'
  }
  return map[formData.provider] || '请输入 Endpoint'
})
const domainPlaceholder = computed(() =>
  formData.provider === 'local' ? '/files' : '留空则使用 endpoint 访问'
)

// 'default' 不在 el-tag 的合法 type 里（primary/success/info/warning/danger），会触发 Vue prop
// 校验告警并回落到 default='primary'。本地存储与未识别厂商用中性的 'info'。
const getProviderType = (provider) => {
  const types = { qiniu: 'warning', aliyun: 'primary', tencent: 'success', aws: 'info', local: 'info' }
  return types[provider] || 'info'
}
const getProviderLabel = (provider) => {
  const labels = { local: '本地存储', aliyun: '阿里云OSS', qiniu: '七牛云', tencent: '腾讯云COS', aws: 'AWS S3' }
  return labels[provider] || provider
}

const handleProviderChange = (provider) => {
  formData.access_key = '';
  formData.secret_key = ''
  formData.bucket = ''
  formData.region = ''
  formData.endpoint = ''
  formData.domain = ''

  switch (provider) {
    case 'local':
      formData.endpoint = './uploads'
      formData.domain = '/files'
      break
    case 'aliyun':
      formData.endpoint = 'https://xxx.oss-cn-hangzhou.aliyuncs.com'
      break
    case 'qiniu':
      formData.endpoint = 'http://xxx.bkt.clouddn.com'
      break
    case 'tencent':
      formData.endpoint = 'https://xxx.cos.ap-guangzhou.myqcloud.com'
      break
    case 'aws':
      formData.region = 'ap-southeast-1'
      formData.endpoint = 'https://s3.ap-southeast-1.amazonaws.com'
      break
  }
};

const loadConfigList = async () => {
  loading.value = true
  try {
    const response = await getObsConfigList()
    configList.value = (response && response.list) || []
  } catch (error) {
    ElMessage.error(i18n.global.t('获取配置列表失败'))
  } finally {
    loading.value = false
  }
}

const handleCreate = () => {
  currentId.value = null
  resetForm()
  dialogVisible.value = true
}

const handleEdit = (row) => {
  currentId.value = row.id
  Object.assign(formData, row)
  dialogVisible.value = true
};

const handleTest = async (row) => {
  try {
    await testObsConnection(row.id)
    ElMessage.success(i18n.global.t('连接测试成功'))
  } catch (error) {
    ElMessage.error('连接测试失败：' + (error.message || '未知错误'))
  }
}

const handleSetDefault = async (row) => {
  try {
    await setDefaultObsConfig(row.id)
    ElMessage.success(i18n.global.t('设置默认配置成功'))
    loadConfigList()
  } catch (error) {
    ElMessage.error(i18n.global.t('设置默认配置失败'))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm('确定要删除该配置吗？', '提示', { type: 'warning' })
    await deleteObsConfig(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    loadConfigList()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error(i18n.global.t('删除失败'))
  }
}

const handleSubmit = () => {
  formRef.value.validate(async (valid) => {
    if (!valid) return
    try {
      if (currentId.value) {
        await updateObsConfig(currentId.value, formData)
        ElMessage.success(i18n.global.t('更新成功'))
      } else {
        await createObsConfig(formData)
        ElMessage.success(i18n.global.t('创建成功'))
      }
      dialogVisible.value = false
      loadConfigList()
    } catch (error) {
      ElMessage.error(currentId.value ? '更新失败' : '创建失败')
    }
  })
}

const handleDialogClose = () => {
  if (formRef.value) formRef.value.resetFields()
}

const resetForm = () => {
  Object.assign(formData, {
    name: '',
    provider: '',
    access_key: '',
    secret_key: '',
    bucket: '',
    region: '',
    endpoint: '',
    domain: '',
    path_prefix: '',
    max_size: 104857600,
    max_count: 1000,
    status: 'active'
  })
}

onMounted(() => {
  loadConfigList()
})
</script>

<style scoped>
.obs-config {
  padding: 20px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.form-tip {
  margin-top: 5px;
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
}
.muted {
  color: #c0c4cc;
}
</style>
