# 配置

日常设置都在管理页里，这里只有启动时需要的几项。环境变量写在 `compose.yaml`，也可以写进 `data/config.yaml`（示例见 [config.example.yaml](../config.example.yaml)）。两边都有时环境变量优先。

| 环境变量 | 配置文件 | 默认 | 说明 |
| --- | --- | --- | --- |
| `TRACKFERRY_URL` | `public_base_url` | 空 | 音箱取歌的局域网地址，如 `http://192.168.1.10:18561`。空着时接管不会开始 |
| `TRACKFERRY_TAKEOVER` | `takeover_speakers` | 空 | 允许设为接管的音箱，名字的一部分，逗号分隔；`*` 为全部；空为一台都不允许 |
| `TRACKFERRY_LISTEN` | `listen` | `:18561` | 监听地址与端口。只监听一个地址时写成 `192.168.1.10:18561` |
| `TRACKFERRY_TV_LISTEN` | `tv_listen` | `:18562` | Android TV 独立 TLS 监听地址。设为 `off` 停用 TV 连接和局域网广播 |
| `TRACKFERRY_CAST` | `cast` | `on` | 投放到局域网里的 DLNA 和 Chromecast 设备。设为 `off` 不再搜索、也不驱动已打开的设备 |
| `TRACKFERRY_FFMPEG` | `ffmpeg` | 空 | “无损转 MP3”用的 ffmpeg。空着时用 PATH 里的；默认镜像里没有，见下面“无损转 MP3” |
| `TRACKFERRY_MP3_CACHE_MB` | `mp3_cache_mb` | `2048` | 转好的 MP3 最多占多少 MB，放在 `data/mp3`，超出先删最久没放的 |
| `TRACKFERRY_UPDATE_CHECK` | `update_check` | 开 | `off` 关闭定时检查新版本；页面上的手动检查仍可用 |
| 无，在管理页设置 | 无 | 直接连接 | 更新代理，“设置 → 关于”，只用于检查和下载更新 |
| `TRACKFERRY_LOG` | 无 | 文字 | `json` 输出机器可读的日志 |
| `TRACKFERRY_DEBUG` | 无 | 关 | 任意值打开调试日志，包含每个页面请求 |
| `TRACKFERRY_CONFIG` | 无 | `/data/config.yaml` | 配置文件位置，可以不存在 |
| `TRACKFERRY_RELAY_TARGET` | 无 | `127.0.0.1:5667` | 只用于备选的 `relay` 容器 |
| `PUID`、`PGID` | 无 | `10001` | 程序运行的普通身份，启动时 `data` 会改成它的；不能是 0。`compose.yaml` 写了 `user` 时不起作用 |
| 挂载 `/cache` | 无 | 不挂载 | 放听过的线上歌，例如机械盘上的目录；不挂载就放在 `data/cache` |
| `TZ` | 无 | `Asia/Shanghai` | 时区 |
| 无 | `data_dir` | `/data` | 容器内数据目录 |
| `TRACKFERRY_MUSIC_DIR` | `music_dir` | `/music` | 容器内音乐目录，可选。没连飞牛音乐时它就是曲库：读标签、内嵌封面、同目录的 cover/folder 图片和 .lrc 歌词，按专辑名和专辑艺术家分专辑。只读挂载 |

配置写错时程序不会启动，日志会指出是哪一项。

## 管理页里的设置

| 设置 | 位置 |
| --- | --- |
| 音箱模式、压住小爱的方式、无损转 MP3、接管测试 | 音箱，点开那一台 |
| 飞牛音乐、小米账号、线上音源开关、线上歌曲缓存上限、Home Assistant 接口、管理密码、检查更新 | 设置 |

## 命令

| 命令 | 作用 |
| --- | --- |
| `docker exec trackferry /trackferry setup-code` | 显示初始化码 |
| `docker exec trackferry /trackferry reset-admin` | 清除管理密码，退出所有浏览器 |
| `docker exec trackferry /trackferry version` | 版本 |

## 无损转 MP3

有些型号（L05B、L05C、LX05、LX06、L16A）放不了 FLAC 等无损格式，取了流也不出声。给这台音箱打开“无损转 MP3”后，曲库里的无损歌（flac、wav、ape、aiff、wv、dsf、dff）先在 NAS 上转成 320k MP3 再交给它，其他格式照原样放。

- 需要 ffmpeg。默认镜像不带，换成 `madrays/trackferry:ffmpeg`（或 `1.10-ffmpeg`、`1.10.0-ffmpeg`），约 220 MB，只在 Docker Hub 上。这个镜像不在设置页一键更新，用 `docker compose pull && docker compose up -d` 更新。不用 Docker 的，装好 ffmpeg 放进 PATH 或用 `TRACKFERRY_FFMPEG` 指定。
- 没转过的歌第一次要等几秒才出声，ARM 小盒子可能十几秒，最多等 90 秒；同一时间只转一首，占一个 CPU 核。下一首会提前转好，转过的直接放。
- 转不成（超时、源文件读不到、ffmpeg 报错）就照常放原文件，原因写在这首歌的“链路”里。
- MP3 是有损的。音箱能放无损的就别开。

## 投放到 DLNA / Chromecast

在“更多 → 电视和投放”里搜索并打开设备，打开后它就和电视一样出现在“切换到”里，由服务器直接控制，网页和手机关了也接着放。要点：

- 服务器搜索设备用 SSDP（UDP 1900 组播）和 mDNS（UDP 5353 组播），Docker 需要 `network_mode: host`（`compose.yaml` 默认就是），防火墙要放行这两个端口。
- 设备是自己来服务器取歌的，`TRACKFERRY_URL` 必须是设备能访问到的局域网地址。
- 设备连续 3 次不应答时会显示离线，恢复后自动接上；设备换了 IP 会在下次搜索时自动跟过去；放不了的歌最多连跳 3 首。
- 有别的应用在设备上放东西时，TrackFerry 不抢，等你在网页上再点播放。
