FROM golang:1.24 AS builder

WORKDIR /app

RUN go install github.com/swaggo/swag/cmd/swag@v1.8.12

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN swag init -g ./cmd/main.go

RUN CGO_ENABLED=0 GOOS=linux go build -o /crypto-service ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /crypto-service .
COPY --from=builder /app/config/config.yaml .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/docs ./docs

EXPOSE 8080
CMD ["./crypto-service"]