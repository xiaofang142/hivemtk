<template>
  <div class="help-center">
    <header class="hc-header">
      <h1>帮助中心</h1>
      <p class="hc-sub">查找产品使用指南与常见问题</p>
      <el-input v-model="keyword" class="hc-search" size="large" placeholder="搜索文章…" clearable @input="onSearch">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
    </header>

    <div class="hc-body">
      <aside class="hc-cats">
        <div class="cat" role="button" tabindex="0" :class="{ active: activeCat === '' }" @click="setCat('')" @keydown.enter.prevent="setCat('')" @keydown.space.prevent="setCat('')">全部</div>
        <div v-for="c in categories" :key="c.category" class="cat" role="button" tabindex="0" :class="{ active: activeCat === c.category }" @click="setCat(c.category)" @keydown.enter.prevent="setCat(c.category)" @keydown.space.prevent="setCat(c.category)">
          {{ c.category }} <span class="cnt">{{ c.count }}</span>
        </div>
      </aside>

      <main class="hc-articles">
        <div v-if="!detail" v-loading="loading" class="article-list">
          <div v-for="a in articles" :key="a.id" class="article-card" role="button" tabindex="0" @click="openArticle(a.id)" @keydown.enter.prevent="openArticle(a.id)" @keydown.space.prevent="openArticle(a.id)">
            <div class="a-cat">{{ a.category }}</div>
            <div class="a-title">{{ a.title }}</div>
            <div class="a-summary">{{ a.summary || '（暂无摘要）' }}</div>
            <div class="a-time">{{ fmtTime(a.updated_at) }}</div>
          </div>
          <el-empty v-if="!loading && articles.length === 0" description="暂无公开文章" />
        </div>

        <article v-else class="article-detail">
          <el-button text @click="detail = null"><el-icon><ArrowLeft /></el-icon> 返回列表</el-button>
          <h2>{{ detail.title }}</h2>
          <div class="a-meta">{{ detail.category }} · 更新于 {{ fmtTime(detail.updated_at) }}</div>
          <div class="a-content">{{ detail.content }}</div>
        </article>
      </main>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { Search, ArrowLeft } from '@element-plus/icons-vue'

const categories = ref([])
const articles = ref([])
const detail = ref(null)
const activeCat = ref('')
const keyword = ref('')
const loading = ref(false)

const fmtTime = (t) => (t ? String(t).replace('T', ' ').slice(0, 10) : '')

const loadCategories = async () => {
  const res = await fetch('/api/public/help-center/categories').then((r) => r.json()).catch(() => null)
  categories.value = res?.data?.list || []
}

const loadArticles = async () => {
  loading.value = true
  try {
    const qs = new URLSearchParams()
    if (activeCat.value) qs.set('category', activeCat.value)
    if (keyword.value) qs.set('q', keyword.value)
    const res = await fetch(`/api/public/help-center/articles?${qs}`).then((r) => r.json()).catch(() => null)
    articles.value = res?.data?.list || []
  } finally {
    loading.value = false
  }
}

let searchTimer = null
const onSearch = () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(loadArticles, 350)
}

const setCat = (c) => { activeCat.value = c; detail.value = null; loadArticles() }

const openArticle = async (id) => {
  loading.value = true
  try {
    const res = await fetch(`/api/public/help-center/articles/${id}`).then((r) => r.json()).catch(() => null)
    detail.value = res?.data || null
  } finally {
    loading.value = false
  }
}

onMounted(() => { loadCategories(); loadArticles() })
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
