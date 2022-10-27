FROM golang:1.16 as builder

COPY / /go/src/kubesphere.io/ks-upgrade
WORKDIR /go/src/kubesphere.io/ks-upgrade
RUN CGO_ENABLED=0 GO111MODULE=on GOOS=linux GOFLAGS=-mod=vendor go build -o ks-upgrade cmd/ks-upgrage.go


FROM alpine:3.16
RUN apk add --update ca-certificates && update-ca-certificates
COPY --from=builder /go/src/kubesphere.io/ks-upgrade/ks-upgrade /usr/local/bin/
CMD ["sh"]