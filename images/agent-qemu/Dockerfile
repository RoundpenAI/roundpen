# Headless Ubuntu 24.04 agent VM: systemd + virtio kernel + Claude Code.
# LLM traffic goes through Roundpen llmgw (virtual key), not Anthropic/OpenAI directly.
# Docker export is packed into a qcow2; QEMU boots with -kernel/-initrd (no GRUB).
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=UTC \
    NODE_VERSION=22.23.2 \
    NPM_CONFIG_REGISTRY=https://registry.npmmirror.com

# Ubuntu 24.04 uses DEB822. Use USTC over HTTP: the base image has no
# ca-certificates yet, so HTTPS apt fails with "issuer is unknown".
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
    fi

RUN apt-get update && apt-get install -y --no-install-recommends \
        systemd systemd-sysv udev dbus \
        linux-image-virtual initramfs-tools \
        iproute2 iputils-ping \
        ca-certificates curl wget gnupg xz-utils \
        git openssh-server sudo \
        python3 python3-minimal \
        build-essential \
        locales \
    && rm -rf /var/lib/apt/lists/*

# qemu_fw_cfg.ko lives in linux-modules-extra (~100MB). Copy only that module
# so -fw_cfg opt/roundpen/env is visible under /sys/firmware/qemu_fw_cfg.
RUN set -eux; \
    kern="$(ls /lib/modules)"; \
    apt-get update; \
    apt-get install -y --no-install-recommends zstd; \
    apt-get install -y --download-only "linux-modules-extra-${kern}"; \
    mkdir -p /tmp/extra "/lib/modules/${kern}/kernel/drivers/firmware"; \
    dpkg-deb -x /var/cache/apt/archives/linux-modules-extra-*.deb /tmp/extra; \
    cp "/tmp/extra/lib/modules/${kern}/kernel/drivers/firmware/qemu_fw_cfg.ko.zst" \
       "/lib/modules/${kern}/kernel/drivers/firmware/"; \
    zstd -d -f "/lib/modules/${kern}/kernel/drivers/firmware/qemu_fw_cfg.ko.zst"; \
    depmod -a "${kern}"; \
    printf 'qemu_fw_cfg\n' > /etc/modules-load.d/qemu-fw-cfg.conf; \
    rm -rf /tmp/extra /var/cache/apt/archives/* /var/lib/apt/lists/*

RUN locale-gen en_US.UTF-8 && update-locale LANG=en_US.UTF-8

# Node 22 + Claude Code. USTC's node dist and npmreg 403 from this Docker
# network (anti-leech); npmmirror serves the same binaries.
RUN curl -fsSL "https://cdn.npmmirror.com/binaries/node/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
      | tar -xJ -C /usr/local --strip-components=1 \
    && node --version && npm --version \
    && npm install -g @anthropic-ai/claude-code \
    && claude --version \
    && npm install -g @agentclientprotocol/claude-agent-acp \
    && command -v claude-agent-acp

COPY guest/modules /etc/initramfs-tools/modules
COPY guest/fstab /etc/fstab
COPY guest/20-wired.network /etc/systemd/network/20-wired.network
COPY guest/sshd_config.d/roundpen.conf /etc/ssh/sshd_config.d/roundpen.conf
COPY guest/roundpen-apply-env /usr/local/sbin/roundpen-apply-env
COPY guest/roundpen-apply-env.service /etc/systemd/system/roundpen-apply-env.service

RUN chmod 0755 /usr/local/sbin/roundpen-apply-env \
    && useradd -m -s /bin/bash -G sudo,adm roundpen \
    && echo 'roundpen:roundpen' | chpasswd \
    && echo 'roundpen ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/roundpen \
    && chmod 0440 /etc/sudoers.d/roundpen \
    && mkdir -p /home/roundpen/.claude /home/roundpen/workspace /home/roundpen/.roundpen \
               /etc/roundpen /etc/claude-code /var/run/sshd /workspace \
    && chown -R roundpen:roundpen /home/roundpen /workspace \
    && echo agent > /etc/hostname \
    && printf '127.0.0.1 localhost agent\n' > /etc/hosts \
    && rm -f /etc/machine-id /var/lib/dbus/machine-id \
    && touch /etc/machine-id \
    && ssh-keygen -A \
    && update-initramfs -u \
    && ln -sf /lib/systemd/system/multi-user.target /etc/systemd/system/default.target \
    && mkdir -p /etc/systemd/system/multi-user.target.wants \
    && ln -sf /lib/systemd/system/systemd-networkd.service /etc/systemd/system/multi-user.target.wants/systemd-networkd.service \
    && ln -sf /lib/systemd/system/ssh.service /etc/systemd/system/multi-user.target.wants/ssh.service \
    && ln -sf /etc/systemd/system/roundpen-apply-env.service /etc/systemd/system/multi-user.target.wants/roundpen-apply-env.service \
    && ln -sf /dev/null /etc/systemd/system/systemd-networkd-wait-online.service \
    && ln -sf /dev/null /etc/systemd/system/apt-daily.service \
    && ln -sf /dev/null /etc/systemd/system/apt-daily.timer \
    && ln -sf /dev/null /etc/systemd/system/apt-daily-upgrade.service \
    && ln -sf /dev/null /etc/systemd/system/apt-daily-upgrade.timer

EXPOSE 22

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
