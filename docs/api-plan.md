# my_media API 计划

统一响应：GoFrame `MiddlewareHandlerResponse`（`code/message/data`）。

## 公开

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/healthz` | 探活 |

## 总后台 `/admin`（`X-Admin-Token`）

| 方法 | 路径 | 说明 | 阶段 |
|------|------|------|------|
| GET | `/admin/assets` | 媒资池列表 | ✅ |
| POST | `/admin/assets` | 创建资产元数据 | ✅ |
| GET | `/admin/assets/{id}` | 详情 | ✅ |
| POST | `/admin/assets/{id}/upload-url` | MinIO PUT 预签名 | ✅ M1 |
| POST | `/admin/assets/{id}/transcode` | 投递 Kafka Job | ✅ M1 |
| PUT | `/admin/assets/{id}` | 更新标题/封面/上下架 | 待做 |

## 子站开放 `/open`（`X-App-Key` / `X-App-Secret`）

| 方法 | 路径 | 说明 | 阶段 |
|------|------|------|------|
| GET | `/open/assets` | 可选用池（仅 status=就绪） | M2 |
| GET | `/open/assets/{id}` | 详情（含 play_url） | M2 |
| POST | `/open/assets/{id}/pick` | 选用（记 `site_asset_pick`） | M2 |

选用后：子站在本站库建立上架关系，播放走池内 `play_url`（或播放服务二次封装）。
