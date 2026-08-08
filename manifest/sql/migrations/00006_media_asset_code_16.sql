-- +goose Up
ALTER TABLE media_asset ALTER COLUMN code TYPE varchar(16);
COMMENT ON COLUMN media_asset.code IS '对外资产ID, 16位随机串(历史数据可能为8位), API id 字段';

-- +goose Down
-- 若存在超过 8 位的 code，回滚会失败，需先处理
ALTER TABLE media_asset ALTER COLUMN code TYPE varchar(8);

