-- M2: 播放策略(按站点防盗链) + 播放统计日表。
-- +goose Up
CREATE TABLE IF NOT EXISTS play_policy (
    site_code         varchar(32) PRIMARY KEY,
    referer_whitelist text        NOT NULL DEFAULT '',   -- 逗号分隔的域名子串, 空=不限制
    ua_blacklist      text        NOT NULL DEFAULT '',   -- 逗号分隔的 UA 子串, 命中即拒(如 curl,wget)
    token_ttl_sec     int         NOT NULL DEFAULT 14400,
    status            smallint    NOT NULL DEFAULT 1,    -- 1=启用 0=停用(停用=不做额外限制)
    updated_at        timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE play_policy IS '播放服务·站点防盗链策略';

CREATE TABLE IF NOT EXISTS play_stat_daily (
    day        date        NOT NULL,
    site_code  varchar(32) NOT NULL,
    asset_code varchar(32) NOT NULL,
    plays      bigint      NOT NULL DEFAULT 0,   -- m3u8 拉取次数(≈播放次数)
    seg_reqs   bigint      NOT NULL DEFAULT 0,   -- 分片请求数
    PRIMARY KEY (day, site_code, asset_code)
);
COMMENT ON TABLE play_stat_daily IS '播放服务·按日播放统计';

-- +goose Down
DROP TABLE IF EXISTS play_stat_daily;
DROP TABLE IF EXISTS play_policy;
