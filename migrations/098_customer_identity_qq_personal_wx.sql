-- Migration 098: 客户身份补 QQ / 个人微信(个微) 两列。
--
-- 背景：Customer 已有 13 渠道身份字段，GenerateCustomerUnifiedID 对
-- QQ / personal_wx 无对应列，回退成随机 UUID——同一 sender 两次消息生成不同
-- OneID，客户 360 出现重复档案（线索发掘 V2 D15 / 对抗审核 P1-3）。
--
-- 幂等：列不存在才 ADD COLUMN，重复执行为空操作。
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'customers' AND column_name = 'qq_open_id'
  ) THEN
    ALTER TABLE customers ADD COLUMN qq_open_id VARCHAR(64);
    CREATE INDEX idx_customers_qq_open_id ON customers(qq_open_id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'customers' AND column_name = 'personal_wx_open_id'
  ) THEN
    ALTER TABLE customers ADD COLUMN personal_wx_open_id VARCHAR(64);
    CREATE INDEX idx_customers_personal_wx_open_id ON customers(personal_wx_open_id);
  END IF;
END $$;
