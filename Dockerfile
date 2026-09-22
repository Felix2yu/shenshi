# 「慎始」镜像：三阶段构建 —— 前端产物 → Go 二进制 → 极简运行层。
#
# 最终镜像只含一个静态链接的可执行文件与 tzdata，约 20MB。
# 编译期依赖（node_modules、Go 工具链）全部留在前面的阶段，不进最终镜像。

# ---------- 1. 前端 ----------
# 与本地开发同版本：Node 24（见根目录 .nvmrc 与 web/package.json 的 engines）。
FROM node:24-alpine AS web

WORKDIR /app/web

# 先只拷贝依赖清单，让 npm ci 这层能被缓存住。
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build


# ---------- 2. 后端 ----------
# 同样与本地对齐：Go 1.27（go.mod 声明 go 1.27.1）。
FROM golang:1.27-alpine AS server

# SQLite 驱动是纯 Go 实现的（modernc.org/sqlite），因此无需 gcc / CGO。
WORKDIR /app/server

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
# 前端产物必须落在 server/dist：main.go 用 go:embed all:dist 打包它。
COPY --from=web /app/web/dist ./dist

# CGO_ENABLED=0 + 静态链接，让二进制可以直接跑在 alpine 上。
# -trimpath 去掉构建机的绝对路径，-s -w 去掉符号表与调试信息。
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shenshi .


# ---------- 3. 运行 ----------
FROM alpine:3.24

# tzdata 不是可选项：「慎始」的日期口径全部基于本机时区
# （今天/逾期/习惯打卡都按本地自然日计算），容器里没有时区库就会退化成 UTC，
# 于是东八区的晚八点会被算成第二天。ca-certificates 供将来对接外部服务时使用。
RUN apk add --no-cache tzdata ca-certificates \
    && addgroup -S shenshi \
    && adduser -S -G shenshi -h /data shenshi

# 默认东八区；可用 TZ 覆盖（见 docker-compose.yml）。
ENV TZ=Asia/Shanghai \
    SHENSHI_DB=/data/shenshi.db

COPY --from=server /out/shenshi /usr/local/bin/shenshi

# 数据落在挂载卷里，容器重建不丢数据。
RUN mkdir -p /data && chown -R shenshi:shenshi /data
VOLUME ["/data"]

USER shenshi
EXPOSE 8787

# 健康检查走 /api/health：该接口即使启用了访问口令也免鉴权，探活不会被 401 挡住。
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O- http://127.0.0.1:8787/api/health >/dev/null || exit 1

ENTRYPOINT ["shenshi"]
CMD ["-addr", ":8787"]
