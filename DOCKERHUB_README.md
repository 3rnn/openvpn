# OpenVPN Web 独立加固版

> 2026-10-25 发布补充：所有者已手动搭建本地环境并确认客户端连接成功，随后授权正式发布。此为用户报告，独立于 89 项自动隔离验收；自动测试的外部 VPN 项仍记为 NOT_TESTED。下文“未验证”网络范围指自动化/完整现场覆盖，不否定上述手动连接结果；不代表完整 DNS、LAN、MTU、吞吐、撤销或真实 LDAP 已验证。

面向 **Debian 13 + Docker** 的 OpenVPN 与 Web 管理面板，源自 `GavinTan/openvpn`，由 `3rnn/openvpn` 独立维护，非上游官方发行；保留 MIT 许可证。应用从审查过的源码编译，不下载上游应用二进制。

**正式发行 `v1.0.0-hardened`，仅 `linux/amd64`。**

```text
3rnn/openvpn:v1.0.0-hardened@sha256:2dc7fc01e3ffe7b458266cde4bc101e14b4f7063cbb5229bcb0bbf501a993c66
构建提交 / OCI revision：35204c82885e87e1165fe1dda24a22f141ead303
源码仓库：https://github.com/3rnn/openvpn
```

已发布版本不可重建覆盖。Git 正式标签应对应构建提交；更新的部署文档单独追溯。生产固定版本 + digest，不自动跟随可变 `stable`/`latest`。

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
