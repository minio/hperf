FROM golang:1.15-alpine as builder

LABEL maintainer="MinIO Inc <dev@min.io>"

ENV GOPATH /go
ENV CGO_ENABLED 0
ENV GO111MODULE on

COPY . /root/mesh-throughput
RUN cd /root/mesh-throughput && go install

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.3

ARG TARGETARCH

COPY --from=builder /go/bin/mesh-throughput /usr/bin/mesh-throughput

RUN  \
     microdnf update --nodocs && \
     microdnf install ca-certificates --nodocs && \
     microdnf clean all

ENTRYPOINT ["mesh-throughput"]

