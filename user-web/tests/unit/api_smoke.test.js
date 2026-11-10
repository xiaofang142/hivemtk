import { describe, it, expect, vi, beforeAll } from 'vitest'
import { readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { flattenApiExports, clearMocks, getCalls, hasValidUrl, ARG_SETS } from './apitest-helper.js'

// 同时 mock 三种 API 调用约定：
//  - import request from '@/utils/request'  -> request({ url, method })
//  - import { http } / import http from     -> http.get/post/put/delete/upload
//  - import axios from 'axios'              -> chatPublic 直连
const mocks = vi.hoisted(() => {
  const ok = () => Promise.resolve({ code: 'SUCCESS', data: {} })
  const request = vi.fn(ok)
  request.get = vi.fn(ok)
  request.post = vi.fn(ok)
  request.put = vi.fn(ok)
  request.delete = vi.fn(ok)
  request.upload = vi.fn(ok)
  const http = {
    get: vi.fn(ok),
    post: vi.fn(ok),
    put: vi.fn(ok),
    delete: vi.fn(ok),
    // patch（2026-11-10）：faq.toggle / orderDraft.update / sopTemplate.toggle
    // 用的是 http.patch——mock 缺这个方法时，凡带 PATCH 的 api 模块进 smoke
    // 必红（扩充清单后实测暴露）。补齐使 mock 方法面与真实 client 对齐。
    patch: vi.fn(ok),
    upload: vi.fn(ok)
  }
  const axios = {
    get: vi.fn(async () => ({ data: {} })),
    post: vi.fn(async () => ({ data: {} })),
    put: vi.fn(async () => ({ data: {} })),
    delete: vi.fn(async () => ({ data: {} }))
  }
  return { request, http, axios }
})

vi.mock('@/utils/request', () => ({
  default: mocks.request,
  http: mocks.http,
  getRequestInstance: () => mocks.request,
  getPublicRequestInstance: () => mocks.axios
}))
vi.mock('axios', () => ({ default: mocks.axios, __esModule: true }))

// API_FILES 不再手写：2026-11-10 全模块审计实测手写清单与磁盘漂移 63 个模块
//（全部被 smoke 跳过，覆盖缺口静默长大）。改从目录直读——新增/删除 api 模块
// 自动进/出 smoke，这类缺口从此打不开。
const API_FILES = readdirSync(resolve(process.cwd(), 'src/api'))
  .filter((f) => f.endsWith('.js'))
  .map((f) => f.replace(/\.js$/, ''))
  .sort()

for (const file of API_FILES) {
  describe(`${file} API`, () => {
    let mod
    beforeAll(async () => {
      mod = await import(`@/api/${file}.js`)
    })

    it('模块可加载且导出有效', () => {
      expect(mod).toBeTruthy()
    })

    it('每个导出函数都可调用并触发 HTTP 请求', async () => {
      const entries = flattenApiExports(mod)
      expect(entries.length).toBeGreaterThan(0)
      const failures = []
      for (const [name, fn] of entries) {
        clearMocks(mocks)
        let passed = false
        let lastErr
        for (const args of ARG_SETS) {
          clearMocks(mocks)
          try {
            const _ret = fn(...args)
            // SSE 型导出（如 streamEgoEvents）返回 {done,abort}：done 内部 fetch
            // 已点火却无人 await，相对 URL 在 Node 直接 TypeError reject，记成
            // 未处理 rejection 假红（基线债：干净 HEAD 单跑同文件亦 RC=1，
            // 2026-11-10 复现证实）。smoke 只断言「可调用且触发 HTTP」，
            // 给 done 挂 noop catch 兜住，不断言流内容。
            if (_ret && typeof _ret.done?.catch === 'function') _ret.done.catch(() => {})
            await _ret
            const calls = getCalls(mocks)
            const urlOk = hasValidUrl(calls)
            if (calls.length > 0 && urlOk === false) {
              lastErr = new Error('HTTP 请求存在空的 url')
              continue // 尝试下一组参数
            }
            // 通过条件：至少一种参数组合下可正常 await（不抛错）。
            // 兼容两种情况：① 正常触发 HTTP 请求（url 有效）；② 有意的后端 stub（不发请求）。
            passed = true
            break
          } catch (e) {
            lastErr = e
          }
        }
        if (!passed) {
          failures.push(`${name}${lastErr ? ` (${lastErr.message})` : ''}`)
        }
      }
      if (failures.length) {
        throw new Error(`未通过的函数: ${failures.join(' | ')}`)
      }
    })
  })
}
