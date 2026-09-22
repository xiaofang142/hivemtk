<script setup>
import PageHeader from '../components/PageHeader.vue'
import FeaturesSection from '../components/FeaturesSection.vue'
import WhyHiveMTKSection from '../components/WhyHiveMTKSection.vue'
</script>

<template>
  <div>
    <PageHeader
      :kicker="'06 项核心能力'"
      :title="['不是又一个 SCRM，', '是真正会卖货的 AI']"
      :gradient-index="1"
      :subtitle="'HiveMTK 覆盖从七端多账号聚合、AI 谈单、销冠 SOP、客户 CDP、全渠道触达、GEO 智能优化到数据驾驶舱的完整链路，94 个业务模块 + 42 个智能体工具，让系统真正能替代销冠的重复劳动。'"
      :breadcrumbs="[{ label: '首页', href: '/' }, { label: '核心功能' }]"
    />
    <FeaturesSection hide-header />
    <WhyHiveMTKSection />
  </div>
</template>
