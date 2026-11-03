# Build context is .codex-temp/laya-offline-build (prepared by build.ps1).
ARG PYTHON_IMAGE=python:3.11-slim-bookworm
FROM ${PYTHON_IMAGE} AS build
ENV PIP_NO_CACHE_DIR=1 PIP_DISABLE_PIP_VERSION_CHECK=1
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
COPY source/docker/check_torch.py /opt/check_torch.py
RUN pip install torch==2.14.0 --index-url https://download.pytorch.org/whl/cpu \
    && python /opt/check_torch.py cpu
WORKDIR /src
COPY source/pyproject.toml source/setup.py source/README.md source/LICENSE ./
COPY source/laya/ ./laya/
RUN pip install '.[serve]' transformers==5.17.0 huggingface_hub==1.32.0 safetensors==0.8.0 \
    && pip check && pip freeze > /opt/requirements-resolved.txt

FROM ${PYTHON_IMAGE}
ARG LAYA_REVISION
LABEL org.opencontainers.image.title="Laya multilingual CPU offline - HSR" \
      org.opencontainers.image.source="https://github.com/NandhaKishorM/laya" \
      org.opencontainers.image.revision="${LAYA_REVISION}" \
      ai.laya.model.repository="convaiinnovations/laya-multilingual" \
      ai.laya.model.revision="e4e9ddf21a7b1903b7acffd8814ad4307bf63a67"
ENV PATH="/opt/venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 \
    USE_TF=0 USE_TORCH=1 TOKENIZERS_PARALLELISM=false \
    TORCH_DISABLE_NATIVE_JIT=1 HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 \
    HF_HUB_DISABLE_TELEMETRY=1 HF_HOME=/tmp/huggingface \
    OMP_NUM_THREADS=4 LAYA_THREADS=4 LAYA_DEVICE=cpu \
    LAYA_MAX_CONCURRENT=4 LAYA_MAX_TOKEN_BUDGET=2048
RUN groupadd --gid 10001 laya && useradd --uid 10001 --gid laya --create-home laya
COPY --from=build /opt/venv /opt/venv
COPY --from=build /opt/requirements-resolved.txt /opt/laya/requirements-resolved.txt
COPY source/LICENSE /usr/share/doc/laya/LICENSE
COPY --chown=10001:10001 model/ /opt/models/laya-multilingual/
COPY offline_server.py smoke_test.py /opt/laya/
RUN echo '9d628fd971b700382ac6f65920a86f149777b2e748e0c955fb3b19695aa8f204  /opt/models/laya-multilingual/model.safetensors' | sha256sum -c -
USER 10001:10001
WORKDIR /home/laya
EXPOSE 8000
ENTRYPOINT ["python", "/opt/laya/offline_server.py"]
