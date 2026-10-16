from fastapi import FastAPI
from pydantic import BaseModel
from fastembed import TextEmbedding

MODEL = "BAAI/bge-small-zh-v1.5"
app = FastAPI()
model = TextEmbedding(model_name=MODEL)


class EmbedRequest(BaseModel):
    texts: list[str]


@app.get("/healthz")
def healthz():
    return {"ok": True, "model": MODEL}


@app.post("/embed")
def embed(req: EmbedRequest):
    vectors = [item.tolist() for item in model.embed(req.texts)]
    return {"vectors": vectors}
