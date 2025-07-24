FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o sbs-logger cmd/sbs-logger/main.go

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/sbs-logger /app/sbs-logger
COPY .env /app/.env

RUN mkdir -p /app/logs

ENV TZ=America/Sao_Paulo

CMD ["/app/sbs-logger"]