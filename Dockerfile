# Cortex — Graph-based memory for AI coding tools
# Multi-stage build

# Stage 1: Build
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /bin/cortex .

# Stage 2: Runtime
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata git

COPY --from=builder /bin/cortex /usr/local/bin/cortex

EXPOSE 8741

ENTRYPOINT ["cortex"]
CMD ["serve"]
