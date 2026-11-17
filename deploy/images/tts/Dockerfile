FROM python:3.15-slim

ENV PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    HF_ENDPOINT=https://hf-mirror.com

RUN pip install --no-cache-dir \
    -i https://mirrors.aliyun.com/pypi/simple/ \
    piper-tts==1.2.0

WORKDIR /app
COPY server.py /app/server.py
COPY zh_CN-huayan-medium.onnx.json /app/voice.json

EXPOSE 8091
CMD ["python", "/app/server.py"]
