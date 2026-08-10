# M2：开放选用 + 调用方凭证（已实现）

## 目标

1. 总后台 / 控制面把站点 `app_key`/`app_secret` **同步**到媒资 `paas_client`
2. 子站用凭证调用 `/open/*`：**列表 / 详情 / 选用 / 已选用**
3. Secret **落库哈希**（SHA-256），鉴权恒定时间比较

## 管理端：调用方

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/admin/clients` | 列表（不回显 secret） |
| PUT | `/admin/clients` | 同步登记（明文 secret 只传一次，落库哈希） |
| POST | `/admin/clients/{app_key}/disable` | 停用 |

Header：`X-Admin-Token`

```bash
curl -s -X PUT http://127.0.0.1:8004/admin/clients \
  -H 'X-Admin-Token: dev-admin-token-change-me' \
  -H 'Content-Type: application/json' \
  -d '{"app_key":"ak_demo","app_secret":"sk_demo","site_code":"DEMO","status":1}'
```

总后台 `my_manage_service`：配置 `paas.media.enabled=true` 后，**开站 / 重置 secret** 会自动 `PUT` 同步（失败只打日志，不阻断开站）。

## 子站开放 API

Header：`X-App-Key` + `X-App-Secret`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/open/assets` | 就绪媒资；带 `picked` |
| GET | `/open/assets/{id}` | 详情 + `play_url` |
| POST | `/open/assets/{id}/pick` | 选用（写 `site_asset_pick`） |
| GET | `/open/picks` | 本站已选用 |

```bash
# 列表
curl -s 'http://127.0.0.1:8004/open/assets' \
  -H 'X-App-Key: ak_demo' -H 'X-App-Secret: sk_demo'

# 选用 id=3
curl -s -X POST http://127.0.0.1:8004/open/assets/3/pick \
  -H 'X-App-Key: ak_demo' -H 'X-App-Secret: sk_demo'
```

选用后：子站在本站库建上架关系；播放可先用返回的 `play_url`（防盗链属后续统一播放）。

## 迁移

```bash
# my_media 库
make migrate   # 含 00004 secret_hashed
```

## 验收

1. `PUT /admin/clients` 成功；`GET /admin/clients` 可见 `app_key`，无明文 secret  
2. 错误 secret → `/open/assets` 401  
3. 正确凭证 → 列表仅 `status=2`；`pick` 后 `picked=true`，`/open/picks` 有记录  
4.（可选）总后台开站后 `paas_client` 自动出现对应 `app_key`
