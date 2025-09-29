# Dockerfile for building and running a Go bot

# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./

# Load dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install certificates
RUN apk --no-cache add ca-certificates tzdata

# Create a non-root user and group
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup
WORKDIR /app
COPY --from=builder /app/main .
RUN mkdir -p /app/data
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# For future (?)
EXPOSE 8080

# Run the Go app
CMD ["./main"]
