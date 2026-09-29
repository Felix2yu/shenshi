# 「慎始」运行时镜像：二进制由 CI 预编译并下载到 bin/ 后拼装。
#
# 编译期依赖（node_modules、Go 工具链）全部留在 CI，不进镜像。
# 前端产物已通过 go:embed 内嵌进二进制，镜像里不再有 web 目录。

FROM alpine:3.24

# tzdata 不是可选项：「慎始」的日期口径全部基于本机时区
# （今天/逾期/习惯打卡都按本地自然日计算），容器里没有时区库就会退化成 UTC。
RUN apk add --no-cache tzdata ca-certificates su-exec \
    && addgroup -S shenshi \
    && adduser -S -G shenshi -h /data shenshi

# 默认东八区；可用 TZ 覆盖（见 docker-compose.yml）
ENV TZ=Asia/Shanghai \
    SHENSHI_DB=/data/shenshi.db

COPY --chmod=755 bin/shenshi /usr/local/bin/shenshi
# 入口脚本：按 PUID/PGID 调整运行身份并修正 /data 属主
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# 数据落在挂载卷里，容器重建不丢数据
RUN mkdir -p /data && chown -R shenshi:shenshi /data
VOLUME ["/data"]

EXPOSE 8787

# 健康检查走 /api/health：该接口即使启用了访问口令也免鉴权，探活不会被 401 挡住
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O- http://127.0.0.1:8787/api/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["shenshi", "-addr", ":8787"]
