# 「慎始」运行时镜像：二进制由 CI 预编译并下载到 bin/ 后拼装。
#
# 编译期依赖（node_modules、Go 工具链）全部留在 CI，不进镜像。
# 前端产物已通过 go:embed 内嵌进二进制，镜像里不再有 web 目录。
#
# 基镜 ghcr.io/felix2yu/base:alpine-3.24 已提供 ca-certificates、tzdata、su-exec
# 与 PUID/PGID 降权入口（ENTRYPOINT /usr/local/bin/docker-entrypoint.sh）：
# 因此这里不再 apk add，也不再自带 scripts/docker-entrypoint.sh。
# 契约见 Felix2yu/.github/docs/docker.md。

FROM ghcr.io/felix2yu/base:alpine-3.24

# 数据落在挂载卷里，容器重建不丢数据；/data 由基镜入口按 PUID/PGID chown。
# ca-certificates：外呼 HTTPS 依赖系统根证书。
ENV SHENSHI_DB=/data/shenshi.db

COPY --chmod=755 bin/shenshi /usr/local/bin/shenshi

RUN mkdir -p /data
VOLUME ["/data"]

EXPOSE 8787

# 健康检查走 /api/health：该接口即使启用了访问口令也免鉴权，探活不会被 401 挡住
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O- http://127.0.0.1:8787/api/health >/dev/null || exit 1

# ENTRYPOINT 由基镜提供（PUID/PGID 降权后 exec 本命令），这里只给服务命令
CMD ["shenshi", "-addr", ":8787"]
