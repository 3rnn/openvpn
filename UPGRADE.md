# 版本升级、迁移与回滚

> 2026-10-25 发布补充：所有者已手动搭建本地环境并确认客户端连接成功，随后授权正式发布。此为用户报告，独立于 89 项自动隔离验收；自动测试的外部 VPN 项仍记为 NOT_TESTED。下文“未验证”网络范围指自动化/完整现场覆盖，不否定上述手动连接结果；不代表完整 DNS、LAN、MTU、吞吐、撤销或真实 LDAP 已验证。

生产部署使用 **版本 + digest**，不自动跟随 `stable`/`latest`。本次正式发行 `v1.1.0-hardened`，已发布内容不能重建覆盖；仓库历史上游 `v1.0.0` 标签不能改写。

```text
镜像：3rnn/openvpn:v1.1.0-hardened@sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683
构建提交：4db21ef895e34071a9dbd8f8878d9615488e1fa9
平台：linux/amd64
```

后续文档提交与构建提交应分开记录。文档更新本身不要求重建已发布镜像。升级不执行旧裸机安装器，不从作者下载应用二进制，不在生产主机通过 `git pull` 自动合并上游。

## 1. 升级前评估

1. 阅读目标版本的 CHANGELOG、SECURITY、迁移说明，确认平台、必需权限、端口及数据格式兼容。
2. 核对目标来源、不可变 tag/digest 与 OCI revision；新 digest 必须来自正式发行记录，不能猜测。
3. 记录部署目录、Compose 项目名、bind mount 源路径、当前版本 + digest 和额外 override/LDAP CA/代理配置。只记录路径和非秘密 metadata，不打印完整 config 或环境。
4. 检查磁盘空间与停机窗口，告知用户断线。预先验证新镜像能拉取，旧镜像仍可获得。
5. 按 [BACKUP_RESTORE.md](BACKUP_RESTORE.md) **停止服务并归档 Compose + 完整 data**，验证摘要与可解包性。迁移演练使用私密副本，不直接测试生产数据库。

```bash
cd /opt/openvpn
docker compose config --images
docker compose ps
CID=$(docker compose ps -q openvpn)
test -n "$CID"
docker inspect "$CID" --format '{{.Image}}'
docker inspect "$CID" --format '{{range .Mounts}}{{println .Source "->" .Destination}}{{end}}'
```

不要把 `docker inspect` 的完整输出发到工单，环境可能含秘密。成功的普通镜像拉取/容器监听不能替代升级验收。

## 2. 仅改变版本和 digest

**先完成停机备份**，然后手动审查并编辑 `docker-compose.yml` 中的 `image` 为目标 **版本 + digest**。保留原 data 映射、端口与最小已验证权限；不要直接拿其他版本 Compose 覆盖本地必要配置。

从旧加固部署升级到本发行时，image 行应为：

```yaml
image: 3rnn/openvpn:v1.1.0-hardened@sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683
```

未来发行需使用未来正式记录中的新版本和新 digest，不复用本标签。核对后：

```bash
set -eu
cd /opt/openvpn
docker compose config --quiet
docker compose config --images
docker compose pull
docker compose up -d --force-recreate openvpn
docker compose ps
```

`up` 可能执行应用数据库迁移，启动前备份不可省略。Compose 的 `json-file` 日志上限为每文件 10m、最多 3 文件，重建容器才生效；不改变已发布镜像，也不限制 data 内所有文件总量。不要改 Docker daemon 全局日志配置影响其他项目。

## 3. 验收与决定是否继续

- 用受保护管理入口验证管理员登录、MFA、旧会话撤销与用户操作。
- 核对原数据库、配置与 CA 连续性；生成与吊销合成客户端证书，确认 CRL 有效。
- 验证实际 UDP 端点、真实外部客户端连接、业务路由/DNS、LAN 回程和必要 NAT。
- 在保留备份的前提下确认重启/重建仍使用同一 data。
- LDAP 用户需与真实目录单独验证，检查证书链/域名/StartTLS，不因升级失败关闭 TLS 或 CSRF。

已有发行验收 **89 项通过**，但真实外部 VPN 握手和真实 LDAP 服务器未验证，不是现场升级保证。验收失败保留日志的脱敏摘要与数据，停止继续变更。

## 4. 安全回滚：恢复旧镜像与旧数据一整套

不要把旧镜像直接接到已被新版本迁移的数据库。维护窗口内：

```bash
cd /opt/openvpn
docker compose stop openvpn
test -z "$(docker compose ps --status running -q openvpn)"
```

保留当前目录用于排查，不覆盖、不删除。按 [BACKUP_RESTORE.md](BACKUP_RESTORE.md) 将**升级前归档**恢复到全新 `/opt/openvpn-restored`，包含归档时旧 Compose 与完整旧 data；校验摘要、CA/密钥、配置、数据库和私有权限，核对旧版本 + digest。只有升级实例已停止且端口无冲突时才启动恢复实例。

恢复指南用新目录形成独立 Compose 项目；切换后记录新的服务路径，后续操作必须在正确目录执行。若旧项目还存在但已停止，可在核对归档后使用 `docker compose down` 移除旧容器/项目网络，**不加 `--volumes`，不删旧 bind mount**。旧目录和故障数据保留至回滚验收完成。若决定恢复旧目录继续运行，先另行设计不覆盖现有 data 的切换方案，勿直接解包覆盖。

## 5. 从上游或旧裸机安装迁移

不是简单替换镜像的兼容承诺。先停止旧服务并私密备份其全部数据库、配置、完整 PKI 和客户端材料，保留原 CA。用隔离副本人工映射本项目 `/data` 布局，审查而非执行未知脚本。

必须注意：

- 首次初始化只适用于空目录；残缺 CA/PKI 会拒绝覆盖，不要删除它让程序“重新初始化”。
- 新设/重置本地密码使用 bcrypt；旧可逆加密密码只有成功认证后迁移，不能把未登录旧账户视作全部已迁移。配置与数据库必须成套备份。
- 不支持公开默认密码、找回原密码 API、关闭认证、任意脚本/plugin/include/路径类配置；管理端配置改为 allowlist，需要逐项审查旧自定义指令。
- `ldap://` 必须 StartTLS，LDAPS 必须校验证书；仅明文目录或无效证书不再默许。目录侧改密/禁用不保证立即撤销既有 Web 会话。
- 旧全局 `.ovip/.ovc` 策略交接不再使用；身份/证书策略、CCD 与权限需验证，不能机械复制不受信任的文件。
- root + 默认 capabilities + NET_ADMIN 与显式 TUN 是本发行已验证模型；不自动改 host network 或 privileged。

缺少数据格式依据或旧凭据时停止并请求维护者设计迁移方案，不借助重新生成 CA、默认密码重置或删除 data 达成“启动成功”。
