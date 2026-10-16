import { api } from '../api'
import type { PageResult } from './types'

export interface KnowledgeGroup {
  id: string
  name: string
  created_by: number
  created_at: string
}
export interface KnowledgeDocument {
  id: string
  document_id: string
  base_id: string
  enabled: boolean
  active_version: string | null
  title: string
  kind: 'editorial' | 'lecture' | 'platform'
  problem_id: number | null
  source_url: string
  tags: string[]
  filename: string
  content_hash: string
  size_bytes: number
  model_revision: string
  splitter_version: string
  created_by: number
  created_at: string
  state: 'PENDING' | 'RUNNING' | 'READY' | 'FAILED'
  chunk_count: number
  error_code: string | null
  content?: string
}
export interface DocumentInput {
  base_id: string
  document_id?: string
  expected_version?: string
  title: string
  kind: KnowledgeDocument['kind']
  problem_id: number | null
  source_url: string
  tags: string[]
  filename: string
  content: string
}
export interface Chunk {
  number: number
  text: string
  section: string
  start: number
  end: number
}
export interface Match extends Chunk {
  score: number
  base_id: string
  document_id: string
  version_id: string
  title: string
  problem_id: number | null
}
export interface KnowledgeSource extends Omit<Match, 'text'> {
  source_id: string
}
export interface RunEvidence {
  run_id: string
  status: string
  agent_key: string
  scope: string[]
  versions: string[]
  attempts: {
    call_id: string
    ok: boolean
    error_code: string | null
    sources: KnowledgeSource[]
  }[]
}
const base = '/api/v1/admin/agent/rag'
const options = (signal?: AbortSignal) => ({ timeout: 30_000, signal })
const write = (requestId: string, signal?: AbortSignal) => ({
  ...options(signal),
  headers: { 'X-Request-ID': requestId },
})
export const ragApi = {
  evidence: (id: string, signal?: AbortSignal) =>
    api.get<{ evidence: RunEvidence }>(`${base}/runs/${id}/evidence`, options(signal)),
  groups: (page = 1, signal?: AbortSignal) =>
    api.get<PageResult<KnowledgeGroup>>(`${base}/knowledge-bases`, {
      ...options(signal),
      params: { page, page_size: 20 },
    }),
  group: (id: string, signal?: AbortSignal) =>
    api.get<{ knowledge_base: KnowledgeGroup }>(`${base}/knowledge-bases/${id}`, options(signal)),
  createGroup: (name: string, requestId: string, signal?: AbortSignal) =>
    api.post<{ knowledge_base: KnowledgeGroup }>(
      `${base}/knowledge-bases`,
      { name },
      write(requestId, signal),
    ),
  renameGroup: (
    id: string,
    name: string,
    expectedName: string,
    requestId: string,
    signal?: AbortSignal,
  ) =>
    api.post<{ knowledge_base: KnowledgeGroup }>(
      `${base}/knowledge-bases/${id}/rename`,
      { name, expected_name: expectedName },
      write(requestId, signal),
    ),
  documents: (baseId: string, page = 1, signal?: AbortSignal) =>
    api.get<PageResult<KnowledgeDocument>>(`${base}/documents`, {
      ...options(signal),
      params: { base_id: baseId, page, page_size: 20 },
    }),
  document: (id: string, signal?: AbortSignal) =>
    api.get<{ document: KnowledgeDocument }>(`${base}/documents/${id}`, options(signal)),
  history: (id: string, page = 1, signal?: AbortSignal) =>
    api.get<PageResult<KnowledgeDocument>>(`${base}/documents/${id}/versions`, {
      ...options(signal),
      params: { page, page_size: 20 },
    }),
  upload: (body: DocumentInput, requestId: string, signal?: AbortSignal) =>
    api.post<{ document: KnowledgeDocument }>(`${base}/documents`, body, write(requestId, signal)),
  action: (
    id: string,
    action: 'enable' | 'archive' | 'retry',
    expected: string,
    requestId: string,
    signal?: AbortSignal,
  ) =>
    api.post(
      `${base}/documents/${id}/${action}`,
      { expected_version: expected },
      write(requestId, signal),
    ),
  preview: (content: string, page: number, requestId: string, signal?: AbortSignal) =>
    api.post<{ chunks: Chunk[]; page: PageResult<Chunk>['page'] }>(
      `${base}/preview`,
      { content, page },
      write(requestId, signal),
    ),
  search: (
    ids: string[],
    query: string,
    problemId: number | null,
    requestId: string,
    signal?: AbortSignal,
  ) =>
    api.post<{ matches: Match[] }>(
      `${base}/search`,
      { base_ids: ids, query, problem_id: problemId, top_k: 5 },
      write(requestId, signal),
    ),
}
export const indexState = {
  PENDING: '等待索引',
  RUNNING: '索引中',
  READY: '索引就绪',
  FAILED: '索引失败',
}
