<template>
  <div class="channel-manage">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>渠道管理（预设表驱动）</span>
          <el-button
            type="primary"
            size="small"
            @click="openCreate"
          >
            <el-icon><Plus /></el-icon>新增渠道
          </el-button>
        </div>
      </template>

      <el-form
        :inline="true"
        :model="filter"
        size="small"
      >
        <el-form-item label="关键字">
          <el-input
            v-model="filter.keyword"
            placeholder="渠道名称/编码"
            clearable
            style="width: 200px"
          />
        </el-form-item>
        <el-form-item>
          <el-button @click="loadList">
            查询
          </el-button>
          <el-button @click="resetFilter">
            重置
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="rows"
        size="small"
        border
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="name"
          label="渠道名称"
          min-width="140"
        />
        <el-table-column
          prop="code"
          label="编码"
          min-width="140"
        />
        <el-table-column
          prop="type"
          label="类型"
          width="100"
        />
        <el-table-column
          prop="enabled"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="remark"
          label="备注"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          label="操作"
          width="200"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="openEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="warning"
              size="small"
              @click="toggleEnabled(row)"
            >
              {{ row.enabled ? '停用' : '启用' }}
            </el-button>
            <el-button
              link
              type="danger"
              size="small"
              @click="remove(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editingId ? '编辑渠道' : '新增渠道'"
      width="480px"
    >
      <el-form
        :model="form"
        label-width="100px"
        size="small"
      >
        <el-form-item
          label="渠道名称"
          required
        >
          <el-input
            v-model="form.name"
            placeholder="如 抖音"
          />
        </el-form-item>
        <el-form-item
          label="渠道编码"
          required
        >
          <el-input
            v-model="form.code"
            placeholder="如 douyin（全局唯一）"
            :disabled="!!editingId"
          />
        </el-form-item>
        <el-form-item label="类型">
          <el-select
            v-model="form.type"
            style="width: 100%"
          >
            <el-option
              label="单聊"
              value="dm"
            />
            <el-option
              label="群聊"
              value="group"
            />
            <el-option
              label="客服"
              value="cs"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.remark"
            type="textarea"
            :rows="2"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="submit"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import channelAccountApi from '@/api/channelAccount.js'

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const filter = reactive({ keyword: '' })
const dialogVisible = ref(false)
const editingId = ref(null)
const form = reactive({ name: '', code: '', type: 'dm', enabled: true, remark: '' })

async function loadList() {
  loading.value = true
  try {
    const res = await channelAccountApi.listChannels({ keyword: filter.keyword })
    rows.value = res?.list || res || []
  } catch (e) {
    ElMessage.error('加载渠道失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

function resetFilter() {
  filter.keyword = ''
  loadList()
}

function resetForm() {
  Object.assign(form, { name: '', code: '', type: 'dm', enabled: true, remark: '' })
}

function openCreate() {
  editingId.value = null
  resetForm()
  dialogVisible.value = true
}

function openEdit(row) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name, code: row.code, type: row.type || 'dm',
    enabled: !!row.enabled, remark: row.remark || ''
  })
  dialogVisible.value = true
}

async function submit() {
  if (!form.name || !form.code) {
    ElMessage.warning('渠道名称与编码必填')
    return
  }
  saving.value = true
  try {
    const payload = { name: form.name, code: form.code, type: form.type, enabled: form.enabled, remark: form.remark }
    if (editingId.value) {
      await channelAccountApi.updateChannel(editingId.value, payload)
      ElMessage.success('更新成功')
    } else {
      await channelAccountApi.createChannel(payload)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    loadList()
  } catch (e) {
    ElMessage.error('保存失败：' + (e.message || '未知错误'))
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(row) {
  try {
    await channelAccountApi.updateChannel(row.id, { name: row.name, code: row.code, type: row.type, enabled: !row.enabled, remark: row.remark })
    ElMessage.success(row.enabled ? '已停用' : '已启用')
    loadList()
  } catch (e) {
    ElMessage.error('操作失败：' + (e.message || '未知错误'))
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确认删除渠道「${row.name}」？`, '提示', { type: 'warning' })
    await channelAccountApi.deleteChannel(row.id)
    ElMessage.success('删除成功')
    loadList()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error('删除失败：' + (e.message || '未知错误'))
  }
}

onMounted(loadList)
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
