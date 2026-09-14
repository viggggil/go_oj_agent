export interface User {
  id: number
  username: string
  email: string
  status: string
  roles: string[]
}
export interface TokenPair {
  access_token: string
  refresh_token: string
  expires_in: number
}

export type ProblemDifficulty = 1 | 2 | 3
export interface Tag {
  id: number
  name: string
}
export interface ProblemSummary {
  id: number
  title: string
  slug: string
  difficulty: ProblemDifficulty
  status: number
}
export interface Problem extends ProblemSummary {
  description: string
  time_limit_ms: number
  memory_limit_kb: number
  created_by: number
  tags: Tag[]
  created_at: string
  updated_at: string
}
export interface ProblemInput {
  title: string
  slug: string
  description: string
  difficulty: ProblemDifficulty
  time_limit_ms: number
  memory_limit_kb: number
  tags: string[]
}
export interface Testcase {
  id: number
  problem_id: number
  case_no: number
  input_object_key: string
  output_object_key: string
  input_sha256: string
  output_sha256: string
  input_size_bytes: number
  output_size_bytes: number
  status: number
  created_at: string
  archived_at?: string
}
