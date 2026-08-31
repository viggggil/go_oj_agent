USE oj_problem;

CREATE TABLE problem_tags (
  problem_id BIGINT NOT NULL,
  tag_id BIGINT NOT NULL,
  PRIMARY KEY (problem_id, tag_id),
  CONSTRAINT fk_problem_tags_problem_id
    FOREIGN KEY (problem_id) REFERENCES problems (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_problem_tags_tag_id
    FOREIGN KEY (tag_id) REFERENCES tags (id)
    ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

