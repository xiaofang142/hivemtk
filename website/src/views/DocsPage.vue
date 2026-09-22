<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const activeSection = ref('quickstart')
const mobileNavOpen = ref(false)

const sections = computed(() => [
  { id: 'quickstart', title: i18n.global.t('快速开始'), group: '入门' },
  { id: 'architecture', title: i18n.global.t('架构与分工'), group: '入门' },
  { id: 'requirements', title: i18n.global.t('系统要求'), group: '入门' },
  { id: 'docker-deploy', title: i18n.global.t('Docker 部署'), group: '部署' },
  { id: 'source-deploy', title: i18n.global.t('源码部署'), group: '部署' },
  { id: 'frp-deploy', title: i18n.global.t('FRP 私域穿透'), group: '部署' },
  { id: 'config', title: i18n.global.t('配置说明'), group: '部署' },
  { id: 'modules', title: i18n.global.t('功能模块'), group: '使用' },
  { id: 'auto-reply', title: i18n.global.t('自动回复配置'), group: '使用' },
  { id: 'rag', title: i18n.global.t('RAG 知识库'), group: '使用' },
  { id: 'troubleshoot', title: i18n.global.t('故障排查'), group: '运维' },
  { id: 'faq', title: i18n.global.t('常见问题'), group: '运维' },
])

const groupedSections = computed(() => {
  const map = new Map()
  for (const s of sections.value) {
    if (!map.has(s.group)) map.set(s.group, [])
    map.get(s.group).push(s)
  }
  return Array.from(map.entries()).map(([group, items]) => ({ group, items }))
})

function scrollTo(id) {
  activeSection.value = id
  mobileNavOpen.value = false
  nextTick(() => {
    const el = document.getElementById(id)
    if (el) {
      const top = el.getBoundingClientRect().top + window.scrollY - 90
      window.scrollTo({ top, behavior: 'smooth' })
    }
  })
}

onMounted(() => {
  if (route.hash) {
    const id = route.hash.slice(1)
    if (sections.value.find(s => s.id === id)) {
      scrollTo(id)
    }
  }
  window.addEventListener('scroll', handleScroll, { passive: true })
})

onUnmounted(() => {
  window.removeEventListener('scroll', handleScroll)
})

function handleScroll() {
  const scrollY = window.scrollY + 120
  for (let i = sections.value.length - 1; i >= 0; i--) {
    const el = document.getElementById(sections.value[i].id)
    if (el && el.offsetTop <= scrollY) {
      activeSection.value = sections.value[i].id
      break
    }
  }
}
</script>

<template>
  <div class="page">
    <div class="container docs-container">
      <!-- 侧边栏 -->
      <aside class="docs-sidebar" :class="{ 'sidebar-open': mobileNavOpen }">
        <div class="sidebar-inner">
          <div v-for="g in groupedSections" :key="g.group" class="sidebar-group">
            <div class="sidebar-group-title">{{ $t(g.group) }}</div>
            <ul>
              <li v-for="s in g.items" :key="s.id">
                <a
                  href="javascript:void(0)"
                  :class="{ active: activeSection === s.id }"
                  @click="scrollTo(s.id)"
                >{{ s.title }}</a>
              </li>
            </ul>
          </div>
        </div>
      </aside>

      <!-- 内容区 -->
      <main class="docs-content">
        <button class="mobile-nav-toggle" @click="mobileNavOpen = !mobileNavOpen">
          {{ mobileNavOpen ? $t('关闭目录') : $t('查看目录') }}
        </button>

        <h1 class="docs-title">{{ $t('安装使用文档') }}</h1>
        <p class="docs-update">{{ $t('最后更新：2026-07-22 · 适用版本：v1.0.0+') }}</p>

        <!-- 快速开始 -->
        <section id="quickstart" class="doc-section">
          <h2>{{ $t('快速开始') }}</h2>
          <p class="lead">{{ $t('4 步完成用户端部署并体验全部功能（开源免费，无需注册）。') }}</p>

          <div class="step-list">
            <div class="step-item">
              <div class="step-num">1</div>
              <div class="step-body">
                <h4>{{ $t('获取源码') }}</h4>
                <p>{{ $t('克隆开源用户端仓库（') }}<code>hivemtk</code>{{ $t('）：') }}</p>
                <pre><code>git clone https://gitee.com/xhpmayun/hivemtk.git
cd hivemtk</code></pre>
              </div>
            </div>
            <div class="step-item">
              <div class="step-num">2</div>
              <div class="step-body">
                <h4>{{ $t('配置与启动') }}</h4>
                <p>{{ $t('生成配置并一键启动全部组件（PostgreSQL + Redis + user-server + 本地推理栈）：') }}</p>
                <pre><code>cp .env-example .env   # {{ $t('编辑 PLATFORM_ADMIN_PASSWORD / JWT_SECRET 等必填项') }}
vim .env
make install           # {{ $t('生成 docker-compose.yml、构建前端与 SDK、拉起推理栈并启动') }}
make up                # docker compose up -d{{ $t('（如已生成配置文件）') }}</code></pre>
                <p>{{ $t('默认管理员账号为') }} <code>admin</code>{{ $t('，密码即') }} <code>.env</code> {{ $t('中的') }} <code>PLATFORM_ADMIN_PASSWORD</code>{{ $t('；首次访问按向导完成初始化即可。') }}</p>
              </div>
            </div>
            <div class="step-item">
              <div class="step-num">3</div>
              <div class="step-body">
                <h4>{{ $t('访问与使用') }}</h4>
                <p>{{ $t('浏览器打开') }} <code>http://{{ $t('服务器IP') }}:8204</code> {{ $t('即可使用。AI 默认走本地推理栈（数据不出域）；如需使用云端大模型，编辑 .env 的 LLM_BASE_URL 与 LLM_API_KEY 即可。') }}</p>
              </div>
            </div>
          </div>
        </section>

        <!-- 架构与分工 -->
        <section id="architecture" class="doc-section">
          <h2>{{ $t('架构与分工') }}</h2>
          <p class="lead">{{ $t('本项目由「用户端」与一个可选的「平台端」组成，开源与部署边界清晰：') }}</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('角色') }}</th><th>{{ $t('说明') }}</th><th>{{ $t('是否需自行部署') }}</th></tr></thead>
              <tbody>
                <tr>
                  <td><strong>{{ $t('用户端（HiveMTK）') }}</strong></td>
                  <td>{{ $t('你私有化部署的业务系统：user-server（Go）+ user-web（Vue）+ PostgreSQL + Redis + 本地推理栈。包含全部 AI 营销功能。') }}</td>
                  <td>{{ $t('是（开源，AGPL-3.0）') }}</td>
                </tr>
                <tr>
                  <td>{{ $t('平台端（可选本地组件）') }}</td>
                  <td>{{ $t('独立仓库提供的资产市场等服务，用户端默认不启用（PLATFORM_ENABLED=false）。启用与否都不影响任何 AI 营销功能；关闭时用户端不加载平台配置、不发一个出站请求，资产市场返回空列表。') }}</td>
                  <td>{{ $t('否') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p class="note">{{ $t('用户端默认') }} <code>PLATFORM_ENABLED=false</code> {{ $t('，不接任何平台端即可私有化部署；确需本地平台端时把它改为') }} <code>true</code> {{ $t('，并用') }} <code>PLATFORM_API_HOST</code> {{ $t('指向本机地址。') }}</p>
          <p class="note">{{ $t('开源仓库：') }}<a class="inline-link" href="https://gitee.com/xhpmayun/hivemtk" target="_blank" rel="noopener">gitee.com/xhpmayun/hivemtk</a></p>
        </section>

        <!-- 系统要求 -->
        <section id="requirements" class="doc-section">
          <h2>{{ $t('系统要求') }}</h2>
          <p class="lead">{{ $t('下列配置不是「模型常驻内存」的孤立数字，而是按') }}<strong>{{ $t('操作系统 + Docker 守护进程 + 数据层 + 应用进程 + 本地推理栈 + 业务并发余量') }}</strong>{{ $t('六层叠加后的整机真实需求，并参考「日均 1 万咨询 × 40 条消息」企业级业务量反推。') }}</p>

          <h4>{{ $t('业务量 → 硬件换算基准') }}</h4>
          <p>{{ $t('以企业典型场景') }} <strong>10,000 {{ $t('咨询/天 × 40 条/咨询 = 40 万消息/天') }}</strong> {{ $t('为基准换算：') }}</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('维度') }}</th><th>{{ $t('日量') }}</th><th>{{ $t('半年累积') }}</th><th>{{ $t('资源映射') }}</th></tr></thead>
              <tbody>
                <tr><td>{{ $t('消息表（含索引）') }}</td><td>{{ $t('40 万条 / ~400 MB') }}</td><td>~72 GB</td><td>{{ $t('PG 数据文件 + WAL') }}</td></tr>
                <tr><td>RAG {{ $t('向量索引（pgvector HNSW）') }}</td><td>{{ $t('28 万条 / ~1.1 GB') }}</td><td>~200 GB</td><td>{{ $t('1024 维 float32 + HNSW 索引') }}</td></tr>
                <tr><td>LLM {{ $t('调用（按 AI 承接率 70% 估算）') }}</td><td>{{ $t('28 万次') }}</td><td>{{ $t('5,000 万次') }}</td><td>{{ $t('高峰 ~16 QPS，需 4–8 并发槽位') }}</td></tr>
                <tr><td>Embedding / Rerank</td><td>{{ $t('28 万次 / 28 万次') }}</td><td>—</td><td>{{ $t('每次 RAG 检索 1 embed + 1 rerank') }}</td></tr>
                <tr><td>{{ $t('桥接扩展（社媒五端自动回复）') }}</td><td>{{ $t('运行在员工自己的浏览器') }}</td><td>—</td><td>{{ $t('服务端零常驻，扩展在线即可收发') }}</td></tr>
                <tr><td>{{ $t('Redis 会话 / 队列 / 限流') }}</td><td>{{ $t('1 万') }} {{ $t('并发会话') }}</td><td>—</td><td>{{ $t('会话状态 + 消息缓冲 + 频控') }}</td></tr>
              </tbody>
            </table>
          </div>
          <p class="note">{{ $t('高峰按 80/20 法则估算：80% 消息集中在 20% 时间（4.8 小时），峰值 QPS 约为日均的 2–3 倍。') }}</p>

          <h4>{{ $t('硬件要求（整机真实配置）') }}</h4>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('部署场景') }}</th><th>CPU</th><th>{{ $t('内存') }}</th><th>{{ $t('磁盘') }}</th><th>{{ $t('业务规模上限') }}</th></tr></thead>
              <tbody>
                <tr>
                  <td>{{ $t('开发 / 测试（云端 LLM）') }}</td>
                  <td>{{ $t('2 核') }}</td>
                  <td>6 GB</td>
                  <td>50 GB SSD</td>
                  <td>{{ $t('仅功能验证，无并发要求') }}</td>
                </tr>
                <tr>
                  <td>{{ $t('开发 / 测试（本地推理 dev 档）') }}</td>
                  <td>{{ $t('4 核') }}</td>
                  <td>12 GB</td>
                  <td>80 GB SSD</td>
                  <td>{{ $t('单用户调试，AI 走 1.5B 本地模型') }}</td>
                </tr>
                <tr>
                  <td>{{ $t('小型生产（dev 档，单机全栈）') }}</td>
                  <td>{{ $t('8 核') }}</td>
                  <td>24 GB</td>
                  <td>200 GB SSD</td>
                  <td>≤ 1,000 {{ $t('咨询/天，1–5 坐席') }}</td>
                </tr>
                <tr>
                  <td>{{ $t('中型生产（prod 档，单机全栈）') }}</td>
                  <td>{{ $t('16 核') }}</td>
                  <td>64 GB</td>
                  <td>500 GB SSD</td>
                  <td>1,000–5,000 {{ $t('咨询/天，10–30 坐席') }}</td>
                </tr>
                <tr>
                  <td>{{ $t('企业级生产（prod 档，单机全栈）') }}</td>
                  <td>{{ $t('32 核') }}</td>
                  <td>96 GB</td>
                  <td>1 TB NVMe SSD</td>
                  <td>~10,000 {{ $t('咨询/天 × 40 条消息，30–50 坐席') }}</td>
                </tr>
                <tr>
                  <td>{{ $t('大型集群（多节点水平扩展）') }}</td>
                  <td>{{ $t('32+ 核 × N') }}</td>
                  <td>64+ GB × N</td>
                  <td>1 TB+ NVMe × N</td>
                  <td>{{ $t('&gt; 1 万咨询/天，按业务域拆分独立节点') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p class="note warning"><strong>{{ $t('不要按「模型常驻内存」选机器') }}</strong>{{ $t('。下方分解表显示：1 万咨询/天的企业级场景下，OS+Docker 占 ~3GB、数据层占 ~12GB、应用层占 ~4GB、推理栈占 ~19GB（prod 档），叠加后整机需 ~38GB，加上 30% 安全余量即 64GB。低于此配置会在高峰期触发 OOM 或推理排队。') }}</p>

          <h4>{{ $t('整机资源开销分解（企业级 1 万咨询/天）') }}</h4>
          <p>{{ $t('以下为「dev 档推理 + prod 档推理」两套配置在 1 万咨询/天业务量下的真实内存占用分解，可作为容量规划依据：') }}</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('层级') }}</th><th>{{ $t('组件') }}</th><th>dev {{ $t('档内存') }}</th><th>prod {{ $t('档内存') }}</th><th>{{ $t('说明') }}</th></tr></thead>
              <tbody>
                <tr><td rowspan="2">{{ $t('系统层') }}</td><td>Linux + systemd + ssh</td><td>~1.0 GB</td><td>~1.0 GB</td><td>{{ $t('内核 + 守护进程') }}</td></tr>
                <tr><td>Docker daemon + {{ $t('监控') }}</td><td>~1.5 GB</td><td>~2.0 GB</td><td>{{ $t('容器运行时 + 宝塔/监控 agent') }}</td></tr>
                <tr><td rowspan="2">{{ $t('数据层') }}</td><td>PostgreSQL 15 + pgvector</td><td>~3.0 GB</td><td>~10 GB</td><td>{{ $t('shared_buffers + 200GB HNSW 索引缓存') }}</td></tr>
                <tr><td>Redis 7</td><td>~1.0 GB</td><td>~2.0 GB</td><td>{{ $t('1 万并发会话 + 消息队列 + 频控') }}</td></tr>
                <tr><td rowspan="2">{{ $t('应用层') }}</td><td>user-server (Go)</td><td>~1.5 GB</td><td>~4.0 GB</td><td>{{ $t('Gin + WebSocket 长连接 + 业务逻辑') }}</td></tr>
                <tr><td>{{ $t('桥接扩展（客户端侧）') }}</td><td>Chrome {{ $t('扩展') }} × {{ $t('按账号') }}</td><td>—</td><td>—</td><td>{{ $t('运行在员工浏览器，服务端零占用；五端社媒收发依赖扩展在线') }}</td></tr>
                <tr><td rowspan="3">{{ $t('本地推理栈') }}</td><td>LLM (Qwen2.5-1.5B / 14B Q4_K_M)</td><td>~3.0 GB</td><td>~12 GB</td><td>{{ $t('常驻 + KV cache 8192 ctx × 4 并发槽位') }}</td></tr>
                <tr><td>Embedding (bge-m3 Q4 / F16)</td><td>~3.0 GB</td><td>~5.0 GB</td><td>{{ $t('1024 维，RAG 检索高峰批处理') }}</td></tr>
                <tr><td>Rerank (bge-reranker-v2-m3)</td><td>~1.5 GB</td><td>~1.5 GB</td><td>{{ $t('cross-encoder 精排') }}</td></tr>
                <tr><td><strong>{{ $t('合计') }}</strong></td><td><strong>{{ $t('小规模全栈') }}</strong></td><td><strong>~16 GB</strong></td><td><strong>~38 GB</strong></td><td>{{ $t('未含 30% 安全余量') }}</td></tr>
                <tr><td><strong>{{ $t('推荐整机') }}</strong></td><td><strong>{{ $t('含 30% 余量') }}</strong></td><td><strong>24 GB</strong></td><td><strong>64 GB</strong></td><td>{{ $t('生产环境必须留余量应对突发流量') }}</td></tr>
              </tbody>
            </table>
          </div>

          <h4>{{ $t('磁盘占用测算（1 万咨询/天）') }}</h4>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('数据类型') }}</th><th>{{ $t('日均增量') }}</th><th>{{ $t('半年累积') }}</th><th>{{ $t('一年累积') }}</th></tr></thead>
              <tbody>
                <tr><td>{{ $t('消息表（含索引）') }}</td><td>~400 MB</td><td>~72 GB</td><td>~144 GB</td></tr>
                <tr><td>RAG {{ $t('向量索引') }}</td><td>~1.1 GB</td><td>~200 GB</td><td>~400 GB</td></tr>
                <tr><td>{{ $t('操作日志 / 审计') }}</td><td>~200 MB</td><td>~36 GB</td><td>~72 GB</td></tr>
                <tr><td>{{ $t('模型文件（dev / prod）') }}</td><td>—</td><td>3 GB / 12 GB</td><td>3 GB / 12 GB</td></tr>
                <tr><td>PostgreSQL WAL + {{ $t('临时表') }}</td><td>~100 MB</td><td>~18 GB</td><td>~36 GB</td></tr>
                <tr><td><strong>{{ $t('合计') }}</strong></td><td><strong>~1.8 GB</strong></td><td><strong>~330 GB</strong></td><td><strong>~660 GB</strong></td></tr>
              </tbody>
            </table>
          </div>
          <p class="note">{{ $t('半年累积 ~330GB 是企业级生产配置「1 TB NVMe SSD」的依据；NVMe 优先，pgvector HNSW 索引随机读密集，SATA SSD 高峰期会成为瓶颈。') }}</p>

          <h4>{{ $t('多节点拆分建议（&gt; 5,000 咨询/天）') }}</h4>
          <p>{{ $t('单机全栈到 96GB 内存后，再往上扩建议按业务域拆分独立节点，避免 Chrome 内存波动拖垮推理栈：') }}</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('节点角色') }}</th><th>CPU</th><th>{{ $t('内存') }}</th><th>{{ $t('磁盘') }}</th><th>{{ $t('承载组件') }}</th></tr></thead>
              <tbody>
                <tr><td>{{ $t('推理节点（独立）') }}</td><td>{{ $t('16 核') }}</td><td>32 GB</td><td>100 GB SSD</td><td>llama-server × 3（LLM + Embed + Rerank）</td></tr>
                <tr><td>{{ $t('（可选）GPU 推理节点') }}</td><td>{{ $t('8 核') }}</td><td>16 GB</td><td>100 GB SSD</td><td>NVIDIA GPU + llama-server（NGL=999）</td></tr>
              </tbody>
            </table>
          </div>

          <h4>{{ $t('单组件实测资源占用（基线参考）') }}</h4>
          <p>{{ $t('以下为 cgroup v2 memory.peak 实测值，仅作为基线参考；上方整机配置已叠加 OS / Docker / 并发余量，请勿用本表数字直接选机。') }}</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('组件') }}</th><th>{{ $t('CPU 限额') }}</th><th>{{ $t('内存峰值（单实例）') }}</th><th>{{ $t('说明') }}</th></tr></thead>
              <tbody>
                <tr><td>mtk-postgres (pgvector:pg15)</td><td>{{ $t('1 核') }}</td><td>~463 MB</td><td>{{ $t('shared_buffers=256MB，max_connections=500') }}</td></tr>
                <tr><td>mtk-redis (7-alpine)</td><td>{{ $t('1 核') }}</td><td>~256 MB</td><td>{{ $t('maxmemory=1gb，allkeys-lru') }}</td></tr>
                <tr><td>mtk-user-server (Go)</td><td>{{ $t('1–2 核') }}</td><td>~512 MB</td><td>{{ $t('空载基线') }}</td></tr>
                <tr><td>llama-server LLM (Qwen2.5-1.5B Q4_K_M)</td><td>{{ $t('2–4 核') }}</td><td>~1.6 GB</td><td>{{ $t('常驻 1.1GB + KV cache (ctx=8192)') }}</td></tr>
                <tr><td>llama-server Embedding (bge-m3 Q4_K_M)</td><td>{{ $t('1–2 核') }}</td><td>~2.5 GB</td><td>{{ $t('1024 维，warmup 后稳定') }}</td></tr>
                <tr><td>llama-server Rerank (bge-reranker-v2-m3 Q4_K_M)</td><td>{{ $t('1 核') }}</td><td>~1.0 GB</td><td>{{ $t('cross-encoder，无 pooling') }}</td></tr>
                <tr><td>{{ $t('模型文件（dev 档）') }}</td><td>—</td><td>~3.0 GB {{ $t('磁盘') }}</td><td>LLM 1.1G + Embed 1.2G + Rerank 0.6G</td></tr>
                <tr><td>{{ $t('模型文件（prod 档）') }}</td><td>—</td><td>~12.0 GB {{ $t('磁盘') }}</td><td>14B Q4 9G + bge-m3 F16 2.4G + Rerank 0.6G</td></tr>
              </tbody>
            </table>
          </div>

          <p class="note">
            <strong>{{ $t('社媒五端桥接（自动回复）') }}</strong>{{ $t('：抖音/快手/小红书/闲鱼/TikTok 经 Chrome 扩展桥接收发消息，扩展运行在员工自己的登录态浏览器中，') }}<strong>{{ $t('服务端无需为浏览器自动化预留任何内存或节点') }}</strong>{{ $t('（旧版 Chrome Headless 无头模式已废弃）。仅需保证关键时段扩展在线、平台登录态有效。') }}
          </p>
          <p class="note">
            <strong>{{ $t('prod 档推理') }}</strong>{{ $t('：LLM 切换为 Qwen2.5-14B-Instruct Q4_K_M（常驻 ~9GB），Embedding 切换为 bge-m3 F16（~4.5GB），Rerank 不变；三服务在 4 并发槽位下峰值 ~18GB，企业级 8 并发槽位峰值 ~25GB。') }}
          </p>
          <p class="note">
            <strong>{{ $t('GPU 加速（可选）') }}</strong>：{{ $t('纯 CPU 部署可用；如配 NVIDIA GPU（≥ 16GB 显存），将') }} <code>.env</code> {{ $t('中') }} <code>NGL=999</code> {{ $t('即可全卸载，显著降低 LLM 推理延迟并提升吞吐；macOS Apple Silicon 默认 Metal 加速（') }}<code>NGL=999</code>{{ $t('）。GPU 节点可大幅降低整机内存需求（LLM 不再占 RAM）。') }}
          </p>

          <h4>{{ $t('软件要求') }}</h4>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('软件') }}</th><th>{{ $t('版本') }}</th><th>{{ $t('用途') }}</th></tr></thead>
              <tbody>
                <tr><td>Docker</td><td>20.10+</td><td>{{ $t('容器运行时（数据层 PG / Redis）') }}</td></tr>
                <tr><td>Docker Compose</td><td>v2.0+</td><td>{{ $t('服务编排') }}</td></tr>
                <tr><td>Go</td><td>1.25.0</td><td>{{ $t('源码部署后端 user-server') }}</td></tr>
                <tr><td>Node.js</td><td>18+</td><td>{{ $t('源码构建前端 user-web / embed-sdk') }}</td></tr>
                <tr><td>PostgreSQL</td><td>{{ $t('15+（含 pgvector）') }}</td><td>{{ $t('主数据库 + RAG 向量检索') }}</td></tr>
                <tr><td>Redis</td><td>7+</td><td>{{ $t('缓存 / 限流 / 会话') }}</td></tr>
                <tr><td>llama.cpp（llama-server）</td><td>{{ $t('最新版（2026-07+）') }}</td><td>{{ $t('宿主机本地推理栈（LLM / Embedding / Rerank）') }}</td></tr>
                <tr><td>Chrome / Edge {{ $t('浏览器') }}</td><td>{{ $t('最新版') }}</td><td>{{ $t('社媒五端桥接扩展宿主（员工客户端侧，可选）') }}</td></tr>
                <tr><td>FRP（frpc / frps）</td><td>v0.70.0+</td><td>{{ $t('私域穿透（内网部署可选）') }}</td></tr>
                <tr><td>Make</td><td>GNU Make 4+</td><td>{{ $t('执行 Makefile 一键命令') }}</td></tr>
              </tbody>
            </table>
          </div>
          <p class="note">{{ $t('Docker 部署仅需安装 Docker / Docker Compose 与 llama.cpp；源码部署额外需要 Go 与 Node.js。') }}</p>
        </section>

        <!-- Docker 部署 -->
        <section id="docker-deploy" class="doc-section">
          <h2>{{ $t('Docker 部署（推荐）') }}</h2>
          <p class="lead">{{ $t('推荐用 Docker Compose 一键部署用户端，无需安装 Go / Node，适合生产环境。') }}</p>

          <h4>{{ $t('1. 克隆并生成配置') }}</h4>
          <pre><code>git clone https://gitee.com/xhpmayun/hivemtk.git
cd hivemtk
cp .env-example .env
vim .env   # {{ $t('修改 PLATFORM_ADMIN_PASSWORD / JWT_SECRET / POSTGRES_PASSWORD 等必填项') }}</code></pre>
          <p class="note">{{ $t('必填项缺失时') }} <code>make install</code> {{ $t('会提示用') }} <code>openssl rand -hex 32</code> {{ $t('生成随机密钥。') }}</p>

          <h4>{{ $t('2. 一键安装与启动') }}</h4>
          <pre><code>make install   # {{ $t('生成 docker-compose.yml、构建前端与 SDK、拉起本地推理栈并启动全栈') }}
make up        # {{ $t('若已生成配置，仅启动服务') }}
docker compose ps   # {{ $t('查看运行状态') }}</code></pre>
          <p>{{ $t('用户端 Web 默认监听') }} <code>8204</code>{{ $t('，后端 user-server 同一端口提供 API；默认管理员账号') }} <code>admin</code>{{ $t('，密码为 .env 中的 PLATFORM_ADMIN_PASSWORD。') }}</p>

          <h4>{{ $t('3. 关键环境变量') }}</h4>
          <p>{{ $t('在') }} <code>.env</code> {{ $t('中设置（字段优先于镜像默认值）：') }}</p>
          <pre><code># {{ $t('PostgreSQL（用户端，库名 user_db）') }}
POSTGRES_USER=admin
POSTGRES_PASSWORD=ChangeMe_To_Strong_Password_123!
USER_DB_NAME=user_db
USER_POSTGRES_HOST_PORT=8202

# Redis
REDIS_HOST_PORT=8203

# {{ $t('应用端口（用户端 Web / API）') }}
PORT=8204
USER_SERVER_PORT=8204

# {{ $t('JWT / 安全密钥（务必修改为随机串）') }}
JWT_SECRET=ChangeMe_To_32Char_Random_JWT_Secret_Key_For_Production
JWT_EXPIRE=24h
PLATFORM_ADMIN_PASSWORD=ChangeMe_To_Strong_Password_For_User_Admin

# {{ $t('平台端（可选本地组件，默认关闭；只有开启时才读取下面三项）') }}
PLATFORM_ENABLED=false
PLATFORM_API_HOST=http://127.0.0.1:8205   # {{ $t('指向本机自建的平台端') }}
MERCHANT_API_SECRET=ChangeMe_To_Random_Secret   # {{ $t('用户端与平台端共用的签名密钥') }}</code></pre>

          <h4>{{ $t('端口对照表') }}</h4>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ $t('服务') }}</th><th>{{ $t('容器端口') }}</th><th>{{ $t('宿主机端口（默认）') }}</th></tr></thead>
              <tbody>
                <tr><td>user-server（Web + API）</td><td>8204</td><td>8204</td></tr>
                <tr><td>{{ $t('PostgreSQL（用户端）') }}</td><td>8202</td><td>8202</td></tr>
                <tr><td>Redis</td><td>8203</td><td>8203</td></tr>
                <tr><td>{{ $t('mtk-llm（本地推理）') }}</td><td>8207</td><td>8207</td></tr>
                <tr><td>mtk-embedding</td><td>8208</td><td>8208</td></tr>
                <tr><td>mtk-rerank</td><td>8209</td><td>8209</td></tr>
              </tbody>
            </table>
          </div>
          <p class="note">{{ $t('本地推理栈为可选项：关闭后用户端会回退到 .env 中配置的云端大模型（LLM_BASE_URL / LLM_API_KEY）。') }}</p>
        </section>

        <!-- 源码部署 -->
        <section id="source-deploy" class="doc-section">
          <h2>{{ $t('源码部署') }}</h2>
          <p class="lead">{{ $t('适用于二次开发或无法使用 Docker 的场景。需自行准备 PostgreSQL 15+（含 pgvector）与 Redis。') }}</p>

          <h4>{{ $t('1. 准备运行环境') }}</h4>
          <pre><code># {{ $t('安装 Go 1.25.0') }}
wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# {{ $t('安装 Node.js 18+') }}
# {{ $t('安装 PostgreSQL 15 + pgvector 扩展') }}
# {{ $t('安装 Redis 7') }}</code></pre>

          <h4>{{ $t('2. 构建并启动后端（user-server）') }}</h4>
          <pre><code>cd hivemtk/user-server
go mod download
go build -o user-server ./cmd/api/main.go
./user-server   # {{ $t('读取同目录 config.yaml / 环境变量') }}</code></pre>

          <h4>{{ $t('3. 构建前端（user-web）') }}</h4>
          <pre><code>cd hivemtk/user-web
npm install &amp;&amp; npm run build      # {{ $t('产物在 dist/，由 user-server 托管') }}</code></pre>
          <p class="note">{{ $t('提示：日常部署更推荐直接使用仓库根目录的 Makefile（make install / make up），它会自动完成上述构建与编排。') }}</p>
        </section>

        <!-- FRP 私域穿透 -->
        <section id="frp-deploy" class="doc-section">
          <h2>{{ $t('FRP 私域穿透') }}</h2>
          <p class="lead">
            {{ $t('HiveMTK 采用私域独立部署：数据库、推理栈、用户数据全部本地化。当客服官网部署在公网，而 HiveMTK 部署在内网（无公网 IP / NAT / 防火墙后）时，需通过 FRP 让公网请求穿透到内网') }} <code>user-server:8204</code>{{ $t('。') }}
          </p>

          <h4>{{ $t('方案选型') }}</h4>
          <p>{{ $t('推荐方案 B（反向代理 终止 TLS + frpc=http），与已有宝塔/反向代理 环境兼容性最好。') }}</p>

          <h4>{{ $t('1. 云端 frps 配置') }}</h4>
          <pre><code># /etc/frp/frps.toml
bindAddr = "0.0.0.0"
bindPort = 7000
auth.method = "token"
auth.token = "CHANGE_ME_RANDOM_64_CHARS"   # openssl rand -hex 32
webServer.addr = "0.0.0.0"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "CHANGE_ME_DASHBOARD_PASS"
transport.tls.force = true</code></pre>

          <h4>{{ $t('2. 云端反代 TLS 终止（方案 B）') }}</h4>
          <pre><code># nginx / 宝塔反代 server 块片段
server {
  listen 443 ssl http2;
  server_name chat.example.com;
  # SSL 证书配置...
  location / {
    proxy_pass http://127.0.0.1:7080;   # frps vhost 端口
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_read_timeout 300s;           # 必须大于 frpc heartbeatTimeout
  }
}</code></pre>

          <h4>{{ $t('3. 本地 frpc 配置') }}</h4>
          <pre><code># /etc/frp/frpc.toml
serverAddr = "frp.example.com"
serverPort = 7000
auth.method = "token"
auth.token = "CHANGE_ME_RANDOM_64_CHARS"   # {{ $t('与 frps 一致') }}
transport.tls.enable = true

[[proxies]]
name = "mtk-user-chat"
type = "http"
localIP = "127.0.0.1"
localPort = 8204
customDomains = ["chat.example.com"]
transport.useCompression = true
transport.heartbeatInterval = 30
transport.heartbeatTimeout = 90</code></pre>

          <h4>{{ $t('4. 验证') }}</h4>
          <pre><code># {{ $t('健康检查') }}
curl -I https://chat.example.com/health        # 200 OK

# {{ $t('浮标脚本') }}
curl -I https://chat.example.com/embed/marketing-chat-widget.iife.js

# WebSocket
wscat -c "wss://chat.example.com/api/ws/visitor?session_id=test&amp;visitor_id=test&amp;channel_id=default"</code></pre>

          <h4>{{ $t('WebSocket 穿透关键参数') }}</h4>
          <p class="note">{{ $t('WebSocket 长连接通过 FRP 时容易断，以下参数必加：') }}<code>transport.heartbeatInterval = 30</code>{{ $t('、') }}<code>transport.heartbeatTimeout = 90</code>{{ $t('；反向代理 端') }} <code>proxy_read_timeout 300s</code> {{ $t('必须大于 heartbeatTimeout。') }}</p>

          <h4>{{ $t('与 docker-compose 集成（可选）') }}</h4>
          <pre><code># docker-compose.yml{{ $t('追加') }}
services:
  frpc:
    image: snowdreamtech/frpc:0.70.0
    container_name: mtk-frpc
    restart: unless-stopped
    volumes:
      - ./frp/frpc.toml:/etc/frp/frpc.toml:ro
    networks:
      - mtk-user-network
    depends_on:
      - user-server</code></pre>

          <p class="note">{{ $t('完整 FRP 部署指南（含方案 A/C、TLS 证书、健康检查、安全要点、回滚预案）见仓库') }} <code>hivemtk/docs/architecture/FRP私域部署指南.md</code>{{ $t('。') }}</p>
        </section>

        <!-- 配置说明 -->
        <section id="config" class="doc-section">
          <h2>{{ $t('配置说明') }}</h2>

          <h4>{{ $t('用户端配置（.env）') }}</h4>
          <p>{{ $t('Docker 部署时，用户端通过仓库根目录的 .env 配置（由 make install 从 .env-example 复制生成）。常用字段：') }}</p>
          <pre><code># {{ $t('数据库与缓存') }}
POSTGRES_USER / POSTGRES_PASSWORD / USER_DB_NAME
USER_POSTGRES_HOST_PORT=8202
REDIS_HOST_PORT=8203
REDIS_PASSWORD=

# {{ $t('服务端口') }}
PORT=8204
USER_SERVER_PORT=8204

# {{ $t('安全') }}
JWT_SECRET / JWT_EXPIRE=24h
PLATFORM_ADMIN_PASSWORD

# {{ $t('平台端（可选，默认关闭）') }}
PLATFORM_ENABLED=false
PLATFORM_API_HOST=http://127.0.0.1:8205

# {{ $t('AI 模型') }}
LLM_BASE_URL / LLM_API_KEY / LLM_MODEL
EMBEDDING_BASE_URL / EMBEDDING_MODEL / EMBEDDING_DIM
RERANK_BASE_URL / RERANK_ENABLED</code></pre>

          <h4>{{ $t('环境变量优先级') }}</h4>
          <p>{{ $t('Docker 部署时，') }}<code>docker-compose.yml</code> {{ $t('中的') }} <code>environment</code> {{ $t('字段优先于') }} <code>config.yaml</code>{{ $t('，便于在不同环境间切换。') }}</p>
        </section>

        <!-- 功能模块 -->
        <section id="modules" class="doc-section">
          <h2>{{ $t('功能模块') }}</h2>
          <p class="lead">{{ $t('HiveMTK 共含 94 个业务模块 + 42 个智能体工具，按业务域划分为以下核心模块，每个模块独立路由，按需启用。') }}</p>

          <div class="module-grid">
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"/><polyline points="22,6 12,13 2,6"/></svg></span>{{ $t('邮件营销') }}</h5>
              <p>{{ $t('SMTP 配置、邮件列表、草稿管理、批量发送任务、发送记录追踪。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="2" width="14" height="20" rx="2" ry="2"/><line x1="12" y1="18" x2="12.01" y2="18"/></svg></span>{{ $t('短信营销') }}</h5>
              <p>{{ $t('阿里云 / 腾讯云 / 华为云短信网关，模板管理、批量任务、发送统计。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="1" y="4" width="22" height="16" rx="2" ry="2"/><line x1="1" y1="10" x2="23" y2="10"/></svg></span>{{ $t('多平台卡片') }}</h5>
              <p>{{ $t('抖音 / 快手 / 小红书 / 闲鱼 / TikTok 卡片生成、短链、活动追踪。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg></span>{{ $t('社群管理') }}</h5>
              <p>{{ $t('WhatsApp、Telegram 付费群、企业微信、社群成员管理。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg></span>{{ $t('短链活码') }}</h5>
              <p>{{ $t('短链生成与统计、活码管理、域名池轮询。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg></span>{{ $t('线索与客户') }}</h5>
              <p>{{ $t('线索导入、客户 360 画像、客服会话、客户事件追踪。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="20" x2="12" y2="10"/><line x1="18" y1="20" x2="18" y2="4"/><line x1="6" y1="20" x2="6" y2="16"/></svg></span>{{ $t('数据分析') }}</h5>
              <p>{{ $t('A/B 实验、流失预警、自定义报表、数据大屏、用户分层。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z"/></svg></span>{{ $t('内容创作') }}</h5>
              <p>{{ $t('AI 内容生成、话术库、模板市场、素材管理。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg></span>{{ $t('系统管理') }}</h5>
              <p>{{ $t('系统配置、运维监控、OBS 配置、备份恢复、角色权限。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg></span>{{ $t('团队协作') }}</h5>
              <p>{{ $t('团队成员、角色权限、营销流程、批量操作、操作日志。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg></span>{{ $t('AI Agent 智能体') }}</h5>
              <p>{{ $t('ReAct 自主智能体（42 工具）、意图识别、SOP 编排、异议处理。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg></span>{{ $t('LLM 路由网关') }}</h5>
              <p>{{ $t('多模型调度（本地/云端）、降级容灾、token 计量、成本归集、审计日志。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg></span>{{ $t('客服会话') }}</h5>
              <p>{{ $t('统一消息中台、17 渠道聚合、WebSocket 实时推送、离线补发、ack 机制。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg></span>{{ $t('坐席看板') }}</h5>
              <p>{{ $t('Vue 3 三栏工作台、实时聊天、用户拉黑（TTL+软删除）、AI/人工切换。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M9 11H3v10h6V11zM21 3h-6v18h6V3z"/><path d="M15 7H9"/></svg></span>{{ $t('销冠 SOP') }}</h5>
              <p>{{ $t('销冠话术 SOP 编排、智能体执行、关键节点人工确认、效果复盘。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg></span>{{ $t('客户 CDP') }}</h5>
              <p>{{ $t('360 画像、OneID 统一身份、RFM 分层、客户事件、旅程地图、意向打分。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/></svg></span>{{ $t('标签分层') }}</h5>
              <p>{{ $t('用户分群、RFM 自动计算、标签市场、分层触达、群体画像。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg></span>{{ $t('触达运营') }}</h5>
              <p>{{ $t('全渠道自动化触达、活动编排、A/B 实验、转化漏斗、合规频控。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg></span>{{ $t('RAG 知识库') }}</h5>
              <p>{{ $t('本地 pgvector 向量检索、文档解析清洗、版本管理、检索效果反馈。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="1" x2="12" y2="23"/><path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg></span>{{ $t('模型计量') }}</h5>
              <p>{{ $t('token 估算（请求/响应侧）、云端 actual 优先、成本归集、降级率统计。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg></span>{{ $t('资产包市场') }}</h5>
              <p>{{ $t('素材库、话术模板、营销卡片、行业方案包、一键导入。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg></span>{{ $t('用户黑名单') }}</h5>
              <p>{{ $t('user_id 维度拉黑、TTL 过期、软删除、风控屏蔽。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg></span>{{ $t('心跳与安装') }}</h5>
              <p>{{ $t('安装信息与活跃度指标只在本地记录；仅当自建并开启平台端时才向它上报，默认配置下不发一个出站请求。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="20" rx="2"/><path d="M7 7h10v10H7z"/></svg></span>{{ $t('嵌入式聊天窗') }}</h5>
              <p>{{ $t('浮标 SDK、iframe 嵌入、postMessage 跨域、访客会话、多租户。') }}</p>
            </div>
            <div class="module-card">
              <h5><span class="mod-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg></span>{{ $t('数据驾驶舱') }}</h5>
              <p>{{ $t('实时大屏、转化漏斗、渠道 ROI、坐席绩效、AI 承接率、SSE 实时推送。') }}</p>
            </div>
          </div>
        </section>

        <!-- 自动回复 -->
        <section id="auto-reply" class="doc-section">
          <h2>{{ $t('自动回复配置') }}</h2>
          <p class="lead">{{ $t('社媒五端（抖音/快手/小红书/闲鱼/TikTok）经 Chrome 扩展桥接实现私信自动回复——使用你自己的登录态浏览器收发，无需启动 Chrome Headless（无头模式已废弃）。') }}</p>

          <h4>{{ $t('1. 安装桥接扩展') }}</h4>
          <p>{{ $t('在常用浏览器中安装 HiveMTK Bridge 扩展，并用已绑定的平台账号登录对应站点。扩展会与 user-server 的 Bridge 通道建立长连接。') }}</p>

          <h4>{{ $t('2. 配置自动回复规则') }}</h4>
          <p>{{ $t('进入「抖音卡片 → 自动回复」（或对应平台），配置：') }}</p>
          <ul class="doc-list">
            <li>{{ $t('触发关键词与匹配模式（精确 / 模糊 / 正则）') }}</li>
            <li>{{ $t('回复内容（文本 / 图片 / 卡片链接）') }}</li>
            <li>{{ $t('工作时间（非工作时间不触发）') }}</li>
            <li>{{ $t('冷却时间（避免频繁回复被风控）') }}</li>
          </ul>

          <h4>{{ $t('3. 启动自动回复') }}</h4>
          <p>{{ $t('保持扩展所在浏览器在线并登录对应平台，新私信会自动进入统一收件箱，AI 生成的回复经扩展在真实会话中发出。可在「日志」页查看触发记录。') }}</p>

          <p class="note warning"><span class="note-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg></span>{{ $t('自动回复依赖桥接扩展在线：浏览器关闭、平台登录态失效、扩展断连都会导致该账号自动回复暂停，建议为关键账号配置监控告警。') }}</p>
        </section>

        <!-- RAG 知识库 -->
        <section id="rag" class="doc-section">
          <h2>{{ $t('RAG 知识库') }}</h2>
          <p class="lead">{{ $t('基于 pgvector 实现私有知识库语义检索，配合自动回复实现智能客服。') }}</p>

          <h4>{{ $t('1. 配置 RAG 产品') }}</h4>
          <p>{{ $t('进入「RAG 配置 → 产品管理」创建产品，每个产品对应一个独立的知识库。') }}</p>

          <h4>{{ $t('2. 导入知识文档') }}</h4>
          <p>{{ $t('支持 PDF、Word、TXT、Markdown 格式。系统会自动：') }}</p>
          <ul class="doc-list">
            <li>{{ $t('文档解析与清洗') }}</li>
            <li>{{ $t('分块（Chunk）切分') }}</li>
            <li>{{ $t('向量化（Embedding）') }}</li>
            <li>{{ $t('存入 pgvector 索引') }}</li>
          </ul>

          <h4>{{ $t('3. 配置自动回复集成') }}</h4>
          <p>{{ $t('在自动回复规则中选择「RAG 检索」作为回复策略，系统会按三层架构决策：') }}</p>
          <ol class="doc-list">
            <li>{{ $t('规则匹配：高优先级，关键词命中直接返回预设回复') }}</li>
            <li>{{ $t('语义检索：规则未命中时，用 pgvector 检索最相似文档片段') }}</li>
            <li>{{ $t('LLM 生成：将检索结果作为上下文，调用大模型生成自然语言回复') }}</li>
          </ol>

          <h4>{{ $t('4. 知识库维护') }}</h4>
          <p>{{ $t('支持文档版本管理、单条删除、重新向量化、检索效果反馈。') }}</p>
        </section>

        <!-- 故障排查 -->
        <section id="troubleshoot" class="doc-section">
          <h2>{{ $t('故障排查') }}</h2>

          <h4>{{ $t('服务无法启动') }}</h4>
          <pre><code># {{ $t('查看具体错误') }}
docker compose logs mtk-user-server | tail -50

# {{ $t('常见原因') }}
# 1. {{ $t('数据库未就绪：等待 mtk-postgres healthy 后再启动') }}
docker compose up -d mtk-postgres
docker compose up -d mtk-user-server

# 2. {{ $t('端口冲突：修改 .env 中端口映射') }}
# 3. {{ $t('配置文件错误：检查 .env 字段名与必填项') }}</code></pre>

          <h4>{{ $t('数据库连接失败') }}</h4>
          <pre><code># {{ $t('检查 PostgreSQL 状态') }}
docker compose exec mtk-postgres pg_isready -U admin -p 8202

# {{ $t('检查网络') }}
docker compose exec mtk-user-server sh -c 'nc -zv mtk-postgres 8202'

# {{ $t('重置数据库密码') }}
docker compose exec mtk-postgres psql -U admin -c "ALTER USER admin PASSWORD 'new_password';"</code></pre>

          <h4>{{ $t('Chrome 自动回复失效') }}</h4>
          <pre><code># {{ $t('检查 Chrome 进程') }}
docker compose exec user-server ps aux | grep chromium

# {{ $t('重启 Chrome') }}
docker compose restart user-server

# {{ $t('清理浏览器数据（Cookie / Cache）') }}
docker compose exec user-server sh -c 'rm -rf /data/chromium/*'
docker compose restart user-server

# {{ $t('检查账号登录状态') }}
curl http://localhost:9222/json/version</code></pre>

          <h4>{{ $t('前端访问白屏') }}</h4>
          <pre><code># {{ $t('前端由 user-server 托管，检查其日志') }}
docker compose logs mtk-user-server | tail -30

# {{ $t('确认前端构建产物已挂载') }}
docker compose exec mtk-user-server ls /app/user-web-dist

# {{ $t('清理浏览器缓存后重试') }}</code></pre>
        </section>

        <!-- FAQ -->
        <section id="faq" class="doc-section">
          <h2>{{ $t('常见问题') }}</h2>

          <div class="faq-item">
            <h4>{{ $t('Q: 数据保存在哪里？') }}</h4>
            <p>{{ $t('A: 数据完全保存在你自己的服务器（私有化部署），不依赖任何外部授权服务。本软件完全开源，可自由使用、部署与二次开发。') }}</p>
          </div>

          <div class="faq-item">
            <h4>{{ $t('Q: 是否支持公有云 SaaS 模式？') }}</h4>
            <p>{{ $t('A: 当前版本仅支持私有化部署。SaaS 模式在路线图中，预计后续版本提供。') }}</p>
          </div>

          <div class="faq-item">
            <h4>{{ $t('Q: 自动回复会被平台封号吗？') }}</h4>
            <p>{{ $t('A: 社媒五端通过你自己的登录态浏览器 + Chrome 扩展桥接收发（非无头浏览器、非第三方协议），风险已显著低于传统自动化。仍建议：1）使用小号测试；2）设置合理冷却时间（建议 ≥30 秒）；3）工作时间触发；4）回复内容贴近真人语气；5）避免短时间内大量群发。') }}</p>
          </div>

          <div class="faq-item">
            <h4>{{ $t('Q: 数据库选择') }}</h4>
            <p>{{ $t('A: 项目已统一使用 PostgreSQL 15+ 并依赖 pgvector 扩展（RAG 向量检索），这是唯一支持的数据库。') }}</p>
          </div>

          <div class="faq-item">
            <h4>{{ $t('Q: 如何备份数据？') }}</h4>
            <p>{{ $t('A: 使用用户端自带的运维命令（Makefile）备份 PostgreSQL（库名 user_db）：') }}</p>
            <pre><code># 备份（转储到 backup_*.sql）
make backup

# 恢复
make restore FILE=backup_20260101_120000.sql</code></pre>
            <p>{{ $t('也可直接备份') }} <code>./pg_data</code> {{ $t('数据目录（Docker 绑定挂载）。') }}</p>
          </div>

          <div class="faq-item">
            <h4>{{ $t('Q: 如何升级到新版本？') }}</h4>
            <p>{{ $t('A: HiveMTK 为开源项目，直接拉取最新源码并重启即可：') }}</p>
            <pre><code>git pull
make restart        # 必要时先 make build 重新构建
# 数据库迁移（如有）会随启动自动执行</code></pre>
            <p>{{ $t('版本变更说明请查看官网底部「开源项目」列的「更新日志」入口。') }}</p>
          </div>

          <div class="faq-item">
            <h4>{{ $t('Q: 需要帮助怎么办？') }}</h4>
            <p>{{ $t('A: 在官网底部查看作者联系方式（微信），或提交 Issue 到代码仓库。') }}</p>
          </div>
        </section>

        <div class="docs-footer">
          <p>{{ $t('本文档持续更新，如有疑问请在代码仓库提交 Issue，或通过官网底部联系方式反馈。') }}</p>
          <div class="docs-footer-actions">
            <router-link to="/deploy" class="btn-link">{{ $t('查看部署指南') }}</router-link>
          </div>
        </div>
      </main>
    </div>
  </div>
</template>

<style scoped>
.page {
  padding-top: 80px;
  background: var(--bg-base);
}

.docs-container {
  display: grid;
  grid-template-columns: 240px 1fr;
  gap: 48px;
  padding-top: 32px;
  padding-bottom: 100px;
}

/* 侧边栏 */
.docs-sidebar {
  position: sticky;
  top: 100px;
  align-self: start;
  max-height: calc(100vh - 120px);
  overflow-y: auto;
}

.sidebar-inner {
  border-left: 1px solid var(--border);
  padding-left: 20px;
}

.sidebar-group {
  margin-bottom: 24px;
}

.sidebar-group-title {
  font-size: 0.78rem;
  color: var(--text-dim);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 10px;
  font-weight: 600;
}

.sidebar-group ul {
  list-style: none;
  padding: 0;
  margin: 0;
}

.sidebar-group li a {
  display: block;
  padding: 7px 12px;
  color: var(--text-muted);
  font-size: 0.88rem;
  border-radius: 6px;
  transition: background 0.15s ease, color 0.15s ease;
}

.sidebar-group li a:hover {
  color: var(--text);
  background: var(--bg-subtle);
}

.sidebar-group li a.active {
  color: var(--primary);
  background: var(--primary-soft);
  font-weight: 600;
  border-left: 2px solid var(--primary);
}

/* 内容区 */
.docs-content {
  min-width: 0;
}

.docs-title {
  font-size: 2.2rem;
  margin-bottom: 6px;
}

.docs-update {
  color: var(--text-dim);
  font-size: 0.85rem;
  margin-bottom: 40px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--border);
}

.doc-section {
  margin-bottom: 56px;
  scroll-margin-top: 100px;
}

.doc-section h2 {
  font-size: 1.6rem;
  margin-bottom: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.doc-section h4 {
  font-size: 1.05rem;
  margin: 24px 0 12px;
  color: var(--text);
}

.doc-section p {
  color: var(--text-muted);
  margin-bottom: 12px;
  line-height: 1.8;
}

.doc-section p.lead {
  font-size: 1.05rem;
  color: var(--text);
}

.doc-section ul.doc-list,
.doc-section ol.doc-list {
  padding-left: 22px;
  margin-bottom: 16px;
  color: var(--text-muted);
}

.doc-section ul.doc-list li,
.doc-section ol.doc-list li {
  margin-bottom: 6px;
  line-height: 1.7;
}

.doc-section code {
  background: var(--primary-soft);
  color: var(--primary-deep);
  padding: 2px 7px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 0.86em;
  border: 1px solid var(--primary-border);
}

.doc-section pre {
  background: var(--bg-ink);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-md);
  padding: 18px 20px;
  overflow-x: auto;
  margin: 12px 0 18px;
  box-shadow: var(--shadow-md);
}

.doc-section pre code {
  background: transparent;
  color: var(--text-inv);
  padding: 0;
  font-family: var(--font-mono);
  font-size: 0.86rem;
  line-height: 1.75;
}

.inline-link {
  color: var(--accent);
  border-bottom: 1px dashed var(--accent);
}

.note {
  background: var(--accent-soft);
  border-left: 3px solid var(--accent);
  padding: 12px 16px;
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
  font-size: 0.88rem;
  color: var(--text);
}

.note.warning {
  background: rgba(248, 113, 113, 0.08);
  border-left-color: var(--danger);
}
.note-ico {
  display: inline-flex;
  vertical-align: -3px;
  margin-right: 7px;
  color: var(--danger);
}
.note-ico :deep(svg) { width: 16px; height: 16px; }

/* 步骤列表 */
.step-list {
  display: flex;
  flex-direction: column;
  gap: 18px;
  margin: 20px 0;
}

.step-item {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.step-num {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--primary);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--font-display);
  font-weight: 800;
  flex-shrink: 0;
  box-shadow: var(--shadow-primary);
}

.step-body h4 {
  margin: 4px 0 6px;
}

.step-body p {
  margin-bottom: 6px;
}

/* 表格 */
.table-wrap {
  overflow-x: auto;
  margin: 12px 0 18px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.88rem;
}

th, td {
  padding: 10px 14px;
  text-align: left;
  border-bottom: 1px solid var(--border);
}

th {
  background: var(--bg-subtle);
  color: var(--text);
  font-weight: 700;
  font-family: var(--font-display);
  letter-spacing: -0.01em;
}

td {
  color: var(--text-muted);
}

tr:last-child td {
  border-bottom: none;
}

/* 模块卡片 */
.module-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 14px;
  margin-top: 20px;
}

.module-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: 16px 18px;
  transition: border-color 0.2s ease, transform 0.2s ease, box-shadow 0.2s ease;
}

.module-card:hover {
  border-color: var(--primary);
  transform: translateY(-2px);
  box-shadow: var(--shadow-md);
}

.module-card h5 {
  font-size: 1rem;
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.mod-ico {
  display: inline-flex;
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  color: var(--primary);
  background: var(--primary-soft);
}
.mod-ico :deep(svg) { width: 17px; height: 17px; }

.module-card p {
  font-size: 0.84rem;
  color: var(--text-muted);
  margin: 0;
}

/* FAQ */
.faq-item {
  margin-bottom: 24px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--border);
}

.faq-item h4 {
  color: var(--text);
  margin-bottom: 8px;
}

.faq-item p {
  margin-bottom: 8px;
}

/* 文档底部 */
.docs-footer {
  margin-top: 60px;
  padding-top: 24px;
  border-top: 1px solid var(--border);
  text-align: center;
}

.docs-footer p {
  color: var(--text-muted);
  margin-bottom: 16px;
}

.docs-footer-actions {
  display: flex;
  justify-content: center;
  gap: 12px;
}

.btn-link {
  padding: 11px 22px;
  border-radius: var(--radius-md);
  background: var(--primary);
  color: #fff;
  font-weight: 600;
  font-size: 0.88rem;
  transition: transform 0.2s ease, background 0.2s ease;
  box-shadow: var(--shadow-primary);
}

.btn-link:hover {
  transform: translateY(-1px);
  background: var(--primary-deep);
}

/* 移动端目录按钮 */
.mobile-nav-toggle {
  display: none;
  margin-bottom: 20px;
  padding: 9px 16px;
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
  font-family: var(--font-body);
  font-size: 0.85rem;
  font-weight: 600;
  cursor: pointer;
}

@media (max-width: 1024px) {
  .docs-container {
    grid-template-columns: 1fr;
  }
  .docs-sidebar {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    width: 280px;
    background: var(--bg-surface);
    z-index: 1100;
    padding: 80px 20px 20px;
    transform: translateX(-100%);
    transition: transform 0.25s ease;
    max-height: none;
  }
  .docs-sidebar.sidebar-open {
    transform: translateX(0);
    box-shadow: 0 0 40px rgba(0, 0, 0, 0.5);
  }
  .sidebar-inner {
    border-left: none;
    padding-left: 0;
  }
  .mobile-nav-toggle {
    display: inline-block;
  }
}
</style>
