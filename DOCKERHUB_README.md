# OpenVPN Web 独立加固版

> 2026-11-10 `v1.1.2-hardened`：在 v1.1.1 基础上修复客户端配置：已有或已吊销的证书名称不再被静默复用（返回 409，提示使用新名称）；客户端列表只显示合法的 `.ovpn` 文件；clients 目录权限 0700。本版验证：`go vet`、`go test`（含两项新测试）、容器启动冒烟（OpenVPN 初始化与 tun0、`/login` 200、未登录 401/403、`ca.key` 600），以及容器内实际验证：吊销后复用同名被拒绝、新名称正常签发、重启后数据保留。真实客户端连接、转发与 LDAP 未在本版测试。
>
> 2026-11-10 `v1.1.1-hardened`：仅修复 Web 后台展示层（在线状态轮询、表格与弹窗、错误提示），未改 API、认证、证书、防火墙或网络逻辑。本版验证：`go test` 与 `go vet`、29 项浏览器回归、容器启动冒烟（OpenVPN 初始化与 tun0、`/login` 200、未登录 401/403、PKI 私钥权限 600、重启后数据保留）。本版未重跑 89 项隔离验收，真实客户端连接、转发与 LDAP 未在本版测试。
>
> `v1.1.0-hardened` 的 2026-10-25 发布补充：所有者已手动搭建本地环境并确认客户端连接成功，随后授权正式发布。此为用户报告，独立于 89 项自动隔离验收；自动测试的外部 VPN 项仍记为 NOT_TESTED。下文“未验证”网络范围指自动化/完整现场覆盖，不否定上述手动连接结果；不代表完整 DNS、LAN、MTU、吞吐、撤销或真实 LDAP 已验证。

面向 **Debian 13 + Docker** 的 OpenVPN 与 Web 管理面板，源自 `GavinTan/openvpn`，由 `3rnn/openvpn` 独立维护，非上游官方发行；保留 MIT 许可证。应用从审查过的源码编译，不下载上游应用二进制。

**正式发行 `v1.1.2-hardened`，仅 `linux/amd64`。**

```text
3rnn/openvpn:v1.1.2-hardened@sha256:0e637729aab170d31c3acd7a9f05cbf3335a94c52cf9ae00c9912dd71a92620c
构建提交 / OCI revision：d74321ec34139c4fcc8146f4c5e547ca4b666369
源码仓库：https://github.com/3rnn/openvpn
```

已发布版本不可重建覆盖。Git 正式标签应对应构建提交；更新的部署文档单独追溯。生产固定版本 + digest，不自动跟随可变 `stable`/`latest`。`stable` 当前（2026-11-10 起）指向 `v1.1.2-hardened` 同一 digest，仅作便利标签。

## 部署要点

- 使用仓库部署指南和 Compose，不只运行一个没有持久化与权限配置的 `docker run`。
- root + Docker 默认 capabilities + `NET_ADMIN`，显式 `/dev/net/tun`；不使用 privileged 或 host network。
- 默认 VPN `1194/udp`，Web 仅 `127.0.0.1:8833`；远程管理用 SSH 隧道或受控 HTTPS 代理。
- 必须持久化 `/data`，含数据库、配置、完整 PKI/CA、服务端和客户端密钥；备份按凭据保护。
- 首次用户名 `admin`，随机密码在 `/data/bootstrap-admin-password`（`0600`），首次登录强制改密；不是公开默认密码。
- SSH 隧道浏览器本地 HTTP 不启用 Secure Cookie；真正 HTTPS 才启用。
- Compose 容器日志 `json-file`：`max-size: "10m"`、`max-file: "3"`。

## 验证边界

发行验收记录 **89 项通过**，覆盖容器内 OpenVPN/TUN/UDP、登录/MFA/CSRF/会话撤销、证书签发/吊销/CRL 恢复和持久化。**真实外部 VPN 握手/转发与真实 LDAP 服务器未验证**，端口监听不等于现场 VPN 可用，不承诺绝对安全或吞吐容量。

## 完整文档

源码仓库中的 `README.md`、`INSTALL.md`、`UPGRADE.md`、`BACKUP_RESTORE.md` 提供部署、升级、成套恢复和保留数据卸载步骤；`SECURITY.md` 与 `SECURITY_REVIEW_HARDENED.md` 记录安全假设和剩余风险。现有 CA 不随意重建，不公开上传 data、密码或私钥；安全问题先请求私密渠道。
