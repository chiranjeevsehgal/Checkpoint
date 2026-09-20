FROM python:3.14-slim AS deps

WORKDIR /app

COPY embedding-service/requirements.txt .
# Cache mount keeps downloaded wheels on the host between builds; changing
# requirements.txt wont redownload torch (~200 MB) from scratch.
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install torch --index-url https://download.pytorch.org/whl/cpu && \
    pip install -r requirements.txt

FROM deps AS model

# Bake bge-m3 into the image in two cached steps: first the download with
# visible progress (build logs have no tty, so HF's default bar prints
# nothing and a 2.3 GB fetch looks like a hang), then an offline load that
# proves the cache is complete — a missing file fails the build here
# instead of the worker at runtime.
ENV HF_HOME=/opt/hf-cache
COPY tools/download_bge_m3.py ./download_model.py
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
COPY embedding-service/app ./app
ENTRYPOINT ["python", "-u", "-m", "app.main"]

FROM python:3.14-slim AS migrate

WORKDIR /app

RUN --mount=type=cache,target=/root/.cache/pip \
    pip install psycopg[binary]==3.3.5
COPY embedding-service/migrate.py .
COPY embedding-service/migrations ./migrations

ENTRYPOINT ["python", "-u", "migrate.py"]
CMD ["up"]
