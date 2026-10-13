"""将宿主凭据准备到 Agent 专属卷；主密钥持久保留，文件不进入镜像。"""

import base64
import json
import os
from pathlib import Path

source = Path('/host-model-keys/su8.key')
target = Path('/run/model-keys')
target.mkdir(parents=True, exist_ok=True)
os.chmod(target, 0o700)
ring = target / 'keyring.json'
if not ring.exists():
    legacy_ring = Path('/host-model-keys/keyring.json')
    if not legacy_ring.is_file():
        legacy_ring = Path('/legacy-model-keys/keyring.json')
    if legacy_ring.is_file():
        from app.core.credentials import CredentialCipher
        CredentialCipher.from_file(legacy_ring)
        content = legacy_ring.read_bytes()
    else:
        content = json.dumps({
            'active': 'v1', 'keys': {'v1': base64.b64encode(os.urandom(32)).decode()},
        }).encode()
    descriptor = os.open(ring, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(content)
key = target / 'su8.key'
if source.is_file():
    if source.stat().st_mode & 0o077:
        raise SystemExit('su8.key 权限须为 600。')
    with source.open('rb') as stream:
        content = stream.read(4097)
    if not 1 <= len(content.strip()) <= 4096:
        raise SystemExit('su8.key 内容长度无效。')
    key.write_bytes(content)
    os.chmod(key, 0o600)
    os.chown(key, 10001, 10001)
else:
    key.unlink(missing_ok=True)
for path in (ring, target):
    os.chown(path, 10001, 10001)
print('Agent 模型凭据文件已准备。', flush=True)
