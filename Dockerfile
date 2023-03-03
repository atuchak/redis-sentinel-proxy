FROM golang:1.20 AS builder
LABEL Anton Tuchak <anton.tuchak@gmail.com>

ADD . /redis-sentinel-proxy/
WORKDIR /redis-sentinel-proxy
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
RUN go build -o redis-sentinel-proxy .

FROM alpine:3.17

RUN mkdir -p /usr/local/bin/
COPY --from=builder /redis-sentinel-proxy/redis-sentinel-proxy /usr/local/bin/redis-sentinel-proxy
RUN apk --update --no-cache add redis

CMD ["sh"]

