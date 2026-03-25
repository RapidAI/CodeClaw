FROM golang:1.24 AS go-builder

WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/dangbei-api ./cmd/server

FROM debian:bookworm-slim

WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
ENV TZ=Asia/Shanghai

COPY --from=go-builder /out/dangbei-api /usr/local/bin/dangbei-api

EXPOSE 8080

CMD ["dangbei-api"]
