# 多阶段构建让同一个 CMDB 应用镜像同时提供控制台与 API。
FROM node:20-alpine AS frontend-builder
WORKDIR /src/frontend
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM golang:1.25.1-alpine AS backend-builder
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -o cmdb-server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -o cmdb-init-admin ./cmd/init-admin

FROM alpine:3.22
RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Shanghai \
    GIN_MODE=release \
    SERVER_PORT=80 \
    STATIC_DIR=/app/web
WORKDIR /app
COPY --from=backend-builder /src/backend/cmdb-server ./cmdb-server
COPY --from=backend-builder /src/backend/cmdb-init-admin ./cmdb-init-admin
COPY --from=frontend-builder /src/frontend/dist ./web
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD wget -q -O /dev/null http://127.0.0.1/health || exit 1
CMD ["./cmdb-server"]
