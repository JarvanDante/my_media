-- 短剧导入任务执行租约（owner + generation fencing，lease_until 为回收阈值）。
-- 00012 已推送且尚未在运行库执行，本文件只追加，不改写既有迁移历史。
-- 仅源码提交，不在此环境执行。
-- +goose Up
ALTER TABLE drama_import_job
    ADD COLUMN IF NOT EXISTS owner varchar(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS lease_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS lease_until timestamptz;
CREATE INDEX IF NOT EXISTS idx_drama_import_job_lease ON drama_import_job (status, lease_until);
COMMENT ON COLUMN drama_import_job.owner IS '当前执行者；空表示无人持有执行租约';
COMMENT ON COLUMN drama_import_job.lease_generation IS '租约代数，认领时递增，用于 fencing';
COMMENT ON COLUMN drama_import_job.lease_until IS '租约到期时间；到期后可回收。续租、关闭释放与回收使用同一阈值';

-- +goose Down
DROP INDEX IF EXISTS idx_drama_import_job_lease;
ALTER TABLE drama_import_job
    DROP COLUMN IF EXISTS lease_until,
    DROP COLUMN IF EXISTS lease_generation,
    DROP COLUMN IF EXISTS owner;
