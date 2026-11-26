<template>
  <div class="channel-manage">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('channels.catalog') }}</span>
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
        :data="platformRows"
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

      <section
        v-if="serviceBindings.length"
        class="service-bindings"
        data-testid="legacy-service-bindings"
      >
        <h2>{{ $t('channels.legacyBindings') }}</h2>
        <p>{{ $t('channels.legacyBindingsHint') }}</p>
        <ul>
          <li
            v-for="binding in serviceBindings"
            :key="binding.id"
          >
            <strong>{{ binding.name }}</strong>
            <span>SageX · QQ / {{ $t('channels.personalWechat') }}</span>
            <el-button
              link
              type="primary"
              @click="openEdit(binding)"
            >
              {{ $t('channels.editBinding') }}
            </el-button>
          </li>
        </ul>
      </section>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editingServiceBinding ? $t('channels.editBinding') : (editingId ? '编辑渠道' : '新增渠道')"
      width="min(480px, 94vw)"
    >
      <el-form
        :model="form"
        label-width="100px"
        size="small"
      >
        <el-form-item
          :label="editingServiceBinding ? $t('channels.bindingName') : '渠道名称'"
          required
        >
          <el-input
            v-model="form.name"
            :placeholder="editingServiceBinding ? $t('channels.bindingName') : '如 抖音'"
          />
        </el-form-item>
        <el-form-item
          :label="editingServiceBinding ? $t('channels.bindingCode') : '渠道编码'"
          required
        >
          <el-input
            v-model="form.code"
            placeholder="如 douyin（全局唯一）"
            :disabled="!!editingId"
          />
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
import { computed, ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import channelAccountApi from '@/api/channelAccount.js'
import { isLegacyConnectionService } from '@/constants/channel'

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const platformRows = computed(() => rows.value.filter(row => !isLegacyConnectionService(row.code)))
const serviceBindings = computed(() => rows.value.filter(row => isLegacyConnectionService(row.code)))
const filter = reactive({ keyword: '' })
const dialogVisible = ref(false)
const editingId = ref(null)
const form = reactive({ name: '', code: '', enabled: true, remark: '' })
const editingServiceBinding = computed(() => !!editingId.value && isLegacyConnectionService(form.code))

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
  Object.assign(form, { name: '', code: '', enabled: true, remark: '' })
}

function openCreate() {
  editingId.value = null
  resetForm()
  dialogVisible.value = true
}

function openEdit(row) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name, code: row.code,
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
    const payload = { name: form.name, code: form.code, enabled: form.enabled, remark: form.remark }
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
    await channelAccountApi.updateChannel(row.id, { name: row.name, code: row.code, enabled: !row.enabled, remark: row.remark })
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
.service-bindings { margin-top: 24px; }
.service-bindings h2 { margin: 0 0 8px; font-size: 16px; }
.service-bindings p { color: var(--el-text-color-secondary); line-height: 1.6; }
.service-bindings ul { margin: 0; padding: 0; list-style: none; }
.service-bindings li { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 8px 0; }
.service-bindings li span { color: var(--el-text-color-secondary); }
</style>
