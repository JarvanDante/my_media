-- +goose Up
-- 对外暴露的 8 位短码(非自增 id)，API 的 id 字段使用此值
ALTER TABLE media_asset ADD COLUMN IF NOT EXISTS code varchar(8) NOT NULL DEFAULT '';

-- 历史数据回填：用 id 派生临时码，随后应用层只生成随机码
UPDATE media_asset
SET code = substr(md5(random()::text || id::text), 1, 8)
WHERE code = '' OR code IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uk_media_asset_code ON media_asset (code);
COMMENT ON COLUMN media_asset.code IS '对外资产ID, 8位随机串, API id 字段';

-- +goose Down
DROP INDEX IF EXISTS uk_media_asset_code;
ALTER TABLE media_asset DROP COLUMN IF EXISTS code;
