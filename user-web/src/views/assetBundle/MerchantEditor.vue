<template>
  <div class="merchant-editor">
    
    <el-card class="status-bar" shadow="never">
      <div class="status-row">
        <div class="status-left">
          <el-icon v-if="form.status === 'active'" class="status-icon active"><Monitor /></el-icon>
          <el-icon v-else class="status-icon paused"><VideoPause /></el-icon>
          <div class="status-text">
            <div class="status-title">
              <span v-if="form.status === 'active'">🤖 托管中（AI 正在使用该话术包服务独立站和 WhatsApp）</span>
              <span v-else>⏸️ 已暂停托管</span>
            </div>
            <div class="status-sub">
              资产 ID: {{ form.asset_id || '未保存' }} · 版本: {{ form.version || '1.0.0' }}
            </div>
          </div>
        </div>
        <div class="status-right">
          <el-button
            v-if="form.status === 'active'"
            type="warning"
            @click="handleToggleStatus('pause')"
          >
            ⏸️ 暂停托管
          </el-button>
          <el-button
            v-else
            type="success"
            @click="handleToggleStatus('resume')"
          >
            ▶️ 恢复托管
          </el-button>
          <el-button type="primary" :loading="saving" @click="handleSave">
            💾 保存配置并立刻热更新到商户本地 AI 引擎
          </el-button>
        </div>
      </div>
    </el-card>

    <el-form ref="formRef" :model="form" label-width="160px" class="form-body" v-loading="loading">
      
      <el-card class="section" shadow="never">
        <template #header>
          <div class="section-header">
            <el-icon><Goods /></el-icon>
            <span>1. 基础经营策略参数</span>
            <span class="section-hint">（自动复写映射到大模型主 System 指令中）</span>
          </div>
        </template>
        <el-row :gutter="20">
          <el-col :span="6">
            <el-form-item label="封面图">
              <el-upload
                class="cover-uploader"
                :http-request="handleCoverUpload"
                :show-file-list="false"
                accept=".jpg,.jpeg,.png,.gif,.webp"
              >
                <el-image
                  v-if="form.cover_image"
                  :src="form.cover_image"
                  :preview-src-list="[form.cover_image]"
                  fit="cover"
                  class="cover-preview"
                />
                <el-icon v-else class="cover-uploader-icon"><Plus /></el-icon>
              </el-upload>
              <div class="cover-tip">点击上传封面图</div>
            </el-form-item>
          </el-col>
          <el-col :span="18">
            <el-form-item label="资产包名称">
              <el-input v-model="form.title" placeholder="例如：跨境成人用品私域销冠自动留资话术包" />
            </el-form-item>
            <el-form-item label="资产 ID">
              <el-input v-model="form.asset_id" placeholder="例如：hive_sales_vape_cn_001（保存后不可修改）" :disabled="isEdit" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="当前店铺促销活动名称">
              <el-input v-model="form.campaign_name" placeholder="例如：双十一全球年终大促" />
            </el-form-item>
          </el-col>
          <el-col :span="6">
            <el-form-item label="官方全场优惠券比例">
              <el-input v-model="form.discount_pct" placeholder="例如：15%" />
            </el-form-item>
          </el-col>
          <el-col :span="6">
            <el-form-item label="客服联系方式">
              <el-input v-model="form.support_contact" placeholder="例如：WhatsApp +49 176123456" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="核心品牌/店铺名称">
              <el-input v-model="form.shop_name" placeholder="例如：HiveVape 国际独立商城" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="开发者（署名）">
              <el-input v-model="form.author" placeholder="例如：出海精英_阿杰" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <el-card class="section" shadow="never">
        <template #header>
          <div class="section-header">
            <el-icon><Lock /></el-icon>
            <span>2. 6 维拟人门禁指标阀门</span>
            <span class="section-hint">（自动折算为 System 顶层约束权重）</span>
          </div>
        </template>
        <el-row :gutter="20">
          <el-col :span="8">
            <el-form-item label="🔴 危机感触发行">
              <el-select v-model="form.crisis_threshold" placeholder="选择阈值">
                <el-option label="危机感 ≥ 2 分时，强制触发转人工门禁" value="2" />
                <el-option label="危机感 ≥ 3 分时，强制触发转人工门禁" value="3" />
                <el-option label="危机感 ≥ 4 分时，强制触发转人工门禁（推荐）" value="4" />
                <el-option label="危机感 ≥ 5 分时，强制触发转人工门禁" value="5" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="🟢 语气词口语化修饰度">
              <el-select v-model="form.tone_level" placeholder="选择等级">
                <el-option label="低（专业克制，避免情绪化）" value="low" />
                <el-option label="中等（适度增加哈/哦/🥰）" value="medium" />
                <el-option label="高（情绪词频繁，真人感强）" value="high" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="🔵 反审查自由度尺度">
              <el-radio-group v-model="form.censorship_level">
                <el-radio label="strict">严苛合规</el-radio>
                <el-radio label="unlock">100% 完全解锁</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="启用结算意图">
          <el-checkbox-group v-model="form.enabled_intents">
            <el-checkbox label="faq">普通咨询 (faq)</el-checkbox>
            <el-checkbox label="lead_capture">留资意向 (lead_capture)</el-checkbox>
            <el-checkbox label="human_transfer">转人工 (human_transfer)</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
      </el-card>

      
      <el-card class="section" shadow="never">
        <template #header>
          <div class="section-header">
            <el-icon><ChatDotRound /></el-icon>
            <span>3. 可视化商户快捷话术微调</span>
            <span class="section-hint">（Few-Shots 的前端图形化乐高组件）</span>
          </div>
        </template>
        <div class="qa-list">
          <el-card
            v-for="(card, idx) in form.qa_cards"
            :key="card.id"
            class="qa-card"
            shadow="hover"
          >
            <div class="qa-card-header">
              <span class="qa-card-title">▼ 行业经典答疑卡片 {{ idx + 1 }}</span>
              <el-button type="danger" link @click="removeQACard(card.id)">删除</el-button>
            </div>
            <el-form-item label="触发场景描述">
              <el-input v-model="card.trigger" placeholder="例如：关于物流与清关" />
            </el-form-item>
            <el-form-item label="客户提问示例">
              <el-input
                v-model="card.user_example"
                type="textarea"
                :rows="2"
                placeholder="例如：海关扣货、包装是否隐蔽、能不能寄到某国家时"
              />
            </el-form-item>
            <el-form-item label="🤖 AI 的标准应答话术">
              <el-input
                v-model="card.reply"
                type="textarea"
                :rows="4"
                placeholder="例如：我们每天大量发货。采用100%全隐形无标记包装，全网清关率高达99%，请您完全放心！"
              />
            </el-form-item>
          </el-card>
        </div>
        <el-button type="primary" plain class="add-card-btn" @click="addQACard">
          ➕ 新增商户自家店铺专属的提问应答卡片
        </el-button>
      </el-card>

      
      <el-card class="section" shadow="never">
        <template #header>
          <div class="section-header">
            <el-icon><Grid /></el-icon>
            <span>4. 乐高式多媒体卡片消息快捷配置</span>
            <span class="section-hint">（自动转化为 Output 结算约束协议）</span>
          </div>
        </template>
        <el-row :gutter="20">
          <el-col :span="8">
            <el-form-item label="触发意图结算类型">
              <el-radio-group v-model="form.card_config.intent_type">
                <el-radio label="button_card">按钮跳转卡片</el-radio>
                <el-radio label="coupon">优惠券发放表单</el-radio>
                <el-radio label="handoff">转人工提示</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-col>
          <el-col :span="16">
            <el-form-item label="绑定商品主图">
              <el-input v-model="form.card_config.product_image" placeholder="例如：https://xapptool.cn/product.jpg" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-divider>动作按钮链配置</el-divider>
        <div class="button-list">
          <el-card
            v-for="(btn, idx) in form.card_config.buttons"
            :key="idx"
            class="button-card"
            shadow="hover"
          >
            <el-row :gutter="10">
              <el-col :span="8">
                <el-form-item label="按钮标题">
                  <el-input v-model="btn.title" placeholder="例如：🛒 独立站直接购买" />
                </el-form-item>
              </el-col>
              <el-col :span="6">
                <el-form-item label="触发动作">
                  <el-select v-model="btn.action">
                    <el-option label="跳转 URL" value="open_url" />
                    <el-option label="触发本地工具 API" value="call_api" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item v-if="btn.action === 'open_url'" label="跳转 URL">
                  <el-input v-model="btn.url" placeholder="https://shopify.com/..." />
                </el-form-item>
                <el-form-item v-else label="本地工具名">
                  <el-input v-model="btn.api_name" placeholder="例如：reach.send_coupon" />
                </el-form-item>
              </el-col>
              <el-col :span="2">
                <el-button type="danger" link @click="removeButton(idx)">删除</el-button>
              </el-col>
            </el-row>
            <el-form-item v-if="btn.action === 'call_api'" label="工具参数 (JSON)">
              <el-input v-model="btn.api_args" placeholder='例如：{"coupon_id":"SUMMER15"}' />
            </el-form-item>
          </el-card>
        </div>
        <el-button type="primary" plain class="add-card-btn" @click="addButton">
          ➕ 新增动作按钮
        </el-button>
      </el-card>

      
      <div class="footer-actions">
        <el-button type="primary" size="large" :loading="saving" @click="handleSave">
          💾 保存配置并立刻热更新到商户本地 AI 引擎
        </el-button>
        <el-button size="large" @click="$router.push('/asset-bundle/list')">返回列表</el-button>
      </div>
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Monitor, VideoPause, Goods, Lock, ChatDotRound, Grid } from '@element-plus/icons-vue'
import { merchantSave, merchantParse, enableBundle, disableBundle, getBundleByAssetID, uploadCover } from '@/api/assetBundle'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const route = useRoute()
const router = useRouter()
const formRef = ref(null)
const loading = ref(false)
const saving = ref(false)

const aid = computed(() => route.params.aid || '')
const isEdit = computed(() => !!aid.value)

const form = reactive({
  asset_id: '',
  title: '',
  author: '',
  cover_image: '',
  shop_name: '',
  campaign_name: '',
  discount_pct: '',
  support_contact: '',
  crisis_threshold: '4',
  tone_level: 'medium',
  censorship_level: 'strict',
  enabled_intents: ['faq', 'lead_capture', 'human_transfer'],
  qa_cards: [],
  card_config: {
    intent_type: 'button_card',
    product_image: '',
    buttons: []
  },
  status: 'draft',
  version: '1.0.0',
  template_asset_id: ''
});

const genCardId = () => `card_${Date.now()}_${Math.floor(Math.random() * 1000)}`;

const addQACard = () => {
  form.qa_cards.push({
    id: genCardId(),
    trigger: '',
    user_example: '',
    reply: '',
    order: form.qa_cards.length
  })
}

const removeQACard = (id) => {
  const idx = form.qa_cards.findIndex(c => c.id === id)
  if (idx >= 0) form.qa_cards.splice(idx, 1)
}

const addButton = () => {
  form.card_config.buttons.push({
    title: '',
    action: 'open_url',
    url: '',
    api_name: '',
    api_args: '',
    order: form.card_config.buttons.length
  })
}

const removeButton = (idx) => {
  form.card_config.buttons.splice(idx, 1)
}

const loadBundle = async () => {
  if (!aid.value) return
  loading.value = true
  try {
    const detailResp = await getBundleByAssetID(aid.value);
    const detail = detailResp?.data || detailResp
    if (detail) {
      form.title = detail.title || ''
      form.author = detail.author || ''
      form.version = detail.version || '1.0.0'
      form.status = detail.status || 'draft'
    }
    const parseResp = await merchantParse(aid.value);
    const parsed = parseResp?.data || parseResp
    if (parsed) {
      form.shop_name = parsed.shop_name || ''
      form.campaign_name = parsed.campaign_name || ''
      form.discount_pct = parsed.discount_pct || ''
      form.support_contact = parsed.support_contact || ''
      form.qa_cards = (parsed.qa_cards || []).map(c => ({ ...c, id: c.id || genCardId() }))
      if (parsed.card_config) {
        form.card_config = {
          intent_type: parsed.card_config.intent_type || 'button_card',
          product_image: parsed.card_config.product_image || '',
          buttons: parsed.card_config.buttons || []
        }
      }
      if (parsed.enabled_intents && parsed.enabled_intents.length) {
        form.enabled_intents = parsed.enabled_intents
      }
      form.crisis_threshold = parsed.crisis_threshold || '4';
      form.tone_level = parsed.tone_level || 'medium'
      form.censorship_level = parsed.censorship_level || 'default'
    }
  } catch (e) {
    ElMessage.error('加载资产包失败: ' + (e?.message || e))
  } finally {
    loading.value = false
  }
};

const handleCoverUpload = async ({ file }) => {
  try {
    const res = await uploadCover(file)
    form.cover_image = res.data?.url || res.url || res
    ElMessage.success('封面上传成功（保存后生效）')
  } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
};

const handleSave = async () => {
  if (!form.asset_id) {
    ElMessage.warning('请填写资产 ID')
    return
  }
  if (!form.title) {
    ElMessage.warning('请填写资产包名称')
    return
  }
  saving.value = true
  try {
    const payload = {
      asset_id: form.asset_id,
      title: form.title,
      author: form.author || userStore.username,
      shop_name: form.shop_name,
      campaign_name: form.campaign_name,
      discount_pct: form.discount_pct,
      support_contact: form.support_contact,
      crisis_threshold: form.crisis_threshold,
      tone_level: form.tone_level,
      censorship_level: form.censorship_level,
      enabled_intents: form.enabled_intents,
      qa_cards: form.qa_cards.map((c, i) => ({ ...c, order: i })),
      card_config: {
        ...form.card_config,
        buttons: form.card_config.buttons.map((b, i) => ({ ...b, order: i }))
      },
      template_asset_id: form.template_asset_id
    }
    const resp = await merchantSave(payload)
    const data = resp?.data || resp
    if (data && data.id) {
      try {
        await enableBundle(data.id)
        ElMessage.success('保存并热启用成功')
      } catch (e) {
        ElMessage.warning('保存成功，但热启用失败，请手动启用')
      }
      if (!isEdit.value && data.asset_id) {
        router.replace(`/asset-bundle/merchant/${data.asset_id}`)
      }
    } else {
      ElMessage.success('保存成功')
    }
  } catch (e) {
    ElMessage.error('保存失败: ' + (e?.message || e))
  } finally {
    saving.value = false
  }
};

const handleToggleStatus = async (action) => {
  if (!aid.value) {
    ElMessage.warning('请先保存资产包')
    return
  }
  try {
    const detailResp = await getBundleByAssetID(aid.value);
    const detail = detailResp?.data || detailResp
    if (!detail || !detail.id) {
      ElMessage.error('找不到资产包')
      return
    }
    if (action === 'pause') {
      await disableBundle(detail.id)
      form.status = 'inactive'
      ElMessage.success('已暂停托管')
    } else {
      await enableBundle(detail.id)
      form.status = 'active'
      ElMessage.success('已恢复托管')
    }
  } catch (e) {
    ElMessage.error('状态切换失败: ' + (e?.message || e))
  }
};

onMounted(() => {
  if (aid.value) {
    form.asset_id = aid.value
    loadBundle()
  } else {
    addQACard();
  }
})
</script>

<style scoped>
.merchant-editor {
  padding: 16px;
}
.status-bar {
  margin-bottom: 16px;
}
.status-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}
.status-left {
  display: flex;
  align-items: center;
  gap: 12px;
}
.status-icon {
  font-size: 32px;
}
.status-icon.active {
  color: #67c23a;
}
.status-icon.paused {
  color: #e6a23c;
}
.status-title {
  font-size: 16px;
  font-weight: 600;
}
.status-sub {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
.section {
  margin-bottom: 16px;
}
.section-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}
.section-hint {
  font-size: 12px;
  color: #909399;
  font-weight: normal;
}
.qa-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 12px;
}
.qa-card {
  border-left: 4px solid #409eff;
}
.qa-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  font-weight: 600;
}
.add-card-btn {
  width: 100%;
  border: 1px dashed #409eff;
}
.button-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 12px;
}
.button-card {
  border-left: 4px solid #67c23a;
}
.footer-actions {
  display: flex;
  justify-content: center;
  gap: 12px;
  padding: 24px 0;
}
.cover-uploader :deep(.el-upload) {
  border: 1px dashed #d9d9d9;
  border-radius: 8px;
  cursor: pointer;
  overflow: hidden;
  width: 120px;
  height: 120px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: border-color 0.2s;
}
.cover-uploader :deep(.el-upload:hover) {
  border-color: #409eff;
}
.cover-preview {
  width: 120px;
  height: 120px;
  object-fit: cover;
  display: block;
}
.cover-uploader-icon {
  font-size: 28px;
  color: #8c939d;
}
.cover-tip {
  margin-top: 4px;
  font-size: 11px;
  color: #909399;
  text-align: center;
}
</style>
