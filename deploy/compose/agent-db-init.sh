#!/bin/sh
# 本地 Compose 的升级入口；应用容器不持有数据库管理凭据。
set -eu

mysql_root() {
    MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --host=mysql --user=root --batch --skip-column-names "$@"
}

query() {
    mysql_root --execute="$1"
}

table_count() {
    query "SELECT COUNT(*) FROM information_schema.TABLES
        WHERE TABLE_SCHEMA = 'oj_agent' AND TABLE_NAME IN ($1)"
}

column_count() {
    query "SELECT COUNT(*) FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = 'oj_agent' AND TABLE_NAME = '$1' AND COLUMN_NAME = '$2'"
}

apply_migration() {
    migration="$1"
    present="$2"
    expected="$3"
    if [ "$present" = 0 ]; then
        echo "应用 Agent 迁移 $migration"
        mysql_root < "/migrations/agent/$migration.up.sql"
    elif [ "$present" != "$expected" ]; then
        echo "Agent 迁移 $migration 的结构不完整；请检查已有迁移失败记录，保留数据并修复后重试。" >&2
        exit 1
    else
        echo "Agent 迁移 $migration 已存在，保留数据。"
    fi
}

runtime_tables="'agent_conversations','agent_runs','agent_messages'"
apply_migration 000001_create_agent_runtime "$(table_count "$runtime_tables")" 3

config_tables="'agent_config_resources','agent_config_versions','agent_config_links','agent_config_audits'"
config_present="$(table_count "$config_tables")"
agent_key_present="$(column_count agent_conversations agent_key)"
config_fk_present="$(query "SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = 'oj_agent' AND TABLE_NAME = 'agent_config_resources'
    AND CONSTRAINT_NAME = 'fk_agent_config_current'")"
apply_migration 000002_create_agent_configuration \
    "$((config_present + agent_key_present + config_fk_present))" 6

model_tables="'agent_credentials','agent_credential_audits','agent_provider_credentials'"
model_present="$(table_count "$model_tables")"
model_summary_present="$(column_count agent_runs model_summary)"
provider_check_present="$(query "SELECT COUNT(*) FROM information_schema.CHECK_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = 'oj_agent' AND CONSTRAINT_NAME = 'chk_agent_config_kind'
    AND CHECK_CLAUSE LIKE '%provider%'")"
apply_migration 000003_create_model_provider \
    "$((model_present + model_summary_present + provider_check_present))" 5

rag_tables="'agent_knowledge_bases','agent_knowledge_documents','agent_knowledge_versions','agent_knowledge_tasks','agent_knowledge_requests'"
rag_present="$(table_count "$rag_tables")"
rag_fk_present="$(query "SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA='oj_agent' AND CONSTRAINT_NAME IN ('fk_knowledge_current','fk_knowledge_active')")"
apply_migration 000004_create_knowledge "$((rag_present + rag_fk_present))" 7

eval_tables="'agent_eval_datasets','agent_eval_cases'"
eval_present="$(table_count "$eval_tables")"
apply_migration 000005_create_eval "$eval_present" 2

# CREATE USER IF NOT EXISTS 不覆盖已有密码；权限限于 Agent 自有 Schema。
mysql_root < /bootstrap/agent-user.sql
echo "Agent 数据库与本地账户准备完成。"
