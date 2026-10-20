USE oj_agent;

CREATE TABLE agent_tool_descriptions (
  tool_name VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL PRIMARY KEY,
  description VARCHAR(1000) NOT NULL,
  updated_by BIGINT NOT NULL,
  updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
