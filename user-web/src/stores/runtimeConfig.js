import { defineStore } from 'pinia'
import { ref } from 'vue'
import { runtimeConfig } from '@/api/runtimeConfig'
import { markUserReported } from '@/utils/errorReporting'

// useRuntimeConfig 一次性拉取运行时配置并缓存。
//
// 为什么要有这一层：前端此前把一批阈值写死在组件里（agentSocket 的重连次数、
// chatSocket 的心跳间隔、journeyTracker 的 flush 周期、MaterialLibrary 的上传上限
// 等）。它们在运维侧早有对应参数，但前端读不到，于是"运维在参数中心改了、前端
// 还是老值"。这一层把 /api/config/runtime 的值接进 Pinia，各组件用 `num()`/`dur()`
// 读，读不到就落在调用点给的兜底常量上——**兜底常量保留**，因为配置面短暂不可用时
// 站点必须还能跑，不能因为拉不到配置就白屏或把轮询打成 0。
//
// 只调用一次：`ensureLoaded()` 用 Promise 去重，重复调用返回同一个 Promise；
// 拉取失败不抛（只静默回落兜底），因为它是启动链上的一环。

const FALLBACK_DURATION_MS = 0 // 读不到时长参数时返回 0，让调用方自己决定兜底

export const useRuntimeConfigStore = defineStore('runtimeConfig', () => {
  // 后端返回的扁平映射：{ "frontend_ws.max_reconnect_attempts_default": "10", ... }
  const values = ref({})
  const loaded = ref(false)
  const loading = ref(null)
  const failed = ref(false)

  async function ensureLoaded(force = false) {
    if (loaded.value && !force) return values.value
    if (loading.value) return loading.value

    loading.value = runtimeConfig()
      .then((res) => {
        const data = res && typeof res === 'object' ? res : {}
        // 后端可能返回 null/非对象（极端情况），统一归一成 {}，否则 values.value[key] 会在读取侧抛 TypeError。
        values.value = data && typeof data === 'object' ? data : {}
        loaded.value = true
        failed.value = false
        return values.value
      })
      .catch((err) => {
        // 拉不到不阻塞启动：保留 loaded=false，下次访问还会再试一次。
        failed.value = true
        // 与 utils/request.js 同一个口径：标记为用户已感知，避免被错误上报系统
        // 当成未知异常重复上报（配置面不可达在弱网下是常态，不是 bug）。
        markUserReported(err)
        return values.value
      })
      .finally(() => {
        loading.value = null
      })
    return loading.value
  }

  // str 读字符串原样（后端只存字符串）。
  function str(group, key, fallback) {
    const v = values.value[`${group}.${key}`]
    return v === undefined || v === null || v === '' ? fallback : v
  }

  // num 读整数；NaN/非数字/非正数一律回落 fallback，且**截断前先判下限**。
  // 非正数回落与后端 provider 口径一致：0 在阈值语义下几乎总是写坏了
  // （重连 0 次 = 断线即弃，flush 0 毫秒 = 每个事件都刷）。
  // 判下限必须在 Math.trunc **之前**：先截断再判会让 0.4 变成 0 后仍然通过
  // `>0` 检查之外的分支——0.4 秒的心跳间隔截成 0 就是"每条消息都发一次 ping"，
  // 而那时 fallback 明明就在手边。（本批实测到：先 trunc 后判的写法放过了 0.4。）
  function num(group, key, fallback) {
    const raw = values.value[`${group}.${key}`]
    if (raw === undefined || raw === null || raw === '') return fallback
    const n = Number(raw)
    if (!Number.isFinite(n) || n < 1) return fallback
    return Math.trunc(n)
  }

  // dur 读毫秒时长。后端 duration 参数存的是**秒**（parseDurationSeconds 的双口径），
  // 这里统一乘 1000 转成前端 setTimeout/setInterval 用的毫秒。
  function durSeconds(group, key, fallbackMs) {
    const secs = num(group, key, 0)
    return secs > 0 ? secs * 1000 : fallbackMs
  }

  // bool 的回落口径与 num/dur 不同：它必须在「没配过」时才回落 fallback，
  // 而不能把"配了 false"也当没配。第一版写成"非 true 一律回落"，
  // 结果库里明写 false 的开关在 fallback=true 的调用点显示成开——
  // 运维在控制台关掉的开关，前端照样开着，且没有任何报错。
  function bool(group, key, fallback) {
    const raw = values.value[`${group}.${key}`]
    if (raw === undefined || raw === null || raw === '') return fallback
    if (raw === 'true' || raw === '1') return true
    if (raw === 'false' || raw === '0') return false
    // 认不出的值（大小写变体、yes/no、脏数据）回落 fallback 并让调用方可见：
    // 静默当成 false 会让"配错一个值"表现为"功能被关掉"，比回落更难查。
    return fallback
  }

  function reset() {
    values.value = {}
    loaded.value = false
    loading.value = null
    failed.value = false
  }

  return {
    values,
    loaded,
    failed,
    ensureLoaded,
    str,
    num,
    durSeconds,
    bool,
    reset,
    FALLBACK_DURATION_MS,
  }
})
