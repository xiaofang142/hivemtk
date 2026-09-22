<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSite } from '../composables/useSite.js'
import i18n from '@/i18n'
import { LANGS, setStoredLocale, applyDirection } from '@/i18n/locale.js'
const { nav, brand } = useSite()
const locale = i18n.global.locale
function changeLocale(code) {
  locale.value = code
  setStoredLocale(code)
  applyDirection(code)
}

// 语言下拉开关状态
const langOpen = ref(false)
const langMenuRef = ref(null)
function toggleLangMenu() {
  langOpen.value = !langOpen.value
}
function closeLangMenu() {
  langOpen.value = false
}
const currentLangLabel = ref('')
function refreshCurrentLangLabel() {
  const cur = LANGS.find((l) => l.code === locale.value)
  currentLangLabel.value = cur ? cur.label : '简体中文'
}

const route = useRoute()
const router = useRouter()
const scrolled = ref(false)
const mobileOpen = ref(false)
const mobileMenuId = 'mobile-menu'

const handleScroll = () => {
  scrolled.value = window.scrollY > 24
}

function handleDocClick(e) {
  if (!langMenuRef.value) return
  if (!langMenuRef.value.contains(e.target)) {
    langOpen.value = false
  }
}

onMounted(() => {
  window.addEventListener('scroll', handleScroll, { passive: true })
  window.addEventListener('click', handleDocClick)
  refreshCurrentLangLabel()
})
onUnmounted(() => {
  window.removeEventListener('scroll', handleScroll)
  window.removeEventListener('click', handleDocClick)
})

watch(locale, () => refreshCurrentLangLabel())
</script>

<template>
  <nav class="navbar" :class="{ 'navbar-scrolled': scrolled }">
    <div class="container navbar-inner">
      <router-link to="/" class="brand" :aria-label="$t('HiveMTK 首页')">
        <span class="brand-seal" aria-hidden="true">H</span>
        <span class="brand-text">
          <span class="brand-name">HiveMTK</span>
          <span class="brand-tag">{{ $t('私域 AI 营销操作系统') }}</span>
        </span>
      </router-link>

      <div class="nav-links" :class="{ 'nav-open': mobileOpen }" :id="mobileMenuId">
        <router-link
          v-for="link in nav.links"
          :key="link.href"
          :to="link.href"
          class="nav-link"
          :class="{ 'nav-link-active': route.path === link.href }"
          @click="mobileOpen = false"
        >
          {{ link.label }}
        </router-link>

        <div class="nav-actions-mobile">
          <router-link to="/deploy" class="btn btn-primary nav-cta-mobile" @click="mobileOpen = false">
            {{ nav.cta.label }}
          </router-link>
          <div class="nav-repos-mobile">
            <a href="https://github.com/xiaofang142/hivemtk" target="_blank" rel="noopener noreferrer">
              GitHub
            </a>
            <span class="dot-sep">·</span>
            <a href="https://gitee.com/xhpmayun/hivemtk" target="_blank" rel="noopener noreferrer">
              Gitee
            </a>
          </div>
        </div>
      </div>

      <!-- 桌面端:开源仓库链接 + 语言 + CTA -->
      <div class="nav-tools">
        <div class="nav-repos">
          <a class="repo-icon" href="https://github.com/xiaofang142/hivemtk" target="_blank" rel="noopener noreferrer" aria-label="GitHub 仓库">
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 .5C5.7.5.5 5.7.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.3.8-.6v-2c-3.2.7-3.9-1.5-3.9-1.5-.5-1.3-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 .1.8 1.7 2.6 1.2.1-.7.4-1.2.7-1.5-2.6-.3-5.3-1.3-5.3-5.8 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.7 18.3 5 18.3 5c.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.5-2.7 5.5-5.3 5.8.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.7 18.3.5 12 .5z"/></svg>
          </a>
          <a class="repo-icon" href="https://gitee.com/xhpmayun/hivemtk" target="_blank" rel="noopener noreferrer" aria-label="Gitee 仓库">
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M11.9 2.4A9.6 9.6 0 0 0 2.3 12c0 4.3 2.8 7.9 6.7 9.2.5.1.7-.2.7-.5v-1.7c-2.7.6-3.3-1.3-3.3-1.3-.4-1.1-1.1-1.4-1.1-1.4-.9-.6.1-.6.1-.6 1 .1 1.5 1 1.5 1 .9 1.5 2.3 1.1 2.9.8.1-.6.3-1.1.6-1.3-2.2-.3-4.5-1.1-4.5-4.8 0-1.1.4-2 1-2.7-.1-.3-.4-1.3.1-2.7 0 0 .8-.3 2.7 1a9.4 9.4 0 0 1 4.9 0c1.9-1.3 2.7-1 2.7-1 .5 1.4.2 2.4.1 2.7.6.7 1 1.6 1 2.7 0 3.7-2.3 4.5-4.5 4.8.3.3.6.9.6 1.8v2.6c0 .3.2.6.7.5A9.6 9.6 0 0 0 21.5 12 9.6 9.6 0 0 0 11.9 2.4z"/></svg>
          </a>
        </div>

        <div class="nav-lang" ref="langMenuRef">
          <button
            type="button"
            class="lang-trigger"
            :class="{ active: langOpen }"
            :aria-expanded="langOpen"
            :aria-label="$t('切换语言')"
            @click.stop="toggleLangMenu"
          >
            <svg class="lang-globe" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
            </svg>
            <span class="lang-trigger-label">{{ currentLangLabel }}</span>
            <svg class="lang-caret" :class="{ open: langOpen }" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <polyline points="2 4 6 8 10 4" />
            </svg>
          </button>
          <ul v-if="langOpen" class="lang-menu" role="menu" @keydown.escape="closeLangMenu">
            <li
              v-for="l in LANGS"
              :key="l.code"
              class="lang-menu-item"
              :class="{ active: locale === l.code }"
              role="menuitem"
              tabindex="0"
              @click="changeLocale(l.code); closeLangMenu()"
              @keydown.enter="changeLocale(l.code); closeLangMenu()"
              @keydown.escape="closeLangMenu"
            >{{ l.label }}</li>
          </ul>
        </div>

        <router-link to="/deploy" class="btn btn-primary nav-cta">
          {{ nav.cta.label }}
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
        </router-link>
      </div>

      <button
        class="mobile-toggle"
        @click="mobileOpen = !mobileOpen"
        :aria-label="$t('切换菜单')"
        :aria-expanded="mobileOpen"
        :aria-controls="mobileMenuId"
      >
        <span></span>
        <span></span>
        <span></span>
      </button>
    </div>
  </nav>
</template>

<style scoped>
.navbar {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 1000;
  transition: background 0.3s ease, box-shadow 0.3s ease, border-color 0.3s ease;
  padding: 20px 0;
  border-bottom: 1px solid transparent;
}

.navbar-scrolled {
  background: rgba(250, 247, 242, 0.88);
  backdrop-filter: blur(16px) saturate(180%);
  -webkit-backdrop-filter: blur(16px) saturate(180%);
  box-shadow: 0 1px 0 var(--border);
  padding: 12px 0;
}

.navbar-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
}

/* —— 品牌 —— */
.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  flex-shrink: 0;
}

.brand-seal {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  border-radius: 7px;
  background: var(--primary);
  color: #fff;
  font-family: var(--font-display);
  font-weight: 900;
  font-size: 1.35rem;
  letter-spacing: -0.04em;
  box-shadow: 0 3px 10px rgba(200, 57, 47, 0.3);
  position: relative;
  flex-shrink: 0;
}
.brand-seal::after {
  content: '';
  position: absolute;
  inset: 2px;
  border: 1.2px solid rgba(255, 255, 255, 0.32);
  border-radius: 5px;
  pointer-events: none;
}

.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.1;
}

.brand-name {
  font-family: var(--font-display);
  font-size: 1.2rem;
  font-weight: 800;
  letter-spacing: -0.02em;
  color: var(--text);
}

.brand-tag {
  font-family: var(--font-body);
  font-size: 0.66rem;
  font-weight: 500;
  color: var(--text-muted);
  letter-spacing: 0.04em;
  margin-top: 2px;
}

/* —— 导航链接 —— */
.nav-links {
  display: flex;
  align-items: center;
  gap: 32px;
  flex: 1;
  justify-content: center;
}

.nav-link {
  color: var(--text-soft);
  font-size: 0.92rem;
  font-weight: 500;
  transition: color 0.18s ease;
  position: relative;
  cursor: pointer;
  padding: 4px 0;
}

.nav-link:hover {
  color: var(--text);
}

.nav-link-active {
  color: var(--primary);
  font-weight: 600;
}

.nav-link::after {
  content: '';
  position: absolute;
  bottom: -2px;
  left: 50%;
  transform: translateX(-50%);
  width: 0;
  height: 2px;
  background: var(--primary);
  transition: width 0.22s ease;
}

.nav-link:hover::after,
.nav-link-active::after {
  width: 18px;
}

/* —— 桌面端工具区 —— */
.nav-tools {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-shrink: 0;
}

.nav-repos {
  display: flex;
  align-items: center;
  gap: 4px;
}

.nav-repos .repo-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 7px;
  color: var(--text-muted);
  border: 1px solid transparent;
  transition: color 0.18s ease, border-color 0.18s ease, background 0.18s ease;
}

.nav-repos .repo-icon svg {
  width: 18px;
  height: 18px;
}

.nav-repos .repo-icon:hover {
  color: var(--text);
  border-color: var(--border);
  background: var(--bg-surface);
}

/* —— 语言切换 —— */
.nav-lang {
  position: relative;
}

.lang-trigger {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  border: 1px solid var(--border-strong);
  color: var(--text-soft);
  font-size: 0.82rem;
  font-weight: 500;
  padding: 7px 12px;
  min-height: 36px;
  border-radius: 7px;
  cursor: pointer;
  transition: all 0.18s ease;
  outline: none;
  font-family: var(--font-body);
}
.lang-trigger.active,
.lang-trigger:hover,
.lang-trigger:focus-visible {
  color: var(--text);
  border-color: var(--text);
}
.lang-globe {
  width: 14px;
  height: 14px;
}
.lang-trigger-label {
  line-height: 1;
  white-space: nowrap;
}
.lang-caret {
  width: 10px;
  height: 10px;
  transition: transform 0.18s ease;
}
.lang-caret.open {
  transform: rotate(180deg);
}

.lang-menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  list-style: none;
  margin: 0;
  padding: 4px;
  min-width: 140px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow-lg);
  z-index: 1100;
}

.lang-menu-item {
  padding: 9px 12px;
  min-height: 40px;
  display: flex;
  align-items: center;
  font-size: 0.86rem;
  color: var(--text-soft);
  border-radius: 5px;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.lang-menu-item:hover {
  background: var(--bg-subtle);
  color: var(--text);
}

.lang-menu-item.active {
  color: var(--primary);
  font-weight: 600;
  background: var(--primary-soft);
}

/* —— CTA —— */
.nav-cta {
  padding: 9px 18px;
  font-size: 0.88rem;
  border-radius: 7px;
}

/* —— 移动端菜单 —— */
.mobile-toggle {
  display: none;
  flex-direction: column;
  gap: 5px;
  background: transparent;
  padding: 8px;
}

.mobile-toggle span {
  width: 22px;
  height: 2px;
  background: var(--text);
  border-radius: 2px;
  transition: transform 0.25s ease, opacity 0.25s ease;
}

.nav-actions-mobile {
  display: none;
}

@media (max-width: 992px) {
  .brand-tag {
    display: none;
  }
  .nav-links {
    gap: 22px;
  }
}

@media (max-width: 860px) {
  .nav-tools {
    display: none;
  }
  .nav-cta-mobile {
    display: inline-flex;
  }
  .nav-actions-mobile {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-top: 20px;
    padding-top: 20px;
    border-top: 1px solid var(--border);
  }
  .nav-repos-mobile {
    display: flex;
    gap: 8px;
    align-items: center;
    color: var(--text-muted);
    font-size: 0.9rem;
  }
  .nav-repos-mobile a {
    color: var(--primary);
    font-weight: 600;
  }
  .dot-sep {
    color: var(--text-dim);
  }
  .nav-links {
    position: fixed;
    top: 72px;
    left: 0;
    right: 0;
    bottom: 0;
    background: var(--bg-base);
    backdrop-filter: blur(20px);
    flex-direction: column;
    padding: 32px 24px;
    gap: 8px;
    transform: translateX(100%);
    transition: transform 0.3s ease;
    align-items: stretch;
    justify-content: flex-start;
    overflow-y: auto;
  }
  .nav-open {
    transform: translateX(0);
  }
  .nav-link {
    font-size: 1.3rem;
    font-family: var(--font-display);
    font-weight: 700;
    padding: 14px 4px;
    border-bottom: 1px solid var(--border);
  }
  .nav-link::after {
    display: none;
  }
  .mobile-toggle {
    display: flex;
  }
}

[dir='rtl'] .nav-repos {
  order: -1;
}

[dir='rtl'] .nav-lang .lang-menu {
  right: auto;
  left: 0;
}
</style>
