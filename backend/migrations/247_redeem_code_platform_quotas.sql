-- 邀请码可以在注册时写入平台周限额。空值表示沿用系统默认平台限额。
ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS platform_quotas JSONB;
