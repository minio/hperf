FROM scratch
MAINTAINER MinIO Development "dev@min.io"

EXPOSE 9999

COPY ./hperf /hperf

ENTRYPOINT ["/hperf"]
