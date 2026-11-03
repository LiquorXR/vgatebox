ARG BASE_IMAGE=debian:12
FROM ${BASE_IMAGE}
ENV container=docker
# CentOS Stream metalinks can advertise a stale repomd hash while mirrors sync.
# Keep the normal path, then retry this disposable test container against the
# canonical mirror with explicit baseurls.
RUN if command -v apt-get >/dev/null; then \
      apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y systemd systemd-sysv curl ca-certificates util-linux procps; \
    else \
      packages="systemd ca-certificates util-linux procps-ng"; \
      command -v curl >/dev/null || packages="$packages curl"; \
      dnf -y install $packages || \
        { \
          stream=$(cat /etc/dnf/vars/stream 2>/dev/null || true); \
          [ -n "$stream" ] || stream=$(. /etc/os-release && echo "$VERSION_ID")-stream; \
          arch=$(uname -m); \
          dnf -y clean all; \
          dnf -y \
            --setopt=baseos.metalink= \
            --setopt=baseos.baseurl="https://mirror.stream.centos.org/$stream/BaseOS/$arch/os/" \
            --setopt=appstream.metalink= \
            --setopt=appstream.baseurl="https://mirror.stream.centos.org/$stream/AppStream/$arch/os/" \
            install $packages; \
        }; \
    fi
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
