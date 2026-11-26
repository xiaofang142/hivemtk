import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import channelCatalogApi from '@/api/channelCatalog.js'
import channelAccountApi from '@/api/channelAccount.js'
import { getChannelLabel } from '@/constants/channel'

// 渠道/账号统一目录 store（027 渠道统一化）。
//
// 口径（与后端 ChannelCatalogService 一致，fail-closed）：
//   - channels / accounts 只收「已开启渠道 + 其下已启用账号」——下拉只给这些，禁止输入。
//   - allChannels 是管理面全量（含停用），仅用于翻译历史数据与管理页开关；
//     历史行里出现已停用渠道时必须仍能显示汉字，所以标签映射不能只建立在开启项上。
//   - 后端拉不到时回落本地 constants/channel.js 的码表（只做显示兜底，不冒充下拉数据源）；
//     failed 记下来便于排查，不写进 ready，后端恢复后还能再拉。
//
// 每个请求 Promise 去重：全站几十个页面共用一份目录，不能每个组件各拉一遍。

export const useChannelCatalogStore = defineStore('channelCatalog', () => {
  const channels = ref([])
  const accounts = ref([])
  const allChannels = ref([])
  const ready = ref(false)
  const failed = ref(false)
  let pending = null
  let allPending = null

  function pickList(res) {
    return Array.isArray(res) ? res : (Array.isArray(res?.list) ? res.list : [])
  }

  async function load(force = false) {
    if (!force && ready.value) return
    if (pending) return pending
    pending = channelCatalogApi.catalog()
      .then((res) => {
        channels.value = pickList(res?.channels)
        accounts.value = pickList(res?.accounts)
        ready.value = true
        failed.value = false
      })
      .catch(() => {
        failed.value = true
        // 不上报 ready：下次 ensure() 还会重试，避免一次网络抖动让全站下拉永久为空。
      })
      .finally(() => { pending = null })
    return pending
  }

  async function loadAllChannels(force = false) {
    if (!force && allChannels.value.length) return
    if (allPending) return allPending
    allPending = channelAccountApi.listChannels()
      .then((res) => { allChannels.value = pickList(res) })
      .catch(() => { /* 管理面标签回落本地码表，不阻断页面 */ })
      .finally(() => { allPending = null })
    return allPending
  }

  async function ensure(force = false) {
    await Promise.all([load(force), loadAllChannels(force)])
  }

  /** 下拉源：只允许选已开启渠道，value 用规范码（与历史业务表字符串列同口径）。 */
  const channelOptions = computed(() => channels.value.map((c) => ({
    value: c.code,
    label: c.label || c.name || c.code,
    id: c.id,
    code: c.code,
    name: c.name
  })))

  /** 下拉源：已启用账号；channel 可传渠道码或数字 ID。 */
  function accountOptions(channel) {
    const byId = typeof channel === 'number' || /^\d+$/.test(String(channel || ''))
    return accounts.value
      .filter((a) => {
        if (channel == null || channel === '') return true
        return byId ? Number(a.channel_id) === Number(channel) : a.channel_code === channel
      })
      .map((a) => ({
        value: a.id,
        label: a.label || a.name || a.identify_code || String(a.id),
        id: a.id,
        channel_id: a.channel_id,
        channel_code: a.channel_code,
        identify_code: a.identify_code
      }))
  }

  const channelByCode = computed(() => {
    const m = {}
    for (const c of allChannels.value) m[c.code] = c
    for (const c of channels.value) if (!m[c.code]) m[c.code] = c
    return m
  })
  const channelById = computed(() => {
    const m = {}
    for (const c of allChannels.value) m[c.id] = c
    for (const c of channels.value) if (!m[c.id]) m[c.id] = c
    return m
  })
  const accountById = computed(() => {
    const m = {}
    for (const a of accounts.value) m[a.id] = a
    return m
  })

  /** 历史数据/列表显示：只返回汉字，译不出回落本地码表再回落原值。 */
  function channelLabel(value) {
    if (value == null || value === '') return ''
    const s = String(value)
    if (/^\d+$/.test(s)) {
      const c = channelById.value[Number(s)]
      if (c) return c.label || c.name || c.code
      const a = accountById.value[Number(s)]
      if (a) return a.label || a.name || a.channel_code
      return s
    }
    const c = channelByCode.value[s]
    if (c) return c.label || c.name || c.code
    return getChannelLabel(s)
  }

  function accountLabel(id) {
    if (id == null || id === '') return ''
    const a = accountById.value[Number(id)]
    return a ? (a.label || a.name || a.identify_code || String(id)) : String(id)
  }

  function accountIdentifyCode(id) {
    const a = accountById.value[Number(id)]
    return a?.identify_code || ''
  }

  /** 管理页开关：写完立刻刷新两路目录，让全站下拉同步生效。 */
  async function setTenantEnabled(id, enabled) {
    await channelCatalogApi.setTenantEnabled(id, enabled)
    await Promise.all([load(true), loadAllChannels(true)])
  }

  function reset() {
    channels.value = []
    accounts.value = []
    allChannels.value = []
    ready.value = false
    failed.value = false
    pending = null
    allPending = null
  }

  return {
    channels, accounts, allChannels, ready, failed,
    channelOptions, accountOptions, channelByCode, channelById, accountById,
    ensure, load, loadAllChannels, reset,
    channelLabel, accountLabel, accountIdentifyCode, setTenantEnabled
  }
})
