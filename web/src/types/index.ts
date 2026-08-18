export interface Paged<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  has_more: boolean
}

export interface ReadingSummaryDTO {
  id: string
  collector_id: string
  timestamp: string
  queue_count: number
  duration_ms: number
  fault_code: string
  shard_id: string
  line_no: number
}

export interface ReadingDTO extends ReadingSummaryDTO {
  raw_metrics?: Record<string, number>
  source?: string
  seq?: number
}

export interface AlertDTO {
  id: string
  collector_id: string
  rule_id: string
  reading_id: string
  severity: string
  state: string
  message: string
  assignee_id: string
  first_seen: string
  last_seen: string
  suppressed_until?: string
  version: number
  created_at: string
  updated_at: string
}

export interface AssignmentDTO {
  id: string
  alert_id: string
  handler_id: string
  state: string
  accepted_at: string
  completed_at?: string
  note?: string
  version: number
}

export interface CollectorDTO {
  id: string
  code: string
  name: string
  kind: string
  location: string
  status: string
  handler_id: string
  version: number
  created_at: string
  updated_at: string
}

export interface RuleDTO {
  id: string
  collector_id: string | null
  metric: string
  window_seconds: number
  min_value: number | null
  max_value: number | null
  fault_trigger: string
  severity: string
  enabled: boolean
  version: number
  created_at: string
  updated_at: string
}

export interface AuditDTO {
  id: string
  actor: string
  role: string
  action: string
  target_type: string
  target_id: string
  detail: Record<string, unknown>
  at: string
}

export interface FailureDTO {
  id: string
  task_type: string
  target_id: string
  payload: string
  last_error: string
  attempts: number
  status: string
  failed_at: string
  resolved: boolean
}

export interface ShardManifestDTO {
  shard_id: string
  count: number
  checksum: string
  created_at: string
}

export interface BacklogSummary {
  open_alerts: number
  assigned_alerts: number
  handling_alerts: number
  overdue_alerts: AlertDTO[]
  pending_ingest: number
  leased_ingest: number
  dead_lettered: number
  failures: FailureDTO[]
}

export interface StatsDTO {
  collectors: number
  active_collectors: number
  rules: number
  readings: number
  alerts: number
  open_alerts: number
  resolved_alerts: number
  pending_ingest: number
  leased_ingest: number
  permanent_failures: number
}

export type Role = 'duty' | 'handler' | 'ops' | 'system'

export interface AcceptResult {
  assignment: AssignmentDTO
  alert: AlertDTO
}
