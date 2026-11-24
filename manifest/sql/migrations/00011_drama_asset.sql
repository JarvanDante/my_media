-- 短剧 kind=5：整部 parent_id=0，剧集 parent_id=整部主键且 seq 从 1 起唯一。
-- +goose Up
ALTER TABLE media_asset
    ADD COLUMN IF NOT EXISTS parent_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS seq int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS update_status smallint NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_media_asset_parent ON media_asset (parent_id, seq) WHERE parent_id > 0;
CREATE UNIQUE INDEX IF NOT EXISTS uk_media_asset_parent_seq ON media_asset (parent_id, seq) WHERE parent_id > 0;
CREATE INDEX IF NOT EXISTS idx_media_asset_kind_parent ON media_asset (kind, parent_id, created_at DESC);
COMMENT ON COLUMN media_asset.kind IS '0视频 1漫画 2动漫 3抖音 4小说 5短剧';
COMMENT ON COLUMN media_asset.parent_id IS '短剧剧集指向整部 id；0=整部或其他类型';
COMMENT ON COLUMN media_asset.seq IS '短剧集序号，从 1 起；整部或其他类型为 0';
COMMENT ON COLUMN media_asset.update_status IS '1连载 2完结；仅短剧整部使用，其它类型为 0';

-- +goose Down
DROP INDEX IF EXISTS uk_media_asset_parent_seq;
DROP INDEX IF EXISTS idx_media_asset_parent;
DROP INDEX IF EXISTS idx_media_asset_kind_parent;
ALTER TABLE media_asset
    DROP COLUMN IF EXISTS parent_id,
    DROP COLUMN IF EXISTS seq,
    DROP COLUMN IF EXISTS update_status;
COMMENT ON COLUMN media_asset.kind IS '0视频 1漫画 2动漫 3抖音 4小说';
