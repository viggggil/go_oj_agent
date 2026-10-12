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
compose config --services | rg -x agent-service
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

# DDL 中断留下的部分结构必须明确失败，不能盲目重跑或删除。
sql <<< 'DROP DATABASE oj_agent; CREATE DATABASE oj_agent; CREATE TABLE oj_agent.agent_runs (id INT PRIMARY KEY); INSERT INTO oj_agent.agent_runs VALUES (7);'
if bootstrap > "$test_artifacts_dir/partial.log" 2>&1; then
    echo "部分迁移应阻止继续启动。" >&2
    exit 1
fi
rg '结构不完整' "$test_artifacts_dir/partial.log"
assert_eq 7 "$(sql <<< 'SELECT id FROM oj_agent.agent_runs;')" "失败时保留数据"
echo "基础设施升级回归检查通过。"
