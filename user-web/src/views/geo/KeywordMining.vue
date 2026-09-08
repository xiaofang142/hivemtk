<template>
  <div class="geo-page">

    
    <el-card shadow="never" class="search-card">
      <el-form :model="query" label-width="92px" class="search-form">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8">
            <el-form-item label="种子词">
              <el-input
                v-model="query.seed_words"
                placeholder="每行一个种子词，如：CRM、客户管理"
                type="textarea"
                :rows="2"
                clearable
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8">
            <el-form-item label="挖掘模式">
              <el-select v-model="query.mode" style="width: 100%">
                <el-option label="AI 生成（LLM）" value="llm" />
                <el-option label="托词组合（词库）" value="combination" />
                <el-option label="混合模式" value="mixed" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="4" class="action-col">
            <el-button type="primary" :loading="mining" @click="handleMine">
              <el-icon><Search /></el-icon>
              <span>挖掘关键词</span>
            </el-button>
          </el-col>
        </el-row>
      </el-form>
    </el-card>

    
    <el-card shadow="never" class="table-card">
      <template #header>
        <div class="card-header">
          <span>关键词列表</span>
          <div class="header-actions">
            <el-input
              v-model="listQuery.search"
              placeholder="搜索关键词 / 类别 / 意图"
              clearable
              style="width: 240px"
              @keyup.enter="loadList"
              @clear="loadList"
            />
            <el-button @click="loadList">查询</el-button>
          </div>
        </div>
      </template>

      <el-table v-loading="loading" :data="tableData" stripe style="width: 100%">
        <el-table-column type="selection" width="44" />
        <el-table-column prop="keyword" label="关键词" min-width="180" show-overflow-tooltip />
        <el-table-column prop="category" label="类别" width="120" />
        <el-table-column prop="source" label="来源" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="sourceTagType(row.source)">{{ row.source || '—' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="search_volume" label="搜索量" width="100" align="right" />
        <el-table-column prop="difficulty" label="难度" width="90" align="center">
          <template #default="{ row }">
            <el-progress
              v-if="row.difficulty != null"
              :percentage="Number(row.difficulty) || 0"
              :stroke-width="6"
              :show-text="false"
            />
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column prop="intent" label="意图" width="100">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ row.intent || '—' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="cluster" label="话题集群" width="120" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === '已优化' ? 'success' : 'info'">
              {{ row.status || '待处理' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" :loading="row._expanding" @click="handleExpand(row)">语义扩展</el-button>
            <el-button link type="primary" :loading="row._clustering" @click="handleCluster(row)">话题聚类</el-button>
            <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="listQuery.page"
          v-model:page-size="listQuery.limit"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          background
          @size-change="loadList"
          @current-change="loadList"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { geoApi } from '@/api/geo'

const brandName = ref('')

const mining = ref(false)
const loading = ref(false)
const tableData = ref([])
const total = ref(0)

const query = reactive({
  seed_words: '',
  mode: 'llm'
})

const listQuery = reactive({
  search: '',
  page: 1,
  limit: 20
})

const sourceTagType = (source) => {
  const map = { AI: 'primary', 词库: 'warning', 混合: 'success' }
  return map[source] || 'info'
}

const loadList = async () => {
  loading.value = true
  try {
    const res = await geoApi.getKeywordList({ ...listQuery })
    tableData.value = res?.list || res?.items || res || []
    total.value = res?.total || tableData.value.length
  } catch (e) {
    ElMessage.error(e.message || '关键词列表加载失败')
    tableData.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

const handleMine = async () => {
  if (!query.seed_words.trim()) {
    ElMessage.warning('请先填写种子词')
    return
  }
  mining.value = true
  try {
    const seedWords = query.seed_words.split(/[\n\r，,]/).map(s => s.trim()).filter(Boolean)
    const res = await geoApi.mineKeywords({
      seed_words: seedWords,
      mode: query.mode
    })
    ElMessage.success(`已挖掘 ${Array.isArray(res) ? res.length : (res?.total || 0)} 个关键词`)
    listQuery.page = 1
    await loadList()
  } catch (e) {
    ElMessage.error(e.message || '关键词挖掘失败')
  } finally {
    mining.value = false
  }
}

const handleExpand = async (row) => {
  row._expanding = true
  try {
    const res = await geoApi.semanticExpand({ keywords: [row.keyword], brand_name: brandName.value })
    const expanded = Array.isArray(res) ? res : (res?.keywords || [])
    ElMessage.success(`语义扩展完成，新增 ${expanded.length} 个近义 / 长尾词`)
    await loadList()
  } catch (e) {
    ElMessage.error(e.message || '语义扩展失败')
  } finally {
    row._expanding = false
  }
}

const handleCluster = async (row) => {
  row._clustering = true
  try {
    await geoApi.topicCluster({ keywords: [row.keyword], brand_name: brandName.value })
    ElMessage.success('话题聚类完成')
    await loadList()
  } catch (e) {
    ElMessage.error(e.message || '话题聚类失败')
  } finally {
    row._clustering = false
  }
}

const handleDelete = (row) => {
  ElMessageBox.confirm(`确认删除关键词「${row.keyword}」？`, '删除确认', {
    type: 'warning'
  })
    .then(async () => {
      try {
        await geoApi.deleteKeyword(row.id)
        ElMessage.success('已删除')
        loadList()
      } catch (e) {
        ElMessage.error(e.message || '删除失败')
      }
    })
    .catch(() => {})
}

onMounted(async () => {
  loadList()
  try {
    const cfg = await geoApi.getConfig()
    brandName.value = cfg?.brand_name || ''
  } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
})
</script>

<style lang="scss" scoped>
.geo-page {
  padding: $spacing-lg 24px;
}
.page-header h2 {
  margin: 0 0 6px;
  font-size: $font-size-extra-large;
  font-weight: 700;
  color: $text-primary;
}
.page-header .sub {
  margin: 0 0 16px;
  color: $info-color;
  font-size: $font-size-small;
}
.search-card,
.table-card {
  border: 1px solid $border-base;
  border-radius: 10px;
  margin-bottom: $spacing-md;
}
.action-col {
  display: flex;
  align-items: flex-end;
  justify-content: flex-end;
  padding-bottom: $spacing-xs;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: $spacing-md;
}
.header-actions {
  display: flex;
  gap: $spacing-sm;
}
.pager {
  margin-top: $spacing-md;
  display: flex;
  justify-content: flex-end;
}
</style>
