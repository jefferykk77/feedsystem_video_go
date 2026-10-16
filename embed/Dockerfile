FROM python:3.12-slim

WORKDIR /app
RUN pip install --no-cache-dir fastembed fastapi uvicorn
COPY embed_server.py .
# 构建时把最轻量的中文检索模型从 Hugging Face 下载进镜像。
RUN python -c "from fastembed import TextEmbedding; TextEmbedding(model_name='BAAI/bge-small-zh-v1.5')"

EXPOSE 8000
CMD ["uvicorn", "embed_server:app", "--host", "0.0.0.0", "--port", "8000"]
