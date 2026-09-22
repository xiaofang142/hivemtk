import { computed } from 'vue'
import i18n from '@/i18n'
import * as content from '@/config/content.js'

function wrap(node) {
  if (typeof node === 'string') return i18n.global.t(node)
  if (Array.isArray(node)) return node.map(wrap)
  if (node && typeof node === 'object') {
    const out = {}
    for (const k in node) out[k] = wrap(node[k])
    return out
  }
  return node
}

export function useSite() {
  const result = {}
  for (const key of Object.keys(content)) {
    result[key] = computed(() => wrap(content[key]))
  }
  return result
}

