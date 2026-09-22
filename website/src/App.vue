<script setup>
import Navbar from './components/Navbar.vue'
import FooterSection from './components/FooterSection.vue'
import ToastHost from './components/ToastHost.vue'
import BackToTop from './components/BackToTop.vue'
</script>

<template>
  <a href="#main" class="skip-link">跳到主内容</a>
  <Navbar />
  <main id="main" tabindex="-1">
    <router-view />
  </main>
  <FooterSection />
  <BackToTop />
  <ToastHost />
</template>

<style scoped>
main {
  position: relative;
  min-height: 60vh;
}
</style>
