#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
project_name="oj-infra-test-${RANDOM}-$$"
test_artifacts_dir="$(mktemp -d)"

compose() {
    docker compose --project-name "$project_name" \
        -f "$repository_root/deploy/compose/compose.yaml" \
        -f "$repository_root/tests/infra/compose.yaml" "$@"
}

cleanup() {
    status=$?
    trap - EXIT
    if [ "$status" -ne 0 ]; then
        compose logs --no-color mysql agent-db-init || true
    fi
    # 只清理本次随机测试项目，不访问开发数据卷。
    compose down --volumes --remove-orphans
    rm -rf -- "$test_artifacts_dir"
    exit "$status"
}
trap cleanup EXIT

sql() {
    compose exec -T mysql sh -c \
        'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot --batch --skip-column-names'
}

assert_eq() {
    if [ "$1" != "$2" ]; then
        echo "断言失败：$3，预期 $1，实际 $2" >&2
        exit 1
    fi
}

bootstrap() {
    compose run --rm -T agent-db-init
}

# 确保默认服务集合包含 Agent，无需额外 profile。
compose config --services | grep -Fx agent-service
compose up --detach --wait mysql
sql <<'SQL'
CREATE DATABASE legacy_business;
CREATE TABLE legacy_business.sentinel (id INT PRIMARY KEY);
INSERT INTO legacy_business.sentinel VALUES (1);
SQL

# 空 Agent schema / 旧业务数据卷：补齐表和受限账户。
bootstrap
assert_eq 10 "$(sql <<< "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='oj_agent';")" "完整 Agent 表"
compose exec -T mysql sh -c \
    'MYSQL_PWD=local-agent-password mysql -h127.0.0.1 -uoj_agent -e "SELECT COUNT(*) FROM oj_agent.agent_runs"'
if compose exec -T mysql sh -c \
    'MYSQL_PWD=local-agent-password mysql -h127.0.0.1 -uoj_agent -e "SELECT * FROM mysql.user"' \
    > "$test_artifacts_dir/permission.log" 2>&1; then
    echo "Agent 账户不应具有系统表读取权限。" >&2
    exit 1
fi

sql <<'SQL'
INSERT INTO oj_agent.agent_conversations (id,user_id,title,created_at,updated_at)
VALUES ('00000000-0000-0000-0000-000000000001',1,'保留历史',NOW(3),NOW(3));
INSERT INTO oj_agent.agent_config_resources (id,kind,config_key,created_at)
VALUES ('00000000-0000-0000-0000-000000000003','prompt','preserved_prompt',NOW(3));
INSERT INTO oj_agent.agent_credentials (id,name,encrypted_secret,nonce,key_version,created_by,created_at)
VALUES ('00000000-0000-0000-0000-000000000004','保留凭据',X'0102',UNHEX('000000000000000000000000'),'test',1,NOW(3));
ALTER USER 'oj_agent'@'%' IDENTIFIED BY 'preserved-test-password';
SQL
# 完整的历史初始化路径 / 重复启动：保留会话和已有账户密码。
bootstrap
assert_eq 1 "$(sql <<< 'SELECT COUNT(*) FROM oj_agent.agent_conversations;')" "重复启动保留会话"
assert_eq preserved_prompt "$(sql <<< 'SELECT config_key FROM oj_agent.agent_config_resources;')" "重复启动保留配置"
assert_eq 0102 "$(sql <<< 'SELECT HEX(encrypted_secret) FROM oj_agent.agent_credentials;')" "重复启动保留凭据"
compose exec -T mysql sh -c \
    'MYSQL_PWD=preserved-test-password mysql -h127.0.0.1 -uoj_agent -e "SELECT 1"'

# PR2 历史版本：只补齐配置与 Provider 迁移，保留旧会话。
sql <<< 'DROP DATABASE oj_agent;'
sql < "$repository_root/migrations/agent/000001_create_agent_runtime.up.sql"
sql <<'SQL'
INSERT INTO oj_agent.agent_conversations (id,user_id,title,created_at,updated_at)
VALUES ('00000000-0000-0000-0000-000000000002',1,'升级历史',NOW(3),NOW(3));
SQL
bootstrap
assert_eq demo "$(sql <<< 'SELECT agent_key FROM oj_agent.agent_conversations;')" "历史会话迁移"
assert_eq 1 "$(sql <<< 'SELECT COUNT(*) FROM legacy_business.sentinel;')" "保留其他服务数据"

# 默认页面配置：真实仓储创建示例，检查目录、Skill 和重启保留管理员状态。
compose build agent-service
compose run --rm --no-deps -T \
    -e AGENT_DATABASE_URL=mysql+asyncmy://oj_agent:preserved-test-password@mysql:3306/oj_agent \
    --entrypoint python agent-service - <<'PY'
import asyncio
import runpy
from uuid import uuid4

from app.core.database import DatabaseProbe
from app.core.management import ManagementService
from app.core.settings import load_settings
from app.models.admin import PageQuery
from app.models.configuration import ConfigWrite
from app.models.runtime import Principal
from app.storage.configuration import ConfigurationStore
from app.storage.schema import agent_config_versions
from app.tools.registry import ToolRegistry
from sqlalchemy import func, select

bootstrap_demo = runpy.run_path('/bootstrap/agent-dev-init.py')['main']

async def check():
    settings = load_settings()
    assert settings.admin_enabled and settings.config_mode == 'database'
    await bootstrap_demo()
    db = DatabaseProbe(settings)
    try:
        store = ConfigurationStore(db.engine, ToolRegistry())
        service = ManagementService(store, settings, ToolRegistry())
        principal = Principal(user_id=1, roles=frozenset({'user'}), request_id=str(uuid4()))
        catalog = await service.catalog(PageQuery(), principal)
        assert catalog['items'][0]['key'] == 'learning_assistant'
        assert catalog['items'][0]['is_default']
        skills = await service.catalog(PageQuery(), principal, 'learning_assistant')
        assert skills['default_skill_key'] == 'algorithm_explain'
        agent = await store.resolve('agent', 'learning_assistant')
        await bootstrap_demo()
        assert (await store.resolve('agent', 'learning_assistant')).id == agent.id
        await store.change_state(ConfigWrite(kind='agent', key=agent.key, actor_id=1,
            request_id=str(uuid4()), expected_id=agent.id), 'disable')
        await bootstrap_demo()
        assert (await store.resolve('agent', 'learning_assistant')).disabled
        assert not (await service.catalog(PageQuery(), principal))['items']
        async with db.engine.connect() as c:
            count = await c.scalar(select(func.count()).select_from(agent_config_versions))
        settings.runtime_mode = 'model'
        import os
        os.environ['AGENT_RUNTIME_MODE'] = 'model'
        await bootstrap_demo()
        async with db.engine.connect() as c:
            assert await c.scalar(select(func.count()).select_from(agent_config_versions)) == count
        # 默认 Agent 尚不存在、同名依赖被停用时，准备步骤也不能阻塞管理服务。
        os.environ['AGENT_RUNTIME_MODE'] = 'langgraph_fake'
        from sqlalchemy import delete
        from app.storage.schema import agent_config_audits, agent_config_links, agent_config_resources
        async with db.engine.begin() as c:
            await c.execute(delete(agent_config_audits))
            await c.execute(delete(agent_config_links))
            await c.execute(agent_config_resources.update().values(current_id=None))
            await c.execute(delete(agent_config_versions))
            await c.execute(delete(agent_config_resources))
        os.environ['AGENT_RUNTIME_MODE'] = 'model'
        await bootstrap_demo()
        async with db.engine.connect() as c:
            assert await c.scalar(select(func.count()).select_from(agent_config_versions)) == 0
        os.environ['AGENT_RUNTIME_MODE'] = 'langgraph_fake'
        prompt = await store.create_or_replace(ConfigWrite(kind='prompt', key='learning_identity',
            actor_id=1, request_id=str(uuid4())), {'text':'管理员保留配置'})
        await store.change_state(ConfigWrite(kind='prompt', key=prompt.key, actor_id=1,
            request_id=str(uuid4()), expected_id=prompt.id), 'disable')
        await bootstrap_demo()
        assert (await store.resolve('prompt', prompt.key)).disabled
        async with db.engine.connect() as c:
            assert await c.scalar(select(func.count()).select_from(agent_config_resources)) == 1

        # 无外网/计费的真实配置准备：三个模型、内置 Skill、重启幂等和停用保留。
        from pathlib import Path
        from tempfile import TemporaryDirectory
        from app.credential_cli import init_keyring
        from app.development_bootstrap import bootstrap_su8, SU8_MODELS
        from app.core.provider_policy import ProviderPolicy
        from app.core.runtime_config import DatabaseConfigReader
        from app.models.runtime import ChatRequest
        from app.storage.schema import agent_credentials
        with TemporaryDirectory() as directory:
            ring, key_file = Path(directory) / 'keyring.json', Path(directory) / 'su8.key'
            init_keyring(ring)
            key_file.write_text('infra-test-key')
            key_file.chmod(0o600)
            real_settings = settings.model_copy(update={
                'runtime_mode':'model', 'default_agent_key':'su8_learning_assistant',
                'credential_keyring_file':ring,
                'provider_allowed_origins':('https://www.su8.codes',),
            })
            await bootstrap_su8(real_settings, key_file=key_file)
            real_store = ConfigurationStore(db.engine, ToolRegistry(), provider_policy=ProviderPolicy(real_settings))
            service = ManagementService(real_store, real_settings, ToolRegistry())
            models = await service.models(PageQuery())
            assert {v['model'] for v in models['items']} == set(SU8_MODELS)
            assert all(v['verbosity']=='high' for v in models['items'])
            root = await real_store.resolve('agent', 'su8_learning_assistant')
            reader = DatabaseConfigReader(real_store, real_settings, ToolRegistry())
            snapshot = await reader.read(ChatRequest(message='hello'), principal)
            assert snapshot.skill_prompt_text and snapshot.skill_prompt_id is None
            assert not snapshot.provider_config['text_verbosity_supported']
            async with db.engine.connect() as c:
                version_count = await c.scalar(select(func.count()).select_from(agent_config_versions))
                encrypted = await c.scalar(select(agent_credentials.c.encrypted_secret).where(agent_credentials.c.name=='su8'))
                assert b'infra-test-key' not in encrypted
            await bootstrap_su8(real_settings, key_file=key_file)
            assert (await real_store.resolve('agent', root.key)).id == root.id
            async with db.engine.connect() as c:
                assert await c.scalar(select(func.count()).select_from(agent_config_versions)) == version_count
            selected = models['items'][0]
            await real_store.change_state(ConfigWrite(kind='model', key=selected['key'], actor_id=1,
                request_id=str(uuid4()), expected_id=selected['id']), 'disable')
            try:
                await bootstrap_su8(real_settings, key_file=key_file)
            except Exception as error:
                assert error.code == 'AGENT_CONFIGURATION_REFERENCE_INVALID'
            else:
                raise AssertionError('不能恢复停用的模型')
            assert (await real_store.resolve('model', selected['key'])).disabled
    finally:
        await db.close()

asyncio.run(check())
print('默认页面配置与重启保留检查通过。')
PY

# 旧默认宿主 keyring 首次导入新卷，DB 凭据继续解密；重启不覆盖主密钥。
mkdir -p "$test_artifacts_dir/legacy-model-keys" "$test_artifacts_dir/empty-model-keys"
python3 - "$test_artifacts_dir/legacy-model-keys" <<'PY_KEYS'
import base64, json, os, sys
from pathlib import Path
path = Path(sys.argv[1]) / 'keyring.json'
path.write_text(json.dumps({'active':'legacy', 'keys':{'legacy':base64.b64encode(os.urandom(32)).decode()}}))
path.chmod(0o600)
PY_KEYS
compose run --rm --no-deps -T \
    --volume "$test_artifacts_dir/legacy-model-keys:/legacy-model-keys:ro" \
    --volume "$test_artifacts_dir/empty-model-keys:/host-model-keys:ro" agent-model-key-init
compose run --rm --no-deps -T --user 0:0 \
    -e AGENT_DATABASE_URL=mysql+asyncmy://oj_agent:preserved-test-password@mysql:3306/oj_agent \
    --volume "$test_artifacts_dir/legacy-model-keys:/legacy-model-keys:ro" \
    --entrypoint python agent-service - <<'PY_CREDENTIAL'
import asyncio
from pathlib import Path
from uuid import uuid4
from pydantic import SecretStr
from app.core.credentials import CredentialCipher
from app.core.database import DatabaseProbe
from app.core.settings import load_settings
from app.storage.credentials import CredentialStore, CredentialWrite

async def check():
    db = DatabaseProbe(load_settings())
    try:
        old = CredentialStore(db.engine, CredentialCipher.from_file(Path('/legacy-model-keys/keyring.json')))
        credential = await old.create(CredentialWrite(actor_id=1, request_id=str(uuid4())),
            '旧部署凭据', SecretStr('legacy-integration-key'))
        new = CredentialStore(db.engine, CredentialCipher.from_file(Path('/run/model-keys/keyring.json')))
        assert (await new.read(credential['id'])).get_secret_value() == 'legacy-integration-key'
    finally:
        await db.close()
asyncio.run(check())
print('旧部署凭据升级后仍可解密。')
PY_CREDENTIAL
# 清除旧来源后再次初始化，也不能生成新主密钥。
compose run --rm --no-deps -T \
    --volume "$test_artifacts_dir/empty-model-keys:/legacy-model-keys:ro" \
    --volume "$test_artifacts_dir/empty-model-keys:/host-model-keys:ro" agent-model-key-init
compose run --rm --no-deps -T --entrypoint python agent-service - <<'PY_REUSE'
import json
from pathlib import Path
assert json.loads(Path('/run/model-keys/keyring.json').read_text())['active'] == 'legacy'
print('主密钥重启复用检查通过。')
PY_REUSE

# DDL 中断留下的部分结构必须明确失败，不能盲目重跑或删除。
sql <<< 'DROP DATABASE oj_agent; CREATE DATABASE oj_agent; CREATE TABLE oj_agent.agent_runs (id INT PRIMARY KEY); INSERT INTO oj_agent.agent_runs VALUES (7);'
if bootstrap > "$test_artifacts_dir/partial.log" 2>&1; then
    echo "部分迁移应阻止继续启动。" >&2
    exit 1
fi
grep -F '结构不完整' "$test_artifacts_dir/partial.log"
assert_eq 7 "$(sql <<< 'SELECT id FROM oj_agent.agent_runs;')" "失败时保留数据"
echo "基础设施升级回归检查通过。"
