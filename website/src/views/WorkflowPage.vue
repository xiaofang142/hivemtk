<script setup>
import PageHeader from '../components/PageHeader.vue'
import WorkflowSection from '../components/WorkflowSection.vue'
</script>

<template>
  <div>
    <PageHeader
      :kicker="'全链路自动化'"
      :title="['从线索接入，', '到复购激活全链路']"
      :gradient-index="1"
      :subtitle="'七端社媒消息统一接入 → AI 自动承接 → ReAct 智能体谈单 → 销冠 SOP 执行 → 客户 CDP 沉淀 → 全渠道触达复购。每一步都可观测、可审计、可优化。'"
      :breadcrumbs="[{ label: '首页', href: '/' }, { label: '业务流程' }]"
    />
    <WorkflowSection />
  </div>
</template>
