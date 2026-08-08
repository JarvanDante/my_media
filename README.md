# my_media · 媒资中心（PaaS）

GoFrame 媒资服务：统一视频上传编排、转码投递、中央媒资池；总后台维护池，子站用 `app_key` 选用后上架。

**不做**：自带运营后台 UI（用总后台 `/paas/media`）；ffmpeg 本体（交给 [`my_transcode`](../my_transcode) Worker）。

## 边界

| 做 | 不做 |
|----|------|
| 媒资资产库 / 中央池 | 站点用户业务、本站上架内容表 |
| 上传凭证 / 原片入库 | ffmpeg 进程 |
| 投递 Kafka Job → `my_transcode` | Nacos 下发支付/广告等业务配置 |
| 消费 Result 回写 `play_*` | 总后台前端 |
| 开放 API（`app_key`）供子站选用 | |

```text
总后台 /paas/media ──HTTP──► my_media ──Job──► Kafka ──► my_transcode
子站 my_service    ──HTTP──► my_media ◄─Result─┘              │
                              │  PG(my_media)  MinIO ◄────────┘
```

配置通道仍是 Nacos（站点只拿 `paas.app_key/secret` + 本服务 base URL）。详见 `my_manage_service/docs/nacos-vs-paas.md`。

## 快速开始

```bash
# 建库
createdb my_media   # 或等价 SQL

# 迁移
make migrate

cp manifest/config/config.example.yaml manifest/config/config.yaml
# 按需改 database / minio / kafka / security.admin_token

go mod tidy
make dev
# 探活: curl http://127.0.0.1:8004/healthz
# 管理端示例: curl -H 'X-Admin-Token: dev-admin-token-change-me' http://127.0.0.1:8004/admin/assets
```

## 目录

```text
api/                 接口契约(admin 总后台 / open 子站)
docs/                架构与 API 计划
internal/
  cmd/               启动装配
  dao/               仓储实现
  modules/           asset / open / health
  shared/            middleware、转码协议、errcode
manifest/
  config/            本地配置(不依赖 Nacos 启动)
  sql/migrations/    goose → my_media 库
```

## 相关仓库

| 仓库 | 角色 |
|------|------|
| `my_manage_service` + `my_manage_backend` | 控制面 / 门户 UI |
| `my_transcode` | HLS 转码 Worker |
| `my_service` | 子站数据面，选用后上架 |
