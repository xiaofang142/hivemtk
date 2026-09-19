/**
 * 第十一轮 popup XSS 守卫：源自第三方页面 DOM / 后端回传的字符串
 * （channel 键、state、错误码、失败原因、accountId、currentConvId）
 * 不得以原始形态出现在 innerHTML 模板产物中。
 */
import { describe, it, expect } from 'vitest';

import { renderHealthPanel, escapeHtml } from '../src/popup/health.js';
import { renderAccountRows } from '../src/popup/accounts.js';

const SCRIPT = '<img src=x onerror=alert(1)>';
const ATTR_BREAK = '"><svg onload=alert(2)>';

describe('popup XSS 转义守卫', () => {
  it('escapeHtml 覆盖五种危险字符', () => {
    expect(escapeHtml(`<>&"'`)).toBe('&lt;&gt;&amp;&quot;&#39;');
    expect(escapeHtml(null)).toBe('');
    expect(escapeHtml(0)).toBe('0');
  });

  it('健康面板：恶意 channel 键/state/错误码/失败原因全转义', () => {
    const html = renderHealthPanel({
      [ATTR_BREAK]: {
        state: ATTR_BREAK,
        healthy: false,
        failureCount: SCRIPT,
        recentReasons: [SCRIPT],
        errorCodeDistribution: { [SCRIPT]: 2 },
        latencyMs: { count: 1, p50: SCRIPT, p95: SCRIPT, max: SCRIPT },
        totals: { calls: 1, ok: 0, fail: 1, okRate: 0 },
        idempotency: { keysTracked: SCRIPT, dedupeHits: SCRIPT },
      },
    });
    for (const evil of [
      '<img src=x',
      '<svg onload',
      // 属性位注入必须保留引号转义形态
      '"><svg',
    ]) {
      expect(html).not.toContain(evil);
    }
    expect(html).toContain('&lt;img');
    expect(html).toContain('&quot;&gt;&lt;svg');
    // 合法渲染不破：卡片骨架与数字仍在
    expect(html).toContain('health-card');
  });

  it('账号行：accountId/currentConvId 恶意串全转义', () => {
    const states = {
      douyin: {
        [SCRIPT]: { enabled: true },
      },
    };
    const html = renderAccountRows(states);
    expect(html).not.toContain('<img src=x');
    expect(html).toContain('&lt;img');
    const html2 = renderAccountRows({ [ATTR_BREAK]: { 'x': { enabled: true } } });
    expect(html2).not.toContain('"><svg');
    expect(html2).toContain('&quot;&gt;&lt;svg');
  });
});
