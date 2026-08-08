# 与 my_transcode 的消息协议

与 [`my_transcode/docs/protocol.md`](../../my_transcode/docs/protocol.md) **字段兼容**。  
生产方从 `my_service` 迁移为本服务；Worker 无需改协议即可接入。

## Topics

| Topic | 方向 |
|-------|------|
| `media.transcode.jobs` | my_media → worker |
| `media.transcode.results` | worker → my_media |

## 约定

| 字段 | my_media 取值 |
|------|----------------|
| `biz` | `media` |
| `biz_ref` | `asset:{id}` |
| `profile` | 第一期 `h264_hls` |
| `input` | 原片 MinIO bucket/key |
| `output.prefix` | 建议 `media/hls/{asset_id}/` |

代码契约：`internal/shared/transcode`（与 Worker `internal/protocol` 对齐）。
