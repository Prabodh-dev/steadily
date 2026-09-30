FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/steadily ./cmd/steadily
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/echo-backend ./backends/echo

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/steadily /app/steadily
COPY --from=builder /app/echo-backend /app/echo-backend
EXPOSE 8080 9090
CMD ["/app/steadily", "-config", "/app/steadily.yaml"]
