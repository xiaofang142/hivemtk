<script setup>
defineProps({
  kicker: { type: String, default: '' },
  title: { type: [String, Array], required: true },
  subtitle: { type: String, default: '' },
  gradientIndex: { type: Number, default: -1 },
  breadcrumbs: { type: Array, default: () => [] },
  variant: { type: String, default: 'default' },
})
</script>

<template>
  <header class="page-header" :class="`page-header-${variant}`">
    <div class="container">
      <!-- 面包屑 -->
      <nav v-if="breadcrumbs.length" class="breadcrumbs" :aria-label="$t('面包屑导航')">
        <ol>
          <li v-for="(crumb, i) in breadcrumbs" :key="i" class="crumb-item">
            <router-link v-if="crumb.href" :to="crumb.href">{{ $t(crumb.label) }}</router-link>
            <span v-else class="crumb-current">{{ $t(crumb.label) }}</span>
            <span v-if="i < breadcrumbs.length - 1" class="crumb-sep" aria-hidden="true">/</span>
          </li>
        </ol>
      </nav>

      <!-- 标签 -->
      <div v-if="kicker" class="page-kicker">
        <span class="tag-line">{{ $t(kicker) }}</span>
      </div>

      <!-- 主标题 -->
      <h1 class="page-title">
        <template v-if="Array.isArray(title)">
          <template v-for="(line, i) in title" :key="i">
            <span v-if="i === gradientIndex" class="ink-mark">{{ $t(line) }}</span>
            <template v-else>{{ $t(line) }}</template>
          </template>
        </template>
        <template v-else>{{ $t(title) }}</template>
      </h1>

      <!-- 副标题 -->
      <p v-if="subtitle" class="page-subtitle">{{ $t(subtitle) }}</p>
    </div>
  </header>
</template>

<style scoped>
.page-header {
  padding: 120px 0 56px;
  position: relative;
  border-bottom: 1px solid var(--border);
}

.page-header-default {
  background: var(--bg-base);
}

.page-header-subtle {
  background: var(--bg-subtle);
}

.page-header-ink {
  background: var(--bg-ink);
  color: var(--text-inv);
  border-bottom: none;
}

.page-header-ink :deep(.tag-line) {
  color: #FF6B5E;
}
.page-header-ink :deep(.tag-line::before) {
  background: #FF6B5E;
}

.page-header-ink .page-title {
  color: var(--text-inv);
}

.page-header-ink .page-subtitle {
  color: var(--text-inv-muted);
}

.page-header-ink .breadcrumbs a,
.page-header-ink .breadcrumbs .crumb-current {
  color: var(--text-inv-muted);
}

.page-header-ink .breadcrumbs a:hover {
  color: #FF6B5E;
}

/* —— 面包屑 —— */
.breadcrumbs ol {
  list-style: none;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0;
  margin-bottom: 20px;
  font-size: 0.82rem;
}

.crumb-item {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.breadcrumbs a {
  color: var(--text-muted);
  transition: color 0.18s ease;
  font-weight: 500;
}

.breadcrumbs a:hover {
  color: var(--primary);
}

.crumb-current {
  color: var(--text);
  font-weight: 600;
}

.crumb-sep {
  color: var(--text-dim);
  margin: 0 8px;
}

/* —— 标签 —— */
.page-kicker {
  margin-bottom: 16px;
}

/* —— 主标题 —— */
.page-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5.5vw, 3.6rem);
  font-weight: 900;
  line-height: 1.08;
  letter-spacing: -0.032em;
  margin-bottom: 20px;
  max-width: 900px;
}

/* —— 副标题 —— */
.page-subtitle {
  font-size: 1.1rem;
  color: var(--text-muted);
  line-height: 1.78;
  max-width: 720px;
}

@media (max-width: 768px) {
  .page-header {
    padding: 96px 0 40px;
  }
  .page-title {
    font-size: clamp(1.8rem, 7vw, 2.6rem);
  }
  .page-subtitle {
    font-size: 1rem;
  }
}
</style>
