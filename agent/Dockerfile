# syntax=docker/dockerfile:1.7

FROM python:3.12-slim AS builder

# Pin the build tool separately from application dependencies so the image does
# not depend on the availability of the GHCR uv image.
RUN python -m pip install --no-cache-dir --disable-pip-version-check "uv==0.12.23"
WORKDIR /app
ENV UV_PYTHON_DOWNLOADS=never UV_LINK_MODE=copy
COPY pyproject.toml uv.lock README.md ./
COPY app ./app
RUN uv sync --frozen --no-dev --no-editable

FROM python:3.12-slim
RUN groupadd --gid 10001 agent \
    && useradd --uid 10001 --gid 10001 --no-create-home agent
WORKDIR /app
COPY --from=builder /app/.venv /app/.venv
ENV PATH="/app/.venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    AGENT_HOST=0.0.0.0 \
    AGENT_PORT=8000
USER agent
EXPOSE 8000
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD python -c 'import os, urllib.request; urllib.request.urlopen("http://127.0.0.1:" + os.getenv("AGENT_PORT", "8000") + "/healthz", timeout=2).close()'
ENTRYPOINT ["python", "-m", "app"]
