FROM golang:alpine as builder
WORKDIR /app
COPY go.mod go.sum ./
COPY . .
RUN go mod download \
    && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ./cmd/proxy ./cmd/main.go

FROM alpine:3.11
ENV TZ=Asia/Jakarta

RUN apk add --no-cache --upgrade \
    bash \
    tzdata\
    && cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
RUN mkdir /app
COPY --from=builder /app/cmd/proxy /app
WORKDIR /app
RUN chmod +x proxy
EXPOSE 8080
CMD ["./proxy"]
