# 生产运维入口

> 2026-10-25 发布补充：所有者已手动搭建本地环境并确认客户端连接成功，随后授权正式发布。此为用户报告，独立于 89 项自动隔离验收；自动测试的外部 VPN 项仍记为 NOT_TESTED。下文“未验证”网络范围指自动化/完整现场覆盖，不否定上述手动连接结果；不代表完整 DNS、LAN、MTU、吞吐、撤销或真实 LDAP 已验证。

- 新服务器：[INSTALL.md](../INSTALL.md)
- 版本切换、迁移与回滚：[UPGRADE.md](../UPGRADE.md)
- 停机一致性归档、私有权限恢复、CA 连续性：[BACKUP_RESTORE.md](../BACKUP_RESTORE.md)
- 网络、认证、故障与保留数据卸载：[README.md](../README.md)
- 安全假设和剩余风险：[SECURITY.md](../SECURITY.md)、[SECURITY_REVIEW_HARDENED.md](../SECURITY_REVIEW_HARDENED.md)

## 操作前先确认目标

在实际部署目录执行，不在新克隆的任意目录直接 `up`。默认部署 `/opt/openvpn`，`./data` 是 bind mount；换目录就可能切换到另一个空数据目录。

```bash
cd /opt/openvpn
docker compose config --quiet
docker compose config --images
docker compose ps
```

生产镜像固定 `3rnn/openvpn:v1.0.0-hardened@sha256:2dc7fc01e3ffe7b458266cde4bc101e14b4f7063cbb5229bcb0bbf501a993c66`，构建提交 `35204c82885e87e1165fe1dda24a22f141ead303`，仅 amd64。不要重建覆盖版本，也不自动拉取 stable。

## 例行检查

```bash
cd /opt/openvpn
docker compose logs --tail=100 openvpn
docker compose exec -T openvpn test -c /dev/net/tun
docker compose exec -T openvpn ss -lun
```

仅在私密终端查日志，外发前脱敏。不要输出完整 `config.json`、`.env`、完整容器 inspect、客户端 `.ovpn`、Cookie 或认证头。定期检查备份恢复可用性、磁盘、证书/CRL 有效期、MFA 与撤销流程，并按维护窗口跟踪 OS/浏览器依赖安全更新。

新增 Docker 容器级日志轮转 `json-file` / `max-size: "10m"` / `max-file: "3"`。这只约束 stdout/stderr 日志文件保留；`data` 中数据库、应用日志和其他文件仍需容量监控。已运行旧容器需要在备份与维护窗口后重建才能采用该设置，无需更改 Docker daemon 全局参数。

## 重启、停止与移除

```bash
cd /opt/openvpn
docker compose restart openvpn
# 停止，保留容器：
docker compose stop openvpn
# 移除容器和项目网络，保留 ./data：
docker compose down
```

`restart` 不应用 Compose 变更；变更后按升级流程 `up -d --force-recreate`，先备份。不要加 `--volumes`，也不使用 prune 或自动删 data；上述默认 bind mount 数据应保留用于恢复。

## 现场网络验收不能省略

已有记录 89 项通过；真实外部客户端 VPN 握手/数据转发、现场 LAN/NAT/MTU 和真实 LDAP 未验证。检查公网 UDP 1194、安全组、容器 TUN/默认 capabilities + NET_ADMIN、网段不重叠、push route、回程及必要 NAT。不要为排障清空宿主机 firewall、改 host network/privileged、禁用 CSRF 或 LDAP TLS 校验。

SSH 隧道管理在浏览器侧是本地 HTTP，不启用 Secure Cookie；HTTPS 管理才启用 Secure，反代保留实际外部 Host 与端口，来源限制单独管理。公网不直接开放默认 HTTP 8833。
