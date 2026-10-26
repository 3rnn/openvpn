# Docker Hub 发布资料与溯源

> 2026-10-25 发布补充：所有者已手动搭建本地环境并确认客户端连接成功，随后授权正式发布。此为用户报告，独立于 89 项自动隔离验收；自动测试的外部 VPN 项仍记为 NOT_TESTED。下文“未验证”网络范围指自动化/完整现场覆盖，不否定上述手动连接结果；不代表完整 DNS、LAN、MTU、吞吐、撤销或真实 LDAP 已验证。

## 简短描述

```text
独立维护的 OpenVPN + Web 加固版；Debian 13，amd64，源码构建，固定 digest 部署。
```

Docker Hub 完整描述使用根目录 [DOCKERHUB_README.md](../DOCKERHUB_README.md)。用户安装从 [INSTALL.md](../INSTALL.md) 开始，本文件不替代部署说明。

## 不可变发布记录

| 字段 | 值 |
| --- | --- |
| 仓库 | `3rnn/openvpn` |
| 正式版本 | `v1.1.0-hardened` |
| 构建提交 / OCI revision | `4db21ef895e34071a9dbd8f8878d9615488e1fa9` |
| 平台 | `linux/amd64` |
| 已发布 digest | `sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683` |

```text
3rnn/openvpn:v1.1.0-hardened@sha256:fa5ff5f042e08d7f9ca9fe48b4faeb07609eadb0b05ba076d16193cb1e75e683
```

上游历史 `v1.0.0` 已存在，保持原状；正式加固发行继续用 `v1.1.0-hardened`。发布者核对 Git 正式标签指向构建提交。后续部署文档提交与构建提交分别记录，不用较新的文档提交伪装镜像 OCI revision，不重新构建覆盖已发布版本。

## 发布核对（维护者操作）

1. 发行证据记录 89 项验收通过，且安全审查没有未解决的发行阻断项。明确真实外部 VPN 握手/转发和真实 LDAP 尚未验证。
2. 只读核对版本 manifest digest、`linux/amd64` 与镜像 revision；不能把本地 image ID 当 registry digest。
3. 同步根安装、升级、备份文档及发布说明的固定文档入口，不把构建提交里的旧说明当新文档。
4. `stable` 是可变便利标签，只有所有者单独授权且门禁通过后才可指向**现有同一镜像内容**；不是新构建，不改变版本 digest。本文件不宣称 stable 已发布。
5. 发布后读取准确远程目标并记录验证结果。认证本身不证明发布成功；不得把 token、密码、私钥或完整认证配置放入命令输出、文档和报告。

生产 Compose 始终固定版本 + digest，即使 stable 获批也不改为自动跟随。无需重建本次镜像、无需 UI 版本改动。
