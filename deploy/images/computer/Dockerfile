FROM debian:bookworm-slim

# The bot's home lives in its persistent workspace (see env.sh); caches stay
# on the container's tmpfs. The worker passes the same variables when it
# creates a computer (internal/sandbox/env.go); they are repeated here so a
# plain `docker run` of the image behaves the same way.
ENV DEBIAN_FRONTEND=noninteractive \
    HOME=/workspace/.home \
    DISPLAY=:99 \
    PYTHONUNBUFFERED=1 \
    XDG_CACHE_HOME=/tmp/.cache \
    XDG_RUNTIME_DIR=/tmp/.runtime \
    PIP_BREAK_SYSTEM_PACKAGES=1 \
    NPM_CONFIG_PREFIX=/workspace/.home/.local \
    NPM_CONFIG_CACHE=/tmp/.npm \
    APPIMAGE_EXTRACT_AND_RUN=1 \
    NO_AT_BRIDGE=1 \
    PATH=/workspace/.home/.local/bin:/workspace/.home/.local/apt/usr/bin:/workspace/.home/.local/apt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    LD_LIBRARY_PATH=/workspace/.home/.local/apt/usr/lib/x86_64-linux-gnu:/workspace/.home/.local/apt/lib/x86_64-linux-gnu:/workspace/.home/.local/apt/usr/lib \
    XDG_DATA_DIRS=/workspace/.home/.local/apt/usr/share:/usr/local/share:/usr/share

RUN apt-get update \
    && apt-get -o Acquire::Retries=5 install -y --no-install-recommends \
        binutils \
        ca-certificates \
        chromium \
        curl \
        dbus-x11 \
        file \
        fonts-noto-cjk \
        git \
        jq \
        novnc \
        procps \
        proot \
        python-is-python3 \
        python3 \
        python3-pip \
        python3-venv \
        ripgrep \
        scrot \
        squashfs-tools \
        tesseract-ocr \
        tesseract-ocr-chi-sim \
        tesseract-ocr-eng \
        unzip \
        websockify \
        wget \
        wmctrl \
        x11-utils \
        x11vnc \
        xclip \
        xdotool \
        xvfb \
        xz-utils \
        zstd \
    && rm -rf /var/lib/apt/lists/*

# Node.js LTS from the official build (Debian's is too old for most current
# npm packages). The mirror serves the same files as nodejs.org; the
# checksum is the one nodejs.org publishes for this release.
ARG NODE_VERSION=24.22.3
ARG NODE_SHA256=4f2b175a6d53a5c617a80dba2676238a0688cef28c6da4eba35c7d844e66fcfb
RUN set -eux; \
    file="node-v${NODE_VERSION}-linux-x64.tar.xz"; \
    for base in https://cdn.npmmirror.com/binaries/node https://nodejs.org/dist; do \
        if curl -fsSL --retry 3 -o "/tmp/$file" "$base/v${NODE_VERSION}/$file"; then break; fi; \
    done; \
    echo "${NODE_SHA256}  /tmp/$file" | sha256sum -c -; \
    tar -xJf "/tmp/$file" -C /usr/local --strip-components=1 --no-same-owner \
        --exclude='*/include' --exclude='*/share/doc' --exclude='*/share/man' \
        --exclude='*/CHANGELOG.md' --exclude='*/README.md'; \
    rm -f "/tmp/$file"; \
    node --version; npm --version

# Office and document work is the most common thing bots do on their
# computer: reading a Word template a user sent, filling it in, producing a
# spreadsheet, slides, a PDF or a chart. They ship with the image so a new
# computer can do this without installing anything.
RUN pip3 install --no-cache-dir --break-system-packages \
        python-docx \
        python-pptx \
        openpyxl \
        pypdf \
        reportlab \
        matplotlib \
        pillow \
        lxml

RUN useradd -m -u 1000 -d /home/bot -s /bin/bash bot \
    && mkdir -p /workspace /usr/share/novnc /usr/local/lib/superbot \
    && chown 1000:1000 /workspace

COPY env.sh computer-init.sh gate.py /usr/local/lib/superbot/
COPY userapt appimage-install /usr/local/bin/
COPY start-browser.sh /usr/local/bin/start-browser.sh
RUN chmod 644 /usr/local/lib/superbot/env.sh /usr/local/lib/superbot/computer-init.sh /usr/local/lib/superbot/gate.py \
    && chmod 755 /usr/local/bin/start-browser.sh /usr/local/bin/userapt /usr/local/bin/appimage-install \
    && ln -s /usr/local/lib/superbot/env.sh /etc/profile.d/superbot.sh \
    && printf '\n# Superbot computer environment\n[ -r /usr/local/lib/superbot/env.sh ] && . /usr/local/lib/superbot/env.sh\n' >> /etc/bash.bashrc

WORKDIR /workspace
USER 1000:1000
EXPOSE 9223 6080

CMD ["/usr/local/bin/start-browser.sh"]
