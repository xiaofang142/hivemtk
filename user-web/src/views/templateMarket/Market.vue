<template>
  <div class="template-market">
    <el-card
      shadow="never"
      class="mb-12"
    >
      <div class="workbench">
        <div class="workbench-title">
          {{ tmT('templateMarketPage.subtitle') }}
        </div>
        <div class="workbench-actions">
          <!-- S-22 闭环工作台入口：发布/我的/审核。匿名访客只看不发
               （发布与订单都要登录；审核台仅 admin，后端 AdminAuthMiddleware 收口）。 -->
          <el-button
            v-if="userStore.isLoggedIn"
            type="primary"
            size="small"
            @click="publishVisible = true"
          >
            {{ tmT('templateMarketPage.publishCta') }}
          </el-button>
          <el-button
            v-if="userStore.isLoggedIn"
            size="small"
            @click="$router.push('/template-market/my')"
          >
            {{ tmT('templateMarketPage.myWorkspace') }}
          </el-button>
          <el-button
            v-if="userStore.isAdmin"
            size="small"
            @click="$router.push('/template-market/review')"
          >
            {{ tmT('templateMarketPage.reviewConsole') }}
          </el-button>
        </div>
      </div>
    </el-card>

    <el-card
      shadow="never"
      class="mb-12"
    >
      <el-form inline>
        <el-form-item label="类型">
          <el-select
            v-model="filter.kind"
            clearable
            placeholder="全部"
            style="width: 120px"
          >
            <el-option
              v-for="(label, k) in ENTRY_KIND_LABEL"
              :key="k"
              :label="label"
              :value="k"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="分类">
          <el-select
            v-model="filter.category"
            clearable
            placeholder="全部"
            style="width: 160px"
          >
            <el-option
              v-for="c in categories"
              :key="c"
              :label="categoryLabel(c)"
              :value="c"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-input
            v-model="filter.keyword"
            placeholder="名称 / 简介 / 标签"
            clearable
            style="width: 220px"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            @click="fetch"
          >
            刷新
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <div
      v-loading="loading"
      class="grid"
    >
      <el-card
        v-for="m in visible"
        :key="m.id"
        class="entry-card"
        shadow="hover"
      >
        <div class="head">
          <span class="icon">
            <el-icon><component :is="m.kind === 'skill' ? 'MagicStick' : 'Document'" /></el-icon>
          </span>
          <div class="titles">
            <div class="name">
              {{ m.name }}
              <el-tag
                size="small"
                :type="m.kind === 'skill' ? 'primary' : 'success'"
                effect="plain"
              >
                {{ ENTRY_KIND_LABEL[m.kind] || m.kind }}
              </el-tag>
            </div>
            <div class="id">
              {{ m.id }}
            </div>
          </div>
        </div>

        <p class="summary">
          {{ m.summary || '暂无描述' }}
        </p>

        <div class="meta">
          <el-tag
            size="small"
            effect="plain"
          >
            {{ categoryLabel(m.category) }}
          </el-tag>
          <el-tag
            size="small"
            :type="AVAILABILITY_TAG_TYPE[m.availability] || 'info'"
            effect="plain"
          >
            {{ AVAILABILITY_LABEL[m.availability] || m.availability }}
          </el-tag>
        </div>

        <div
          v-if="m.tags && m.tags.length"
          class="tags"
        >
          <el-tag
            v-for="t in m.tags"
            :key="t"
            size="small"
            type="info"
            effect="plain"
            class="tag"
          >
            {{ t }}
          </el-tag>
        </div>

        <div class="actions">
          <el-button
            size="small"
            @click="openDetail(m)"
          >
            查看详情
          </el-button>
        </div>
      </el-card>
    </div>

    <el-empty
      v-if="!loading && visible.length === 0"
      description="没有匹配的模板或技能"
    />

    <el-dialog
      v-model="detailVisible"
      :title="detail ? detail.name : ''"
      width="560px"
    >
      <div
        v-if="detail"
        class="detail-body"
      >
        <div class="row">
          <span class="k">ID</span>
          <span class="v">{{ detail.id }}</span>
        </div>
        <div class="row">
          <span class="k">类型</span>
          <span class="v">{{ ENTRY_KIND_LABEL[detail.kind] || detail.kind }}</span>
        </div>
        <div class="row">
          <span class="k">分类</span>
          <span class="v">{{ categoryLabel(detail.category) }}</span>
        </div>
        <div class="row">
          <span class="k">可用性</span>
          <span class="v">{{ AVAILABILITY_LABEL[detail.availability] || detail.availability }}</span>
        </div>
        <div class="row">
          <span class="k">简介</span>
          <span class="v">{{ detail.summary }}</span>
        </div>
        <div class="row">
          <span class="k">来源</span>
          <span class="v mono">{{ detail.source }}</span>
        </div>
        <div class="row">
          <span class="k">价格</span>
          <span class="v">{{ marketPriceText(detail.price_cents) }}</span>
        </div>
      </div>
      <template #footer>
        <span
          v-if="detail && detail.price_cents > 0"
          class="pay-hint"
        >{{ tmT('templateMarketPage.payGuide') }}</span>
      </template>
    </el-dialog>

    <!-- 发布入口（S-22）：提审落审核台，审核通过自动上橱窗。
         v-if 门保证匿名橱窗（含单测挂载）不会实例化表单组件。 -->
    <el-dialog
      v-model="publishVisible"
      :title="tmT('templateMarketPage.publishTitle')"
      width="560px"
      destroy-on-close
    >
      <PublishForm
        v-if="publishVisible"
        ref="publishFormRef"
        @submitted="onSubmitted"
      />
      <template #footer>
        <el-button @click="publishVisible = false">
          {{ tmT('templateMarketPage.cancelBtn') }}
        </el-button>
        <el-button
          type="primary"
          :loading="publishing"
          @click="doPublish"
        >
          {{ tmT('templateMarketPage.submitForReview') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import i18n from '@/i18n'
import {
  listMarketEntries, marketCategories,
  marketPriceText,
  ENTRY_KIND_LABEL, AVAILABILITY_LABEL, AVAILABILITY_TAG_TYPE
} from '@/api/templateMarket'
import PublishForm from './PublishForm.vue'

// S-22 闭环接线说明：本页是公开橱窗（种子 + DB 已上架合并，list 接口不变），
// 新增的只有「工作台入口」——发布弹窗（登录后）、我的工作台/审核台跳转按钮。
// i18n 口径：新增面走 templateMarketPage.*（9 语种键已追加）；既有橱窗文案保持
// 中文硬编码（tests/unit/templateMarket_render.test.js 钉住了这些渲染文案）。
// 刻意**不走 useI18n()/$t**：本页会被无 i18n 插件的渲染单测直接挂载，
// $t 缺席会当场 TypeError；tmT 用 i18n 单例的 global composer（无需 app.use）。
const userStore = useUserStore()

function tmT (key) {
  try {
    return i18n.global.t(key)
  } catch {
    return key
  }
}

// 分类展示文案：目录里是英文键（channel_card 等），前端给中文名。
// 未知分类原样显示 —— 后端新增分类不该让页面变成空白。
const CATEGORY_LABEL = {
  channel_card: '渠道卡片',
  live_code: '活码页',
  reply: '回复话术',
  integration: '第三方对接',
  whatsapp: 'WhatsApp',
  mcp: 'MCP 工具',
  routing: '智能路由',
  assignment: '会话分配',
  agentic: 'Agentic'
}

const list = ref([])
const categories = ref([])
const loading = ref(false)
const detailVisible = ref(false)
const detail = ref(null)

// 发布入口（S-22）
const publishVisible = ref(false)
const route = useRoute()
const publishing = ref(false)
const publishFormRef = ref()

const filter = reactive({ kind: '', category: '', keyword: '' })

const visible = computed(() => {
  let out = list.value
  if (filter.kind) out = out.filter((m) => m.kind === filter.kind)
  if (filter.category) out = out.filter((m) => m.category === filter.category)
  if (filter.keyword) {
    const k = filter.keyword.trim().toLowerCase()
    out = out.filter((m) =>
      (m.name || '').toLowerCase().includes(k) ||
      (m.summary || '').toLowerCase().includes(k) ||
      (m.tags || []).some((t) => (t || '').toLowerCase().includes(k)))
  }
  return out
})

function categoryLabel (c) {
  return CATEGORY_LABEL[c] || c || '未分类'
}

function openDetail (m) {
  detail.value = m
  detailVisible.value = true
}

async function fetch () {
  loading.value = true
  try {
    const res = await listMarketEntries({ page: 1, page_size: 200 })
    list.value = res?.list || []
  } finally {
    loading.value = false
  }
}

async function doPublish () {
  publishing.value = true
  try {
    await publishFormRef.value.submit()
    publishVisible.value = false
  } catch (e) {
    if (e?.message) ElMessage.error(e.message)
  } finally {
    publishing.value = false
  }
}

function onSubmitted () {
  // 提审条目未上架不会出现在橱窗，刷新只是让分类候选等保持同源。
  fetch()
}

onMounted(async () => {
  // 从「我的发布/订单」点「我要发布」会带 ?publish=1 直接打开发布表单，
  // 省掉"先进市场页再点一次发布"的一跳。
  if (route.query.publish === '1' && userStore.isLoggedIn) publishVisible.value = true
  // 分类与列表并行拉；分类拉不到不阻塞列表（筛选器空着也能逛）。
  try {
    categories.value = await marketCategories({ _silent: true })
  } catch {
    categories.value = []
  }
  fetch()
})
</script>

<style scoped>
.mb-12 { margin-bottom: 12px; }
.workbench { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.workbench-title { color: var(--el-text-color-secondary); font-size: 13px; }
.workbench-actions { display: flex; gap: 0; }
.pay-hint { color: var(--el-text-color-secondary); font-size: 12px; }
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}
.entry-card { display: flex; flex-direction: column; }
.head { display: flex; gap: 10px; align-items: flex-start; }
.icon {
  width: 36px; height: 36px; border-radius: 8px; flex: 0 0 36px;
  display: inline-flex; align-items: center; justify-content: center;
  background: var(--el-fill-color-light); font-size: 18px;
}
.titles { min-width: 0; }
.name { font-weight: 600; display: flex; align-items: center; gap: 6px; }
.id { color: var(--el-text-color-secondary); font-size: 12px; font-family: monospace; }
.summary { color: var(--el-text-color-secondary); font-size: 13px; line-height: 1.6; margin: 10px 0; min-height: 42px; }
.meta { display: flex; gap: 6px; margin-bottom: 8px; }
.tags { margin-bottom: 10px; }
.tag { margin-right: 4px; }
.actions { margin-top: auto; display: flex; gap: 8px; }
.detail-body .row { display: flex; gap: 12px; margin-bottom: 8px; }
.detail-body .k { width: 70px; color: var(--el-text-color-secondary); flex-shrink: 0; }
.detail-body .v.mono { font-family: monospace; font-size: 12px; word-break: break-all; }
</style>
