<template>
  <div
    v-loading="loading"
    class="asset-detail"
  >
    <el-page-header
      content="资产详情"
      @back="$router.back()"
    />
    <el-card
      v-if="!detail && !loading"
      class="mt-16"
    >
      <el-empty :description="$t('assetMarketPage.notFound')" />
    </el-card>
    <el-card
      v-if="detail"
      class="mt-16"
    >
      <template #header>
        <div class="header">
          <div class="header-with-cover">
            <el-image
              v-if="asset.cover_url"
              :src="asset.cover_url"
              :preview-src-list="[asset.cover_url]"
              fit="cover"
              class="cover-big"
            >
              <template #error>
                <div class="img-error-fallback">
                  {{ $t('图片加载失败') }}
                </div>
              </template>
            </el-image>
            <div
              v-else
              class="cover-big-placeholder"
            >
              {{ (asset.name || '?').slice(0, 1) }}
            </div>
            <div class="header-text">
              <h2>{{ asset.name }}</h2>
              <div class="tags">
                <el-tag>{{ asset.industry }}</el-tag>
                <el-tag type="info">
                  {{ asset.asset_type }}
                </el-tag>
                <el-tag type="success">
                  v{{ asset.latest_version || version?.version }}
                </el-tag>
              </div>
            </div>
          </div>
          <el-button
            type="primary"
            @click="handlePurchase"
          >
            免费试用
          </el-button>
        </div>
      </template>
      <p class="desc">
        {{ asset.description || '暂无描述' }}
      </p>
      <el-divider>数据预览</el-divider>
      <pre class="json-preview">{{ previewText }}</pre>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { assetDetail, purchaseAsset } from '@/api/assetMarket'
import { ElMessage, ElMessageBox } from 'element-plus'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const detail = ref(null)

const asset = computed(() => detail.value?.asset || detail.value || {})
const version = computed(() => detail.value?.latest_version || {})
const previewText = computed(() => {
  const p = detail.value?.data_preview || detail.value?.data
  try {
    return JSON.stringify(typeof p === 'string' ? JSON.parse(p) : p, null, 2)
  } catch {
    return String(p || '')
  }
})

const fetchDetail = async () => {
  loading.value = true
  try {
    const resp = await assetDetail(route.params.id)
    const data = (resp && (resp.asset_id || resp.id || resp.name)) ? resp : (resp?.data || resp)
    // 平台未接入/资产不存在时后端回 200 + {}（不是 404）；空对象 truthy，
    // 旧实现会渲染成「?」+免费试用按钮的空壳资产。无可识别字段即视为不存在。
    detail.value = (data && (data.asset_id || data.id || data.name)) ? data : null
  } catch (e) {
    ElMessage.error(e?.message || '获取资产详情失败')
  } finally {
    loading.value = false
  }
}

const handlePurchase = async () => {
  try {
    await ElMessageBox.confirm(`确认免费试用「${asset.value.name}」？`, '免费试用')
  } catch {
    return
  }
  try {
    await purchaseAsset({ asset_id: asset.value.asset_id || route.params.id })
  } catch {
    // 失败原因 request.js 已经提示过（平台未启用/积分不足/上游异常），
    // 这里不接成功文案，也不让 promise 逸出成组件事件处理器里的未捕获错误。
    return
  }
  ElMessage.success('试用并同步成功')
  router.push('/asset-market/my-assets')
}

onMounted(fetchDetail)
</script>

<style scoped>
.mt-16 {
  margin-top: 16px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}
.header-with-cover {
  display: flex;
  align-items: center;
  gap: 16px;
}
.cover-big {
  width: 96px;
  height: 96px;
  border-radius: 10px;
  flex-shrink: 0;
}
.cover-big-placeholder {
  width: 96px;
  height: 96px;
  border-radius: 10px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: #fff;
  font-size: 36px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.header-text h2 {
  margin: 0 0 8px 0;
  font-size: 20px;
}
.tags .el-tag {
  margin-right: 6px;
}
.desc {
  color: #666;
  line-height: 1.6;
}
.json-preview {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 6px;
  max-height: 480px;
  overflow: auto;
  font-size: 12px;
}
</style>
