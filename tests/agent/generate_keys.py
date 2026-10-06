"""只生成隔离集成测试的临时 RSA 密钥；不输出密钥或 token。"""

import sys
from pathlib import Path

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

directory = Path(sys.argv[1])
for name in ("user", "gateway"):
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    (directory / f"{name}-private.pem").write_bytes(
        key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
    )
    (directory / f"{name}-public.pem").write_bytes(
        key.public_key().public_bytes(
            serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo
        )
    )
for path in directory.glob("*.pem"):
    path.chmod(0o644)
