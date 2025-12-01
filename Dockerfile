# --- Build Stage ---
FROM golang:1.24-alpine as builder

WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the statically linked binary
# CGO_ENABLED=0 is important for a static build
# -ldflags="-s -w" strips debug information to reduce binary size
RUN CGO_ENABLED=0 go build -o /app/usque -ldflags="-s -w" .

# --- Final Stage ---
FROM alpine:latest

# Add certificates
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the binary from the build stage
COPY --from=builder /app/usque .

# Expose the web UI port
EXPOSE 8080

# Declare a volume for persistent data (e.g., config.json)
VOLUME /app

# Run the binary
ENTRYPOINT ["./usque"]
