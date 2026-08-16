FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /termchat-relay ./cmd/termchat-relay

FROM alpine:latest
WORKDIR /
COPY --from=builder /termchat-relay /termchat-relay

ENV PORT=8080
EXPOSE 8080

CMD ["/termchat-relay"]
