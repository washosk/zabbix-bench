FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o zabbix-bench .

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata && \
    adduser -D -u 10001 zabbixbench

WORKDIR /app

COPY --from=builder /src/zabbix-bench /usr/local/bin/zabbix-bench

USER zabbixbench

ENTRYPOINT ["zabbix-bench"]
