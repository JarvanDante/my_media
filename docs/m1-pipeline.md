# M1 媒资链路（已验收）

> 本地联调通过时间：2026-08-08。  
> 验收资产示例：`id=2`，`play_url` 可在浏览器直接播放 HLS。

## 一句话链路

```text
上传原片 → Kafka Job → my_transcode(ffmpeg) → HLS 回写 MinIO
       → Kafka Result → my_media 更新详情(status=2 / ready)
       → 浏览器打开 play_url 可播
```

## 参与组件

| 组件 | 角色 | 本机参考 |
|------|------|----------|
| `my_media` | 媒资 API：预签名、投递 Job、消费 Result、写库 | `:8004` |
| MinIO | 原片 + HLS 对象存储 | 宿主机 `19000→9000` |
| Kafka | `media.transcode.jobs` / `media.transcode.results` | `:9092` |
| `my_transcode` | 下载 → ffprobe 时长 → ffmpeg HLS → 上传 → 发 Result | Worker `:8088` |
| PostgreSQL `my_media` | `media_asset` / `transcode_job` | — |

## 时序

```text
运营/Apifox                 my_media              MinIO           Kafka            my_transcode
    │                          │                    │               │                    │
    │ POST /admin/assets       │                    │               │                    │
    │─────────────────────────►│ 写 media_asset     │               │                    │
    │◄─────────────────────────│ id                 │               │                    │
    │                          │                    │               │                    │
    │ POST .../upload-url      │                    │               │                    │
    │─────────────────────────►│ 绑 source_key      │               │                    │
    │◄──── upload_url ─────────│                    │               │                    │
    │                          │                    │               │                    │
    │ PUT upload_url (文件体)  │                    │               │                    │
    │──────────────────────────────────────────────►│ 存原片 mp4    │                    │
    │                          │                    │               │                    │
    │ POST .../transcode       │                    │               │                    │
    │─────────────────────────►│ Stat 原片存在      │               │                    │
    │                          │── JobMessage ─────────────────────►│                    │
    │◄──── job_id ─────────────│                    │               │── consume ────────►│
    │                          │                    │◄─ download ───┼────────────────────│
    │                          │                    │               │   ffprobe+ffmpeg   │
    │                          │                    │◄─ upload hls ─┼────────────────────│
    │                          │◄─ ResultMessage ───────────────────┼────────────────────│
    │                          │ status=2 ready     │               │                    │
    │                          │ play_url / duration│               │                    │
    │ GET /admin/assets/{id}   │                    │               │                    │
    │─────────────────────────►│                    │               │                    │
    │◄─ play_url ──────────────│                    │               │                    │
    │ 浏览器打开 play_url ─────┼───────────────────►│ index.m3u8+ts │                    │
```

## 验收标准

| 检查项 | 期望 |
|--------|------|
| `POST .../transcode` | `code=0`，返回 `job_id` |
| `my_transcode` 日志 | 消费到对应 `job_id`，ffmpeg 跑完并上传 |
| `GET /admin/assets/{id}` | `status=2`，`transcode_status=ready`，`transcode_error=""` |
| `play_url` | 形如 `http://127.0.0.1:19000/my-media/media/hls/{id}/index.m3u8` |
| 浏览器打开 `play_url` | 能播 HLS（Network 可见 `.m3u8` + `.ts`） |
| `duration_sec` | Worker 用 ffprobe 回填后应 > 0（已验收：`id=3` 为 `249`） |

已验收样例字段：

```json
{
  "id": 3,
  "status": 2,
  "transcode_status": "ready",
  "duration_sec": 249,
  "play_key": "media/hls/3/index.m3u8",
  "play_url": "http://127.0.0.1:19000/my-media/media/hls/3/index.m3u8",
  "source_key": "media/source/3/....mp4"
}
```

## 操作步骤（勿把文件打进 upload-url）

Header（除 PUT 外）：`X-Admin-Token: <config security.admin_token>`

1. `POST /admin/assets` — Body JSON：`{"title":"..."}` → 得 `id`
2. `POST /admin/assets/{id}/upload-url` — Body **仅** JSON：`{"filename":"a.mp4"}`（**不要**附带视频文件）
3. 对返回的 `upload_url` 另开请求：`PUT` + Body binary 上传 mp4（**不要**带 Admin-Token）
4. `POST /admin/assets/{id}/transcode`
5. `GET /admin/assets/{id}` 确认 `ready`，浏览器打开 `play_url`

## 配置注意

- MinIO 宿主机端口常见为 **19000**（容器内 9000）；`my_media` 与 `my_transcode` 的 `endpoint` / 账号密码必须一致。
- Kafka topic：`media.transcode.jobs`、`media.transcode.results`；`biz=media`，`biz_ref=asset:{id}`。
- 协议详情：`docs/transcode-protocol.md`；架构：`docs/architecture.md`。

## 与 Nacos / PaaS 边界

本链路是 **PaaS 能力通道**（运行时调媒资 + Worker），不是 Nacos 配置下发。  
站点基础配置仍走 Nacos；见 `my_manage_service/docs/nacos-vs-paas.md`。
