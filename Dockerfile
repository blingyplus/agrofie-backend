# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOTOOLCHAIN=auto
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/gateway ./cmd/gateway \
 && CGO_ENABLED=0 go build -o /out/auth ./cmd/auth \
 && CGO_ENABLED=0 go build -o /out/booking ./cmd/booking \
 && CGO_ENABLED=0 go build -o /out/payments ./cmd/payments \
 && CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate

FROM alpine:3.21 AS runtime
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/gateway /out/auth /out/booking /out/payments /usr/local/bin/
COPY db/migrations /app/db/migrations
ENTRYPOINT []

FROM alpine:3.21 AS migrate
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY db/migrations /app/db/migrations
WORKDIR /app
ENTRYPOINT ["migrate"]
