FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go mod tidy \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backup-chat .

FROM alpine:3.20

RUN addgroup -S backupchat && adduser -S -G backupchat backupchat \
    && mkdir -p /data \
    && chown backupchat:backupchat /data

WORKDIR /app
COPY --from=builder /out/backup-chat /app/backup-chat

ENV PORT=50000 \
    DATA_FILE=/data/messages.jsonl \
    RETENTION_DAYS=30
EXPOSE 50000
VOLUME ["/data"]
USER backupchat
ENTRYPOINT ["/app/backup-chat"]
