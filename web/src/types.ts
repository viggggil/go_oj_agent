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
export type ContestStatus = 0 | 1 | 2 | 3 | 4 | string
export interface ContestProblem {
  problem_id: number
  sort_order: number
  score: number
}
export interface ContestSummary {
  id: number
  title: string
  status: ContestStatus
  start_at: TimestampValue
  end_at: TimestampValue
}
export interface Contest extends ContestSummary {
  created_by: number
  created_at: TimestampValue
  updated_at: TimestampValue
  problems: ContestProblem[]
}
export type TimestampValue = string | { seconds?: number | string; nanos?: number }
export interface LeaderboardProblemResult {
  problem_id: number
  solved: boolean
  wrong_attempts: number
  accepted_at?: string
}
export interface LeaderboardEntry {
  rank: number
  user_id: number
  score: number
  penalty: number
  accepted_count: number
  solved_count: number
  penalty_seconds: number
  problems: LeaderboardProblemResult[]
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

export type SubmissionStatus =
  | 'SUBMISSION_STATUS_UNSPECIFIED'
  | 'SUBMISSION_STATUS_QUEUED'
  | 'SUBMISSION_STATUS_COMPILING'
  | 'SUBMISSION_STATUS_RUNNING'
  | 'SUBMISSION_STATUS_DONE'
  | 'SUBMISSION_STATUS_RETRY_WAIT'
  | 'SUBMISSION_STATUS_CANCELLED'
  | 'SUBMISSION_STATUS_INVALIDATED'

export type JudgeVerdict =
  | 'JUDGE_VERDICT_UNSPECIFIED'
  | 'JUDGE_VERDICT_AC'
  | 'JUDGE_VERDICT_WA'
  | 'JUDGE_VERDICT_TLE'
  | 'JUDGE_VERDICT_MLE'
  | 'JUDGE_VERDICT_RE'
  | 'JUDGE_VERDICT_CE'
  | 'JUDGE_VERDICT_SYSTEM_ERROR'

export interface Submission {
  id: number
  user_id: number
  problem_id: number
  language: string
  status: SubmissionStatus | number
  verdict: JudgeVerdict | number
  time_ms: number
  memory_kb: number
  judge_revision: string
  retry_count: number
  system_error_reason?: string
  created_at?: TimestampValue
  updated_at?: TimestampValue
  judged_at?: TimestampValue
  invalidated_at?: TimestampValue
}

export interface SubmissionCaseResult {
  id: number
  submission_id: number
  case_no: number
  verdict: JudgeVerdict | number
  time_ms: number
  memory_kb: number
  message?: string
}

export interface JudgeResult {
  submission_id: number
  status: SubmissionStatus | number
  verdict: JudgeVerdict | number
  time_ms: number
  memory_kb: number
  case_results: SubmissionCaseResult[]
  judge_revision: string
  system_error_reason?: string
}

export interface SubmissionSource {
  submission_id: number
  language: string
  source_code: string
  size_bytes: number
  sha256: string
}
