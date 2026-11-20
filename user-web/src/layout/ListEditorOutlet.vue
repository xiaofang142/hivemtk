<template>
  <router-view
    v-slot="{ Component }"
    :name="embedded ? 'list' : 'default'"
  >
    <transition
      name="fade"
      mode="out-in"
    >
      <component
        :is="Component"
        :key="mainKey"
      />
    </transition>
  </router-view>

  <el-drawer
    :model-value="embedded"
    :title="title"
    :before-close="closeDrawer"
    :close-on-click-modal="false"
    size="min(100vw, 1080px)"
    append-to-body
    destroy-on-close
  >
    <router-view
      v-if="embedded"
      v-slot="{ Component }"
    >
      <component
        :is="Component"
        :key="route.fullPath"
      />
    </router-view>
  </el-drawer>
</template>

<script setup>
import { computed, provide, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import i18n from '@/i18n'
import { usePermissionStore } from '@/stores/permission'
import { getMenuActionKey, getNavigationPath } from '@/utils/menuNavigation'
import { listEditorContext } from '@/composables/useListEditor'

const route = useRoute()
const router = useRouter()
const permission = usePermissionStore()
const owner = computed(() => route.meta.listEditor)
const embedded = computed(() => !!owner.value && permission.hasGrantedMenuPath(owner.value))
const listEntry = shallowRef(null)

watch(() => route.fullPath, (_path, previousPath) => {
  if (!owner.value) {
    listEntry.value = null
    return
  }
  const previous = previousPath && router.resolve(previousPath)
  if (previous && (previous.meta.listEditorBase || getNavigationPath(previous)) === owner.value) {
    listEntry.value = { owner: owner.value, fullPath: previous.fullPath }
  } else if (listEntry.value?.owner !== owner.value) {
    listEntry.value = null
  }
}, { flush: 'sync' })

const listUrl = computed(() => listEntry.value?.fullPath || owner.value)
// The owner and action records refer to the same list component. Retain its vnode key during editing.
const mainKey = computed(() => embedded.value ? listUrl.value : route.fullPath)
const title = computed(() => {
  if (!owner.value) return ''
  const actionKey = getMenuActionKey(route)
  return i18n.global.t(actionKey ? `menu.${actionKey}` : route.meta.breadcrumbKey || route.meta.title)
})

const refreshers = new Map()
const close = (path) => {
  if (!embedded.value || owner.value !== path) return false
  if (listEntry.value && router.options.history.state?.back === listEntry.value.fullPath) {
    router.back()
  } else {
    router.replace(listUrl.value)
  }
  return true
}
const closeDrawer = () => close(owner.value)

provide(listEditorContext, {
  register(path, refresh) {
    refreshers.set(path, refresh)
    return () => {
      if (refreshers.get(path) === refresh) refreshers.delete(path)
    }
  },
  refresh: path => {
    if (permission.hasGrantedMenuPath(path)) refreshers.get(path)?.()
  },
  close
})
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.25s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
@media (prefers-reduced-motion: reduce) {
  .fade-enter-active,
  .fade-leave-active { transition: none; }
}
</style>
