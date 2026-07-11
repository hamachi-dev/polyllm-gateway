FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/proxy ./cmd/proxy

FROM scratch
COPY --from=builder /bin/proxy /proxy
COPY config.yaml /config.yaml
EXPOSE 8000 8001
ENTRYPOINT ["/proxy", "/config.yaml"]
