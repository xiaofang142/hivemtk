import { computed, inject, onScopeDispose } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { usePermissionStore } from '@/stores/permission'
import { getNavigationPath } from '@/utils/menuNavigation'

export const listEditorContext = Symbol('listEditorContext')

// A list registers its refresh operation; an editor reports a committed change, not an attempted save.
export function useListEditorRefresh(owner, refresh) {
  const context = inject(listEditorContext, null)
  if (context) onScopeDispose(context.register(owner, refresh))
}

export function useListEditorChange(owner) {
  const context = inject(listEditorContext, null)
  return () => context?.refresh(owner)
}

export function useListEditor(owner) {
  const context = inject(listEditorContext, null)
  const route = useRoute()
  const router = useRouter()
  const permission = usePermissionStore()
  const openedAt = route?.fullPath
  let left = false
  onScopeDispose(() => { left = true })
  const returnPath = computed(() => permission.getMenuReturnPath(owner, getNavigationPath(route)))
  const isEmbedded = computed(() => !!context && route?.meta?.listEditor === owner && permission.hasGrantedMenuPath(owner))

  return {
    returnPath,
    isEmbedded,
    markChanged: () => context?.refresh(owner),
    close: () => {
      if (left || (openedAt && route.fullPath !== openedAt)) return
      if (context?.close(owner)) return
      return router.push(returnPath.value)
    }
  }
}
