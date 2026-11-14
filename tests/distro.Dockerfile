ARG BASE_IMAGE=debian:12
FROM ${BASE_IMAGE}
ENV container=docker
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y systemd systemd-sysv curl ca-certificates util-linux procps
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
