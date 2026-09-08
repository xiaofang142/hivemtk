<template>
  <div class="asset-bundle-list">
    <el-card class="filter-bar" shadow="never">
      <el-form inline>
        <el-form-item label="关键词">
          <el-input v-model="filter.keyword" placeholder="搜索标题 / Asset ID" clearable />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filter.status" placeholder="全部" clearable>
            <el-option label="草稿" value="draft" />
            <el-option label="启用" value="active" />
            <el-option label="停用" value="inactive" />
            <el-option label="归档" value="archived" />
          </el-select>
        </el-form-item>
        <el-form-item label="作用域">
          <el-select v-model="filter.scope" placeholder="全部" clearable>
            <el-option label="私有" value="private" />
            <el-option label="共享" value="shared" />
            <el-option label="官方" value="official" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
          <el-button type="success" @click="$router.push('/asset-bundle/playground')">+ 开发者新建</el-button>
          <el-button type="warning" @click="$router.push('/asset-bundle/merchant-new')">+ 商户低代码新建</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="hot-bar" shadow="never">
      <div class="hot-row">
        <span class="hot-title">🔥 热启用中：</span>
        <div v-if="enabledList.length" class="hot-list">
          <el-tag
            v-for="b in enabledList"
            :key="b.id"
            type="success"
            class="hot-tag"
            @click="$router.push(`/asset-bundle/merchant/${b.asset_id}`)"
          >
            {{ b.title }} ({{ b.asset_id }})
          </el-tag>
        </div>
        <span v-else class="hot-empty">暂无热启用的资产包</span>
        <el-button size="small" link @click="fetchEnabled">刷新</el-button>
      </div>
    </el-card>

    <el-table :data="list" v-loading="loading" border stripe>
      <el-table-column label="封面" width="80">
        <template #default="{ row }">
          <el-image
            v-if="row.cover_image"
            :src="row.cover_image"
            :preview-src-list="[row.cover_image]"
            fit="cover"
            style="width: 64px; height: 48px; border-radius: 6px"
          />
          <div v-else class="cover-placeholder-bundle">{{ (row.title || '?').slice(0, 1) }}</div>
        </template>
      </el-table-column>
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="asset_id" label="Asset ID" width="220" />
      <el-table-column prop="title" label="标题" min-width="200" />
      <el-table-column prop="author" label="作者" width="120" />
      <el-table-column prop="version" label="版本" width="80" />
      <el-table-column prop="industry" label="行业" width="120" />
      <el-table-column label="作用域" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="scopeTagType(row.scope)">{{ scopeLabel(row.scope) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="use_count" label="使用次数" width="90" />
      <el-table-column prop="updated_at" label="更新时间" width="170">
        <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button size="small" type="primary" link @click="$router.push(`/asset-bundle/playground/${row.asset_id}`)">Playground</el-button>
          <el-button size="small" type="warning" link @click="$router.push(`/asset-bundle/merchant/${row.asset_id}`)">商户编辑</el-button>
          <el-button v-if="!hotEnabledIds.has(row.id)" size="small" type="success" link @click="handleEnable(row)">热启用</el-button>
          <el-button v-else size="small" type="info" link @click="handleDisable(row)">热禁用</el-button>
          <el-button v-if="row.status === 'draft'" size="small" type="primary" link @click="handlePublish(row)">发布</el-button>
          <el-button size="small" type="danger" link @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-model:current-page="filter.page"
      v-model:page-size="filter.size"
      :total="total"
      layout="total, prev, pager, next, jumper"
      @current-change="fetchList"
      class="pager"
    />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listBundles, listEnabledBundles, enableBundle, disableBundle,
  publishBundle, deleteBundle
} from '@/api/assetBundle'

const filter = reactive({
  keyword: '',
  status: '',
  scope: '',
  page: 1,
  size: 20
})
const list = ref([])
const total = ref(0)
const loading = ref(false)
const enabledList = ref([])
const hotEnabledIds = ref(new Set());

const scopeLabel = (s) => ({ private: '私有', shared: '共享', official: '官方' }[s] || s)
const scopeTagType = (s) => ({ private: 'info', shared: 'warning', official: 'success' }[s] || '')
const statusLabel = (s) => ({ draft: '草稿', active: '启用', inactive: '停用', archived: '归档' }[s] || s)
const statusTagType = (s) => ({ draft: 'info', active: 'success', inactive: 'warning', archived: 'danger' }[s] || '')

const formatDate = (d) => {
  if (!d) return ''
  try {
    return new Date(d).toLocaleString('zh-CN', { hour12: false })
  } catch (e) {
    return d
  }
}

const fetchList = async () => {
  loading.value = true
  try {
    const resp = await listBundles({
      keyword: filter.keyword,
      status: filter.status,
      scope: filter.scope,
      page: filter.page,
      size: filter.size
    })
    const data = resp?.data || resp || {}
    list.value = data.list || []
    total.value = data.total || 0
  } catch (e) {
    ElMessage.error('查询失败: ' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

const fetchEnabled = async () => {
  try {
    const resp = await listEnabledBundles()
    const data = resp?.data || resp || {}
    const list = data.list || []
    enabledList.value = list
    hotEnabledIds.value = new Set(list.map(b => b.id))
  } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const handleEnable = async (row) => {
  try {
    await enableBundle(row.id)
    ElMessage.success('已热启用（即时生效，重启后需重新热启用）')
    fetchEnabled()
  } catch (e) {
    ElMessage.error('热启用失败: ' + (e?.message || e))
  }
}

const handleDisable = async (row) => {
  try {
    await disableBundle(row.id)
    ElMessage.success('已热禁用（即时生效）')
    fetchEnabled()
  } catch (e) {
    ElMessage.error('热禁用失败: ' + (e?.message || e))
  }
}

const handlePublish = async (row) => {
  try {
    await ElMessageBox.confirm(`确认发布资产包「${row.title}」？`, '确认', { type: 'warning' })
    await publishBundle(row.id)
    row.status = 'active'
    ElMessage.success('已发布')
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('发布失败: ' + (e?.message || e))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除资产包「${row.title}」？此操作不可恢复。`, '危险操作', { type: 'warning' })
    await deleteBundle(row.id)
    ElMessage.success('已删除')
    fetchList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败: ' + (e?.message || e))
  }
}

onMounted(() => {
  fetchList()
  fetchEnabled()
})
</script>

<style scoped>
.asset-bundle-list {
  padding: 16px;
}
.filter-bar {
  margin-bottom: 12px;
}
.hot-bar {
  margin-bottom: 12px;
  background: linear-gradient(90deg, #fdf6ec 0%, #fff 100%);
}
.hot-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.hot-title {
  font-weight: 600;
  color: #e6a23c;
}
.hot-list {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.hot-tag {
  cursor: pointer;
}
.hot-empty {
  font-size: 12px;
  color: #909399;
}
.pager {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
}
.cover-placeholder-bundle {
  width: 64px;
  height: 48px;
  border-radius: 6px;
  background: linear-gradient(135deg, #43e97b 0%, #38f9d7 100%);
  color: #fff;
  font-size: 16px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
