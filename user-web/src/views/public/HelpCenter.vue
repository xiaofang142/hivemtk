<template>
  <div class="help-center">
    <header class="hc-header">
      <h1>帮助中心</h1>
      <p class="hc-sub">
        查找产品使用指南与常见问题
      </p>
      <el-input
        v-model="keyword"
        class="hc-search"
        size="large"
        placeholder="搜索文章…"
        clearable
        @input="onSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
    </header>

    <div class="hc-body">
      <aside class="hc-cats">
        <div
          class="cat"
          role="button"
          tabindex="0"
          :class="{ active: activeCat === '' }"
          @click="setCat('')"
          @keydown.enter.prevent="setCat('')"
          @keydown.space.prevent="setCat('')"
        >
          全部
        </div>
        <div
          v-for="c in categories"
          :key="c.category"
          class="cat"
          role="button"
          tabindex="0"
          :class="{ active: activeCat === c.category }"
          @click="setCat(c.category)"
          @keydown.enter.prevent="setCat(c.category)"
          @keydown.space.prevent="setCat(c.category)"
        >
          {{ c.category }} <span class="cnt">{{ c.count }}</span>
        </div>
        <el-alert
          v-if="categoryError"
          :title="categoryError"
          type="error"
          :closable="false"
          show-icon
        />
        <el-button
          v-if="categoryError"
          @click="loadCategories"
        >
          {{ $t('重试') }}
        </el-button>
      </aside>

      <main
        v-loading="loading"
        class="hc-articles"
      >
        <el-alert
          v-if="loadError"
          :title="loadError"
          type="error"
          :closable="false"
          show-icon
        />
        <el-button
          v-if="loadError"
          @click="retryLoad"
        >
          {{ $t('重试') }}
        </el-button>
        <div
          v-if="!detail"
          class="article-list"
        >
          <div
            v-for="a in articles"
            :key="a.id"
            class="article-card"
            role="button"
            tabindex="0"
            @click="openArticle(a.id)"
            @keydown.enter.prevent="openArticle(a.id)"
            @keydown.space.prevent="openArticle(a.id)"
          >
            <div class="a-cat">
              {{ a.category }}
            </div>
            <div class="a-title">
              {{ a.title }}
            </div>
            <div class="a-summary">
              {{ a.summary || '（暂无摘要）' }}
            </div>
            <div class="a-time">
              {{ fmtTime(a.updated_at) }}
            </div>
          </div>
          <el-empty
            v-if="!loading && !loadError && articles.length === 0"
            description="暂无公开文章"
          />
        </div>

        <article
          v-else
          class="article-detail"
        >
          <el-button
            text
            @click="backToList"
          >
            <el-icon><ArrowLeft /></el-icon> 返回列表
          </el-button>
          <h2>{{ detail.title }}</h2>
          <div class="a-meta">
            {{ detail.category }} · 更新于 {{ fmtTime(detail.updated_at) }}
          </div>
          <div class="a-content">
            {{ detail.content }}
          </div>
        </article>
      </main>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed, watch } from 'vue';
import i18n from '@/i18n'
import { Search, ArrowLeft } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const categories = ref([])
const articles = ref([])
const detail = ref(null)
const activeCat = ref('')
const keyword = ref('')
const loading = ref(false)
const categoryError = ref('')
const loadError = ref('')
let categoryVersion = 0
let contentVersion = 0
let activeArticleId = null
let searchTimer = null
let disposed = false

// 帮助中心是公开页（未登录也能进），但它的数据是**租户级**的
// （knowledge_documents 是租户表，后端按 tenant_id 过滤）。
// SaaS 模式下后端要求调用方声明租户（query 的 tenant_code 或 X-Tenant-Code 头），
// 缺了就回 400「缺少租户短码」——页面于是永远空白。
// 已登录用户从 userInfo 里取本租户短码带上；未登录访客没有短码，
// 此时传空（后端 off 模式照常按默认租户走，on 模式会明确报缺短码而不是静默串租户）。
const tenantCode = computed(() => userStore.userInfo?.tenant_code || '')

const withTenant = (url) => {
  if (!tenantCode.value) return url
  return url + (url.includes('?') ? '&' : '?') + 'tenant_code=' + encodeURIComponent(tenantCode.value)
}

const fmtTime = (t) => (t ? String(t).replace('T', ' ').slice(0, 10) : '')

const fetchPublic = async (url) => {
  const response = await fetch(withTenant(url))
  let envelope
  try { envelope = await response.json() } catch {
    throw new Error(i18n.global.t('加载失败'))
  }
  if (!response.ok || envelope?.code !== 0) throw new Error(envelope?.message || i18n.global.t('加载失败'))
  return envelope.data
}
const readList = (data) => {
  if (!Array.isArray(data?.list)) throw new Error(i18n.global.t('加载失败'))
  return data.list
}
const loadCategories = async () => {
  const version = ++categoryVersion
  categoryError.value = ''
  try {
    const list = readList(await fetchPublic('/api/public/help-center/categories'))
    if (!disposed && version === categoryVersion) categories.value = list
  } catch (error) {
    if (disposed || version !== categoryVersion) return
    categories.value = []
    categoryError.value = error?.message || i18n.global.t('加载失败')
  }
}

const loadArticles = async () => {
  clearTimeout(searchTimer)
  const version = ++contentVersion
  activeArticleId = null
  detail.value = null
  articles.value = []
  loadError.value = ''
  loading.value = true
  try {
    const qs = new URLSearchParams()
    if (activeCat.value) qs.set('category', activeCat.value)
    if (keyword.value.trim()) qs.set('q', keyword.value.trim())
    const list = readList(await fetchPublic(`/api/public/help-center/articles?${qs}`))
    if (!disposed && version === contentVersion) articles.value = list
  } catch (error) {
    if (!disposed && version === contentVersion) loadError.value = error?.message || i18n.global.t('加载失败')
  } finally {
    if (!disposed && version === contentVersion) loading.value = false
  }
}

const onSearch = () => {
  clearTimeout(searchTimer)
  contentVersion += 1
  activeArticleId = null
  detail.value = null
  articles.value = []
  loadError.value = ''
  loading.value = true
  searchTimer = setTimeout(loadArticles, 350)
}
const setCat = (category) => {
  activeCat.value = category
  return loadArticles()
}
const backToList = () => {
  clearTimeout(searchTimer)
  contentVersion += 1
  activeArticleId = null
  detail.value = null
  loadError.value = ''
  loading.value = false
}
const openArticle = async (id) => {
  clearTimeout(searchTimer)
  const version = ++contentVersion
  activeArticleId = id
  detail.value = null
  loading.value = true
  loadError.value = ''
  try {
    const article = await fetchPublic(`/api/public/help-center/articles/${encodeURIComponent(id)}`)
    if (!article || typeof article !== 'object' || Array.isArray(article) || String(article.id) !== String(id)) throw new Error(i18n.global.t('加载失败'))
    if (!disposed && version === contentVersion) detail.value = article
  } catch (error) {
    if (!disposed && version === contentVersion) loadError.value = error?.message || i18n.global.t('加载失败')
  } finally {
    if (!disposed && version === contentVersion) loading.value = false
  }
}
const retryLoad = () => activeArticleId === null ? loadArticles() : openArticle(activeArticleId)
watch(tenantCode, () => { loadCategories(); loadArticles() })
onMounted(() => { loadCategories(); loadArticles() })
onUnmounted(() => {
  disposed = true
  categoryVersion += 1
  contentVersion += 1
  clearTimeout(searchTimer)
})
</script>

<style scoped>
.help-center { min-height: 100vh; background: #f6f8fa; }
.hc-header { padding: 48px 16px 28px; text-align: center; background: linear-gradient(135deg, #4f46e5, #7c3aed); color: #fff; }
.hc-header h1 { margin: 0 0 6px; font-size: 30px; }
.hc-sub { margin: 0 0 20px; opacity: .85; }
.hc-search { max-width: 560px; }
.hc-body { max-width: 1080px; margin: 24px auto; display: flex; gap: 20px; padding: 0 16px; }
.hc-cats { width: 200px; flex-shrink: 0; }
.cat { padding: 10px 14px; border-radius: 8px; cursor: pointer; color: #475569; margin-bottom: 4px; background: #fff; }
.cat.active { background: #4f46e5; color: #fff; font-weight: 600; }
.cnt { float: right; opacity: .7; font-size: 12px; }
.hc-articles { flex: 1; }
.article-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; }
.article-card { background: #fff; border-radius: 10px; padding: 16px; cursor: pointer; transition: box-shadow .2s; }
.article-card:hover { box-shadow: 0 6px 20px rgba(0,0,0,.08); }
.a-cat { font-size: 12px; color: #4f46e5; margin-bottom: 6px; }
.a-title { font-weight: 600; margin-bottom: 6px; }
.a-summary { font-size: 13px; color: #64748b; min-height: 36px; }
.a-time { font-size: 12px; color: #94a3b8; margin-top: 8px; }
.article-detail { background: #fff; border-radius: 10px; padding: 24px; }
.article-detail h2 { margin: 12px 0 4px; }
.a-meta { color: #94a3b8; font-size: 13px; margin-bottom: 16px; }
.a-content { white-space: pre-wrap; line-height: 1.8; color: #334155; }
</style>
