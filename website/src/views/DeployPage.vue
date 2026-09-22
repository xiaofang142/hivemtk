<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSiteContact } from '../composables/useSiteContact.js'
import { useToast } from '../composables/useToast.js'

const { t } = useI18n()
const { wechatId } = useSiteContact()
const { success, error } = useToast()
const activeTab = ref('docker')

// 部署方式标签
const deployMethods = [
  {
    key: 'docker',
    title: 'Docker 一键部署',
    desc: '推荐方式：环境隔离、可一键启停、无需手动配置依赖。',
    cta: '前往部署文档',
    link: '/docs',
  },
  {
    key: 'source',
    title: '源码部署',
    desc: '直接 clone 仓库，本地启动后端 + 前端，适合二次开发。',
    cta: '查看源码',
    link: 'https://gitee.com/xhpmayun/hivemtk',
    external: true,
  },
  {
    key: 'doc',
    title: '完整安装文档',
    desc: 'Docker 部署、源码部署、FRP 私域穿透、数据库迁移与配置说明。',
    cta: '阅读安装文档',
    link: '/docs',
  },
]

// Docker compose 关键片段（开源仓库直接克隆，无二进制下载）
const dockerSnippet = `# 1. 克隆开源仓库
git clone https://gitee.com/xhpmayun/hivemtk.git
cd hivemtk

# 2. 准备环境变量
cp .env-example .env
# 编辑 .env，填写 POSTGRES_PASSWORD / JWT_SECRET / PLATFORM_ADMIN_PASSWORD 等必填项

# 3. 一键安装与启动（PostgreSQL + Redis + user-server + 本地推理栈）
make install   # 生成 docker-compose.yml + 构建前端 + 拉起推理栈
make up        # 若已生成配置，仅启动服务

# 4. 访问
# 用户端 Web/API：http://localhost:8204
# 默认账号 admin + .env 中的 PLATFORM_ADMIN_PASSWORD`

const sourceSnippet = `# 1. 克隆开源仓库
git clone https://gitee.com/xhpmayun/hivemtk.git
cd hivemtk

# 2. 启动后端（需要 Go 1.25 / PostgreSQL 15+ / Redis 7）
cd user-server
go mod download
go build -o user-server ./cmd/api/main.go
./user-server   # 读取 config.yaml / 环境变量

# 3. 启动前端（新终端，需要 Node 18+）
cd ../user-web
npm install
npm run dev

# 4. 访问
# 前端：http://localhost:8211
# 后端：http://localhost:8204`

function copyText(text) {
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text).then(
      () => success(t('已复制')),
      () => error(t('复制失败'))
    )
  } else {
    error(t('复制失败'))
  }
}

function copyWechat() {
  const id = wechatId.value
  if (!id) {
    error(t('复制失败'))
    return
  }
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(id).then(
      () => success(t('已复制')),
      () => error(t('复制失败'))
    )
  } else {
    error(t('复制失败'))
  }
}
</script>

<template>
  <div class="page">
    <!-- 头部 -->
    <section class="section deploy-hero">
      <div class="container">
        <span class="tag tag-primary">{{ $t('部署指南') }}</span>
        <h1 class="section-title">
          <span class="gradient-text">{{ $t('4 步完成部署') }}</span>
          {{ $t('开箱即用') }}
        </h1>
        <p class="section-subtitle">
          {{ $t('HiveMTK 基于 AGPL-3.0 开源协议，无授权码、无版本下载、无任何收费环节。源码可直接克隆、Docker 一键启动、源码部署灵活定制。') }}
        </p>
        <p class="section-subtitle" style="margin-top: 8px;">
          <span class="sub-clarify">{{ $t('本页仅展示部署方式与命令，所有源码与配置均在 GitHub / Gitee 仓库直接克隆，无中间分发环节。AGPL-3.0 要求修改后的网络服务代码也必须开源。') }}</span>
        </p>
      </div>
    </section>

    <!-- 部署方式选择 -->
    <section class="section method-section">
      <div class="container">
        <div class="method-grid">
          <div
            v-for="m in deployMethods"
            :key="m.key"
            class="method-card glass-card"
            :class="{ active: activeTab === m.key }"
            @click="activeTab = m.key"
          >
            <h3>{{ $t(m.title) }}</h3>
            <p>{{ $t(m.desc) }}</p>
            <a
              v-if="m.external"
              :href="m.link"
              target="_blank"
              rel="noopener noreferrer"
              class="btn-link"
            >{{ $t(m.cta) }}</a>
            <router-link v-else :to="m.link" class="btn-link">{{ $t(m.cta) }}</router-link>
          </div>
        </div>
      </div>
    </section>

    <!-- 命令片段 -->
    <section class="section snippet-section">
      <div class="container">
        <div class="snippet-card glass-card">
          <div class="snippet-head">
            <div class="snippet-tabs" role="tablist" :aria-label="$t('部署命令片段切换')">
              <button
                role="tab"
                id="tab-docker"
                :class="{ active: activeTab === 'docker' }"
                :aria-selected="activeTab === 'docker' ? 'true' : 'false'"
                aria-controls="panel-snippet"
                @click="activeTab = 'docker'"
              >Docker</button>
              <button
                role="tab"
                id="tab-source"
                :class="{ active: activeTab === 'source' }"
                :aria-selected="activeTab === 'source' ? 'true' : 'false'"
                aria-controls="panel-snippet"
                @click="activeTab = 'source'"
              >{{ $t('源码') }}</button>
            </div>
            <button class="btn-copy" @click="copyText(activeTab === 'docker' ? dockerSnippet : sourceSnippet)">
              <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
              {{ $t('复制') }}
            </button>
          </div>
          <pre id="panel-snippet" role="tabpanel" aria-labelledby="tab-docker" class="snippet-body"><code>{{ activeTab === 'docker' ? dockerSnippet : sourceSnippet }}</code></pre>
        </div>
      </div>
    </section>

    <!-- 公开提示 -->
    <section class="section public-note-section">
      <div class="container">
        <div class="public-note glass-card">
          <div class="cta-text">
            <h3>{{ $t('AGPL-3.0 开源，零授权门槛') }}</h3>
            <p>{{ $t('HiveMTK 基于 AGPL-3.0 协议开源，无授权码、无版本下载、无任何收费环节。源码与文档在 GitHub / Gitee 公开托管，可自由 fork、二次开发；请注意 AGPL-3.0 要求修改后的网络服务代码必须同样开源。') }}</p>
          </div>
        </div>
      </div>
    </section>

    <!-- 联系作者 -->
    <section class="section contact-section">
      <div class="container">
        <div class="contact-card glass-card">
          <div class="contact-text">
            <h3>{{ $t('部署或使用遇到问题？') }}</h3>
            <p>{{ $t('添加作者微信获取 1v1 部署指导，包含 Docker 调试、源码编译、数据库迁移、桥接扩展配置等问题。') }}</p>
          </div>
          <div class="contact-action">
            <div class="wechat-id">{{ $t('微信号') }}：{{ wechatId || $t('暂未配置') }}</div>
            <button class="btn-outline" @click="copyWechat">
              {{ $t('复制微信号') }}
            </button>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page {
  padding-top: 80px;
  background: var(--bg-base);
}

/* —— 编辑杂志风章节头 —— */
.deploy-hero {
  padding: 56px 0 48px;
  border-top: 2px solid var(--text);
}

.deploy-hero .tag {
  margin-bottom: 18px;
}

.section-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5vw, 3.4rem);
  font-weight: 900;
  line-height: 1.1;
  letter-spacing: -0.028em;
  margin: 16px 0 18px;
  color: var(--text);
}

.section-subtitle {
  color: var(--text-muted);
  font-size: 1.06rem;
  max-width: 760px;
  margin-bottom: 16px;
  line-height: 1.75;
}

.sub-clarify {
  display: inline-block;
  padding: 10px 16px;
  border-radius: var(--radius-sm);
  background: var(--primary-soft);
  border: 1px solid var(--primary-border);
  color: var(--text-soft);
  font-size: 0.92rem;
  line-height: 1.65;
}

/* —— 卡片基础(glass-card 适配为新设计系统) —— */
.glass-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
}

/* —— 部署方式 —— */
.method-section {
  padding: 36px 0;
}

.method-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 18px;
}

.method-card {
  padding: 26px 26px;
  cursor: pointer;
  transition: transform 0.22s ease, border-color 0.22s ease, box-shadow 0.22s ease, background 0.22s ease;
}

.method-card:hover {
  transform: translateY(-3px);
  border-color: var(--primary);
  box-shadow: var(--shadow-lg);
}

.method-card.active {
  border-color: var(--primary);
  background: var(--primary-soft);
  box-shadow: var(--shadow-primary);
}

.method-card h3 {
  font-family: var(--font-display);
  font-size: 1.2rem;
  font-weight: 800;
  letter-spacing: -0.018em;
  margin-bottom: 10px;
  color: var(--text);
}

.method-card p {
  color: var(--text-muted);
  font-size: 0.94rem;
  line-height: 1.65;
  margin-bottom: 16px;
}

.btn-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border-radius: var(--radius-sm);
  background: var(--primary);
  color: #fff;
  font-size: 0.86rem;
  font-weight: 600;
  text-decoration: none;
  transition: background 0.18s ease, transform 0.18s ease;
}

.btn-link:hover {
  background: var(--primary-deep);
  transform: translateX(2px);
}

/* —— 命令片段 —— */
.snippet-section {
  padding: 36px 0;
}

.snippet-card {
  padding: 0;
  overflow: hidden;
}

.snippet-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-subtle);
}

.snippet-tabs {
  display: flex;
  gap: 8px;
}

.snippet-tabs button {
  padding: 9px 16px;
  min-height: 40px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  font-size: 0.85rem;
  font-weight: 600;
  font-family: var(--font-body);
  cursor: pointer;
  transition: all 0.18s ease;
}

.snippet-tabs button:hover {
  border-color: var(--primary-border);
  color: var(--text);
}

.snippet-tabs button.active {
  background: var(--primary);
  border-color: var(--primary);
  color: #fff;
  box-shadow: var(--shadow-primary);
}

.btn-copy {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 9px 14px;
  min-height: 40px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  font-size: 0.84rem;
  font-weight: 600;
  font-family: var(--font-body);
  cursor: pointer;
  transition: all 0.18s ease;
}

.btn-copy:hover {
  color: var(--primary);
  border-color: var(--primary);
  background: var(--primary-soft);
}

.btn-copy :deep(svg) {
  width: 14px;
  height: 14px;
}

/* —— 反白代码块:终端风 —— */
.snippet-body {
  margin: 0;
  padding: 24px 26px;
  background: var(--bg-ink);
  color: var(--text-inv);
  font-family: var(--font-mono);
  font-size: 0.88rem;
  line-height: 1.85;
  overflow-x: auto;
  white-space: pre;
}

.snippet-body code {
  color: inherit;
  font-family: inherit;
}

/* —— 公开提示 —— */
.public-note-section {
  padding: 36px 0;
}

.public-note {
  padding: 30px 36px;
  border-left: 3px solid var(--primary);
}

.cta-text h3 {
  font-family: var(--font-display);
  font-size: 1.3rem;
  font-weight: 800;
  letter-spacing: -0.018em;
  margin-bottom: 10px;
  color: var(--text);
}

.cta-text p {
  color: var(--text-soft);
  font-size: 0.98rem;
  line-height: 1.75;
}

/* —— 联系作者 —— */
.contact-section {
  padding: 36px 0 100px;
}

.contact-card {
  padding: 30px 32px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  flex-wrap: wrap;
  border-left: 3px solid var(--accent);
}

.contact-text h3 {
  font-family: var(--font-display);
  font-size: 1.3rem;
  font-weight: 800;
  letter-spacing: -0.018em;
  margin-bottom: 8px;
  color: var(--text);
}

.contact-text p {
  color: var(--text-muted);
  font-size: 0.94rem;
  line-height: 1.7;
}

.contact-action {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 12px;
}

.wechat-id {
  font-family: var(--font-mono);
  color: var(--accent);
  font-weight: 700;
  font-size: 0.98rem;
  padding: 8px 14px;
  background: var(--accent-soft);
  border: 1px solid var(--accent-border);
  border-radius: var(--radius-sm);
}

.btn-outline {
  padding: 11px 20px;
  background: transparent;
  color: var(--primary);
  border: 1px solid var(--primary);
  border-radius: var(--radius-md);
  font-size: 0.9rem;
  font-weight: 600;
  font-family: var(--font-body);
  cursor: pointer;
  transition: background 0.18s ease, transform 0.18s ease;
}

.btn-outline:hover {
  background: var(--primary-soft);
  transform: translateY(-1px);
}

@media (max-width: 768px) {
  .deploy-hero {
    padding: 40px 0 32px;
  }

  .public-note,
  .contact-card {
    padding: 22px;
  }

  .snippet-body {
    font-size: 0.8rem;
    padding: 18px;
  }

  .contact-action {
    align-items: flex-start;
    width: 100%;
  }
}

@media (max-width: 480px) {
  .method-card {
    padding: 20px;
  }
  .snippet-head {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }
  .snippet-tabs button {
    flex: 1;
  }
}
</style>
