FROM docker.io/library/golang:1.23-alpine AS builder

WORKDIR /src

RUN apk add --no-cache build-base pkgconf

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags musl -o /out/api ./cmd/api

FROM docker.io/library/alpine:3.20

RUN apk add --no-cache ca-certificates wget

WORKDIR /app
COPY --from=builder /out/api /app/api

EXPOSE 8080
ENTRYPOINT ["/app/api"]
