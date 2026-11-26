-- 100_channel_code_unify.sql
-- 2026-11-26 渠道统一：规范渠道主数据补齐 + 历史业务表旧渠道码归一。
-- 与运行期 Go 迁移 v3.116.0（internal/migration/migrations/v3_116_0_channel_code_unify.go）同契约。
-- 幂等：可重复执行；只改旧码 -> 规范码，不删数据、不改规范码。

-- 1) 全局预设渠道补齐（TenantID=0，与 user-web/src/constants/channel.js 的
--    CHANNEL_OPTIONS 一一对应；已存在的 code 不覆盖人工改名/停用）。
INSERT INTO channels (tenant_id, name, code, type, enabled, remark)
VALUES
  (0, '企业微信',   'wecom',         'group', true, ''),
  (0, '微信公众号', 'weixin',        'dm',    true, ''),
  (0, '个人微信',   'personal_wx',   'dm',    true, ''),
  (0, 'QQ',         'qq',            'dm',    true, ''),
  (0, 'WhatsApp',   'whatsapp',      'dm',    true, ''),
  (0, 'Telegram',   'telegram',      'dm',    true, ''),
  (0, '飞书',       'feishu',        'group', true, ''),
  (0, '钉钉',       'dingtalk',      'group', true, ''),
  (0, '抖音',       'douyin',        'dm',    true, ''),
  (0, '快手',       'kuaishou',      'dm',    true, ''),
  (0, '小红书',     'xiaohongshu',   'dm',    true, ''),
  (0, '闲鱼',       'xianyu',        'dm',    true, ''),
  (0, 'TikTok',     'tiktok',        'dm',    true, ''),
  (0, '京东',       'jd',            'dm',    true, ''),
  (0, 'B站',        'bilibili',      'dm',    true, ''),
  (0, '微博',       'weibo',         'dm',    true, ''),
  (0, '拼多多',     'pdd',           'dm',    true, ''),
  (0, '淘宝/天猫',  'taobao',        'dm',    true, ''),
  (0, '抖店飞鸽',   'douyin_shop',   'dm',    true, ''),
  (0, '快手小店',   'kuaishou_shop', 'dm',    true, ''),
  (0, 'Shopee',     'shopee',        'dm',    true, ''),
  (0, 'Walmart',    'walmart_us',    'dm',    true, ''),
  (0, '短信',       'sms',           'dm',    true, ''),
  (0, '邮件',       'email',         'dm',    true, ''),
  (0, '卡片',       'card',          'dm',    true, ''),
  (0, 'Web Widget', 'web',           'dm',    true, ''),
  (0, '网页',       'web_embed',     'dm',    true, '')
ON CONFLICT (code) DO NOTHING;

-- 2) 旧渠道码 -> 规范码映射（与 constants/channel.js 的
--    CHANNEL_LEGACY_WEB_TO_CANONICAL + PLATFORM_GROUP_MEMBERS 一致）。
CREATE TEMP TABLE IF NOT EXISTS channel_code_alias_map (old_code TEXT PRIMARY KEY, new_code TEXT NOT NULL);
TRUNCATE channel_code_alias_map;
INSERT INTO channel_code_alias_map (old_code, new_code) VALUES
  ('xhs_web','xiaohongshu'), ('xhs','xiaohongshu'), ('xiaohongshu_web','xiaohongshu'),
  ('douyin_web','douyin'), ('kuaishou_web','kuaishou'), ('xianyu_web','xianyu'), ('tiktok_web','tiktok'),
  ('jd_web','jd'), ('jingdong','jd'), ('bili','bilibili'), ('bilibili_web','bilibili'),
  ('wb','weibo'), ('weibo_web','weibo'),
  ('pdd_web','pdd'), ('pinduoduo','pdd'),
  ('taobao_web','taobao'), ('tmall','taobao'), ('qianniu','taobao'),
  ('douyin_shop_web','douyin_shop'), ('jinritemai','douyin_shop'), ('feige','douyin_shop'),
  ('kuaishou_shop_web','kuaishou_shop'), ('kwaixiaodian','kuaishou_shop'), ('kuaishouxiao','kuaishou_shop'),
  ('shopee_web','shopee'), ('shopee_seller','shopee'),
  ('walmart','walmart_us'), ('walmart_web','walmart_us'),
  ('wx_personal','personal_wx'), ('wechat_personal','personal_wx'),
  ('wechat','weixin');

-- 3) 历史业务表归一（仅当表与列实际存在时执行，缺表跳过；可重复执行）。
DO $$
DECLARE
  tbl TEXT;
  col TEXT;
  pair RECORD;
  alias RECORD;
  affected INT;
BEGIN
  FOR pair IN
    SELECT * FROM (VALUES
      ('message_hub','platform'), ('customer_sessions','platform'), ('inbox_conversations','platform'),
      ('bridge_accounts','channel'), ('channel_agent_bindings','channel_type'),
      ('customers','source_channel'), ('clues','source_channel'), ('customer_channels','channel'),
      ('contact_consents','channel'), ('contact_preferences','channel'),
      ('follow_up_records','channel'), ('follow_up_reminders','channel'),
      ('customer_do_not_contact','channel'), ('customer_rfm','last_channel'),
      ('lead_mining_judgements','platform'), ('lead_mining_judgements','channel'),
      ('business_account_authorizations','channel'), ('channel_event_audit','channel'),
      ('channel_interaction','channel'), ('channel_route_rule','channel'),
      ('message_trace','channel'), ('integration_accounts','platform'),
      ('external_orders','platform'), ('msg_hourly_summary','platform'),
      ('clue_score','channel')
    ) AS t(tbl, col)
  LOOP
    IF EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema = current_schema() AND table_name = pair.tbl AND column_name = pair.col
    ) THEN
      FOR alias IN SELECT old_code, new_code FROM channel_code_alias_map LOOP
        EXECUTE format('UPDATE %I SET %I = $1 WHERE %I = $2', pair.tbl, pair.col, pair.col)
          USING alias.new_code, alias.old_code;
        GET DIAGNOSTICS affected = ROW_COUNT;
        IF affected > 0 THEN
          RAISE NOTICE '[100] %.% : % -> % (% rows)', pair.tbl, pair.col, alias.old_code, alias.new_code, affected;
        END IF;
      END LOOP;
    END IF;
  END LOOP;
END$$;

DROP TABLE IF EXISTS channel_code_alias_map;
