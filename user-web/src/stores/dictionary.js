import { defineStore } from 'pinia'
import { ref } from 'vue'
import { dictionary } from '@/api/runtimeConfig'

// 业务字典 store。
//
// 口径：**后端 DB 优先，DB 没有这一批就回落本地常量**。不反过来（本地优先、后端只做
// 覆盖），因为字典描述的是存量行正在用的值域——历史数据里躺着的状态码必须一直在册，
// 否则翻译存量行时会翻车。这与后端 DictionaryService.GetDictionary 的口径一致。
//
// 每个 parent 只拉一次（Promise 去重），失败时**静默回落本地常量**：字典是渲染辅助，
// 拉不到就让页面照常显示，不能因为少一份值域就白屏。需要排查时看 failed 集合。

export const useDictionaryStore = defineStore('dictionary', () => {
  // parent -> { items: [], transitions: [] }
  const loaded = ref({})
  const pending = ref({})
  const failed = ref([])

  async function load(parent, localFallback = null) {
    if (loaded.value[parent]) return loaded.value[parent]
    if (pending.value[parent]) return pending.value[parent]

    pending.value[parent] = dictionary(parent)
      .then((res) => {
        const data = res && typeof res === 'object' ? res : {}
        const entry = {
          items: Array.isArray(data.items) ? data.items : [],
          transitions: Array.isArray(data.transitions) ? data.transitions : [],
        }
        loaded.value[parent] = entry
        failed.value = failed.value.filter((p) => p !== parent)
        return entry
      })
      .catch(() => {
        // 404（表里没这一批）与网络/500 的处理完全一样：回落本地常量。
        // 不区分是因为两条路对用户的表现没差别——都是"用本地那份"；而区分了
        // 反倒要在前端复制一份"哪些 parent 算正常未落库"的清单，那份清单会漂移。
        // 一律记进 failed，需要排查时看得见；真正想区分的人看后端日志更准。
        failed.value = [...new Set([...failed.value, parent])]
        const entry = localFallback ? { items: localFallback, transitions: [] } : { items: [], transitions: [] }
        // 不写进 loaded：下次访问还会再试一次（一行缓存否掉会让「后端恢复后前端
        // 一直用本地常量」变成永久状态）。
        return entry
      })
      .finally(() => {
        delete pending.value[parent]
      })
    return pending.value[parent]
  }

  /** 取一个域的全部项（含 Enabled=false：历史行仍可能是停用项）。 */
  async function items(parent, localFallback = null) {
    const entry = await load(parent, localFallback)
    return entry?.items || []
  }

  /** 把值译成人话；译不出回落原值（比显示空白好）。 */
  async function label(parent, code, localFallback = null) {
    const list = await items(parent, localFallback)
    const hit = list.find((d) => d.code === code)
    return hit ? hit.name : code
  }

  function reset() {
    loaded.value = {}
    pending.value = {}
    failed.value = []
  }

  return { loaded, failed, load, items, label, reset }
})
