# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o terrakubed cmd/terrakubed/main.go

# Final Stage
FROM alpine:3.19

WORKDIR /app

# Install dependencies required by Registry (git, openssh-client)
# and Executor (curl, unzip, bash)
RUN apk add --no-cache git openssh-client curl unzip bash ca-certificates

# Ensure cache directory exists and is writable for Terraform
RUN mkdir -p /home/app/.terrakube/terraform-versions && \
  chmod -R 777 /home/app

COPY --from=builder /app/terrakubed .

# Default Environment Variables
ENV SERVICE_TYPE=all
ENV PORT=8075

# Expose both potential ports
EXPOSE 8075
EXPOSE 8090

CMD ["./terrakubed"]
