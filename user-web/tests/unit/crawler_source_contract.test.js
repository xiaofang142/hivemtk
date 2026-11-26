import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ElementPlus, { ElMessage } from 'element-plus'
import { settle } from './settle-helper'
import zh from '@/i18n/locales/crawlerSource.zh.json'
import en from '@/i18n/locales/crawlerSource.en.json'

const api = vi.hoisted(() => ({ getCrawlerStats: vi.fn(), runCrawler: vi.fn() }))
vi.mock('@/api/geoProbe.js', () => api)
import CrawlerStats from '@/views/geo/CrawlerStats.vue'

let wrapper
const source = { kind: 'active_ua_probe', configured_ua_count: 21, configured_engine_groups: 8, window_days: 30 }
const report = () => ({
  data_source: source,
  summary: { last_24h_probes: 7, self_site_probes: 12, competitor_probes: 8 },
  self_site_probe_share_pct: 60,
  domain_compare: [{ domain: 'site.example', is_self_site: true, is_hivemtk: false, probe_count: 12, engines: 2, probe_share_pct: 60 }],
  keyword_stats: [], domain_stats: [],
})
async function page(locale = 'zh') {
  wrapper = mount(CrawlerStats, { global: { plugins: [ElementPlus, createI18n({ legacy: false, locale, messages: { zh: { crawlerSource: zh }, en: { crawlerSource: en } } })] } })
  await settle(wrapper)
}
beforeEach(() => {
  vi.clearAllMocks()
  api.getCrawlerStats.mockResolvedValue(report())
  api.runCrawler.mockResolvedValue({ status: 'started', data_source: source })
})
afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks() })

describe('主动 UA 探测来源契约', () => {
  it('明确说明请求来自系统，成功不代表外部引擎主动抓取或收录', async () => {
    await page()
    expect(wrapper.text()).toContain('不代表外部 AI 引擎主动抓取、收录或推荐')
    expect(wrapper.text()).toContain('21 条 UA，归为 8 个 UA 分组')
    expect(wrapper.text()).toContain('本站探测成功占比')
    expect(wrapper.text()).not.toContain('声量排名')
    expect(wrapper.text()).not.toContain('HiveMTK')
    const row = wrapper.find('.el-table__body tbody tr')
    expect(row.text()).toContain('本站')
    expect(row.text()).toContain('12')
    expect(row.text()).not.toContain('竞品')
  })
  it('旧响应没有来源元信息时隐藏历史计数，不把它们归为主动或真实流量', async () => {
    api.getCrawlerStats.mockResolvedValue({ summary: { today_visits: 987654 }, domain_compare: [{ domain: 'legacy.example', is_hivemtk: true, visits: 987654, share_pct: 100 }], coverage_score: 100 })
    await page()
    expect(wrapper.text()).toContain('旧计数已隐藏')
    expect(wrapper.text()).not.toContain('987654')
    expect(wrapper.text()).not.toContain('legacy.example')
  })
  it('已声明主动探测的响应可识别旧本站字段，规范字段优先', async () => {
    const data = report()
    data.domain_compare = [{ domain: 'old-flag.example', is_hivemtk: true, probe_count: 4, engines: 1, probe_share_pct: 100 }]
    api.getCrawlerStats.mockResolvedValue(data)
    await page()
    expect(wrapper.find('.el-table__body tbody tr').text()).toContain('本站')
  })
  it('加载失败清空上次统计并显示失败，不能遗留旧成功卡片', async () => {
    await page()
    api.getCrawlerStats.mockRejectedValueOnce(new Error('unavailable'))
    await wrapper.get('[data-testid="crawler-refresh"]').trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).toContain('探测统计加载失败')
    expect(wrapper.text()).not.toContain('site.example')
    expect(wrapper.findAll('.el-statistic__number').map(n => n.text())).not.toContain('12')
  })
  it('受理提示只说明主动探测，不声称外部引擎来访或固定完成时间', async () => {
    const toast = vi.spyOn(ElMessage, 'success').mockImplementation(() => ({}))
    await page()
    await wrapper.get('[data-testid="crawler-run"]').trigger('click')
    await settle(wrapper)
    expect(api.runCrawler).toHaveBeenCalledTimes(1)
    expect(toast).toHaveBeenCalledWith(expect.stringContaining('主动 UA 探测已受理'))
    expect(toast.mock.calls[0][0]).toContain('不代表外部引擎抓取或收录')
  })
  it('英文页也保留相同证据边界', async () => {
    await page('en')
    expect(wrapper.text()).toContain('does not establish an external AI engine visit')
    expect(wrapper.text()).toContain('21 UA entries across 8 UA groups')
  })
})
