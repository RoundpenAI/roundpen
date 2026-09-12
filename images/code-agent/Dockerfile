# Cloud Agent (shell) image: git + ssh + curl on Ubuntu 22.04.
# Used by the code-agent template. QEMU Claude Code is a separate disk.
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=UTC

# Ubuntu 22.04 still uses sources.list; 24.04-style DEB822 is handled if present.
# HTTP USTC: the base image may not have a CA bundle yet.
RUN set -eux; \
    if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
      sed -i \
        -e 's|https\?://archive.ubuntu.com/ubuntu|http://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|https\?://security.ubuntu.com/ubuntu|http://mirrors.ustc.edu.cn/ubuntu|g' \
        /etc/apt/sources.list.d/ubuntu.sources; \
    fi; \
    if [ -f /etc/apt/sources.list ]; then \
      sed -i \
        -e 's|https\?://archive.ubuntu.com/ubuntu|http://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|https\?://security.ubuntu.com/ubuntu|http://mirrors.ustc.edu.cn/ubuntu|g' \
        /etc/apt/sources.list; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        openssh-client \
        python3-minimal; \
    rm -rf /var/lib/apt/lists/*; \
    git --version; \
    ssh -V 2>&1 | head -1

WORKDIR /workspace
CMD ["sleep", "infinity"]
