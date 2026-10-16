#!/bin/sh
# 「慎始」容器入口脚本。
#
# 根据 PUID / PGID 环境变量调整运行身份（默认 1000:100），并修正 /data 数据目录的
# 属主，从而让容器以非 root 身份运行的同时，仍能对绑定挂载进来的宿主目录拥有写权限
# （SQLite 以 WAL 模式打开，需要对 /data 本身可写，否则报 error 14:
# unable to open database file）。不设置时使用内置默认值；设置后容器内 shenshi 用户的
# uid/gid 会被改到对应值，并把 /data 重新归属给它，再降级身份执行实际命令。

set -e

PUID="${PUID:-1000}"
PGID="${PGID:-100}"

# 调整组 GID（与已有组冲突时忽略错误）。
if [ "$(id -g shenshi 2>/dev/null)" != "$PGID" ]; then
  groupmod -g "$PGID" shenshi 2>/dev/null || true
fi

# 调整用户 UID 并归入新 GID。
if [ "$(id -u shenshi 2>/dev/null)" != "$PUID" ]; then
  usermod -u "$PUID" -g "$PGID" shenshi 2>/dev/null || true
fi

# 数据目录及其内容归属到新身份；历史属主（如 root）残留会挡住 SQLite 写入。
chown -R "${PUID}:${PGID}" /data 2>/dev/null || true

# 以 shenshi 身份执行传入命令（默认 shenshi -addr :8787），并替换当前进程。
exec su-exec "${PUID}:${PGID}" "$@"
