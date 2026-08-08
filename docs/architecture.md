# my_media 架构

## 定位

平台级 **媒资中心**：中央媒资池 + 上传/转码编排。与 Nacos（配置通道）正交，与 `my_transcode`（转码 Worker）协作。

## 调用方

| 调用方 | 前缀 | 鉴权（目标态） |
|--------|------|----------------|
| 总后台 | `/admin` | 内部 Token / 管理端凭证（骨架：`X-Admin-Token`） |
| 子站 `my_service` | `/open` | `X-App-Key` + `X-App-Secret`（与开站签发一致） |

本服务 **不提供** Vue 后台；总后台 `views/paas/media` 调 `/admin`。

## 数据

独立库 `my_media`（PostgreSQL）：

- `media_asset` — 中央池资产
- `transcode_job` — 转码任务轨迹
- `paas_client` — 可调用开放 API 的站点凭证镜像（由总后台同步或手动录入；骨架期可先本地插入）
- `site_asset_pick` — 子站选用记录（审计；上架内容仍在子站库）

## 与 my_transcode

只通过 Kafka（协议见 `docs/transcode-protocol.md`）：

- 生产：`media.transcode.jobs`
- 消费：`media.transcode.results`
- `biz` 固定 `media`，`biz_ref` = `asset:{id}`

原片与 HLS 均在 MinIO；本服务写元数据，Worker 不碰本库。

## 分期

| 阶段 | 内容 |
|------|------|
| M0 | 健康检查、表结构、API 契约、协议包、路由占位 |
| M1（已实现） | 资产 CRUD、MinIO 预签名上传、Kafka 投递 Job、消费 Result 回写 |
| M2 | `paas_client` 与总后台凭证同步加固、选用联调 |
| M3 | 总后台页面联调、配额/审计 |

## M1 调用顺序

1. `POST /admin/assets` 创建元数据 → 得 `id`
2. `POST /admin/assets/{id}/upload-url` → 用返回的 `upload_url` **PUT** 原片
3. `POST /admin/assets/{id}/transcode` → 得 `job_id`（需 `my_transcode` 消费 jobs）
4. Worker 完成后发 Result；本服务消费后 `status=2` 且写入 `play_url`

**已验收链路说明（时序 / 验收标准 / 踩坑）→ [m1-pipeline.md](./m1-pipeline.md)**
