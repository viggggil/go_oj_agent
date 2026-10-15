"""仅在镜像构建/显式准备阶段访问 Hugging Face，固定提交。"""
import os
from sentence_transformers import SentenceTransformer

SentenceTransformer("BAAI/bge-small-zh-v1.5",
                    revision="7999e1d3359715c523056ef9478215996d62a620",
                    device="cpu").save(os.environ.get("MODEL_PATH", "/models/bge-small-zh-v1.5"))
