# Bootable Browser guest: systemd + virtio kernel + XFCE + Chrome CDP.
# Docker export is packed into a qcow2; QEMU boots with -kernel/-initrd (no GRUB).
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    systemd systemd-sysv udev dbus dbus-user-session \
    linux-image-virtual initramfs-tools \
    iproute2 \
    xfce4 xfce4-terminal \
    lightdm lightdm-gtk-greeter \
    xserver-xorg-core xserver-xorg-video-all x11-xserver-utils \
    ca-certificates curl gnupg fonts-liberation \
    && curl -fsSL https://dl.google.com/linux/linux_signing_key.pub \
      | gpg --dearmor -o /usr/share/keyrings/google-chrome.gpg \
    && echo "deb [arch=amd64 signed-by=/usr/share/keyrings/google-chrome.gpg] http://dl.google.com/linux/chrome/deb/ stable main" \
      > /etc/apt/sources.list.d/google-chrome.list \
    && apt-get update && apt-get install -y --no-install-recommends google-chrome-stable \
    && rm -rf /var/lib/apt/lists/*

COPY guest/modules /etc/initramfs-tools/modules
COPY guest/fstab /etc/fstab
COPY guest/20-wired.network /etc/systemd/network/20-wired.network
COPY guest/lightdm.conf /etc/lightdm/lightdm.conf.d/50-roundpen.conf
COPY guest/chrome-cdp.desktop /etc/xdg/autostart/chrome-cdp.desktop
COPY guest/start-session.sh /usr/local/bin/roundpen-start-session

RUN chmod +x /usr/local/bin/roundpen-start-session \
    && useradd -m -s /bin/bash -G video,audio,input,render,sudo roundpen \
    && echo 'roundpen:roundpen' | chpasswd \
    && mkdir -p /home/roundpen/.config/autostart \
    && cp /etc/xdg/autostart/chrome-cdp.desktop /home/roundpen/.config/autostart/ \
    && chown -R roundpen:roundpen /home/roundpen \
    && echo browser > /etc/hostname \
    && printf '127.0.0.1 localhost browser\n' > /etc/hosts \
    && rm -f /etc/machine-id /var/lib/dbus/machine-id \
    && touch /etc/machine-id \
    && update-initramfs -u \
    && ln -sf /lib/systemd/system/graphical.target /etc/systemd/system/default.target \
    && mkdir -p /etc/systemd/system/graphical.target.wants /etc/systemd/system/multi-user.target.wants \
    && ln -sf /lib/systemd/system/lightdm.service /etc/systemd/system/graphical.target.wants/lightdm.service \
    && ln -sf /lib/systemd/system/systemd-networkd.service /etc/systemd/system/multi-user.target.wants/systemd-networkd.service \
    && ln -sf /dev/null /etc/systemd/system/systemd-networkd-wait-online.service \
    && ln -sf /dev/null /etc/systemd/system/apt-daily.service \
    && ln -sf /dev/null /etc/systemd/system/apt-daily.timer \
    && ln -sf /dev/null /etc/systemd/system/apt-daily-upgrade.service \
    && ln -sf /dev/null /etc/systemd/system/apt-daily-upgrade.timer \
    && echo '/usr/sbin/lightdm' > /etc/X11/default-display-manager

# CDP is published by QEMU user-mode hostfwd; do not listen only on localhost.
EXPOSE 9222

CMD ["/sbin/init"]
