# Build stage
FROM golang:1.26.1-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -o bin/server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -o bin/northbound-gateway ./cmd/northbound-gateway

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/bin/server .
COPY --from=builder /app/bin/northbound-gateway .

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./server"]
