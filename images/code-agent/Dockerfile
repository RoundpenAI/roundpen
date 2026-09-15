# Cloud Agent (shell) image: git + ssh + curl + dev runtimes on Ubuntu 22.04.
# Used by the code-agent template. QEMU Claude Code is a separate disk.
# Python (10) and Node (22) are preinstalled so Skills that invoke scripts
# work out of the box. Popular coding agents are preinstalled and each can be
# launched as an ACP stdio agent (see the per-agent comments below).
FROM ubuntu:22.04

# Node matches the version installed in the agent-qemu image. Agent versions
# are pinned so image builds are reproducible; bump them when refreshing.
ENV DEBIAN_FRONTEND=noninteractive \
    TZ=UTC \
    NODE_VERSION=22.23.2 \
    NPM_CONFIG_REGISTRY=https://registry.npmmirror.com \
    CODEX_VERSION=0.154.0 \
    CODEX_ACP_VERSION=1.11.0 \
    GEMINI_CLI_VERSION=0.59.0 \
    OPENCODE_VERSION=1.18.31 \
    CURSOR_AGENT_VERSION=2026.09.10-fd3934a \
    CLAUDE_CODE_VERSION=2.1.272 \
    CLAUDE_AGENT_ACP_VERSION=0.77.0

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

# Coding agents available on PATH for any sandbox user. codex and opencode
# fetch a platform binary as an optional dependency, so the npm mirror must
# carry those sub-packages (npmmirror does).
# ACP stdio entrypoints: codex via the codex-acp adapter below; gemini and
# opencode natively (`gemini --acp`, `opencode acp`).
RUN npm install -g --no-fund --no-audit \
        "@openai/codex@${CODEX_VERSION}" \
        "@google/gemini-cli@${GEMINI_CLI_VERSION}" \
        "opencode-ai@${OPENCODE_VERSION}" \
    && npm cache clean --force \
    && codex --version \
    && gemini --version \
    && opencode --version

# ACP adapters for the agents without a native ACP mode: Claude Code and
# Codex. claude-agent-acp drives the Claude Agent SDK and is the command the
# `claude` provider launches; codex-acp wraps the codex CLI (the
# @zed-industries predecessor is deprecated). ACP entrypoints:
# `claude-agent-acp` and `codex-acp`.
RUN npm install -g --no-fund --no-audit \
        "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
        "@agentclientprotocol/claude-agent-acp@${CLAUDE_AGENT_ACP_VERSION}" \
        "@agentclientprotocol/codex-acp@${CODEX_ACP_VERSION}" \
    && npm cache clean --force \
    && claude --version \
    && command -v claude-agent-acp \
    && command -v codex-acp

# Cursor Agent CLI ships only as a tarball (no apt/npm package). Installed
# system-wide under /opt with symlinks in /usr/local/bin instead of the
# installer's per-$HOME layout, so it works for whichever uid the sandbox
# runs as. Upstream's primary name is `agent`; ACP stdio entrypoint is
# `cursor-agent acp`.
RUN mkdir -p /opt/cursor-agent \
    && curl -fsSL "https://downloads.cursor.com/lab/${CURSOR_AGENT_VERSION}/linux/x64/agent-cli-package.tar.gz" \
         | tar -xz -C /opt/cursor-agent --strip-components=1 \
    && ln -sf /opt/cursor-agent/cursor-agent /usr/local/bin/cursor-agent \
    && ln -sf /opt/cursor-agent/cursor-agent /usr/local/bin/agent; \
    cursor-agent --version

# Chinese coding agents: Qwen Code (Alibaba), CodeBuddy Code (Tencent) and
# Kimi CLI (Moonshot, below). ACP stdio entrypoints: `qwen --acp`,
# `codebuddy --acp` and `kimi acp`.
ENV QWEN_CODE_VERSION=0.23.4 \
    CODEBUDDY_VERSION=2.151.0 \
    KIMI_CLI_VERSION=1.50.0 \
    UV_VERSION=0.12.15

RUN npm install -g --no-fund --no-audit \
        "@qwen-code/qwen-code@${QWEN_CODE_VERSION}" \
        "@tencent-ai/codebuddy-code@${CODEBUDDY_VERSION}" \
    && npm cache clean --force \
    && command -v qwen \
    && command -v codebuddy

# Kimi CLI (Moonshot) ships as a Python 3.12+ tool on PyPI; uv supplies a
# managed interpreter and installs kimi system-wide, where pip alone cannot
# (no PyPI path for a system Python here). ACP entrypoint: `kimi acp`.
RUN pip3 install --no-cache-dir "uv==${UV_VERSION}" \
    && UV_TOOL_DIR=/opt/uv/tools \
       UV_TOOL_BIN_DIR=/usr/local/bin \
       UV_PYTHON_INSTALL_DIR=/opt/uv/python \
       uv tool install "kimi-cli==${KIMI_CLI_VERSION}" \
    && command -v kimi \
    && kimi --version

# Matches the bind-mount owner uid on typical Linux hosts, so the container
# user can write /workspace with all capabilities dropped.
RUN useradd -m -u 1000 -s /bin/bash roundpen

WORKDIR /workspace
CMD ["sleep", "infinity"]
