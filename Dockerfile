FROM golang:1.23-alpine AS build

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /slotegrator-service ./cmd/server

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=build /slotegrator-service /app/slotegrator-service
COPY --from=build /app/migrations /app/migrations

EXPOSE 8092

CMD ["/app/slotegrator-service"]
