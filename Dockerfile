# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o pgserver .

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/pgserver .

# Copy example config
COPY config.yaml.example ./config.yaml.example

# Create data directory
RUN mkdir -p /data

# Expose PostgreSQL port
EXPOSE 5432

# Set default environment variables
ENV PG_PORT=5432
ENV PG_HOST=0.0.0.0
ENV PG_USER=postgres
ENV PG_PASSWORD=postgres
ENV DB_NAME=myapp
ENV STORAGE=local
ENV LOG_LEVEL=info

# Run the server
CMD ["./pgserver"]
