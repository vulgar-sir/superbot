FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    HOME=/tmp \
    DISPLAY=:99 \
    PYTHONUNBUFFERED=1

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        chromium \
        curl \
        dbus-x11 \
        fonts-noto-cjk \
        git \
        jq \
        novnc \
        procps \
        python-is-python3 \
        python3 \
        python3-pip \
        python3-venv \
        ripgrep \
        scrot \
        websockify \
        x11vnc \
        xdotool \
        xvfb \
    && rm -rf /var/lib/apt/lists/*

# Office and document work is the most common thing bots do on their
# computer: reading a Word template a user sent, filling it in, producing a
# spreadsheet, slides, a PDF or a chart. Installing these per turn with
# `pip install --user` lands them on the tmpfs /tmp, where they vanish on
# restart, so they ship with the image.
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
    && mkdir -p /workspace /usr/share/novnc \
    && chown 1000:1000 /workspace

COPY start-browser.sh /usr/local/bin/start-browser.sh
COPY cdp_proxy.py /usr/local/bin/cdp_proxy.py
RUN chmod 755 /usr/local/bin/start-browser.sh && chmod 644 /usr/local/bin/cdp_proxy.py

WORKDIR /workspace
USER 1000:1000
EXPOSE 9223 6080 5900

CMD ["/usr/local/bin/start-browser.sh"]
