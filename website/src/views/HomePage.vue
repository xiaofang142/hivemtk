<script setup>
import HeroSection from '../components/HeroSection.vue'
import WhyHiveMTKSection from '../components/WhyHiveMTKSection.vue'
import FeaturesSection from '../components/FeaturesSection.vue'
import ArchitectureDiagram from '../components/ArchitectureDiagram.vue'
import ToolchainSection from '../components/ToolchainSection.vue'
import WorkflowSection from '../components/WorkflowSection.vue'
</script>

<template>
  <HeroSection />
  <WhyHiveMTKSection />
  <FeaturesSection />
  <ArchitectureDiagram />
  <ToolchainSection />
  <WorkflowSection />
</template>

<style scoped>
section {
  position: relative;
}
</style>
