# Build Stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install git for go mod
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod ./
# go.sum might not exist if deleted, but copy if it does
COPY go.sum* ./

# Download dependencies
# Force clean go.sum if needed in docker context? 
# In CI we delete it. Here we just tidy.
RUN go mod tidy

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o aether-gateway ./cmd/aether-gateway

# Final Stage
FROM alpine:latest

WORKDIR /app

# Install CA certificates for TLS
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /app/aether-gateway .

# Expose port (WebTransport uses UDP/443 usually, but our app defaults to 4433)
EXPOSE 4433/udp
EXPOSE 4433/tcp

# Entrypoint
ENTRYPOINT ["./aether-gateway"]
