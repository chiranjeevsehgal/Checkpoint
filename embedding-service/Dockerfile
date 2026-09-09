FROM python:3.14-slim AS deps

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir torch --index-url https://download.pytorch.org/whl/cpu && \
    pip install --no-cache-dir -r requirements.txt

FROM deps AS model

# Bake bge-m3 into the image: download the full model into the cache. load+encode once so a bad/incomplete download fails the build instead of production.
ENV HF_HOME=/opt/hf-cache
RUN python -c "from sentence_transformers import SentenceTransformer; m = SentenceTransformer('BAAI/bge-m3'); m.encode(['warmup'])"

FROM python:3.14-slim AS runtime

ENV HF_HOME=/opt/hf-cache \
    HF_HUB_OFFLINE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

COPY --from=deps /usr/local/lib/python3.14/site-packages /usr/local/lib/python3.14/site-packages
COPY --from=model /opt/hf-cache /opt/hf-cache
COPY app ./app
COPY migrations ./migrations
COPY migrate.py .

FROM runtime AS worker

ENTRYPOINT ["python", "-u", "-m", "app.main"]

FROM runtime AS migrate

ENTRYPOINT ["python", "-u", "migrate.py"]
CMD ["up"]
