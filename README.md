# OpenVPN Web 独立加固版

> 2026-10-25 发布补充：所有者已手动搭建本地环境并确认客户端连接成功，随后授权正式发布。此为用户报告，独立于 89 项自动隔离验收；自动测试的外部 VPN 项仍记为 NOT_TESTED。下文“未验证”网络范围指自动化/完整现场覆盖，不否定上述手动连接结果；不代表完整 DNS、LAN、MTU、吞吐、撤销或真实 LDAP 已验证。

## 1. 项目简介

面向 Debian 13 + Docker 的 OpenVPN 与 Web 管理面板。本项目由 `3rnn/openvpn` 独立维护，源自 `GavinTan/openvpn`，初始审查基线为 `cb632205b01b66551342e609e4a6b1bbf771c55f`，**不是上游官方发行版**。保留原 MIT 许可证与版权，见 [LICENSE](LICENSE)。上游仅作参考，不自动同步。

## 2. 正式版本与镜像溯源

本次正式发行名称为 **`v1.1.0-hardened`**。仓库已有上游历史 `v1.0.0` 标签，不能复用或覆盖。

| 项目 | 固定值 |
| --- | --- |
| Docker Hub | `3rnn/openvpn` |
| 版本 | `v1.1.0-hardened` |
| 平台 | **仅 `linux/amd64`** |
| 镜像构建提交 / OCI revision | `4db21ef895e34071a9dbd8f8878d9615488e1fa9` |
| 已发布镜像 digest | `sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683` |

```text
3rnn/openvpn:v1.1.0-hardened@sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683
```

镜像不可变；后续文档提交不改变该构建提交，也不重新构建覆盖该版本。生产使用版本 + digest，不跟随 `latest` 或自动跟随可变 `stable`。Git 正式标签应指向上表构建提交，更新后的部署文档单独追溯。

## 3. 已验证功能与边界

发行验收记录 **89 项通过**：容器内 OpenVPN/TUN/UDP 启动、Web 登录与首次改密、MFA、CSRF、会话撤销、用户与证书创建、证书吊销/CRL 失败恢复，以及重启/重建后的持久化。已执行浏览器安全与相关代码回归；这不是完整安全证明。

**未验证**：真实外部 VPN 客户端握手和数据转发、现场 NAT/路由/MTU、真实 LDAP 服务器集成、多架构部署。LDAP TLS 的代码/回归检查不等于真实目录认证验收。上线前必须在自己的网络补做外部连接与业务验收，不能把端口监听当成 VPN 已可用。

## 4. 环境与容量规划

- Debian 13（trixie），amd64，rootful Docker Engine、Compose 插件；需要宿主机允许 `/dev/net/tun`。
- 起步建议：1 CPU、1 GB RAM、8 GB 可用磁盘；生产规划建议从 2 CPU、2 GB RAM、16 GB 可用磁盘开始，并为备份和数据增长留空间。
- 以上是部署建议，**不是容量基准或并发/吞吐承诺**；按连接数、加密流量、日志和备份实测调整。
- 管理员需有 root 或 sudo 权限；Docker 权限近似宿主机 root 权限，不向普通用户随意开放。

## 5. 部署架构与权限

```text
管理员浏览器 → SSH 隧道或受信任 HTTPS 反向代理 → 127.0.0.1:8833
VPN 客户端 → 服务器公网 IP:1194/udp → 容器 OpenVPN
宿主机 ./data → 容器 /data（数据库、配置、PKI、客户端资料）
```

运行时是 **root + Docker 默认 capabilities，再增加 NET_ADMIN**，不是“仅 NET_ADMIN”。保留 TUN 映射、`no-new-privileges` 与容器内 IPv4 forwarding；不要改为 privileged、host network、挂载 Docker socket，也不要未经验证改为 non-root 或清空全部 capabilities。

## 6. 安装 Docker

全新 Debian 13 请按 [INSTALL.md](INSTALL.md) 逐步配置 Docker 官方 APT 源、安装 Engine 和 Compose 并检查 TUN。已经安装且正常运行的 Docker 不必重复安装；有其他容器业务的主机应先评估升级与网络影响。禁止运行旧裸机安装器或下载作者预编译应用二进制。

## 7. 快速安装

以下在完成 Docker 安装后，由管理员在服务器 root shell 执行。`/opt/openvpn` 必须不存在；现有部署请读 [UPGRADE.md](UPGRADE.md)，不能重复初始化覆盖。

```bash
set -eu
umask 077
test ! -e /opt/openvpn
git clone --branch security/hardened-v1 --single-branch https://github.com/3rnn/openvpn.git /opt/openvpn
cd /opt/openvpn
git rev-parse HEAD
install -d -m 0700 data
test -c /dev/net/tun
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
```

克隆显式选择加固文档所在分支，不假设默认分支包含此部署文件。分支可变，生产应保存 `git rev-parse HEAD` 结果及本地 Compose 副本；镜像仍由固定 digest 决定。正式发布文档固定入口由发布说明提供，不能拿镜像构建提交的旧 README 代替新部署指南。

## 8. 首次管理员登录

首次空数据目录启动后，用户名为 **`admin`**，随机密码保存在容器 **`/data/bootstrap-admin-password`**，本例宿主机对应 **`/opt/openvpn/data/bootstrap-admin-password`**，权限 `0600`。不是 `admin/admin`，日志不输出密码。

仅在私密交互终端查看（不要录屏、复制到工单或报告）：

```bash
cd /opt/openvpn
less -- data/bootstrap-admin-password
```

登录后必须按页面提示立即更换密码，再启用 MFA 并保管恢复所需信息。确认新密码可用后，由管理员单独删除初始密码文件：

```bash
unlink /opt/openvpn/data/bootstrap-admin-password
```

删除普通文件不能保证底层介质安全擦除，首次备份中也可能含旧密码。已有配置不会因设置初始化环境变量而重置密码；丢失凭据请走受控离线恢复，不要删除整个 data 或重建 CA。

## 9. SSH 隧道访问管理面板

在**自己的电脑**运行，替换 SSH 用户和服务器地址：

```bash
ssh -N -L 18833:127.0.0.1:8833 operator@SERVER_IP
```

浏览器打开 `http://127.0.0.1:18833`。这是本地 HTTP 经 SSH 加密，不是浏览器 HTTPS；不要为此启用 `OPENVPN_UI_COOKIE_SECURE=true` 或把站点 URL 改为 HTTPS，否则 Cookie/Origin 可能不匹配。使用同一浏览器地址完成整个登录过程。8833 默认只映射到服务器 loopback，不能直接从公网访问。

## 10. HTTPS 生产管理入口

需要多人远程管理时，用受信任的 HTTPS 反向代理并限制访问来源；不直接公开 HTTP 8833。代理到 `http://127.0.0.1:8833`（同宿主机代理），**保留外部 Host，包括非默认端口**。代理在另一容器时，其 `127.0.0.1` 不是宿主机，须另行设计私有网络，不要为了方便开放公网 8833。

HTTPS 建好后，将应用站点 URL 配为实际 `https://...`，启用 `system.base.cookie_secure` 或 Compose 环境变量 `OPENVPN_UI_COOKIE_SECURE: "true"` 后重建容器。仅在浏览器确实使用 HTTPS 时启用 Secure Cookie。应用不信任任意转发头，不要靠伪造 `X-Forwarded-Proto` 绕过 Origin 校验；核对 Host、站点 URL、Cookie 与实际协议。证书续期、代理 ACL 和日志脱敏需自行运维。

## 11. 端口与网络

默认 `1194/udp` 对外发布，`127.0.0.1:8833/tcp` 仅管理。云安全组、边界路由器、运营商 NAT 与主机策略必须按自己的拓扑评估。Docker 安装和端口发布可能影响防火墙路径，不要假设 UFW 的规则就能限制已发布端口。

示例不执行宿主机 iptables/nftables/sysctl 修改。Compose 的 `net.ipv4.ip_forward=1` 只作用于容器网络命名空间。容器内部 NAT 并不等于云端、宿主机和远端 LAN 已正确转发；不可照抄清空防火墙或全局开启转发的命令。

## 12. 用户、证书与客户端配置

通过管理页面创建用户、设置认证与 MFA、签发/下载客户端配置并管理证书撤销。签发前明确填写客户端能到达的公网域名/IP 与端口，不能用 `127.0.0.1` 或容器私网地址。下载的 `.ovpn` 含客户端私钥，按凭据保管，不放公共链接。

修改监听端口/协议时必须同步应用配置、Compose 映射、安全组和客户端；默认 Compose 是 UDP，不能只在 UI 改 TCP 就期待连通。证书撤销后核对 CRL 生效并重试失败流程；不要手工删索引或配置文件掩盖错误。生产上线要用真实外部客户端验证认证、连接、DNS、路由和撤销效果。

## 13. LAN 路由与访问范围

VPN 网段默认 `10.8.0.0/24`，应避免与用户本地网络、Docker 网络和远端 LAN 重叠。访问目标 LAN 既需要客户端 push route，也需要服务端转发与 LAN 回程路由或经审查的 NAT；**push route 不是授权或防火墙规则**。

先只放行必要子网。`redirect-gateway` 会改变客户端默认路由，须同时验证 DNS、出口与隐私需求。IPv6、全隧道、LAN 多网段及高并发未作完整现场验收。不要把容器内监听成功描述成 LAN/互联网可用。

## 14. 持久化数据

`./data:/data` 是 bind mount，数据在部署目录，不是镜像内部。必须整体保护：`ovpn.db`（及可能存在的 SQLite 附属文件）、`config.json`、`server.conf`、完整 `pki/`（CA/服务端/客户端私钥、证书、CRL、索引、tls-crypt key）、`clients/`、`ccd/`、`connections/` 与其他状态。

限制目录 `0700`、敏感文件 `0600`，勿加入 Git、镜像或公开备份。容器重建会复用它；改部署目录会改变相对 bind mount，必须核对实际挂载路径。残缺 PKI 会拒绝初始化，正确做法是恢复备份，不是重新生成已有 CA。

## 15. 备份与恢复

见 [BACKUP_RESTORE.md](BACKUP_RESTORE.md)：停服务获得一致性备份，归档 **Compose + 整个 data**；私有权限、校验摘要、验证归档可读并演练恢复。必须保护 CA、服务端和客户端密钥；仅备份数据库不足以恢复服务。恢复至新的、未启动的目录，不覆盖现有 data，保留原部署用于回滚。

## 16. 升级、迁移与回滚

见 [UPGRADE.md](UPGRADE.md)。升级前停机整体备份并记录旧版本/digest；只切换到经过审查的新版本 + digest，不自动拉 `stable`、不覆盖当前版本标签。旧数据库迁移、配置 allowlist 与加密旧密码兼容需单独核对；回滚使用成套旧镜像 + 升级前数据，不把旧应用直接接到已迁移的新库。

## 17. 日常运维与日志

```bash
cd /opt/openvpn
docker compose ps
docker compose logs --tail=100 openvpn
docker compose exec -T openvpn test -c /dev/net/tun
docker compose exec -T openvpn ss -lun
docker compose restart openvpn
```

日志仅在私密终端检查，外发前脱敏 IP、用户名、认证头、Cookie、密码、MFA、私钥及完整配置。Compose 新增容器级 `json-file` 日志轮转：`max-size: "10m"`、`max-file: "3"`，限制 Docker stdout/stderr 日志保留，不是应用所有数据/日志的总磁盘配额。已有容器需要重建才采用新日志设置；无需修改 Docker daemon 或重启宿主机 Docker。详细清单见 [docs/OPERATIONS.md](docs/OPERATIONS.md)。

## 18. 故障排查

| 现象 | 先检查 |
| --- | --- |
| Docker 连接失败/Compose 不存在 | Docker 服务状态、root/sudo 权限、是否安装官方 Compose 插件；不开放 Docker TCP socket |
| `no matching manifest` | 此发行仅 amd64；不要宣称 ARM 仿真已支持 |
| TUN 缺失或 Operation not permitted | 宿主机 `/dev/net/tun`、映射、NET_ADMIN；LXC/OpenVZ 还需上层允许 TUN 与相应权限，不直接改 privileged |
| Web 从公网打不开 | 默认就是 loopback；使用 SSH 隧道或受控 HTTPS 代理 |
| 登录循环/403 CSRF | 重新打开登录页，核对同一 Host/协议、Cookie、Origin、HTTPS Secure 设置及代理 Host；不关闭 CSRF |
| LDAP TLS 错误 | CA 链、域名、时钟、StartTLS 支持与证书有效期；不要关证书验证 |
| UDP 1194 启动冲突 | 其他服务占用端口、应用配置与 Compose 不一致；`ss -lun` 只能证明监听 |
| 连上却不能访问 LAN/互联网 | 网段冲突、客户端路由、push route、LAN 回程、分层转发/NAT、MTU/DNS；先做外部实测，不清空主机规则 |
| 吊销/CRL 失败 | 保留 PKI/索引/配置与恢复材料，查时钟/权限/CRL 错误后重试，不删 CA |
| 重建后像新安装 | 当前目录与 bind mount 是否变了；立即停机，恢复原路径，不继续生成新 CA |

## 19. LDAP 与安全限制

LDAP 设置入口存在，但**真实 LDAP 服务器未验收**。`ldaps://` 校验证书链与域名，`ldap://` 要求 StartTLS，没有明文回退；`LDAP_CA_FILE`/`LDAP_CA_DIR` 可挂载仅含公有 CA 证书的只读信任目录。默认 `LDAP_TLS_INSECURE_SKIP_VERIFY=false`，不要把 insecure override 当生产修复。

目录侧禁用或改密不保证立即撤销已有 Web 会话；旧加密密码需通过成功登录迁移或受控重置；同一用户/证书并发使用最新身份策略。配置跨文件发布有恢复路径，但不是多文件断电原子事务。Debian 包和浏览器依赖需持续审查，Go 扫描不覆盖全部组件。请读 [SECURITY.md](SECURITY.md)、[SECURITY_REVIEW_HARDENED.md](SECURITY_REVIEW_HARDENED.md) 和 [CHANGELOG.md](CHANGELOG.md)。没有“绝对安全”保证。

## 20. 停止与卸载（保留数据）

```bash
cd /opt/openvpn
docker compose stop
# 如需移除本项目容器与 Compose 网络：
docker compose down
```

`stop` 保留容器；`down` 移除容器和项目网络，**不会删除本例 bind mount 的 `./data`**。`down --volumes` 会删除适用的 Docker volumes，不用于本指南；它也不是清理 bind mount 的正确方式。保留部署目录和备份即可恢复。这里不提供自动删 data、系统级 prune 或卸载共享 Docker 的命令；永久销毁须另行确认影响与密钥保留策略。

## 21. 文档、源码与贡献

- [INSTALL.md](INSTALL.md)：全新 Debian 13 安装。
- [UPGRADE.md](UPGRADE.md)：升级、迁移与回滚。
- [BACKUP_RESTORE.md](BACKUP_RESTORE.md)：完整一致性备份与私密恢复。
- [DOCKERHUB_README.md](DOCKERHUB_README.md)、[docs/DOCKERHUB.md](docs/DOCKERHUB.md)：镜像介绍和发布溯源。
- [docs/OPERATIONS.md](docs/OPERATIONS.md)：生产操作入口与验收清单。

应用由审查过的源码多阶段编译，运行时为固定基础镜像的 Debian 13，不使用上游应用二进制。源构建入口是 `build/Dockerfile`，不是仓库根 Dockerfile；包仓库未做完整快照固定，重新构建不保证逐字节相同，不能以重建覆盖已发布版本。贡献前完整阅读 [AGENTS.md](AGENTS.md)，安全问题先向维护者请求私密渠道，勿公开凭据或可利用细节。
