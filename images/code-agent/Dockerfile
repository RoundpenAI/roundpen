# Cloud Agent (shell) image: git + ssh + curl + dev runtimes on Ubuntu 22.04.
# Used by the code-agent template. QEMU Claude Code is a separate disk.
# Python (10) and Node (22) are preinstalled so Skills that invoke scripts
# work out of the box.
FROM ubuntu:22.04

# Match the Node version installed in the agent-qemu image.
ENV DEBIAN_FRONTEND=noninteractive \
    TZ=UTC \
    NODE_VERSION=22.23.2 \
    NPM_CONFIG_REGISTRY=https://registry.npmmirror.com

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
        build-essential \
        ca-certificates \
        curl \
        git \
        jq \
        make \
        openssh-client \
        python3 \
        python3-pip; \
    rm -rf /var/lib/apt/lists/*; \
    git --version; \
    ssh -V 2>&1 | head -1; \
    python3 --version; \
    make --version | head -1; \
    jq --version

# Node 22 + npm. USTC's node dist and npmreg 403 from this Docker network
# (anti-leech); npmmirror serves the same binaries. Mirrors agent-qemu.
RUN curl -fsSL "https://cdn.npmmirror.com/binaries/node/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
      | tar -xJ -C /usr/local --strip-components=1 \
    && node --version && npm --version

# Matches the bind-mount owner uid on typical Linux hosts, so the container
# user can write /workspace with all capabilities dropped.
RUN useradd -m -u 1000 -s /bin/bash roundpen

WORKDIR /workspace
CMD ["sleep", "infinity"]
