# 部署教程

## 准备

| 需要 | 说明 |
| --- | --- |
| NAS | x86_64 或 arm64 的 Linux，装有 Docker 与 Compose v2。32 位 armv7 也发布镜像，但未经实机测试 |
| 固定的局域网地址 | 在路由器里给 NAS 绑定 IP。地址一变，音箱就取不到歌 |
| 小爱音箱 | 与 NAS 在同一局域网，绑定在你能登录的小米账号下 |
| 飞牛音乐 | 可选。没有时可以只读挂载一个音乐文件夹 |

## 第一步：放好文件

在 NAS 上新建一个目录，例如 `/vol1/1000/Docker/trackferry`，放入仓库根目录的 `compose.yaml`。只有一处必须改：

```yaml
TRACKFERRY_URL: "http://192.168.1.10:18561"
```

换成这台 NAS 的局域网地址。音箱自己来这个地址取歌，所以不能写 `localhost`、`127.0.0.1`，也不要写只有外网能用的域名。

## 第二步：数据目录

不用准备。数据放在 `compose.yaml` 同级的 `data`，第一次启动会自动建好并设好权限。

程序不以 root 运行：容器启动时把 `data` 交给一个普通身份（默认 `10001`，不对应 NAS 上的真实用户），随即切换成它，特权也一并放弃。只改 `data`，不碰音乐目录。

想让 NAS 上的某个用户能直接查看 `data`，在 `compose.yaml` 里把 `PUID`、`PGID` 填成那个用户 `id -u` 和 `id -g` 的结果，再 `docker compose up -d`。

## 第三步：启动

```sh
docker compose up -d
```

```sh
docker compose logs trackferry
```

日志开头是运行概况，首次启动时最后是这样一段：

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  首次使用，设置管理密码

  初始化码　　ABCD2345
  打开　　　　http://192.168.1.10:18561

  只能用一次，设好密码后失效。
  再看一次：docker exec trackferry /trackferry setup-code

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

找不到就执行最后那条命令，它只显示这一段。

## 第四步：放行端口

默认使用 host 网络，程序直接监听 NAS 的 18561 端口。NAS 开着防火墙时，要允许局域网访问它，否则管理页打不开、音箱取不到歌。

- 飞牛 fnOS：系统设置的防火墙里放行 TCP 18561，来源限定为局域网。
- 使用 ufw（网段换成你自己的）：

  ```sh
  sudo ufw allow from 192.168.1.0/24 to any port 18561 proto tcp
  ```

用电视端还要放行 TCP 18562（电视连接）和 UDP 5353（电视在局域网里自动找到 NAS），同样只对局域网：

- 飞牛 fnOS：防火墙里再放行 TCP 18562、UDP 5353。
- ufw：`sudo ufw allow from 192.168.1.0/24 to any port 18562 proto tcp`，`sudo ufw allow from 192.168.1.0/24 to any port 5353 proto udp`

5353 没放行时电视找不到 NAS，可以在电视连接页选“手动连接”填 NAS 地址；18562 没放行时手动连接也连不上。

不要在路由器上把这些端口映射到外网。

## 第五步：完成设置

1. 打开 `http://<NAS 地址>:18561`，填初始化码，设置管理密码。
2. 登录小米账号：米家扫码，或账号密码。选绑定了音箱的那个账号。
3. 在“设置”登录飞牛音乐。填的是飞牛音乐里的用户名和密码；只用飞牛授权登录过的，先在飞牛音乐里设一个密码。
4. 在“音箱”把一台设为“观察”，对它点几首歌，看“此刻”里的判断对不对。
5. 让 xiaomusic 等程序退出对这台音箱的控制，再把它设为“接管”。用页面下方的“接管测试”先听一声提示音。

## 数据与备份

全部数据在 `data/`：

| 路径 | 内容 |
| --- | --- |
| `data/trackferry.db` | 点歌记录、音箱设置、曲库索引 |
| `data/private/` | 小米、飞牛、网易云的登录凭据、脚本令牌，权限 0600 |
| `data/cache/` | 听过的线上歌和它们的目录，可以随时删，不必备份。挂载了 `/cache` 时歌在那里 |
| `data/config.yaml` | 可选的配置文件 |

### 把听过的线上歌放到机械盘

默认放在 `data/cache`。想放到别的盘，在 `compose.yaml` 的 `volumes` 里加一行（左边换成自己的目录），再执行 `docker compose up -d`：

```yaml
      - /vol2/1000/Cache:/cache
```

只用其中的 `trackferry` 文件夹，文件名是“歌名-歌手.mp3”，重名时加序号。目录里别的内容不动、不改属主。换位置时，原来 `data/cache` 里存的歌会删掉，以后再听再存。机械盘休眠时，2.5 秒内读不出来就直接从音源播放，不让人等。挂载的目录写不了时，会在日志里说明并继续用 `data/cache`。

备份前先停下，保证数据库完整：

```sh
docker compose stop
```

```sh
sudo tar czf trackferry-backup.tar.gz --exclude=data/cache data compose.yaml
```

```sh
docker compose start
```

备份里有登录凭据，不要发给别人。恢复时解压到新目录再启动，权限会自动处理。

## 更新与退回

“设置 → 关于”每天检查两次新版本，不会自己安装。

**在线更新**：在“设置 → 关于”点“更新”。程序从 GitHub 下载新版本，核对签名，备份数据库，然后在容器里重启，`compose.yaml` 不用改。新版本连续三次启动不了，会退回上一个正常的版本并还原数据库。从 1.1.0 开始支持。

连不上 GitHub 时，在同一处填代理地址，支持 http、https、socks5，如 `http://192.168.1.2:7890`。代理只用于检查和下载更新，点歌、飞牛音乐、小米和线上音源都不经过它。

新版本放在 `data/update`。镜像比它新时自动改用镜像里的程序；想放弃在线更新、回到镜像的版本，停下容器，删掉 `data/update/installed.json` 再启动。容器需要 `restart: unless-stopped`，数据目录不能挂载成 `noexec`。

**用命令更新**：先备份，再在 `compose.yaml` 所在目录执行：

```sh
docker compose pull && docker compose up -d
```

`compose.yaml` 默认用 `latest`，拉到的就是最新版；以前写了版本号的，先把它改成 `latest`。

退回旧版本时，把 `latest` 换成那个版本号，如 `1.2.0`，删掉 `data/update/installed.json`，并恢复更新前的 `data`，因为旧程序不一定认识新数据库。

标签有三种：`latest` 跟随最新版，是默认值；`1.2` 只跟随 1.2 的小修复；`1.2.0` 固定不变。

## 从源码构建

想自己修改，或拉不到镜像时：

```sh
git clone https://github.com/madrays/trackferry.git && cd trackferry
```

```sh
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

构建默认使用国内的 npm 与 Go 镜像源，可用 `--build-arg NPM_REGISTRY=… --build-arg GOPROXY=…` 更换。

## 备选：桥接网络

不能用 host 网络时，用 `compose.bridge.yaml`：

```sh
docker compose -f compose.bridge.yaml up -d
```

它多一个 `relay` 容器，专门让桥接容器连到本机的飞牛音乐而不改防火墙。一般用默认的 host 方案就好。

## 常见问题

**启动后反复重启，日志写“数据目录不可写”**
`compose.yaml` 里有 `user` 一行时（1.1.0 及以前的写法），权限要自己处理。换成最新的 `compose.yaml` 和镜像，或者在 `compose.yaml` 所在目录执行 `sudo chown -R 10001:10001 data`，再 `docker compose restart`。没有 `user` 仍然这样，说明 `data` 所在的共享或磁盘不允许改权限，换到 NAS 本机的存储空间。不要用 `chmod 777`，里面有登录凭据。

**日志写“不能切换到普通身份”**
`compose.yaml` 缺少 `cap_add` 一行，照最新的 `compose.yaml` 补上。

**管理页打不开**
先在 NAS 上执行 `curl http://127.0.0.1:18561/healthz/live`。能通，是防火墙没放行，见第四步；不通，看日志。

**日志写端口被占用（address already in use）**
换一个端口，`TRACKFERRY_LISTEN` 和 `TRACKFERRY_URL` 一起改。

**管理页正常，音箱不出声**
多半是音箱取不到歌。用手机在同一 Wi-Fi 打开 `TRACKFERRY_URL`，打不开就是地址或防火墙的问题。访客网络、AP 隔离、音箱与 NAS 不在同一网段都会这样。

**音箱列表是空的**
登录的小米账号不是绑定音箱的那个。在小爱音箱 App 里核对账号，重新登录。

**放歌时有两个声音，或放一下又被换掉**
还有别的程序在控制这台音箱，最常见的是 xiaomusic。让它退出这台音箱。

**找不到飞牛音乐**
确认飞牛音乐已安装并启动，`network_mode: host` 没被删掉。手动填 `https://127.0.0.1:5667`；飞牛改过 HTTPS 端口就填改后的。

**提示飞牛证书变了**
重装或换证书后会这样。确认是自己换的，在“设置”里断开，重新登录。

**镜像拉不下来**
给 Docker 配置可用的镜像加速，或从源码构建。

**自己构建的镜像不见了**
有些 NAS 会定时清理没有容器在用的镜像。构建和启动放在同一条命令里，旧镜像用 `docker save` 存成文件。

**用反向代理后页面不实时更新**
实时数据走 SSE，代理要关掉缓冲。Nginx 加 `proxy_buffering off;` 和 `proxy_read_timeout 1h;`。`TRACKFERRY_URL` 仍填局域网地址，音箱不走代理。

**忘了管理密码**

```sh
docker exec trackferry /trackferry reset-admin
```

所有浏览器会退出登录，并给出新的初始化码。其他设置不变。

更多现象见[问题排查](troubleshooting.md)。

## 卸载

```sh
docker compose down
```

删除目录即清除全部数据。TrackFerry 不在别处写任何文件。之后可在小米账号的登录设备里移除这次登录。
