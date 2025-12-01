# Use a small base image
FROM alpine:latest

# Add certificates
RUN apk add --no-cache ca-certificates

# Set working directory
WORKDIR /app

# Copy the binary from the build stage
COPY usque .
COPY config.json .

# Expose the web UI port
EXPOSE 8080

# Run the binary
ENTRYPOINT ["./usque"]
