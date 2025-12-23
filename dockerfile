# ----------------------------------------
# BUILDER STAGE
# ----------------------------------------
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o application .

# ----------------------------------------
# RUNTIME STAGE
# ----------------------------------------
FROM scratch

WORKDIR /app

# Copy binary
COPY --from=builder /app/application .

EXPOSE 7379

CMD ["/app/application"]
