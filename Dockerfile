FROM golang:1.24 AS builder
ENV GOTOOLCHAIN=auto
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o sharehr .

FROM alpine:3.20
RUN apk add --no-cache bash
WORKDIR /app
COPY --from=builder /app/sharehr .
COPY entrypoint.sh .
RUN chmod +x entrypoint.sh
ENTRYPOINT ["./entrypoint.sh"]
