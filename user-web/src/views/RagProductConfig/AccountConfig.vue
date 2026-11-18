<template>
  <div class="account-config">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('账号配置') }}</span>
        </div>
      </template>
      
      <el-form 
        ref="formRef" 
        :model="form" 
        :rules="rules"
        label-width="120px"
      >
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item
              label="平台"
              prop="platform"
            >
              <el-select
                v-model="form.platform"
                placeholder="选择平台"
                style="width: 100%;"
                @change="onPlatformChange"
              >
                <el-option
                  v-for="opt in cardPlatformOptions"
                  :key="opt.value"
                  :label="opt.label"
                  :value="opt.value"
                />
              </el-select>
            </el-form-item>
          </el-col>
          
          <el-col :span="12">
            <el-form-item
              label="账号ID"
              prop="account_id"
            >
              <el-input 
                v-model="form.account_id" 
                placeholder="输入平台账号ID"
              />
            </el-form-item>
          </el-col>
        </el-row>
        
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="关联RAG产品">
              <el-select 
                v-model="form.rag_product_id" 
                placeholder="选择RAG产品"
                :disabled="!ragProducts.length"
                style="width: 100%;"
              >
                <el-option 
                  v-for="product in ragProducts" 
                  :key="product.id" 
                  :label="product.name" 
                  :value="product.id"
                >
                  <span>{{ product.name }}</span>
                  <el-tag 
                    size="small" 
                    type="info" 
                    style="margin-left: 8px;"
                  >
                    {{ product.category }}
                  </el-tag>
                </el-option>
              </el-select>
            </el-form-item>
          </el-col>
          
          <el-col :span="12">
            <el-form-item label="每日最大查询数">
              <el-input-number 
                v-model="form.max_daily_queries" 
                :min="1" 
                :max="10000"
                style="width: 100%;"
              />
            </el-form-item>
          </el-col>
        </el-row>
        
        <el-form-item label="功能开关">
          <el-switch
            v-model="form.is_auto_reply_enabled"
            active-text="自动回复"
            style="margin-right: 20px;"
          />
          <el-switch
            v-model="form.is_rag_enabled"
            active-text="RAG智能体"
            :disabled="!form.rag_product_id"
          />
        </el-form-item>
        
        <el-form-item label="回复规则">
          <el-button 
            type="primary" 
            size="small" 
            @click="addReplyRule"
          >
            添加规则
          </el-button>
        </el-form-item>
        
        <el-form-item>
          <div 
            v-for="(rule, index) in form.reply_rules" 
            :key="index"
            class="reply-rule-item"
          >
            <el-card shadow="hover">
              <template #header>
                <div class="rule-header">
                  <span>规则 {{ index + 1 }}</span>
                  <el-button 
                    type="danger" 
                    size="small" 
                    @click="removeReplyRule(index)"
                  >
                    删除
                  </el-button>
                </div>
              </template>
              
              <el-row :gutter="10">
                <el-col :span="6">
                  <el-input 
                    v-model.number="rule.priority" 
                    placeholder="优先级"
                    type="number"
                  />
                </el-col>
                
                <el-col :span="12">
                  <el-input 
                    v-model="rule.keywords_str" 
                    placeholder="关键词(逗号分隔)"
                  />
                </el-col>
                
                <el-col :span="6">
                  <el-switch
                    v-model="rule.is_active"
                    active-text="启用"
                  />
                </el-col>
              </el-row>
              
              <el-input 
                v-model="rule.reply_template" 
                placeholder="回复模板"
                type="textarea"
                :rows="2"
                style="margin-top: 10px;"
              />
            </el-card>
          </div>
        </el-form-item>
        
        <el-form-item>
          <el-button 
            type="primary" 
            @click="submitForm"
          >
            保存配置
          </el-button>
          <el-button @click="resetForm">
            重置
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { ragProductConfigAPI } from '@/api/ragProductConfig'
import { CARD_PLATFORM_OPTIONS } from '@/constants/cardPlatform';

const cardPlatformOptions = CARD_PLATFORM_OPTIONS

const formRef = ref()

const form = reactive({
  platform: '',
  account_id: '',
  rag_product_id: '',
  is_auto_reply_enabled: false,
  is_rag_enabled: false,
  max_daily_queries: 1000,
  reply_rules: []
})

const rules = {
  platform: [
    { required: true, message: i18n.global.t('请选择平台'), trigger: 'change' }
  ],
  account_id: [
    { required: true, message: i18n.global.t('请输入账号ID'), trigger: 'blur' }
  ]
}

const ragProducts = ref([])
const currentConfig = ref(null)

onMounted(() => {
  loadRagProducts()
})

const loadRagProducts = async () => {
  try {
    const response = await ragProductConfigAPI.getRagProducts()
    // 信封键是 list；只认 items 时这个下拉恒空。
    ragProducts.value = response?.list || response?.items || [];
  } catch (error) {
    ElMessage.error('加载RAG产品失败: ' + error.message)
  }
}

const onPlatformChange = () => {
  form.account_id = '';
  loadCurrentConfig()
}

const loadCurrentConfig = async () => {
  if (!form.account_id || !form.platform) return
  
  try {
    const response = await ragProductConfigAPI.getAccountConfig({
      account_id: form.account_id,
      platform: form.platform
    })
    
    const config = response;
    Object.assign(form, {
      ...config,
      rag_product_id: config.rag_product_id || '',
      reply_rules: (config.reply_rules || []).map(rule => ({
        ...rule,
        keywords_str: Array.isArray(rule.keywords) ? rule.keywords.join(',') : rule.keywords || ''
      }))
    })
    currentConfig.value = config
  } catch {
    form.reply_rules = [];
    currentConfig.value = null
  }
}

const addReplyRule = () => {
  form.reply_rules.push({
    id: Date.now().toString(),
    priority: 1,
    keywords_str: '',
    reply_template: '',
    is_active: true
  })
}

const removeReplyRule = (index) => {
  form.reply_rules.splice(index, 1)
}

const submitForm = async () => {
  try {
    await formRef.value.validate()
    
    const submitData = {
      ...form,
      reply_rules: form.reply_rules.map(rule => ({
        ...rule,
        keywords: rule.keywords_str ? rule.keywords_str.split(',').map(k => k.trim()).filter(k => k) : []
      }))
    }
    
    await ragProductConfigAPI.updateAccountConfig(submitData)
    ElMessage.success(i18n.global.t('配置保存成功'))
    
    loadCurrentConfig();
  } catch (error) {
    ElMessage.error('保存失败: ' + error.message)
  }
}

const resetForm = () => {
  formRef.value.resetFields()
  if (currentConfig.value) {
    Object.assign(form, currentConfig.value)
  } else {
    form.reply_rules = []
  }
}
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.reply-rule-item {
  margin-bottom: 15px;
}

.rule-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
