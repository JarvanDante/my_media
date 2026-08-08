-- +goose Up
-- app_secret 存 SHA-256 hex(64)；历史明文行鉴权时兼容，Upsert 后升级为哈希
ALTER TABLE paas_client ADD COLUMN IF NOT EXISTS secret_hashed smallint NOT NULL DEFAULT 0;
COMMENT ON COLUMN paas_client.app_secret IS 'APPSECRET: secret_hashed=1 时为 sha256 hex, =0 时为历史明文';
COMMENT ON COLUMN paas_client.secret_hashed IS '1=sha256 哈希存储 0=明文(仅兼容旧数据)';

-- +goose Down
ALTER TABLE paas_client DROP COLUMN IF EXISTS secret_hashed;
