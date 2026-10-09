USE oj_agent;

ALTER TABLE agent_config_resources DROP CHECK chk_agent_config_kind,
  ADD CONSTRAINT chk_agent_config_kind
  CHECK (kind IN ('agent', 'prompt', 'skill', 'model', 'provider'));

CREATE TABLE agent_credentials (
  id CHAR(36) NOT NULL,
  name VARCHAR(128) NOT NULL,
  encrypted_secret VARBINARY(8192) NOT NULL,
  nonce BINARY(12) NOT NULL,
  key_version VARCHAR(64) NOT NULL,
  created_by BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  revoked_by BIGINT NULL,
  revoked_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_agent_credentials_created (created_at, id),
  CONSTRAINT chk_agent_credential_actor CHECK (created_by > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_credential_audits (
  request_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
  credential_id CHAR(36) NOT NULL,
  actor_id BIGINT NOT NULL,
  action VARCHAR(16) NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (request_id),
  KEY idx_agent_credential_audits_credential (credential_id, created_at),
  CONSTRAINT chk_agent_credential_audit_actor CHECK (actor_id > 0),
  CONSTRAINT chk_agent_credential_audit_action CHECK (action IN ('create', 'revoke')),
  CONSTRAINT fk_agent_credential_audit FOREIGN KEY (credential_id)
    REFERENCES agent_credentials(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_provider_credentials (
  version_id CHAR(36) NOT NULL,
  credential_id CHAR(36) NOT NULL,
  PRIMARY KEY (version_id),
  KEY idx_agent_provider_credential (credential_id),
  CONSTRAINT fk_agent_provider_version FOREIGN KEY (version_id)
    REFERENCES agent_config_versions(id) ON DELETE RESTRICT,
  CONSTRAINT fk_agent_provider_credential FOREIGN KEY (credential_id)
    REFERENCES agent_credentials(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE agent_runs ADD COLUMN model_summary JSON NULL;
