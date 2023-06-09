FROM golang:1.18-alpine as builder

ENV GO111MODULE=on

WORKDIR /tmp/go-app

RUN apk add --no-cache make gcc musl-dev linux-headers git

COPY go.mod .

COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o ./out/deyes main.go

# RUN rm /root/.ssh/id_rsa

# Start fresh from a smaller image
FROM alpine:3.9

WORKDIR /app

COPY --from=builder /tmp/go-app/out/deyes /app/deyes

CMD ["./deyes"]
