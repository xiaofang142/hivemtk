import { computed } from 'vue'
import { contact as staticContact } from '../config/content.js'

// 纯静态：站点发布到 GitHub Pages 后没有任何后端可查，联系信息只来自 config/content.js。
// 历史版本会再调平台端 /public/site/contact 覆盖这里的值，并附带占位串探测；
// 平台端降级为可选本地组件后，那条链路连同 placeholder 判据一起删除。
const wechatId = computed(() => staticContact.wechatId || '')
const wechatQrURL = computed(() => staticContact.wechatQrPath || '')
const email = computed(() => staticContact.email || '')
const phone = computed(() => staticContact.phone || '')
const serviceHours = computed(() => staticContact.serviceHours || '')

export function useSiteContact() {
  return { wechatId, wechatQrURL, email, phone, serviceHours }
}
