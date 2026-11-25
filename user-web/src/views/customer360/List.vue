<template>
  <div class="customer-360-page">
    <div class="left-panel">
      <el-card>
        <template #header>
          <span>{{ $t('客户搜索') }}</span>
        </template>
        <el-input
          v-model="searchKeyword"
          placeholder="搜索客户(姓名/手机/邮箱)"
          clearable
        />
        <div class="filter-row">
          <el-select
            v-model="filters.source_channel"
            clearable
            placeholder="来源渠道"
            size="small"
          >
            <el-option
              label="抖音"
              value="douyin"
            />
            <el-option
              label="小红书"
              value="xiaohongshu"
            />
            <el-option
              label="微信"
              value="wechat"
            />
            <el-option
              label="企微"
              value="wecom"
            />
            <el-option
              label="WhatsApp"
              value="whatsapp"
            />
            <el-option
              label="Telegram"
              value="telegram"
            />
            <el-option
              label="Web"
              value="web"
            />
          </el-select>
          <el-input
            v-model="filters.source_account_id"
            clearable
            placeholder="来源账号ID"
            size="small"
          />
        </div>
        <div class="filter-row">
          <el-input
            v-model="filters.industry"
            clearable
            placeholder="行业"
            size="small"
          />
          <el-input
            v-model="filters.company_id"
            clearable
            placeholder="公司ID"
            size="small"
          />
        </div>
        <el-table
          v-loading="loading"
          :data="customers"
          highlight-current-row
          style="margin-top: 15px"
          empty-text="暂无客户"
          @row-click="selectCustomer"
        >
          <el-table-column
            prop="name"
            label="姓名"
            min-width="120"
            show-overflow-tooltip
          />
          <el-table-column
            prop="phone"
            label="手机号"
            min-width="140"
            show-overflow-tooltip
          />
          <el-table-column
            label="来源"
            min-width="140"
            show-overflow-tooltip
          >
            <template #default="{ row }">
              {{ getChannelLabel(row.source) || '-' }}
            </template>
          </el-table-column>
        </el-table>
        <el-pagination
          v-if="total > pageSize"
          v-model:current-page="currentPage"
          :page-size="pageSize"
          :total="total"
          layout="prev, pager, next, total"
          size="small"
          class="pagination"
        />
      </el-card>
    </div>

    <div
      v-if="current"
      class="right-panel"
    >
      <el-card class="basic-info">
        <div class="customer-header">
          <el-avatar :size="60">
            {{ current.name?.charAt(0) }}
          </el-avatar>
          <div class="header-info">
            <h2>{{ current.name }}</h2>
            <div class="tags">
              <el-tag
                v-for="tag in current.tags"
                :key="tag"
                size="small"
              >
                {{ tag }}
              </el-tag>
            </div>
          </div>
          <div class="header-actions">
            <el-button
              type="primary"
              @click="contactCustomer"
            >
              联系客户
            </el-button>
            <el-button @click="addTag">
              添加标签
            </el-button>
          </div>
        </div>

        <el-descriptions
          :column="3"
          border
        >
          <el-descriptions-item label="手机号">
            {{ current.phone }}
          </el-descriptions-item>
          <el-descriptions-item label="邮箱">
            {{ current.email }}
          </el-descriptions-item>
          <el-descriptions-item label="微信号">
            {{ current.wechat }}
          </el-descriptions-item>
          <el-descriptions-item label="来源渠道">
            {{ getChannelLabel(current.sourceChannel || current.source) || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="来源账号">
            {{ current.sourceAccountId || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="来源会话">
            {{ current.sourceConversationId || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="来源消息">
            {{ current.sourceMessageId || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="首次触达">
            {{ fmtTime(current.firstTouchAt) || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="末次触达">
            {{ fmtTime(current.lastTouchAt) || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="注册时间">
            {{ fmtTime(current.createdAt) }}
          </el-descriptions-item>
          <el-descriptions-item label="最后活跃">
            {{ current.lastActive }}
          </el-descriptions-item>
          <el-descriptions-item label="客户状态">
            <el-tag :type="getCustomerStatusTagType(current.status)">
              {{ getCustomerStatusLabel(current.status) }}
            </el-tag>
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <el-tabs
        v-model="activeTab"
        class="detail-tabs"
      >
        <el-tab-pane
          label="基本信息"
          name="basic"
        >
          <el-card>
            <el-descriptions
              :column="2"
              border
            >
              <el-descriptions-item label="年龄">
                {{ current.age }}
              </el-descriptions-item>
              <el-descriptions-item label="性别">
                {{ current.gender }}
              </el-descriptions-item>
              <el-descriptions-item label="职业">
                {{ current.occupation }}
              </el-descriptions-item>
              <el-descriptions-item label="地区">
                {{ current.region }}
              </el-descriptions-item>
              <el-descriptions-item label="生日">
                {{ current.birthday }}
              </el-descriptions-item>
              <el-descriptions-item label="备注">
                {{ current.remark }}
              </el-descriptions-item>
            </el-descriptions>
          </el-card>
        </el-tab-pane>

        <el-tab-pane
          label="行为轨迹"
          name="behavior"
        >
          <el-card>
            <!-- 空态：后端 message_history 为空时，时间线什么都不渲染，
                 留下一张空白卡片像是页面没加载完。 -->
            <el-empty
              v-if="!behaviors.length"
              description="暂无行为记录"
              :image-size="80"
            />
            <el-timeline v-else>
              <el-timeline-item
                v-for="(item, idx) in behaviors"
                :key="idx"
                :timestamp="item.time"
                :type="item.type"
              >
                <h4>{{ item.action || '-' }}</h4>
                <p>{{ item.detail }}</p>
              </el-timeline-item>
            </el-timeline>
          </el-card>
        </el-tab-pane>

        <el-tab-pane
          label="沟通记录"
          name="communications"
        >
          <el-card>
            <el-empty
              v-if="!communications.length"
              description="暂无沟通记录"
              :image-size="80"
            />
            <el-timeline v-else>
              <el-timeline-item
                v-for="(item, idx) in communications"
                :key="idx"
                :timestamp="item.time"
                :type="item.direction === 'in' ? 'primary' : 'success'"
              >
                <h4>{{ getChannelLabel(item.channel) }} - {{ item.direction === 'in' ? '客户' : '我方' }}</h4>
                <p>{{ item.content }}</p>
              </el-timeline-item>
            </el-timeline>
          </el-card>
        </el-tab-pane>

        <el-tab-pane
          label="标签"
          name="tags"
        >
          <el-card>
            <el-tag
              v-for="tag in current.tags"
              :key="tag"
              closable
              style="margin: 5px"
              @close="removeTag(tag)"
            >
              {{ tag }}
            </el-tag>
            <el-button
              link
              type="primary"
              @click="addTag"
            >
              + 添加标签
            </el-button>
          </el-card>
        </el-tab-pane>

        <el-tab-pane
          label="订单记录"
          name="orders"
        >
          <el-card>
            <div
              v-if="!current.phone && !current.name"
              class="order-tip"
            >
              该客户缺少手机号/姓名，无法关联外部订单
            </div>
            <el-table
              v-else
              v-loading="ordersLoading"
              :data="orders"
              empty-text="暂无订单记录"
            >
              <el-table-column
                prop="order_id"
                label="订单号"
                width="170"
              />
              <el-table-column
                label="平台"
                width="100"
              >
                <template #default="{ row }">
                  {{ getChannelLabel(row.platform) }}
                </template>
              </el-table-column>
              <el-table-column
                prop="status"
                label="状态"
                width="110"
              />
              <el-table-column
                label="金额"
                width="120"
              >
                <template #default="{ row }">
                  ¥{{ ((row.total_amount || 0) / 100).toFixed(2) }}
                </template>
              </el-table-column>
              <el-table-column
                prop="pay_time"
                label="支付时间"
                width="160"
              >
                <template #default="{ row }">
                  {{ fmtTime(row.pay_time) }}
                </template>
              </el-table-column>
              <el-table-column
                prop="items"
                label="商品"
                min-width="160"
                show-overflow-tooltip
              />
            </el-table>
          </el-card>
        </el-tab-pane>

        <el-tab-pane
          label="公司关系"
          name="companies"
        >
          <el-card>
            <el-empty
              v-if="!companyRelations.length"
              description="暂无公司关系"
              :image-size="80"
            />
            <el-table
              v-else
              :data="companyRelations"
              empty-text="暂无公司关系"
            >
              <el-table-column
                prop="company_name"
                label="公司名称"
                min-width="160"
                show-overflow-tooltip
              />
              <el-table-column
                prop="industry"
                label="行业"
                min-width="120"
                show-overflow-tooltip
              />
              <el-table-column
                prop="relation_type"
                label="关系类型"
                width="120"
              />
              <el-table-column
                label="主关系"
                width="90"
              >
                <template #default="{ row }">
                  <el-tag
                    :type="row.is_primary ? 'success' : 'info'"
                    size="small"
                  >
                    {{ row.is_primary ? '是' : '否' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column
                prop="source"
                label="来源"
                min-width="120"
                show-overflow-tooltip
              />
            </el-table>
          </el-card>
        </el-tab-pane>

        <el-tab-pane
          label="跟进记录"
          name="followup-records"
        >
          <el-card v-loading="followupRecordsLoading">
            <div class="followup-record-head">
              <span class="followup-record-tip">记录可追溯；关联提醒时显示提醒 ID。</span>
              <el-button
                link
                type="primary"
                @click="openFollowupRecordDialog"
              >
                新增记录
              </el-button>
            </div>
            <el-empty
              v-if="!followupRecords.length && !followupRecordsLoading"
              description="暂无跟进记录"
              :image-size="80"
            />
            <el-timeline v-else>
              <el-timeline-item
                v-for="item in followupRecords"
                :key="item.record_id || item.id"
                :timestamp="fmtTime(item.created_at)"
                :type="item.reminder_id ? 'warning' : 'primary'"
              >
                <div class="followup-record-item">
                  <div class="followup-record-title">
                    <el-tag
                      size="small"
                      effect="plain"
                    >
                      {{ followupTypeLabel(item.followup_type) }}
                    </el-tag>
                    <el-tag
                      v-if="item.channel"
                      size="small"
                      type="info"
                      effect="plain"
                    >
                      {{ item.channel }}
                    </el-tag>
                    <el-tag
                      v-if="item.reminder_id"
                      size="small"
                      type="warning"
                      effect="plain"
                    >
                      提醒 {{ item.reminder_id }}
                    </el-tag>
                  </div>
                  <p
                    v-if="item.content"
                    class="followup-record-content"
                  >
                    {{ item.content }}
                  </p>
                  <p
                    v-if="item.result"
                    class="followup-record-meta"
                  >
                    <strong>结果：</strong>{{ item.result }}
                  </p>
                  <p
                    v-if="item.next_action"
                    class="followup-record-meta"
                  >
                    <strong>下一步：</strong>{{ item.next_action }}
                  </p>
                  <p
                    v-if="item.next_followup_at"
                    class="followup-record-meta"
                  >
                    <strong>下次跟进：</strong>{{ fmtTime(item.next_followup_at) }}
                  </p>
                  <p
                    v-if="item.source"
                    class="followup-record-meta"
                  >
                    <strong>来源：</strong>{{ item.source }}<span v-if="item.source_ref"> / {{ item.source_ref }}</span>
                  </p>
                </div>
              </el-timeline-item>
            </el-timeline>
          </el-card>
        </el-tab-pane>
      </el-tabs>
    </div>

    <div
      v-else
      class="empty-state"
    >
      <el-empty description="请从左侧选择客户" />
    </div>

    <el-dialog
      v-model="contactVisible"
      title="联系客户"
      width="500px"
    >
      <el-form
        :model="contactForm"
        label-position="top"
      >
        <el-form-item label="客户">
          <span>{{ current?.name }}</span>
        </el-form-item>
        <el-form-item label="消息内容">
          <el-input
            v-model="contactForm.content"
            type="textarea"
            :rows="5"
            placeholder="请输入要发送的消息内容..."
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="contactVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="submittingContact"
          @click="submitContact"
        >
          发送
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="followupRecordDialogVisible"
      title="新增跟进记录"
      width="560px"
    >
      <el-form
        :model="followupRecordForm"
        label-width="90px"
      >
        <el-form-item label="跟进类型">
          <el-select
            v-model="followupRecordForm.followup_type"
            style="width: 100%"
          >
            <el-option
              label="备注"
              value="note"
            />
            <el-option
              label="电话"
              value="call"
            />
            <el-option
              label="会议"
              value="meeting"
            />
            <el-option
              label="微信"
              value="wechat"
            />
            <el-option
              label="邮件"
              value="email"
            />
            <el-option
              label="拜访"
              value="visit"
            />
            <el-option
              label="报价"
              value="quote"
            />
            <el-option
              label="其他"
              value="other"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="渠道">
          <el-input
            v-model="followupRecordForm.channel"
            placeholder="如 wechat/douyin/email"
          />
        </el-form-item>
        <el-form-item label="关联提醒ID">
          <el-input
            v-model="followupRecordForm.reminder_id"
            placeholder="可选，关联 FollowUpReminder"
          />
        </el-form-item>
        <el-form-item
          label="跟进内容"
          required
        >
          <el-input
            v-model="followupRecordForm.content"
            type="textarea"
            :rows="4"
            placeholder="记录本次跟进内容"
          />
        </el-form-item>
        <el-form-item label="结果">
          <el-input
            v-model="followupRecordForm.result"
            type="textarea"
            :rows="2"
            placeholder="本次跟进结果"
          />
        </el-form-item>
        <el-form-item label="下一步">
          <el-input
            v-model="followupRecordForm.next_action"
            type="textarea"
            :rows="2"
            placeholder="下一步行动"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="followupRecordDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="followupRecordSubmitting"
          @click="submitFollowupRecord"
        >
          创建
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getCustomerList, getCustomerDetail, addCustomerTag, removeCustomerTag, getCustomerEvents, getCustomerMessages } from '@/api/customer360.js'
import { createSession, sendMessage } from '@/api/customerSession.js'
import { getExternalOrdersByCustomer } from '@/api/integration.js'
import { followupRecordApi } from '@/api/followupRecord.js'
import { toList } from '@/utils/list'
import { getChannelLabel } from '@/constants/channel';
import {
  getCustomerStatusLabel as getCustomerStatusLabelFromConst,
  getCustomerStatusTagType as getCustomerStatusTagTypeFromConst,
} from '@/constants/customerTag';
import { getEnabledLabel, getEnabledTagType } from '@/constants/enabled';
import { useRoute } from 'vue-router'
import { fmtTime } from '@/utils/time'
const route = useRoute()

const getCustomerStatusLabel = (s) => getCustomerStatusLabelFromConst(s) || getEnabledLabel(s);
const getCustomerStatusTagType = (s) => getCustomerStatusTagTypeFromConst(s) || getEnabledTagType(s)

const loading = ref(false)
const searchKeyword = ref('')
const filters = ref({
  source_channel: '',
  source_account_id: '',
  industry: '',
  company_id: ''
})
const customers = ref([])
const total = ref(0)
const current = ref(null)
const activeTab = ref('basic')
const behaviors = ref([])
const communications = ref([])
const orders = ref([])
const ordersLoading = ref(false)
const companyRelations = ref([])
const followupRecords = ref([])
const followupRecordsLoading = ref(false)
const followupRecordDialogVisible = ref(false)
const followupRecordSubmitting = ref(false)
const followupRecordForm = ref({
  followup_type: 'note',
  channel: '',
  content: '',
  result: '',
  next_action: '',
  reminder_id: '',
  source: 'manual'
})
const contactVisible = ref(false)
const contactForm = ref({ content: '' })
const currentPage = ref(1)
const selectingId = ref(null)
const orderRequestVersion = ref(0)
const followupRecordRequestVersion = ref(0)
const pageSize = ref(20)

let listRequestVersion = 0

const loadCustomers = async () => {
  const requestVersion = ++listRequestVersion
  loading.value = true
  customers.value = []
  total.value = 0
  try {
    const res = await getCustomerList({
      page: currentPage.value,
      page_size: pageSize.value,
      keyword: searchKeyword.value.trim(),
      source_channel: filters.value.source_channel || undefined,
      source_account_id: filters.value.source_account_id || undefined,
      industry: filters.value.industry || undefined,
      company_id: filters.value.company_id || undefined
    })
    if (requestVersion !== listRequestVersion) return
    const map = res?.list
    customers.value = map && typeof map === 'object' && !Array.isArray(map)
      ? Object.entries(map).map(([uid, c]) => {
          const b = c?.basic_info || {}
          return {
            id: b.user_id || uid,
            name: b.user_name || uid,
            phone: b.user_phone || '',
            email: b.user_email || '',
            source: b.source_channel || b.source_platform || ''
          }
        })
      : toList(res);
    total.value = Number(res?.total) || 0
  } catch {
    if (requestVersion !== listRequestVersion) return
    customers.value = []
    total.value = 0
  } finally {
    if (requestVersion === listRequestVersion) loading.value = false
  }
}

watch(searchKeyword, () => {
  if (currentPage.value !== 1) currentPage.value = 1
  else loadCustomers()
}, { flush: 'sync' })
watch(filters, () => {
  if (currentPage.value !== 1) currentPage.value = 1
  else loadCustomers()
}, { deep: true, flush: 'sync' })
watch(currentPage, loadCustomers, { flush: 'sync' })
onBeforeUnmount(() => { listRequestVersion += 1 })

const toCustomerModel = (raw, fallbackId) => {
  const b = raw?.basic_info || {}
  return {
    id: b.user_id || fallbackId,
    name: b.user_name || b.user_id || fallbackId,
    phone: b.user_phone || '',
    email: b.user_email || '',
    wechat: '',
    source: b.source_platform || b.source_channel || '',
    sourceAccountId: b.source_account_id || '',
    sourceChannel: b.source_channel || b.source_platform || '',
    sourceConversationId: b.source_conversation_id || '',
    sourceMessageId: b.source_message_id || '',
    firstTouchAt: b.first_touch_at || '',
    lastTouchAt: b.last_touch_at || '',
    companyId: b.company_id || null,
    createdAt: b.first_seen_at || '',
    lastActive: b.last_seen_at || '',
    status: 'active',
    tags: raw?.tags || []
  }
};

const selectCustomer = async (row) => {
  // 竞态守卫：快速连点客户行时，先点客户的慢响应不得覆盖后点客户的面板
  selectingId.value = row.id
  orderRequestVersion.value += 1
  followupRecordRequestVersion.value += 1
  orders.value = []
  ordersLoading.value = false
  current.value = null
  behaviors.value = []
  communications.value = []
  companyRelations.value = []
  followupRecords.value = []
  followupRecordsLoading.value = false
  try {
    // 详情接口只返回 basic + tags（message_history 恒为空数组），
    // 行为轨迹要事件流水、沟通记录要消息流水，必须各自单独拉。
    const [raw, eventsRes, messagesRes] = await Promise.allSettled([
      getCustomerDetail(row.id),
      getCustomerEvents(row.id, { limit: 50 }),
      getCustomerMessages(row.id, { limit: 50 })
    ])
    if (selectingId.value !== row.id) return
    const detail = eventsRes.status === 'fulfilled' ? eventsRes.value : null
    const msgs = messagesRes.status === 'fulfilled' ? messagesRes.value : null
    if (raw.status !== 'fulfilled' || !raw.value) {
      current.value = null
      behaviors.value = []
      communications.value = []
      orders.value = []
      companyRelations.value = []
      followupRecords.value = []
      ElMessage.error('加载客户详情失败，请稍后重试')
      return
    }
    current.value = toCustomerModel(raw.value, row.id)
    companyRelations.value = Array.isArray(raw.value?.company_relations) ? raw.value.company_relations : []
    // 行为轨迹：customer_events 事件流水（event_type / event_source / event_data）
    const events = toList(detail?.events)
    behaviors.value = events.map((ev) => ({
      time: fmtTime(ev.occurred_at || ev.created_at),
      type: 'primary',
      action: ev.event_type || '-',
      detail: [ev.event_source, ev.event_data].filter(Boolean).join(' · ')
    }))
    // 沟通记录：消息流水（sender_type=user 为「客户」，其余为「我方」）
    const messages = toList(msgs?.message_history)
    communications.value = messages.map((m) => ({
      time: fmtTime(m.created_at),
      channel: 'web',
      direction: m.sender_type === 'user' ? 'in' : 'out',
      content: m.content
    }))
    const requestVersion = ++orderRequestVersion.value
    loadOrders(current.value.phone, current.value.name, requestVersion);
    const frVersion = ++followupRecordRequestVersion.value
    loadFollowupRecords(current.value.id, frVersion);
  } catch (e) {
    console.error('加载客户详情失败:', e)
  }
}

const loadOrders = async (phone, name, requestVersion = ++orderRequestVersion.value) => {
  orders.value = []
  if (!phone && !name) return
  ordersLoading.value = true
  try {
    const res = await getExternalOrdersByCustomer(phone, name)
    if (requestVersion !== orderRequestVersion.value) return
    orders.value = toList(res?.list)
  } catch {
    if (requestVersion === orderRequestVersion.value) orders.value = []
  } finally {
    if (requestVersion === orderRequestVersion.value) ordersLoading.value = false
  }
};

const followupTypeLabel = (type) => ({
  note: '备注',
  call: '电话',
  meeting: '会议',
  wechat: '微信',
  email: '邮件',
  visit: '拜访',
  quote: '报价',
  other: '其他'
}[type] || type || '跟进')

const loadFollowupRecords = async (customerId, requestVersion = ++followupRecordRequestVersion.value) => {
  followupRecords.value = []
  if (!customerId) return
  followupRecordsLoading.value = true
  try {
    const res = await followupRecordApi.list({ customer_id: customerId, page: 1, page_size: 50 })
    if (requestVersion !== followupRecordRequestVersion.value) return
    const payload = res?.data || res
    followupRecords.value = toList(payload?.list ?? payload?.items ?? payload)
  } catch {
    if (requestVersion === followupRecordRequestVersion.value) followupRecords.value = []
  } finally {
    if (requestVersion === followupRecordRequestVersion.value) followupRecordsLoading.value = false
  }
};

const openFollowupRecordDialog = () => {
  followupRecordForm.value = {
    followup_type: 'note',
    channel: '',
    content: '',
    result: '',
    next_action: '',
    reminder_id: '',
    source: 'manual'
  }
  followupRecordDialogVisible.value = true
}

const submitFollowupRecord = async () => {
  if (!current.value?.id) return
  if (!followupRecordForm.value.content?.trim()) {
    ElMessage.warning('请输入跟进内容')
    return
  }
  if (followupRecordSubmitting.value) return
  followupRecordSubmitting.value = true
  try {
    await followupRecordApi.create({
      customer_id: String(current.value.id),
      followup_type: followupRecordForm.value.followup_type || 'note',
      channel: followupRecordForm.value.channel || '',
      content: followupRecordForm.value.content.trim(),
      result: followupRecordForm.value.result || '',
      next_action: followupRecordForm.value.next_action || '',
      reminder_id: followupRecordForm.value.reminder_id || '',
      source: followupRecordForm.value.source || 'manual'
    })
    ElMessage.success('跟进记录已创建')
    followupRecordDialogVisible.value = false
    const frVersion = ++followupRecordRequestVersion.value
    await loadFollowupRecords(current.value.id, frVersion)
  } catch {
    ElMessage.error('创建跟进记录失败，请稍后重试')
  } finally {
    followupRecordSubmitting.value = false
  }
}

const contactCustomer = () => {
  contactForm.value.content = ''
  contactVisible.value = true
}

const submittingContact = ref(false)
const submitContact = async () => {
  if (!contactForm.value.content.trim()) {
    ElMessage.warning(i18n.global.t('请输入消息内容'))
    return
  }
  // 提交锁：createSession+sendMessage 两串行请求期间双击会创建重复会话/消息
  if (submittingContact.value) return
  submittingContact.value = true
  try {
    const res = await createSession({
      platform: 'web',
      account_id: 'default',
      user_id: String(current.value.id),
      user_name: current.value.name || '',
      user_phone: current.value.phone || ''
    })
    const sessionId = res?.id;
    await sendMessage({ sessionId, content: contactForm.value.content, sender_type: 'agent' })
    ElMessage.success(i18n.global.t('会话已创建，消息已发送'))
    contactVisible.value = false
  } catch {
    ElMessage.error(i18n.global.t('发送失败，请稍后重试'))
  } finally {
    submittingContact.value = false
  }
}

const addTag = async () => {
  try {
    const { value } = await ElMessageBox.prompt('请输入标签名', '添加标签', { confirmButtonText: '添加' })
    if (value) {
      await addCustomerTag(current.value.id, value)
      ElMessage.success(i18n.global.t('标签已添加'))
      if (!current.value.tags) current.value.tags = []
      current.value.tags.push(value)
    }
  } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
}

const removeTag = async (tag) => {
  await removeCustomerTag(current.value.id, tag)
  current.value.tags = current.value.tags.filter((t) => t !== tag)
  ElMessage.success(i18n.global.t('标签已删除'))
}

onMounted(async () => {
  await loadCustomers()
  const id = route.query?.id
  if (typeof id === 'string' && id) await selectCustomer({ id })
})
</script>

<style scoped lang="scss">
.customer-360-page {
  padding: 20px;
  display: grid;
  grid-template-columns: 350px 1fr;
  gap: 20px;
  min-height: calc(100vh - 100px);
}
.left-panel {
  .filter-row {
    display: flex;
    gap: 8px;
    margin-top: 8px;
    > * { flex: 1; min-width: 0; }
  }
}
.right-panel {
  .customer-header {
    display: flex;
    align-items: center;
    gap: 20px;
    margin-bottom: 20px;
    .header-info {
      flex: 1;
      h2 { margin: 0 0 5px 0; }
      .tags { display: flex; gap: 5px; flex-wrap: wrap; }
    }
    .header-actions { display: flex; gap: 10px; }
  }
  .basic-info { margin-bottom: 20px; }
}
.empty-state {
  grid-column: 2;
  display: flex;
  align-items: center;
  justify-content: center;
}
.pagination {
  margin-top: 12px;
  justify-content: flex-end;
}
.followup-record-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.followup-record-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.followup-record-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.followup-record-title {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.followup-record-content {
  margin: 0;
  white-space: pre-wrap;
  color: var(--el-text-color-primary);
}
.followup-record-meta {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: pre-wrap;
}
</style>
