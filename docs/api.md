# 接口

管理页使用的 JSON 接口，也可以给脚本用。接口可能随版本调整。

## 鉴权

- 浏览器：登录后的会话 Cookie，写操作加请求头 `X-Requested-With: trackferry`。
- 脚本：`Authorization: Bearer <data/private/admin_token 的内容>`。

错误返回 `{"error": "代号", "message": "给人看的说明"}`。

## 无需登录

| 接口 | 说明 |
| --- | --- |
| `GET /healthz/live` | 进程在运行 |
| `GET /healthz/ready` | 数据库与目录可用，含版本 |
| `GET /api/session` | 登录状态与版本 |
| `POST /api/setup` · `/api/login` · `/api/logout` | 初始化、登录、退出。登录可带 `remember`：`true` 180 天，`false` 随浏览器关闭失效（最长 12 小时），不写 30 天 |
| `GET /stream/<令牌>` | 音箱取歌 |

## 点歌与音箱

| 接口 | 说明 |
| --- | --- |
| `GET /api/commands?limit=&before=` | 点歌记录 |
| `PATCH /api/commands/{id}` | 标注小爱当时的反应 |
| `POST /api/route/try` `{"text":"播放晴天"}` | 试一句，不出声 |
| `GET /api/intent` · `POST /api/intent` | 说法示例；解释一句话 |
| `GET /api/devices` · `POST /api/devices/sync` · `PATCH /api/devices/{id}` | 音箱列表、同步、改模式、别名，或压住小爱的方式 `{"hush":"normal\|strong\|wait"}` |
| `POST /api/devices/{id}/takeover-test` | 接管测试，会出声 |
| `GET·POST /api/devices/{id}/control` | 播放状态与音量；控制：`{"action":"play\|pause\|stop\|next\|previous\|wake"}`、`{"action":"volume","volume":35}`、`{"action":"seek","position":毫秒,"run_id":播放记录ID}` 或 `{"action":"mode","mode":"loop\|shuffle\|one"}`。`run_id` 可选，控制端应传当前记录 ID；换歌后该定位返回 409。成功返回 204；播放中的定位要等音箱回读，暂停中的定位留到恢复播放时生效 |
| `GET·POST /api/devices/{id}/queue` | 播放列表；点一首放、移出 |
| `POST /api/devices/{id}/play` | `source` 支持 `favorites`、`playlist`、`artist`、`album`、`picks`、`track`、`online`。`id` 为歌单、专辑或线上歌曲，`name` 为歌手，`track_ids` 为本次“随手翻到的”列表（最多 12 首）。列表可用 `track_id` 指定从哪首开始，`track` 用 `track_id` 指定单曲。单曲可用 `next: true` 排到下一首，曲库歌曲也可用 `last: true` 放到队尾。 |
| `GET·PUT /api/playing` `{"artist":true,"after":false,"hours":3}` | 连续播放的设置，`hours` 为 0 不限；没写的不变 |
| `GET /api/takeover/runs?limit=` | 接管经过 |
| `GET /api/observer` | 监听状态 |
| `GET /api/live` | 实时事件（SSE）：`hello` 全量，`flow` 一条点歌的变化，`pulse` 一次监听 |

## 曲库与音源

| 接口 | 说明 |
| --- | --- |
| `GET /api/library` · `POST /api/library/scan` | 状态，含自动同步间隔 `every_minutes`；同步 |
| `PUT /api/library/every` `{"minutes":360}` | 自动同步间隔，分钟：0（关）、5、15、30、60、180、360、720、1440 |
| `GET /api/here` · `GET /api/here/audio?track=\|online=` | 在浏览器里放：要放的列表（`source=random` 是随机的 30 首）；音频，`index=1` 时 FLAC 装进带索引的 MP4，不重新编码，给跳不准 FLAC 的浏览器 |
| `GET /api/library/search?q=` · `/shelf` · `/albums/{id}` · `/artist` | 搜索与浏览 |
| `GET /api/library/favorites` · `/playlists` · `/playlists/{guid}` | 收藏、歌单、歌单里的歌 |
| `GET /api/library/online?q=` | 搜在线源 |
| `GET·PUT /api/library/tracks/{id}/lyrics` | 歌词，`offset_ms` 为正表示提前；PUT `{"offset_ms":500}` 记下偏移 |
| `GET /api/library/missing` · `/api/library/missing.csv` | 缺歌清单，不含已补上和移除的；导出同一份 |
| `POST /api/library/missing/dismiss` `{"title":"","artist":"","back":false}` | 从缺歌清单移除；`back` 为撤销 |
| `POST /api/library/playlists/{guid}/tracks` | 把曲库的歌加入飞牛歌单 |
| `GET /api/cache` · `PUT` `{"limit_mb": 1024}` · `DELETE` | 线上歌曲缓存的用量；上限，`0` 为关闭，`-1` 为不限；清空 |
| `GET·PUT /api/library/tracks/{id}/favorite` | 收藏 |
| `GET /api/cover/{id}` | 封面 |
| `PATCH /api/sources/{kuwo\|kugou\|migu\|netease}` | 音源开关 |
| `GET / POST / DELETE /api/sources/netease/account` | 网易云登录状态 / 用浏览器的 MUSIC_U 登录（`cookie`）/ 退出 |
| `POST / GET / DELETE /api/sources/netease/qr` | 取二维码内容 / 查扫码进度（waiting、scanned、expired、done）/ 取消 |
| `GET·POST·DELETE /api/feiniu` · `GET /api/feiniu/discover` | 飞牛音乐登录与查找 |

## Android TV

Android TV 使用独立的 HTTPS 端口（默认 `18562`）。`/api/tv/v1/info` 用于发现后的探测，`POST /api/tv/v1/pair`、`/pair/status` 和 `/pair/cancel` 用于配对。`/api/tv/v1/shelf`、`/search`、`/playlists`、`/queue`、`/cover/{id}`、`/lyrics/{id}` 和 `/audio?track=` 只接受 TV 设备凭据。管理员从 Web 端的 `/api/tv/devices` 查看或确认配对，经 `/api/tv/devices/{id}` 撤销。

已配对设备可通过 `GET /api/tv/v1/tracks/{id}/favorite` 查收藏状态，通过同一路径的 `PUT` 设置 `{"on":true}` 或 `{"on":false}`，并用 `POST /api/tv/v1/playlists/{guid}/tracks` 把 `{"track_id":123}` 加入现有歌单。加入成功后返回 `changed`，歌曲已在歌单中时返回 `false`。

`GET /api/tv/v1/queue?source=random` 返回随机的 30 首，电视点播单曲后用它接着放。`/lyrics/{id}` 带上这首歌的 `offset_ms`，`PUT /api/tv/v1/lyrics/{id}/offset` 用 `{"offset_ms":300}` 改它，范围是 ±60000，正数让歌词提前。这个值和网页共用。

`GET /api/tv/v1/app?abi=armeabi-v7a|arm64-v8a` 说明和服务器同版本的电视应用：`version`、`phase`（空、`download`、`ready`、`failed`）、进度 `done`/`total`、就绪后的 `size` 和 `sha256`，失败时的 `error`。`POST` 同一路径让服务器从 Release 取包并核对签名。就绪后 `GET /api/tv/v1/app/apk?abi=` 下载安装包，响应头 `X-TrackFerry-SHA256` 和 `X-TrackFerry-Version` 给出校验值和版本。

## 账号与系统

| 接口 | 说明 |
| --- | --- |
| `GET·DELETE /api/xiaomi` | 关联状态、解除 |
| `/api/xiaomi/qr`、`/qr.png` | 扫码登录 |
| `/api/xiaomi/password`、`/captcha`、`/verify` | 密码登录 |
| `POST /api/password` | 改管理密码 |
| `GET /api/status` | 运行状态 |
| `GET·PUT /api/home` · `POST /api/home/token` | Home Assistant 接口的开关 `{"enabled": true}`；换新令牌，只返回这一次 |
| `/api/ha/…` | 给 Home Assistant 用，另有令牌，见[接入 Home Assistant](homeassistant.md) |
| `GET /api/update` · `POST /api/update` | 上次检查结果与安装进度；立即检查 |
| `POST /api/update/install` | 安装最新版本并重启 |
| `PUT /api/update/proxy` | 设置更新用的代理 `{"proxy": "http://192.168.1.2:7890"}`，空字符串为直接连接 |
