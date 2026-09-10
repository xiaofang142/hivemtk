<template>
  <div class="rich-card" :class="'card-' + (card.type || 'generic')">
    <img v-if="card.image_url" :src="card.image_url" class="card-img" alt="" loading="lazy" />
    <div class="card-body">
      <div class="card-title">{{ card.title }}</div>
      <div v-if="card.subtitle" class="card-subtitle">{{ card.subtitle }}</div>
      <div v-if="card.description" class="card-desc">{{ card.description }}</div>
      <div v-if="card.fields && Object.keys(card.fields).length" class="card-fields">
        <div v-for="(val, key) in card.fields" :key="key" class="card-field">
          <span class="card-field-key">{{ key }}</span>
          <span class="card-field-val">{{ val }}</span>
        </div>
      </div>
      <div v-if="card.buttons && card.buttons.length" class="card-buttons">
        <a
          v-for="(btn, i) in card.buttons"
          :key="i"
          class="card-btn"
          :href="safeUrl(btn.url)"
          :target="safeUrl(btn.url) ? '_blank' : undefined"
          :rel="safeUrl(btn.url) ? 'noopener noreferrer' : undefined"
          @click="onBtn(btn)"
        >{{ btn.text }}</a>
      </div>
    </div>
  </div>
</template>

<script setup>
const props = defineProps({
  card: { type: Object, required: true }
})
const emit = defineEmits(['action'])

// 卡片按钮 URL 协议白名单：AI 产出的 ai_cards 直通访客端，
// 不校验协议的话 javascript:/data: 伪协议 href 会成为 XSS 注入点
const safeUrl = (url) => {
  if (!url || typeof url !== 'string') return undefined
  try {
    const u = new URL(url, window.location.origin)
    return ['http:', 'https:'].includes(u.protocol) ? url : undefined
  } catch {
    return undefined
  }
}

const onBtn = (btn) => {
  if (btn.action) {
    emit('action', btn)
  }
}
</script>

<style scoped>
.rich-card {
  width: 100%;
  background: #fff;
  border: 1px solid #ebeef5;
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}
.card-img {
  width: 100%;
  max-height: 180px;
  object-fit: cover;
  display: block;
  background: #f5f5f5;
}
.card-body {
  padding: 12px 14px;
}
.card-title {
  font-size: 15px;
  font-weight: 600;
  color: #1f2329;
  line-height: 1.4;
}
.card-subtitle {
  font-size: 13px;
  color: #909399;
  margin-top: 2px;
}
.card-desc {
  font-size: 13px;
  color: #4e5969;
  margin-top: 8px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
.card-fields {
  margin-top: 10px;
  border-top: 1px dashed #f0f0f0;
  padding-top: 8px;
}
.card-field {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  padding: 3px 0;
  gap: 12px;
}
.card-field-key {
  color: #86909c;
  flex-shrink: 0;
}
.card-field-val {
  color: #1f2329;
  text-align: right;
  word-break: break-word;
}
.card-buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.card-btn {
  display: inline-block;
  background: #1989fa;
  color: #fff;
  font-size: 13px;
  padding: 7px 14px;
  border-radius: 6px;
  text-decoration: none;
  cursor: pointer;
  transition: background 0.2s;
}
.card-btn:hover {
  background: #409eff;
}
.card-product .card-title {
  color: #d4380d;
}
.card-order .card-title {
  color: #096dd9;
}
.card-promo .card-title {
  color: #cf1322;
}
</style>
