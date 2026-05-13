FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN go build -o /out/chaospay ./cmd/chaospay

FROM alpine:3.19
COPY --from=builder /out/chaospay /usr/local/bin/chaospay

EXPOSE 8532
ENTRYPOINT ["chaospay"]
