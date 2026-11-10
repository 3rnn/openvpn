# Docker 部署指南（从零到第一个客户端连上）

本文按顺序带你完成：准备服务器 → 启动容器 → 登录管理后台 → 开放端口 → 创建第一个 VPN 客户端 → 日常维护与升级。每一步都写了"怎么确认成功"。

更细的说明和安全背景见 [README.md](README.md)、[INSTALL.md](INSTALL.md)、[UPGRADE.md](UPGRADE.md)、[BACKUP_RESTORE.md](BACKUP_RESTORE.md)。

当前版本：

```text
3rnn/openvpn:v1.1.2-hardened@sha256:0e637729aab170d31c3acd7a9f05cbf3335a94c52cf9ae00c9912dd71a92620c
```

---

## 0. 部署后的样子

```text
VPN 客户端 ──UDP 1194──▶ 服务器 ──▶ 容器 openvpn（OpenVPN + Web 管理后台）
管理员浏览器 ──SSH 隧道 / 内网地址 / HTTPS 反代──▶ 容器 8833 端口（管理后台）
服务器目录 /opt/openvpn/data ◀──▶ 容器 /data（数据库、证书、客户端配置，全部数据都在这里）
```

- 只有一个容器，里面同时运行 OpenVPN 服务和 Web 管理后台。
- **所有数据都在宿主机的 `data` 目录**。删掉或重建容器不会丢数据；丢了 `data` 目录就等于丢了 CA 和全部客户端证书。

## 1. 准备服务器

| 项目 | 要求 |
| --- | --- |
| 系统 | Debian 13（trixie）amd64，已验证。其他 Linux 发行版理论上可以运行，但未验证 |
| 架构 | 仅 `linux/amd64`，不支持 ARM |
| 配置 | 起步 1 核 / 1 GB 内存 / 8 GB 磁盘；正式使用建议 2 核 / 2 GB / 16 GB |
| 权限 | root 或 sudo |
| 网络 | 客户端能访问到的公网 IP 或域名；UDP 1194 端口未被占用 |
| 内核 | 有 `/dev/net/tun`（大多数 VPS 默认有；LXC/OpenVZ 需服务商开启 TUN） |

在服务器上确认：

```bash
cat /etc/os-release | grep PRETTY_NAME
dpkg --print-architecture        # 应输出 amd64
test -c /dev/net/tun && echo TUN-OK
ss -lun | grep ':1194 ' || echo "1194 空闲"
```

## 2. 安装 Docker

已经装好 Docker 和 Compose 插件的，跳过本节，只需确认：

```bash
docker version
docker compose version
```

全新的 Debian 13 请按 [INSTALL.md 第 2、3 节](INSTALL.md#2-配置-docker-官方-apt-源) 用 Docker 官方 APT 源安装（不要用来路不明的一键脚本）。

## 3. 获取部署文件并启动

以 root 身份执行。部署目录用 `/opt/openvpn`，它必须是新目录。**已经部署过的服务器不要再执行这一步**，升级请看第 9 节。

```bash
set -eu
umask 077
test ! -e /opt/openvpn                       # 防止覆盖已有部署
git clone --branch security/hardened-v1 --single-branch https://github.com/3rnn/openvpn.git /opt/openvpn
cd /opt/openvpn
install -d -m 0700 data                      # 数据目录，只允许 root 访问
docker compose config --images               # 应显示上面"当前版本"那一行
docker compose pull
docker compose up -d
```

每一步做了什么：

- `git clone` 只是为了拿到 `docker-compose.yml` 和文档，**不会在服务器上编译代码**，镜像直接从 Docker Hub 下载。
- `docker-compose.yml` 用"版本号 + digest"固定镜像，保证每台服务器拉到的是同一份内容。
- 服务器无法访问 Docker Hub 时，看第 10 节的离线导入方法。

**确认启动成功**（OpenVPN 初始化通常只需几秒）：

```bash
cd /opt/openvpn
docker compose ps                                                        # 状态为 Up / running
docker compose logs openvpn | grep "Initialization Sequence Completed"   # 出现这一行说明 OpenVPN 已就绪
docker compose exec -T openvpn ip -br a show tun0                        # 应看到 10.8.0.1/24
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8833/login      # 应输出 200
```

## 4. 首次登录管理后台

### 4.1 取得初始密码

- 用户名是 `admin`。
- 密码是首次启动时随机生成的，只保存在一个文件里。它**不是 admin/admin，日志里也不会打印**。

```bash
less -- /opt/openvpn/data/bootstrap-admin-password
```

只在私密终端查看，不要截图、录屏或贴到聊天/工单里。

### 4.2 打开管理后台

默认配置下，管理后台**只监听服务器本机的 `127.0.0.1:8833`**，公网访问不到。这是有意的安全设计。按你的情况选一种方式：

**方式 A：SSH 隧道（推荐，最安全）**

在**你自己的电脑**上执行（把用户名和 IP 换成你的）：

```bash
ssh -N -L 18833:127.0.0.1:8833 root@你的服务器IP
```

保持这个窗口不关，浏览器打开 `http://127.0.0.1:18833`。

**方式 B：只在内网访问（服务器在公司或家庭内网时）**

1. 把 `docker-compose.yml` 里的 `"127.0.0.1:8833:8833"` 改成服务器的**内网 IP**，例如 `"192.168.1.10:8833:8833"`。
2. 执行 `docker compose up -d` 让修改生效。
3. 浏览器打开 `http://192.168.1.10:8833`。

**绝对不要填公网 IP 或 `0.0.0.0`**，否则管理后台会直接暴露到互联网。

**方式 C：多人长期远程管理**

用 HTTPS 反向代理，并限制允许访问的来源 IP。详见 [README 第 10 节](README.md#10-https-生产管理入口)。

### 4.3 登录后马上做

1. 按页面提示**修改初始密码**（新密码 12–72 位，必须包含大写字母、小写字母、数字和符号）。
2. 用新密码重新登录，确认可用。
3. 建议在后台启用 MFA（二次验证）。
4. 确认新密码可用后，删除初始密码文件：

   ```bash
   unlink /opt/openvpn/data/bootstrap-admin-password
   ```

不想用随机密码时，也可以在**第一次启动前**通过环境变量 `OPENVPN_UI_ADMIN_PASSWORD` 指定初始密码。只在空的数据目录首次启动时生效，密码规则同上。指定后不会生成密码文件。用完记得从 compose 文件里删掉这个变量。

## 5. 开放 VPN 端口

客户端要能从外部访问服务器的 **UDP 1194**。需要检查以下几处：

- **云服务器**：在服务商控制台的"安全组 / 防火墙"放行入站 UDP 1194。
- **服务器本机防火墙**：Docker 发布的端口通常会绕过 ufw 的规则，以实际测试结果为准。
- **内网服务器**：在路由器上把公网 UDP 1194 转发到服务器的 UDP 1194。

管理端口 8833 **不需要也不应该**对公网开放。

## 6. 创建第一个 VPN 客户端

连接 VPN 需要两样东西：一个**VPN 账号**（用户名和密码），以及一个**客户端配置文件**（`.ovpn`，里面包含证书）。

1. **创建 VPN 账号**
   - 左侧菜单进入 **VPN 账号**，点击 **添加**。
   - 填写用户名和密码。
2. **生成客户端配置**
   - 左侧菜单进入 **客户端**，点击 **添加**，按下表填写：

     | 字段 | 填什么 |
     | --- | --- |
     | 名称 | 客户端名称，例如 `zhangsan-laptop`。只能用字母、数字和 `_ . @ -` |
     | VPNServer | 客户端能访问到的**公网 IP 或域名**。不能填 `127.0.0.1` 或容器 IP |
     | 端口 | `1194`（如果改过映射，就填对外的端口） |
     | CCD 配置 / 自定义配置 | 第一次部署留空即可 |

   - 生成后在列表里下载 `.ovpn` 文件。

   名称不能和已有客户端或已吊销的客户端重复。重复时会提示"请使用新名称"，换一个名字即可。
3. **在设备上连接**
   - 把 `.ovpn` 导入 OpenVPN Connect（Windows / macOS / iOS / Android 都有）或其他 OpenVPN 客户端。
   - 连接时输入第 1 步创建的 VPN 账号和密码。
4. **确认连上**
   - 后台 **在线状态** 页能看到这个连接。
   - 在客户端上访问你需要访问的地址，确认能通。

`.ovpn` 文件里有客户端私钥，等同于密码。只能用私密渠道发给使用者，不要放进公开网盘链接。

不再使用的客户端，到 **证书管理** 里吊销它的证书。

## 7. 让客户端访问内网（可选）

默认 VPN 网段是 `10.8.0.0/24`，不能和客户端所在网络、服务器内网重叠。想通过 VPN 访问服务器所在的内网时，需要两件事：

- **客户端路由**：在客户端的"自定义配置"里加上路由，例如 `route 192.168.8.0 255.255.255.0`。
- **回程路由**：内网要能把回包送回 VPN 网段，通常是在内网路由器上把 `10.8.0.0/24` 指向这台服务器，或者使用经过审查的 NAT。

想让客户端的全部流量都走 VPN（`redirect-gateway`），要同时确认 DNS 和出口是否符合预期。详见 [README 第 13 节](README.md#13-lan-路由与访问范围)。

## 8. 日常维护

```bash
cd /opt/openvpn
docker compose ps                         # 运行状态
docker compose logs --tail=100 openvpn    # 最近日志
docker compose restart openvpn            # 重启（客户端会自动重连）
```

**备份**：先停止服务，再把整个部署目录（`docker-compose.yml` 和 `data/`）一起打包：

```bash
cd /opt/openvpn
umask 077
docker compose stop
tar -czf /root/openvpn-backup-$(date +%Y%m%d-%H%M%S).tar.gz docker-compose.yml data
docker compose start
```

备份包含 CA 私钥和客户端私钥，要按机密保存。只备份数据库是不够的。完整流程和恢复演练见 [BACKUP_RESTORE.md](BACKUP_RESTORE.md)。

## 9. 升级到新版本

1. 在 [CHANGELOG.md](CHANGELOG.md) 或 [README 第 2 节](README.md#2-正式版本与镜像溯源) 找到新版本的完整镜像引用，格式是"版本号 + digest"。
2. 按第 8 节**停机备份**。
3. 修改 `docker-compose.yml` 里的 `image:` 行，换成新版本的完整引用。也可以 `git pull` 拿到仓库里已经更新的 compose；如果你改过端口，记得把改动重新加回去。
4. 拉取新镜像并重建容器：

   ```bash
   cd /opt/openvpn
   docker compose pull
   docker compose up -d
   ```

5. 按第 3 节的"确认启动成功"检查一遍。

重建时 OpenVPN 只会中断几秒钟，`data` 目录原样复用。

**回滚**：把 `image:` 改回旧版本，执行 `docker compose up -d`。如果新版本已经改动过数据，还要先停机，再用升级前的备份恢复 `data`。详见 [UPGRADE.md](UPGRADE.md)。

不要用 `latest` 或 `stable` 这类会变化的标签部署生产环境。它们会在你不知情的情况下换成别的版本。

## 10. 服务器无法访问 Docker Hub（离线导入）

内网或网络受限的服务器，`docker compose pull` 会超时。这时找一台能访问 Docker Hub 的 amd64 Linux 机器中转：

在**能联网的机器**上：

```bash
docker pull 3rnn/openvpn:v1.1.2-hardened@sha256:0e637729aab170d31c3acd7a9f05cbf3335a94c52cf9ae00c9912dd71a92620c
docker tag  3rnn/openvpn@sha256:0e637729aab170d31c3acd7a9f05cbf3335a94c52cf9ae00c9912dd71a92620c 3rnn/openvpn:v1.1.2-hardened
docker save 3rnn/openvpn:v1.1.2-hardened | gzip > openvpn-v1.1.2-hardened.tar.gz
```

把文件拷到目标服务器（`scp`、U 盘均可），然后在**目标服务器**上导入：

```bash
gunzip -c openvpn-v1.1.2-hardened.tar.gz | docker load
docker image inspect 3rnn/openvpn:v1.1.2-hardened \
  --format '{{.Os}}/{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.version"}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
# 预期：linux/amd64 v1.1.2-hardened d74321ec34139c4fcc8146f4c5e547ca4b666369
```

`docker load` 导入的镜像不带 registry digest，所以 compose 文件要改成按标签引用，并禁止自动拉取：

```yaml
    # 3rnn/openvpn:v1.1.2-hardened registry digest sha256:0e637729aab170d31c3acd7a9f05cbf3335a94c52cf9ae00c9912dd71a92620c
    image: 3rnn/openvpn:v1.1.2-hardened
    pull_policy: never
```

之后不执行 `docker compose pull`，直接 `docker compose up -d`。

## 11. 常见问题

| 现象 | 原因和处理 |
| --- | --- |
| `no matching manifest` | 服务器不是 amd64。本项目只发布 amd64 镜像 |
| 日志里有 TUN 相关的 `Operation not permitted` | 宿主机缺少 `/dev/net/tun`，或 LXC/OpenVZ 没有开放 TUN。联系服务商开启，不要改成 privileged 容器 |
| 公网打不开管理后台 | 默认就只监听本机。用第 4.2 节的 SSH 隧道或内网方式访问 |
| 登录时反复跳回登录页或提示 403 | 整个登录过程必须用同一个地址；用 http 访问时不要开启 Secure Cookie；用反向代理时要保留原始 Host |
| 客户端连不上 | 依次检查：安全组或路由器是否放行 UDP 1194；`.ovpn` 里的服务器地址是不是公网可达；VPN 账号和密码是否正确；容器日志里的报错 |
| 连上了但访问不了内网 | 检查客户端路由、内网回程路由、网段是否冲突（见第 7 节） |
| 添加客户端时提示"请使用新名称" | 这个名字以前用过，证书可能已经吊销。换一个新名字 |
| 重建容器后像全新安装、数据没了 | 很可能是在别的目录执行了 `up`，`./data` 指向了一个空目录。立即停止容器，回到原部署目录 |

更多问题见 [README 第 18 节](README.md#18-故障排查)。

## 12. 停止与卸载

```bash
cd /opt/openvpn
docker compose stop     # 停止服务，保留容器
docker compose down     # 删除容器和网络，data 目录保留
```

`down` **不会删除** `data` 目录。彻底删除前请确认已经不再需要 CA 和客户端证书，或者已经做好备份。
