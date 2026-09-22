<script setup>
import PageHeader from '../components/PageHeader.vue'
import ArchitectureDiagram from '../components/ArchitectureDiagram.vue'
import ToolchainSection from '../components/ToolchainSection.vue'
import WhyHiveMTKSection from '../components/WhyHiveMTKSection.vue'
</script>

<template>
  <div>
    <PageHeader
      :kicker="'42 个智能体工具'"
      :title="['ReAct 智能体编排，', '为真实业务量设计']"
      :gradient-index="1"
      :subtitle="'42 个智能体工具 + 9 步 LLM 路由 Pipeline + 三层缓存 + 全链路 TraceID + 限流重试降级审计计费。不是玩具 Demo，是扛得住生产的工程。'"
      :breadcrumbs="[{ label: '首页', href: '/' }, { label: '工程能力' }]"
    />
    <ArchitectureDiagram />
    <ToolchainSection />
    <WhyHiveMTKSection />
  </div>
</template>
