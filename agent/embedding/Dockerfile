FROM python:3.12-slim
ENV PYTHONUNBUFFERED=1 OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 TOKENIZERS_PARALLELISM=false
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY prepare.py .
RUN python prepare.py && chmod -R a+rX /models && rm -rf /root/.cache/huggingface
COPY server.py .
ENV HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1
RUN useradd --uid 10001 --no-create-home embedding
USER embedding
EXPOSE 8001
HEALTHCHECK --interval=5s --timeout=3s --start-period=30s --retries=30 CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8001/healthz',timeout=2).close()"
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8001", "--limit-concurrency", "8"]
