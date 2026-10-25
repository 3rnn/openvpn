# 一致性备份与恢复

适用根目录 `docker-compose.yml` 的 `./data:/data` bind mount。命令在 root shell 执行；按实际部署路径调整。备份包含私钥、数据库和应用密钥，**等同服务控制权**：不公开上传、不进入 Git、工单或日志；应另做加密离线副本并独立保管解密材料。本指南没有自动删数据的步骤。

## 1. 必须保存的完整状态

不要只导出数据库或几个证书，必须备份 **实际部署 Compose 文件 + 整个 data 目录**：

- `data/config.json`：应用 secret、管理员与会话相关配置、服务端身份和设置；它与数据库成套恢复。
- `data/ovpn.db` 及可能存在的 SQLite journal/WAL/SHM 附属文件：用户、会话等状态；停机后整体归档，不热拷贝单个 `.db`。
- `data/server.conf` 与其他运行配置。
- 完整 `data/pki/`：**CA 私钥和 CA 证书、服务端私钥/证书、客户端私钥/证书、tls-crypt key、索引、serial、CRL 及 EasyRSA 状态**。
- `data/clients/` 内联私钥配置、`ccd/`、`connections/` 与其他持久化文件，包含仍保留的首次初始化密码文件。
- 若有额外 Compose override、`.env`、只读 LDAP CA 挂载、代理配置等，应单独列入受保护的备份清单并验证恢复；以下标准命令仅针对仓库默认 Compose，无额外文件。

旧 CA **不得随意重新生成**。CA 不同会使现有客户端失去信任；丢失私钥无法靠重拉镜像找回。

## 2. 停机并制作完整归档

维护窗口内断开业务连接。先确认实际挂载目录；输出仅含路径，不打印配置或环境变量：

```bash
set -eu
umask 077
cd /opt/openvpn
CID=$(docker compose ps -q openvpn)
test -n "$CID"
docker inspect "$CID" --format '{{range .Mounts}}{{println .Source "->" .Destination}}{{end}}'
```

确保 `/data` 确实来自 `/opt/openvpn/data`，没有另一项目在写该目录，然后执行：

```bash
set -eu
umask 077
cd /opt/openvpn
install -d -m 0700 /root/openvpn-backups
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
BACKUP_DIR=$(mktemp -d "/root/openvpn-backups/${STAMP}.XXXXXXXX")
docker compose stop openvpn
test -z "$(docker compose ps --status running -q openvpn)"
test -f docker-compose.yml
test -d data
docker compose config --images > "$BACKUP_DIR/image-reference.txt"
tar --numeric-owner -C /opt/openvpn -cpf "$BACKUP_DIR/deployment.tar" docker-compose.yml data
chmod 0600 "$BACKUP_DIR/deployment.tar" "$BACKUP_DIR/image-reference.txt"
(cd "$BACKUP_DIR" && sha256sum deployment.tar image-reference.txt > SHA256SUMS)
(cd "$BACKUP_DIR" && sha256sum -c SHA256SUMS)
tar -tf "$BACKUP_DIR/deployment.tar" >/dev/null
printf 'Backup directory: %s\n' "$BACKUP_DIR"
```

`mktemp -d` 创建独有私有目录，避免意外覆盖历史备份。若任何命令失败，不把结果标为有效备份；服务保持停止，先检查归档和数据。`sha256sum -c` 只验证完整性，不证明来源可信、密钥正确或可以成功运行业务。需要下节恢复演练。

若本次只是备份，成功后可恢复服务：

```bash
cd /opt/openvpn
docker compose start openvpn
docker compose ps
```

若为了升级/迁移停机，不要在旧部署上重新启动，继续升级计划。备份文件要加密复制到异地/离线介质，限制访问并定期抽查可解密性，切勿发送归档内容给维护者。

## 3. 恢复到全新、停止的目录

先安排维护窗口并停止旧部署，避免两个实例同时使用相同 CA/身份或占用端口。**不覆盖现有 data**；保留旧目录、旧 Compose、旧镜像和备份，便于回滚。恢复目录必须尚不存在。

以下 `BACKUP_DIR` 必须替换为自己核对过的可信备份目录，且命令只用于本指南产生的归档；不解包来源不明的 tar（即使摘要匹配也不可信）。

```bash
set -eu
umask 077
BACKUP_DIR=/root/openvpn-backups/REPLACE_WITH_VERIFIED_BACKUP
RESTORE_DIR=/opt/openvpn-restored
test -d "$BACKUP_DIR"
(cd "$BACKUP_DIR" && sha256sum -c SHA256SUMS)
tar -tf "$BACKUP_DIR/deployment.tar" >/dev/null
test ! -e "$RESTORE_DIR"
install -d -m 0700 "$RESTORE_DIR"
tar --numeric-owner --same-owner --same-permissions -C "$RESTORE_DIR" -xpf "$BACKUP_DIR/deployment.tar"
cd "$RESTORE_DIR"
test -f docker-compose.yml
test -f data/config.json
test -f data/ovpn.db
test -f data/pki/ca.crt
test -f data/pki/private/ca.key
chown -R root:root data
find data -type d -exec chmod 0700 {} +
find data -type f -exec chmod 0600 {} +
chmod 0600 docker-compose.yml
docker compose config --quiet
docker compose config --images
```

只对管理员自己管理、可信的恢复数据执行以上权限修复；拒绝含意外符号链接/特殊文件的归档并排查。不要把 data chown 成普通用户或 `chmod -R 777`。如迁移到非默认存储体系，先重新审查所有权/权限。

## 4. 核对归档内容，再启动

仍然保持恢复服务停止，比较归档中的整个 data 与恢复结果（不是只比较文件名）：

```bash
tar --compare --numeric-owner -f "$BACKUP_DIR/deployment.tar" -C "$RESTORE_DIR" data
```

对标准部署，成功时不输出差异；若旧备份的权限本来不私有，前面的权限修复会导致 mode 差异，需逐项审查记录，不能忽略内容差异。核对 CA、服务端和客户端私钥/证书、数据库与 config 的摘要或可信清单；不要输出其内容。记录并确认旧镜像版本 + digest，保持成套恢复。应用首次启动后可能写状态，此时不能再期望与停机归档逐字节一致。

旧部署已停止、端口不会冲突并确认恢复数据正确后才执行：

```bash
cd /opt/openvpn-restored
docker compose pull
docker compose up -d
docker compose ps
```

从私密管理入口验证登录、MFA、已有用户、同一 CA、证书/CRL 与实际外部 VPN 连接。隔离演练可只解包、比对且不启动网络；若要运行第二实例，必须单独规划隔离网络和端口，不能让它复用生产端点。

## 5. 回滚与保留策略

若恢复验收失败，停止恢复实例，不删数据或改 CA。保持旧数据原样，只有旧镜像与其兼容旧数据配套时才重启旧部署；数据库已迁移则从升级前备份另行恢复，见 [UPGRADE.md](UPGRADE.md)。

保留最近多个已验证备份、升级前备份以及对应版本/digest/Compose；定期演练恢复与加密副本解密。删除旧归档或永久销毁私钥应走独立授权流程，不通过系统级 prune 或无确认递归删除完成。
