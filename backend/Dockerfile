FROM golang:1.22-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/monitoring-tool-backend ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 appuser

USER appuser
WORKDIR /app

COPY --from=build /out/monitoring-tool-backend /app/monitoring-tool-backend

EXPOSE 8080
ENTRYPOINT ["/app/monitoring-tool-backend"]
