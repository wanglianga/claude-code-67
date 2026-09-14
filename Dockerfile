# ---------- 前端构建 ----------
FROM node:20-alpine AS frontend
WORKDIR /fe
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---------- 后端构建 ----------
FROM golang:1.23-alpine AS backend
ENV GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
WORKDIR /app
COPY backend/go.mod ./
COPY backend/ ./
# 用前端产物覆盖占位 dist（go:embed 需要）
RUN rm -rf ./dist
COPY --from=frontend /fe/dist ./dist
RUN go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o server .

# ---------- 运行时 ----------
FROM alpine:3.20
RUN adduser -D -u 10001 appuser && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /app/server ./server
USER appuser
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=25s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/api/health || exit 1
CMD ["./server"]
