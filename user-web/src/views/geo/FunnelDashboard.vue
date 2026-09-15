<template>
  <div class="funnel-dashboard">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>🎯 漏斗总览 · 全链路 SEO × GEO</span>
          <el-button type="primary" size="small" @click="load">刷新</el-button>
        </div>
      </template>

      <div v-if="loading">加载中...</div>
      <el-empty v-else-if="!stats" description="暂无数据，先去做关键词蒸馏和文章生成吧" />

      <template v-else>
        <!-- 双漏斗 -->
        <el-row :gutter="16" class="funnel-row">
          <el-col v-for="(val, key) in funnel" :key="key" :span="4">
            <div class="funnel-step" :style="{ background: gradientColor(key) }">
              <div class="step-num">{{ val || 0 }}</div>
              <div class="step-label">{{ labelMap[key] }}</div>
            </div>
          </el-col>
        </el-row>

        <!-- 引擎对比 -->
        <el-row :gutter="16" class="stats-row">
          <el-col :span="12">
            <h4>📈 搜索引擎收录</h4>
            <el-table :data="engineIndexed" size="small" border>
              <el-table-column prop="engine" label="引擎" width="120" />
              <el-table-column prop="count" label="收录数" />
              <el-table-column label="收录率">
                <template #default="{ row }">
                  <el-progress :percentage="rate(row.count)" />
                </template>
              </el-table-column>
            </el-table>
          </el-col>
          <el-col :span="12">
            <h4>🤖 AI 引擎引用</h4>
            <el-table :data="engineCited" size="small" border>
              <el-table-column prop="engine" label="引擎" width="120" />
              <el-table-column prop="count" label="引用数" />
              <el-table-column label="引用率">
                <template #default="{ row }">
                  <el-progress :percentage="rate(row.count)" />
                </template>
              </el-table-column>
            </el-table>
          </el-col>
        </el-row>

        <!-- 转化率 -->
        <el-row :gutter="16" class="rate-row">
          <el-col v-for="(val, key) in stats.conversion_rates" :key="key" :span="8">
            <el-statistic :title="labelMap[key]" :value="val" :precision="1" suffix="%" />
          </el-col>
        </el-row>
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import request from '@/api/index'

const stats = ref(null)
const loading = ref(false)

const labelMap = {
  seed_count: '种子词',
  longtail_count: '长尾词',
  article_count: '文章',
  deployed_count: '已部署',
  indexed: '被收录',
  ai_cited: '被 AI 引用',
  longtail_to_article: '长尾→文章',
  article_to_deployed: '文章→部署'
}

const funnel = computed(() => {
  if (!stats.value) return {}
  return {
    seed_count: stats.value.seed_count + stats.value.longtail_count,
    article_count: stats.value.article_count,
    deployed_count: stats.value.deployed_count,
  }
})

const engineIndexed = computed(() => {
  const m = stats.value?.indexed_by_engine || {}
  return Object.entries(m).map(([engine, count]) => ({ engine, count }))
})
const engineCited = computed(() => {
  const m = stats.value?.ai_cited_by_engine || {}
  return Object.entries(m).map(([engine, count]) => ({ engine, count }))
})

function gradientColor(key) {
  const colors = ['#409EFF', '#67C23A', '#E6A23C', '#F56C6C', '#909399']
  const idx = { seed_count: 0, article_count: 1, deployed_count: 2 }[key] ?? 3
  return colors[idx]
}
function rate(n) {
  const base = stats.value?.deployed_count || 1
  return Math.round(n / base * 100)
}

async function load() {
  loading.value = true
  try {
    const res = await request.get('/geo/index-tracking/funnel')
    stats.value = res.data
  } catch (e) {
    console.warn(e)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.funnel-row { margin-bottom: 20px; }
.stats-row { margin-bottom: 20px; }
.rate-row { text-align: center; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.funnel-step {
  padding: 20px; border-radius: 8px; color: #fff; text-align: center;
  margin: 8px 4px;
}
.step-num { font-size: 28px; font-weight: bold; }
.step-label { font-size: 13px; opacity: 0.9; margin-top: 4px; }
</style>
