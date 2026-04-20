FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service .

FROM alpine:3.19
RUN adduser -D -u 10001 app
COPY --from=build /out/service /service
USER app
EXPOSE 8082
ENTRYPOINT ["/service"]
