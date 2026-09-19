FROM python:3.14-slim AS deps

WORKDIR /app

COPY requirements.txt .
# Cache mount keeps downloaded wheels on the host between builds; changing
# requirements.txt wont redownload torch (~200 MB) from scratch.
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install torch --index-url https://download.pytorch.org/whl/cpu && \
    pip install -r requirements.txt

FROM deps AS model

# Bake bge-m3 into the image so the query embedder and the indexer share one
# offline cache. The final offline load proves the cache is complete at build
# time instead of failing the MCP at first query.
ENV HF_HOME=/opt/hf-cache
COPY download_model.py .
RUN python download_model.py
RUN HF_HUB_OFFLINE=1 python -c "\
    print('[model] loading bge-m3 from cache...', flush=True); \
    from sentence_transformers import SentenceTransformer; \
    m = SentenceTransformer('BAAI/bge-m3'); \
    print('[model] warmup encode...', flush=True); \
    v = m.encode(['warmup'], normalize_embeddings=True); \
    print(f'[model] warmup ok: dim={len(v[0])}', flush=True)"

FROM model AS worker

ENV HF_HUB_OFFLINE=1 \
    PYTHONUNBUFFERED=1
COPY app ./app
ENTRYPOINT ["python", "-u", "-m", "app.main"]

FROM python:3.14-slim AS migrate

WORKDIR /app

RUN --mount=type=cache,target=/root/.cache/pip \
    pip install psycopg[binary]==3.3.5
COPY migrate.py .
COPY migrations ./migrations

ENTRYPOINT ["python", "-u", "migrate.py"]
CMD ["up"]
