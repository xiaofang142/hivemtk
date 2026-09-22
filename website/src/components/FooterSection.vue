<script setup>
import { useSiteContact } from '../composables/useSiteContact.js'

const { wechatId, wechatQrURL, email, phone, serviceHours } = useSiteContact()

// HiveMTK 官方开源仓库(GitHub / Gitee),与官网全局信息保持一致
const repoLinks = [
  {
    label: 'GitHub',
    full: 'xiaofang142/hivemtk',
    href: 'https://github.com/xiaofang142/hivemtk',
    icon: 'github',
  },
  {
    label: 'Gitee',
    full: 'xhpmayun/hivemtk',
    href: 'https://gitee.com/xhpmayun/hivemtk',
    icon: 'gitee',
  },
]

const productLinks = [
  { label: '七端多账号聚合', href: '/#features' },
  { label: 'AI 自动谈单', href: '/#feature-ai-sales' },
  { label: '销冠 SOP 智能体', href: '/#feature-sop' },
  { label: '客户 CDP', href: '/#feature-cdp' },
]

const resourceLinks = [
  { label: '部署指南', href: '/deploy' },
  { label: '安装文档', href: '/docs' },
  { label: '业务流程', href: '/#workflow' },
  { label: '核心功能', href: '/features' },
  { label: '工程能力', href: '/toolchain' },
  { label: '常见问题', href: '/faq' },
]

// HiveMTK 开源资料入口(AGPL-3.0 协议、贡献指南、免责声明)
const openSourceLinks = [
  { label: 'AGPL-3.0 开源协议', href: 'https://gitee.com/xhpmayun/hivemtk/raw/master/LICENSE' },
  { label: '贡献指南', href: 'https://gitee.com/xhpmayun/hivemtk/blob/master/CONTRIBUTING.md' },
  { label: '免责声明', href: 'https://gitee.com/xhpmayun/hivemtk/blob/master/DISCLAIMER.md' },
]
</script>

<template>
  <footer class="footer">
    <!-- 顶部:仓库强引导(引流入口) -->
    <div class="footer-cta">
      <div class="container cta-inner">
        <div class="cta-text">
          <span class="cta-kicker">{{ $t('立即开始') }}</span>
          <h2 class="cta-title">{{ $t('克隆仓库,4 步完成私有化部署') }}</h2>
          <p class="cta-desc">{{ $t('AGPL-3.0 开源,无授权码、无版本下载、无任何收费环节。完整源码均在 GitHub / Gitee 公开托管。') }}</p>
        </div>
        <div class="cta-repos">
          <a
            v-for="repo in repoLinks"
            :key="repo.href"
            class="cta-repo"
            :href="repo.href"
            target="_blank"
            rel="noopener noreferrer"
          >
            <span class="cta-repo-icon" aria-hidden="true">
              <svg v-if="repo.icon === 'github'" viewBox="0 0 24 24"><path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56v-2c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.71 1.26 3.37.96.1-.75.4-1.26.74-1.55-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.04 0 0 .97-.31 3.18 1.18a11.05 11.05 0 0 1 5.79 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.58.24 2.75.12 3.04.74.81 1.18 1.84 1.18 3.1 0 4.43-2.7 5.41-5.27 5.69.41.36.78 1.07.78 2.16v3.2c0 .31.21.68.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5z" /></svg>
              <svg v-else-if="repo.icon === 'gitee'" viewBox="0 0 24 24"><path d="M11.97 1.6c-5.7 0-10.32 4.62-10.32 10.32s4.62 10.32 10.32 10.32 10.32-4.62 10.32-10.32S17.67 1.6 11.97 1.6zm-.04 5.16c2.05 0 3.71 1.66 3.71 3.71 0 .55-.45 1-1 1H10.3v3.94c0 .55-.45 1-1 1s-1-.45-1-1V6.76c0-.55.45-1 1-1h2.63c.55 0 1 .45 1 1z" /></svg>
            </span>
            <span class="cta-repo-text">
              <span class="cta-repo-label">{{ repo.label }}</span>
              <span class="cta-repo-full">{{ repo.full }}</span>
            </span>
            <svg class="cta-repo-arrow" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7"/><path d="M7 7h10v10"/></svg>
          </a>
        </div>
      </div>
    </div>

    <!-- 主体:四列布局 -->
    <div class="container footer-grid">
      <div class="footer-brand">
        <div class="brand">
          <span class="brand-seal">H</span>
          <div class="brand-text">
            <span class="brand-name">HiveMTK</span>
            <span class="brand-tag">{{ $t('私域 AI 营销操作系统') }}</span>
          </div>
        </div>
        <p class="footer-desc">
          {{ $t('七端打透 · AI 真自主 · 数据封死在域内。基于 AGPL-3.0 开源,克隆仓库即可私有化部署。') }}
        </p>
        <div class="footer-badges">
          <span class="badge">AGPL-3.0</span>
          <span class="badge">{{ $t('私有化部署') }}</span>
          <span class="badge">{{ $t('数据不出域') }}</span>
        </div>
      </div>

      <div class="footer-col">
        <h4 class="footer-col-title">{{ $t('产品功能') }}</h4>
        <ul>
          <li v-for="link in productLinks" :key="link.label">
            <router-link :to="link.href">{{ $t(link.label) }}</router-link>
          </li>
        </ul>
      </div>

      <div class="footer-col">
        <h4 class="footer-col-title">{{ $t('快速导航') }}</h4>
        <ul>
          <li v-for="link in resourceLinks" :key="link.label">
            <router-link :to="link.href">{{ $t(link.label) }}</router-link>
          </li>
        </ul>
      </div>

      <div class="footer-col">
        <h4 class="footer-col-title">{{ $t('开源项目') }}</h4>
        <ul>
          <li v-for="link in openSourceLinks" :key="link.label">
            <a :href="link.href" target="_blank" rel="noopener noreferrer">{{ $t(link.label) }}</a>
          </li>
        </ul>
      </div>

      <div class="footer-col footer-contact-col">
        <h4 class="footer-col-title">{{ $t('联系作者') }}</h4>

        <!-- 微信二维码 + 微信号 -->
        <div class="contact-wechat">
          <div class="qr-wrap">
            <img
              v-if="wechatQrURL"
              :src="wechatQrURL"
              :alt="$t('作者微信二维码')"
              class="qr-image"
            />
            <div v-else class="qr-placeholder" :aria-label="$t('微信二维码占位')">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
            </div>
            <span class="qr-label">{{ $t('扫码添加作者微信') }}</span>
          </div>
          <div class="wechat-info">
            <div class="wechat-label">{{ $t('作者微信') }}</div>
            <div class="wechat-id">{{ wechatId || 'xiao142000' }}</div>
          </div>
        </div>

        <!-- 其他联系方式 -->
        <div class="contact-list">
          <div v-if="email" class="contact-item">
            <span class="contact-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"/><polyline points="22,6 12,13 2,6"/></svg></span>
            <a :href="`mailto:${email}`">{{ email }}</a>
          </div>
          <div v-if="phone" class="contact-item">
            <span class="contact-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.79 0 0 1-3.07-8.67A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72c.13.96.36 1.9.7 2.81a2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45c.9.34 1.85.57 2.81.7A2 2 0 0 1 22 16.92z"/></svg></span>
            <a :href="`tel:${phone}`">{{ phone }}</a>
          </div>
          <div v-if="serviceHours" class="contact-item">
            <span class="contact-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg></span>
            <span>{{ serviceHours }}</span>
          </div>
        </div>

        <!-- 仓库快捷入口(再强化一次) -->
        <div class="contact-repos">
          <a
            v-for="repo in repoLinks"
            :key="repo.href"
            :href="repo.href"
            target="_blank"
            rel="noopener noreferrer"
            class="contact-repo"
          >
            <span class="contact-repo-icon" aria-hidden="true">
              <svg v-if="repo.icon === 'github'" viewBox="0 0 24 24"><path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56v-2c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.71 1.26 3.37.96.1-.75.4-1.26.74-1.55-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.04 0 0 .97-.31 3.18 1.18a11.05 11.05 0 0 1 5.79 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.58.24 2.75.12 3.04.74.81 1.18 1.84 1.18 3.1 0 4.43-2.7 5.41-5.27 5.69.41.36.78 1.07.78 2.16v3.2c0 .31.21.68.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5z" /></svg>
              <svg v-else-if="repo.icon === 'gitee'" viewBox="0 0 24 24"><path d="M11.97 1.6c-5.7 0-10.32 4.62-10.32 10.32s4.62 10.32 10.32 10.32 10.32-4.62 10.32-10.32S17.67 1.6 11.97 1.6zm-.04 5.16c2.05 0 3.71 1.66 3.71 3.71 0 .55-.45 1-1 1H10.3v3.94c0 .55-.45 1-1 1s-1-.45-1-1V6.76c0-.55.45-1 1-1h2.63c.55 0 1 .45 1 1z" /></svg>
            </span>
            {{ repo.label }}
          </a>
        </div>
      </div>
    </div>

    <!-- 底部:版权 + 免责声明 -->
    <div class="footer-bottom">
      <div class="container">
        <hr class="rule" />
        <div class="bottom-inner">
          <p class="copyright">© {{ new Date().getFullYear() }} HiveMTK · {{ $t('私域 AI 营销操作系统(AGPL-3.0 开源)') }}</p>
          <p class="footer-disclaimer">{{ $t('页脚免责声明') }}</p>
        </div>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.footer {
  background: var(--bg-deep);
  border-top: 2px solid var(--text);
  padding: 0;
  color: var(--text);
}

/* —— 顶部 CTA 仓库引导 —— */
.footer-cta {
  background: var(--bg-ink);
  color: var(--text-inv);
  padding: 56px 0;
}
.cta-inner {
  display: grid;
  grid-template-columns: 1.2fr 1fr;
  gap: 48px;
  align-items: center;
}
.cta-kicker {
  display: inline-block;
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--primary);
  margin-bottom: 12px;
  /* 在深色背景上需要稍亮的红 */
  color: #FF6B5E;
}
.cta-title {
  font-family: var(--font-display);
  font-size: clamp(1.6rem, 3vw, 2.2rem);
  font-weight: 800;
  line-height: 1.18;
  letter-spacing: -0.022em;
  margin-bottom: 14px;
  color: var(--text-inv);
}
.cta-desc {
  font-size: 0.96rem;
  color: var(--text-inv-muted);
  line-height: 1.75;
  max-width: 480px;
}
.cta-repos {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.cta-repo {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 20px;
  border-radius: var(--radius-md);
  background: rgba(250, 247, 242, 0.05);
  border: 1px solid rgba(250, 247, 242, 0.12);
  color: var(--text-inv);
  transition: background 0.2s ease, border-color 0.2s ease, transform 0.2s ease;
}
.cta-repo:hover {
  background: rgba(250, 247, 242, 0.1);
  border-color: rgba(255, 107, 94, 0.5);
  transform: translateX(4px);
}
.cta-repo-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  color: #FF6B5E;
  flex-shrink: 0;
}
.cta-repo-icon svg {
  width: 24px;
  height: 24px;
  fill: currentColor;
}
.cta-repo-text {
  display: flex;
  flex-direction: column;
  flex: 1;
  line-height: 1.2;
}
.cta-repo-label {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: 1.05rem;
}
.cta-repo-full {
  font-family: var(--font-mono);
  font-size: 0.82rem;
  color: var(--text-inv-muted);
  margin-top: 2px;
}
.cta-repo-arrow {
  color: var(--text-inv-muted);
  flex-shrink: 0;
  transition: color 0.2s ease, transform 0.2s ease;
}
.cta-repo:hover .cta-repo-arrow {
  color: #FF6B5E;
  transform: translate(2px, -2px);
}

/* —— 主体网格 —— */
.footer-grid {
  display: grid;
  grid-template-columns: 1.6fr 1fr 1fr 1fr 1.4fr;
  gap: 40px;
  padding: 64px 32px 48px;
}

/* —— 品牌列 —— */
.footer-brand .brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 18px;
}
.brand-seal {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 7px;
  background: var(--primary);
  color: #fff;
  font-family: var(--font-display);
  font-weight: 900;
  font-size: 1.25rem;
  letter-spacing: -0.04em;
  box-shadow: 0 3px 10px rgba(200, 57, 47, 0.3);
  flex-shrink: 0;
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
  font-size: 0.7rem;
  font-weight: 500;
  color: var(--text-muted);
  letter-spacing: 0.04em;
  margin-top: 2px;
}

.footer-desc {
  color: var(--text-muted);
  font-size: 0.92rem;
  line-height: 1.75;
  max-width: 320px;
  margin-bottom: 20px;
}

.footer-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.badge {
  display: inline-flex;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  background: var(--primary-soft);
  color: var(--primary);
  border: 1px solid var(--primary-border);
}

/* —— 列标题与链接 —— */
.footer-col-title {
  font-family: var(--font-display);
  font-size: 0.92rem;
  font-weight: 800;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  margin-bottom: 18px;
  color: var(--text);
  padding-bottom: 10px;
  border-bottom: 2px solid var(--text);
}
.footer-col ul {
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.footer-col a {
  color: var(--text-muted);
  font-size: 0.9rem;
  transition: color 0.18s ease;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.footer-col a:hover {
  color: var(--primary);
}

/* —— 联系列 —— */
.footer-contact-col {
  /* 略宽以容纳二维码 */
}
.contact-wechat {
  display: flex;
  gap: 14px;
  align-items: flex-start;
  margin-bottom: 18px;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--border);
}
.qr-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.qr-image {
  width: 110px;
  height: 110px;
  border-radius: 7px;
  border: 1px solid var(--border);
  object-fit: contain;
  background: #fff;
}
.qr-placeholder {
  width: 110px;
  height: 110px;
  border-radius: 7px;
  border: 1px dashed var(--border-strong);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-surface);
  color: var(--text-dim);
}
.qr-placeholder svg {
  width: 40px;
  height: 40px;
}
.qr-label {
  font-size: 0.7rem;
  color: var(--text-muted);
  text-align: center;
  max-width: 110px;
  line-height: 1.3;
}
.wechat-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-top: 6px;
}
.wechat-label {
  font-size: 0.74rem;
  color: var(--text-muted);
  letter-spacing: 0.04em;
}
.wechat-id {
  font-family: var(--font-mono);
  font-size: 1rem;
  font-weight: 700;
  color: var(--text);
  word-break: break-all;
}

.contact-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 18px;
}
.contact-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.86rem;
  color: var(--text-muted);
}
.contact-item a {
  color: var(--text-muted);
  transition: color 0.18s ease;
}
.contact-item a:hover {
  color: var(--primary);
}
.contact-ico {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--primary);
  flex-shrink: 0;
}
.contact-ico svg {
  width: 15px;
  height: 15px;
}

.contact-repos {
  display: flex;
  gap: 8px;
}
.contact-repo {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 7px 12px;
  border-radius: 7px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  color: var(--text-soft);
  font-size: 0.82rem;
  font-weight: 600;
  transition: all 0.18s ease;
}
.contact-repo:hover {
  background: var(--primary);
  color: #fff;
  border-color: var(--primary);
}
.contact-repo-icon {
  display: inline-flex;
  width: 14px;
  height: 14px;
}
.contact-repo-icon svg {
  width: 14px;
  height: 14px;
  fill: currentColor;
}

/* —— 底部 —— */
.footer-bottom {
  padding: 0 0 28px;
}
.bottom-inner {
  padding-top: 22px;
  text-align: center;
}
.copyright {
  font-size: 0.84rem;
  color: var(--text-muted);
  font-weight: 500;
}
.footer-disclaimer {
  max-width: 880px;
  margin: 10px auto 0;
  padding: 0 16px;
  font-size: 0.74rem;
  line-height: 1.7;
  color: var(--text-dim);
  text-align: center;
}

/* —— 响应式 —— */
@media (max-width: 1100px) {
  .footer-grid {
    grid-template-columns: 1fr 1fr 1fr;
    gap: 32px;
  }
  .footer-brand {
    grid-column: 1 / -1;
  }
  .footer-contact-col {
    grid-column: 1 / -1;
  }
}

@media (max-width: 768px) {
  .cta-inner {
    grid-template-columns: 1fr;
    gap: 28px;
  }
  .footer-grid {
    grid-template-columns: 1fr 1fr;
    gap: 28px;
    padding: 48px 20px 36px;
  }
  .footer-brand,
  .footer-contact-col {
    grid-column: 1 / -1;
  }
  .contact-wechat {
    flex-direction: row;
  }
}

@media (max-width: 480px) {
  .footer-grid {
    grid-template-columns: 1fr;
  }
  .contact-wechat {
    flex-direction: column;
  }
}
</style>
