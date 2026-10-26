FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app && mkdir /data && chown app:app /data
WORKDIR /app
COPY --from=build /out/api ./api
USER app
ENTRYPOINT ["/app/api"]
