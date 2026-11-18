<template>
  <div class="lead-group-selection">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('从线索库选择群体') }}</span>
        </div>
      </template>

      
      <el-form
        :model="filterForm"
        inline
        label-width="80px"
        style="margin-bottom: 20px;"
      >
        <el-form-item label="状态">
          <el-select
            v-model="filterForm.status"
            placeholder="选择线索状态"
            clearable
          >
            <el-option
              v-for="opt in leadStatusOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        
        <el-form-item label="来源">
          <el-input
            v-model="filterForm.source"
            placeholder="线索来源"
            clearable
          />
        </el-form-item>
        
        <el-form-item label="最低评分">
          <el-slider
            v-model="filterForm.minScore"
            :min="0"
            :max="100"
            show-input
          />
        </el-form-item>
        
        <el-form-item label="搜索">
          <el-input
            v-model="filterForm.search"
            placeholder="姓名或电话"
            clearable
          />
        </el-form-item>
        
        <el-form-item>
          <el-button
            type="primary"
            @click="applyFilters"
          >
            筛选
          </el-button>
          <el-button @click="resetFilters">
            重置
          </el-button>
        </el-form-item>
      </el-form>

      
      <el-table 
        v-loading="loading" 
        :data="leads" 
        style="width: 100%; margin-top: 20px;"
        @selection-change="handleSelectionChange"
      >
        <el-table-column
          type="selection"
          width="55"
        />
        <el-table-column
          prop="name"
          label="姓名"
          width="120"
        />
        <el-table-column
          prop="phone"
          label="手机号"
          width="150"
        />
        <el-table-column
          prop="email"
          label="邮箱"
          width="200"
        />
        <el-table-column
          prop="company"
          label="公司"
          width="150"
        />
        <el-table-column
          prop="source"
          label="来源"
          width="100"
        />
        <el-table-column
          prop="score"
          label="质量评分"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="getScoreType(row.score)">
              {{ row.score }}分
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)">
              {{ getStatusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      
      <el-pagination
        v-if="pagination.total > 0"
        class="pagination"
        :current-page="pagination.currentPage"
        :page-sizes="[10, 20, 50, 100]"
        :page-size="pagination.pageSize"
        layout="total, sizes, prev, pager, next, jumper"
        :total="pagination.total"
        @size-change="handleSizeChange"
        @current-change="handleCurrentChange"
      />
    </el-card>

    
    <el-card
      v-if="selectedLeads.length > 0"
      style="margin-top: 20px;"
    >
      <template #header>
        <div class="card-header">
          <span>已选择 {{ selectedLeads.length }} 个联系人</span>
          <el-button
            type="primary"
            @click="proceedToMessaging"
          >
            下一步：群发消息
          </el-button>
        </div>
      </template>
      
      <el-table
        :data="selectedLeads"
        style="width: 100%;"
      >
        <el-table-column
          prop="name"
          label="姓名"
          width="120"
        />
        <el-table-column
          prop="phone"
          label="手机号"
          width="150"
        />
        <el-table-column
          prop="email"
          label="邮箱"
          width="200"
        />
        <el-table-column
          prop="company"
          label="公司"
          width="150"
        />
        <el-table-column
          prop="score"
          label="质量评分"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="getScoreType(row.score)">
              {{ row.score }}分
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox as _ElMessageBox } from 'element-plus'
import * as clueApi from '@/api/clue'
import { useRouter } from 'vue-router'
import { LEAD_STATUS_OPTIONS_SIMPLE, getLeadStatusLabel as _getLeadStatusLabel, getLeadStatusTagType as _getLeadStatusTagType } from '@/constants/leadStatus'

const router = useRouter()
const loading = ref(false)
const leads = ref([])
const selectedLeads = ref([])
const leadStatusOptions = LEAD_STATUS_OPTIONS_SIMPLE

const pagination = reactive({
  currentPage: 1,
  pageSize: 20,
  total: 0
})

const filterForm = reactive({
  status: '',
  source: '',
  minScore: 0,
  search: ''
})

const fetchLeads = async () => {
  try {
    loading.value = true
    const response = await clueApi.clueApi.list(
      pagination.currentPage,
      pagination.pageSize
    );
    const clueData = response?.list || response || [];
    leads.value = clueData.map(clue => ({
      id: clue.ID,
      name: clue.Name || clue.name || '未知',
      phone: clue.Account || clue.account || '',
      email: '',
      company: clue.Address || clue.address || clue.City || clue.city || '',
      source: 'clue',
      score: 80,
      status: 'new'
    }));
    pagination.total = response?.total || response?.length || 0
  } catch  {
    ElMessage.error(i18n.global.t('获取线索列表失败'))
  } finally {
    loading.value = false
  }
}

const applyFilters = () => {
  pagination.currentPage = 1
  fetchLeads()
}

const resetFilters = () => {
  filterForm.status = ''
  filterForm.source = ''
  filterForm.minScore = 0
  filterForm.search = ''
  pagination.currentPage = 1
  fetchLeads()
}

const handleSelectionChange = (selection) => {
  selectedLeads.value = selection
}

const proceedToMessaging = () => {
  localStorage.setItem('selectedLeads', JSON.stringify(selectedLeads.value.map(lead => lead.id)));
  router.push('/whatsapp/group-messaging');
}

const getScoreType = (score) => {
  if (score >= 80) return 'success'
  if (score >= 60) return 'warning'
  return 'danger'
}

const getStatusType = (status) => {
  switch (status) {
    case 'new': return 'info'
    case 'contacted': return 'warning'
    case 'converted': return 'success'
    case 'lost': return 'danger'
    default: return 'info'
  }
}

const getStatusText = (status) => {
  switch (status) {
    case 'new': return '新线索'
    case 'contacted': return '已联系'
    case 'converted': return '已转化'
    case 'lost': return '已流失'
    default: return '未知'
  }
}

const handleSizeChange = (val) => {
  pagination.pageSize = val
  fetchLeads()
}

const handleCurrentChange = (val) => {
  pagination.currentPage = val
  fetchLeads()
}

onMounted(() => {
  fetchLeads()
});
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination {
  margin-top: 20px;
  text-align: right;
}
</style>
