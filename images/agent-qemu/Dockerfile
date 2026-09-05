# Headless Ubuntu 24.04 agent VM: systemd + virtio kernel + Claude Code.
# LLM traffic goes through Roundpen llmgw (virtual key), not Anthropic/OpenAI directly.
# Docker export is packed into a qcow2; QEMU boots with -kernel/-initrd (no GRUB).
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=UTC \
    NODE_VERSION=22.23.2 \
    NPM_CONFIG_REGISTRY=https://npmreg.proxy.ustc.edu.cn

# Ubuntu 24.04 uses DEB822; rewrite both formats so apt never hits archive.ubuntu.com.
RUN set -eux; \
    if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
      sed -i \
        -e 's|http://archive.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|https://archive.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|http://security.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|https://security.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
        /etc/apt/sources.list.d/ubuntu.sources; \
    fi; \
    if [ -f /etc/apt/sources.list ]; then \
      sed -i \
        -e 's|http://archive.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|https://archive.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|http://security.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
        -e 's|https://security.ubuntu.com/ubuntu|https://mirrors.ustc.edu.cn/ubuntu|g' \
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

RUN locale-gen en_US.UTF-8 && update-locale LANG=en_US.UTF-8

# Node 22 from USTC dist mirror (USTC's nodesource repo sync is stopped).
RUN curl -fsSL "https://mirrors.ustc.edu.cn/node/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
      | tar -xJ -C /usr/local --strip-components=1 \
    && node --version && npm --version \
    && npm install -g @anthropic-ai/claude-code \
    && claude --version

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
               /etc/roundpen /etc/claude-code /var/run/sshd \
    && chown -R roundpen:roundpen /home/roundpen \
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
