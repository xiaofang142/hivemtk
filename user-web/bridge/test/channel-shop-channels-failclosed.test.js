import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { buildTaobaoAdapter } from '../src/channels/taobao.js';
import { buildDouyinShopAdapter } from '../src/channels/douyin_shop.js';
import { buildKuaishouShopAdapter } from '../src/channels/kuaishou_shop.js';
import { buildShopeeAdapter } from '../src/channels/shopee.js';
import { buildWalmartUsAdapter } from '../src/channels/walmart_us.js';
import * as taobaoMod from '../src/channels/taobao.js';
import * as douyinShopMod from '../src/channels/douyin_shop.js';
import * as kuaishouShopMod from '../src/channels/kuaishou_shop.js';
import * as shopeeMod from '../src/channels/shopee.js';
import * as walmartMod from '../src/channels/walmart_us.js';
import { PROTOCOL, EXPERIMENTAL_CHANNELS } from '../src/core/constants.js';

// 2026-11-18 电商店铺五渠道骨架（CH-15/18/19/28/35）——模拟信号（fake 契约）验收集。
// 口径：服务端已全链注册（channelgw registry 十五渠道 + message_hub 词表 + 桥出站
// 能力位），桥侧适配器为 fail-closed 骨架：SEL 全空=选择器零证据，match() 恒 false、
// getConversationId 恒 null、sendText/sendMedia 恒抛错。本文件钉住「fail-closed 不
// 误启/不冒充发送」这一行为契约 + 接线五件套（manifest/build/injector/词表/实验集）
// 的在场性；真机校准（授权账号现场取 DOM）后按 pdd 模式补选择器并放开 match()。

const CASES = [
  { id: 'taobao', key: 'TAOBAO', label: 'CH-15 千牛', build: buildTaobaoAdapter, hostFn: taobaoMod.isHost, content: 'content-taobao.js', entry: 'https://work.taobao.com/' },
  { id: 'douyin_shop', key: 'DOUYIN_SHOP', label: 'CH-18 抖店飞鸽', build: buildDouyinShopAdapter, hostFn: douyinShopMod.isHost, content: 'content-douyin_shop.js', entry: 'https://im.jinritemai.com/pc_seller_v2/main/workspace' },
  { id: 'kuaishou_shop', key: 'KUAISHOU_SHOP', label: 'CH-19 快语', build: buildKuaishouShopAdapter, hostFn: kuaishouShopMod.isHost, content: 'content-kuaishou_shop.js', entry: 'https://im.kwaixiaodian.com/' },
  { id: 'shopee', key: 'SHOPEE', label: 'CH-28 Shopee', build: buildShopeeAdapter, hostFn: shopeeMod.isHost, content: 'content-shopee.js', entry: 'https://seller.shopee.com.my/webchat/login' },
  { id: 'walmart_us', key: 'WALMART_US', label: 'CH-35 Walmart', build: buildWalmartUsAdapter, hostFn: walmartMod.isHost, content: 'content-walmart_us.js', entry: 'https://seller.walmart.com/' },
];

describe('电商店铺五渠道：fail-closed 行为契约', () => {
  for (const c of CASES) {
    it(`${c.id}（${c.label}）：match 恒 false、会话恒 null、发送恒拒`, async () => {
      const a = c.build();
      expect(a.name).toBe(c.id);
      expect(a.channel).toBe(c.id);

      // fail-closed：match 恒 false（授权域也不启动）。
      expect(a.hooks.match()).toBe(false);
      expect(a.hooks.matchMode()).toBe(null);

      // 选择器零证据：mergedSelectors 五组全空。
      const m = a.hooks.mergedSelectors();
      for (const k of ['itemSelectors', 'listSelectors', 'textSelectors', 'inputSelectors', 'sendSelectors']) {
        expect(m[k], `${c.id}.${k} 必须为空数组`).toEqual([]);
      }

      // 会话/消息采集面恒空——不产出任何入站。
      expect(a.hooks.getConversationId()).toBe(null);
      expect(a.hooks.getConversationList()).toEqual([]);
      expect(a.hooks.getMessageItems()).toEqual([]);
      expect(a.hooks.parseMessageItem({})).toBe(null);

      // 发送面恒拒——不冒充发送成功。
      await expect(a.hooks.sendText('hi')).rejects.toThrow(/选择器未校准（fail-closed）/);
      await expect(a.hooks.sendMedia({})).rejects.toThrow(/fail-closed/);
    });

    it(`${c.id}：词表与实验集合契约`, () => {
      expect(PROTOCOL.CHANNELS[c.key]).toBe(c.id);
      expect(EXPERIMENTAL_CHANNELS[c.id]).toBe(true);
    });

    it(`${c.id}：host 判定只认入口域`, () => {
      // jsdom 里 location.hostname 为 localhost：host 判定应 false（不误认）。
      expect(c.hostFn()).toBe(false);
    });
  }

  it('五渠道 SEL 键结构与 pdd 同名（校准时逐键填入防漂移）', () => {
    for (const c of CASES) {
      const a = c.build();
      for (const k of ['CHAT_LIST', 'CONV_ITEM', 'UNREAD', 'MSG_LIST', 'MSG_ITEM', 'TEXT', 'INPUT', 'SEND', 'PEER_NAME', 'SYSTEM', 'SELF_ITEM', 'BUYER_ITEM']) {
        expect(a.SEL, `${c.id}.SEL.${k}`).toHaveProperty(k);
      }
    }
  });
});

describe('电商店铺五渠道：接线五件套在场性（源码契约）', () => {
  const root = process.cwd();

  it('manifest.json 五渠道 content_scripts 注册', () => {
    const m = JSON.parse(readFileSync(join(root, 'manifest.json'), 'utf8'));
    const js = m.content_scripts.flatMap((cs) => cs.js);
    for (const c of CASES) {
      expect(js, `${c.content} 应在 manifest`).toContain(c.content);
    }
    // Shopee 多区 matches 抽查（BR/MY/SG/PH/ID/TH + TW/VN）。
    const shopeeMatches = m.content_scripts.find((cs) => cs.js.includes('content-shopee.js')).matches;
    for (const d of ['seller.shopee.com.my', 'seller.shopee.co.id', 'seller.shopee.sg', 'seller.shopee.com.br', 'shopee.tw']) {
      expect(shopeeMatches.some((x) => x.includes(d)), `shopee matches 缺 ${d}`).toBe(true);
    }
  });

  it('build.mjs 五渠道 entries 注册', () => {
    const s = readFileSync(join(root, 'scripts', 'build.mjs'), 'utf8');
    for (const c of CASES) {
      expect(s).toContain(`'content-${c.id}': 'src/content/${c.id}.js'`);
    }
  });

  it('injector.js SUPPORTED_HOSTS 五入口域注册', () => {
    const s = readFileSync(join(root, 'src', 'background', 'injector.js'), 'utf8');
    for (const d of ['work.taobao.com', 'im.jinritemai.com', 'im.kwaixiaodian.com', 'seller.shopee', 'seller.walmart.com']) {
      expect(s).toContain(`'${d}'`);
    }
  });
});
