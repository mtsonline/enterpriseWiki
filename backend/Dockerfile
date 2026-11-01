FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
WORKDIR /app/backend
RUN rm -f go.sum && go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/config.yaml ./config.yaml
COPY --from=builder /app/backend/internal/db/schema.sql ./schema.sql

EXPOSE 8080
CMD ["./server"]
