-- M3-2: 播放链接失效闸。按 站点/资产 记录 not_before(签发时间 iat < not_before 的令牌一律拒绝)。
-- +goose Up
CREATE TABLE IF NOT EXISTS play_revoke (
    site_code  varchar(32) NOT NULL,
    asset_code varchar(32) NOT NULL DEFAULT '*',   -- '*' = 整站; 否则单资产
    not_before bigint      NOT NULL DEFAULT 0,      -- 令牌签发时间(iat) < 此值即失效
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (site_code, asset_code)
);
COMMENT ON TABLE play_revoke IS '播放服务·链接失效闸(iat<not_before 即失效)';

-- +goose Down
DROP TABLE IF EXISTS play_revoke;
