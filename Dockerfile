FROM golang:1.23.6 AS build_context

WORKDIR /workspace
ADD . /workspace/

RUN make build-local

FROM alpine:3.21.3 AS cert_context

RUN apk update && apk add curl &&  curl -SsOL https://curl.se/ca/cacert.pem

FROM scratch

COPY --from=build_context /workspace/bin/ks-upgrade /usr/local/bin/ks-upgrade
COPY --from=cert_context /cacert.pem /etc/ssl/certs/

ADD config.yaml /etc/kubesphere/

WORKDIR /

CMD ["ks-upgrade"]