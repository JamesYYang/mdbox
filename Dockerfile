# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# web/ 通过 go:embed 打进二进制
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mdbox .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S mdbox && adduser -S -G mdbox mdbox \
    && mkdir -p /data /config && chown mdbox:mdbox /data /config
COPY --from=build /out/mdbox /usr/local/bin/mdbox
USER mdbox
# /data：文档存储；/config：config.yaml 与 users.yaml（含密码哈希/token）。两者都应挂载持久卷。
VOLUME ["/data", "/config"]
EXPOSE 8080
ENTRYPOINT ["mdbox", "-addr", ":8080", "-data", "/data", "-config", "/config/config.yaml", "-users", "/config/users.yaml"]
