# Debian 13 全新安装指南

> 2026-10-25 发布补充：所有者已手动搭建本地环境并确认客户端连接成功，随后授权正式发布。此为用户报告，独立于 89 项自动隔离验收；自动测试的外部 VPN 项仍记为 NOT_TESTED。下文“未验证”网络范围指自动化/完整现场覆盖，不否定上述手动连接结果；不代表完整 DNS、LAN、MTU、吞吐、撤销或真实 LDAP 已验证。

适用：全新 Debian 13 / trixie amd64，rootful Docker。仅发行 `linux/amd64` 镜像。1 CPU / 1 GB RAM / 8 GB 可用磁盘为起步建议，2 CPU / 2 GB / 16 GB 为生产规划建议，均非压测结果。已有部署转 [UPGRADE.md](UPGRADE.md)。

以下命令由管理员在 root shell 分段执行（普通管理员先 `sudo -i`）；它们会安装系统包、启动 Docker 并在部署阶段创建容器。请先理解影响，不在共享生产主机未经评估执行。本文编写时已查阅 Docker 官方 Debian 安装页；本次文档验证不在宿主机执行 APT、修改网络或安装服务。

官方资料地址：

```text
https://docs.docker.com/engine/install/debian/
https://docs.docker.com/engine/logging/drivers/json-file/
```

## 1. 核对系统、权限和已有服务

```bash
id
cat /etc/os-release
dpkg --print-architecture
test "$(. /etc/os-release; printf '%s' "$ID:$VERSION_ID")" = 'debian:13'
test "$(dpkg --print-architecture)" = amd64
```

不是 Debian 13 amd64 就停止，不依赖 CPU 仿真。确认主机没有承载需保留的其他 Docker/containerd 服务。Docker 官方列出的潜在冲突包包括 `docker.io`、`docker-compose`、`docker-doc`、`docker-buildx`、`podman-docker`、`containerd` 和 `runc`。只检查，不自动卸载：

```bash
dpkg-query -W docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc 2>/dev/null || true
```

若存在冲突，由管理员评估现有容器和依赖、备份后按 Docker 官方文档处理；不要批量删除其他业务组件。下文假设全新无冲突环境。

## 2. 配置 Docker 官方 APT 源

不使用一键脚本、不使用第三方镜像源、不下载作者应用二进制。确认没有需保留的同名源文件；若有则先审查，不覆盖：

```bash
set -eu
test ! -e /etc/apt/sources.list.d/docker.sources
test ! -e /etc/apt/keyrings/docker.asc
apt update
apt install -y ca-certificates curl git less
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $(. /etc/os-release && printf '%s' "$VERSION_CODENAME")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
apt update
```

## 3. 安装 Engine 与 Compose

```bash
apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
systemctl is-active docker
```

若 Docker 未运行，先查明原因；确认可以启动后手动 `systemctl start docker`。不自动修改 daemon 配置。核对 CLI 和 Compose 插件：

```bash
docker version
docker compose version
docker run --rm hello-world
```

`hello-world` 是 Docker 官方推荐的安装冒烟测试，会联网拉取验证镜像；它不是本项目运行验收。APT 安装获得执行时仓库提供的版本，生产应按维护窗口验证升级。不要随意把用户加入 docker 组或开放无认证 Docker API。

## 4. 检查 TUN、网络与存储

```bash
test -c /dev/net/tun
ls -l /dev/net/tun
ss -lunt
df -h /opt
```

应能看到字符设备，UDP 1194 与 TCP 8833 不应被其他项目冲突占用。若 TUN 不存在，向 VPS/LXC 提供方确认内核及设备透传；本指南不自动创建节点、加载模块、改 LXC 配置或切换 privileged。LXC 中即使文件存在也可能被上层权限阻止。

先规划无重叠 VPN 网段、客户端可达的公网地址与 UDP 1194、安全组和回程路径。Docker 官方提醒端口发布可能绕过 UFW/firewalld 的常规规则。这里只核对，不改宿主机 firewall/sysctl；是否需要 forwarding/NAT、在哪一层配置，须按实际网络单独审查。禁止清空宿主机规则。

## 5. 获取部署文件并启动固定镜像

`/opt/openvpn` 应不存在；不克隆上游，不执行仓库旧安装器。

```bash
set -eu
umask 077
test ! -e /opt/openvpn
git clone --branch security/hardened-v1 --single-branch https://github.com/3rnn/openvpn.git /opt/openvpn
cd /opt/openvpn
git rev-parse HEAD
install -d -m 0700 data
docker compose config --quiet
docker compose config --images
docker compose pull
docker compose up -d
docker compose ps
```

显式克隆加固文档分支，不能假设 `main` 已含本版文件。保存文档提交 ID；正式发布后使用发布说明提供的固定文档入口。本 Compose 必须显示以下不可变镜像：

```text
3rnn/openvpn:v1.1.0-hardened@sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683
```

构建提交固定为 `4db21ef895e34071a9dbd8f8878d9615488e1fa9`，后续文档提交不同是正常的，不因此重建已发布镜像。核对拉取后的 metadata：

```bash
docker image inspect 3rnn/openvpn:v1.1.0-hardened@sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683 \
  --format '{{.Os}}/{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
```

预期平台 `linux/amd64` 和上述构建提交；不一致就停止并检查，不自行替换 digest。

## 6. 首次私密登录

默认用户名 **`admin`**。自动生成的初始密码只在 `/data/bootstrap-admin-password`（宿主机 `/opt/openvpn/data/bootstrap-admin-password`），不是 Docker 日志里的默认密码。仅在未录屏、不记录输出的私密终端执行：

```bash
less -- /opt/openvpn/data/bootstrap-admin-password
```

在管理员电脑建立隧道：

```bash
ssh -N -L 18833:127.0.0.1:8833 operator@SERVER_IP
```

浏览器访问 `http://127.0.0.1:18833`，完成强制首次改密，确认重新登录，再启用 MFA。此路径是本地 HTTP，经 SSH 加密传输；不要启用 HTTPS-only Secure Cookie。新密码确认可用后管理员可 `unlink /opt/openvpn/data/bootstrap-admin-password`；不要把密码或文件内容提交到报告。

长期 HTTPS 管理参考 [README 第 10 节](README.md#10-https-生产管理入口)：仅对真正的 HTTPS 启用 Secure，代理保留外部 Host，限制来源。

## 7. 首次上线检查

- [ ] Web 只映射到 `127.0.0.1:8833`，UDP 1194 暴露符合批准范围。
- [ ] root + Docker 默认 capabilities + NET_ADMIN、显式 TUN；无 privileged/host network。
- [ ] `/opt/openvpn/data` 为私有持久化目录，容器重建后保留同一数据库和 CA。
- [ ] 初始密码已更改、文件按策略移除、MFA 已配置。
- [ ] 实际生成、下载、撤销合成测试客户端证书；客户端材料未外泄。
- [ ] 从真实外部客户端测试握手、DNS、业务路由、断线重连和吊销，确认 LAN 回程/需要的 NAT。
- [ ] 按 [BACKUP_RESTORE.md](BACKUP_RESTORE.md) 备份并演练恢复。

发行记录 89 项通过是已有隔离验收结果，**不代表你的网络或真实 LDAP 已验收**。真实外部 VPN 握手、转发及真实 LDAP 集成本次发行未验证。运维、升级和保留数据卸载入口见 [README](README.md) 与 [docs/OPERATIONS.md](docs/OPERATIONS.md)。
