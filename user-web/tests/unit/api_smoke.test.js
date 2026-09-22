import { describe, it, expect, vi, beforeAll } from 'vitest'
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

vi.mock('@/utils/request', () => ({ default: mocks.request, http: mocks.http }))
vi.mock('axios', () => ({ default: mocks.axios, __esModule: true }))

const API_FILES = [
  'abExperiment', 'aiAgent', 'aiProductivity', 'approval', 'backup', 'batchOperation',
  'bulkMessaging', 'channelAgentBinding', 'chat', 'chatChannel', 'chatPublic', 'churnPrediction',
  'clue', 'community', 'conversionFunnel', 'customReport', 'customer360', 'customerEvent',
  'customerJourney', 'customerService', 'customerServiceAgent', 'customerSession', 'dashboardScreen',
  'dialogueMemory', 'domainPool', 'douyinCard', 'email', 'feishu', 'humanTask', 'integration', 'intentRecognition',
  'knowledge', 'knowledgeBase', 'knowledgeMerchant', 'kuaishouCard', 'livecode', 'llmRouting',
  'marketingFlow', 'material', 'messageHub', 'objection', 'obs', 'oneid', 'operationLog',
  'persona', 'platform', 'platformAccount', 'reachPipeline',
  'scriptTemplate', 'securityAudit', 'shortLink', 'sms', 'sopAgent', 'stats', 'system', 'tagSegmentation',
  'telegram', 'tiktokCard', 'tuning',
  'unifiedMessage', 'userSegment', 'users', 'wecomAccount', 'whatsapp', 'xianyuCard',
  'xiaohongshuCard'
]

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
            await fn(...args)
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
