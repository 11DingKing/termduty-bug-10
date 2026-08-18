import axios, { AxiosInstance, AxiosError } from 'axios'
import type {
  Paged, AlertDTO, ReadingSummaryDTO, ReadingDTO, CollectorDTO,
  RuleDTO, AuditDTO, AssignmentDTO, FailureDTO, ShardManifestDTO,
  BacklogSummary, StatsDTO, AcceptResult, Role
} from '../types'

const STORAGE_KEY = 'termduty_actor'

interface ActorState { id: string; role: Role }

const DEFAULT_ACTOR: ActorState = { id: 'ops-01', role: 'ops' }

export function loadActor(): ActorState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return JSON.parse(raw) as ActorState
  } catch { /* ignore */ }
  return { ...DEFAULT_ACTOR }
}

export function saveActor(a: ActorState): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(a))
}

class ApiClient {
  private http: AxiosInstance
  actor: ActorState

  constructor() {
    this.actor = loadActor()
    this.http = axios.create({ baseURL: '/api', timeout: 15000 })
    this.http.interceptors.request.use(cfg => {
      cfg.headers['X-Actor-ID'] = this.actor.id
      cfg.headers['X-Actor-Role'] = this.actor.role
      return cfg
    })
  }

  setActor(a: ActorState): void {
    this.actor = a
    saveActor(a)
  }

  // ---- Readings ----
  listReadings(params: Record<string, unknown>): Promise<Paged<ReadingSummaryDTO>> {
    return this.http.get('/readings', { params }).then(r => r.data)
  }
  getReading(id: string): Promise<ReadingDTO> {
    return this.http.get(`/readings/${id}`).then(r => r.data)
  }
  exportReadingsUrl(params: Record<string, unknown>): string {
    const sp = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) {
      if (v !== '' && v !== undefined && v !== null) sp.set(k, String(v))
    }
    return `/api/readings/export?${sp.toString()}`
  }

  // ---- Alerts ----
  listAlerts(params: Record<string, unknown>): Promise<Paged<AlertDTO>> {
    return this.http.get('/alerts', { params }).then(r => r.data)
  }
  getAlert(id: string): Promise<AlertDTO> {
    return this.http.get(`/alerts/${id}`).then(r => r.data)
  }
  acceptAlert(id: string, handlerId: string, note: string): Promise<AcceptResult> {
    return this.http.post(`/alerts/${id}/accept`, { handler_id: handlerId, note }).then(r => r.data)
  }
  startAlert(id: string, handlerId: string): Promise<AlertDTO> {
    return this.http.post(`/alerts/${id}/start`, { handler_id: handlerId }).then(r => r.data)
  }
  resolveAlert(id: string, handlerId: string, note: string): Promise<AlertDTO> {
    return this.http.post(`/alerts/${id}/resolve`, { handler_id: handlerId, note }).then(r => r.data)
  }
  releaseAlert(id: string, handlerId: string): Promise<AlertDTO> {
    return this.http.post(`/alerts/${id}/release`, { handler_id: handlerId }).then(r => r.data)
  }
  revokeAlert(id: string): Promise<AlertDTO> {
    return this.http.post(`/alerts/${id}/revoke`, {}).then(r => r.data)
  }
  closeAlert(id: string): Promise<AlertDTO> {
    return this.http.post(`/alerts/${id}/close`, {}).then(r => r.data)
  }

  // ---- Collectors ----
  listCollectors(params: Record<string, unknown>): Promise<Paged<CollectorDTO>> {
    return this.http.get('/collectors', { params }).then(r => r.data)
  }
  getCollector(id: string): Promise<CollectorDTO> {
    return this.http.get(`/collectors/${id}`).then(r => r.data)
  }
  createCollector(data: Record<string, unknown>): Promise<CollectorDTO> {
    return this.http.post('/collectors', data).then(r => r.data)
  }
  updateCollector(id: string, data: Record<string, unknown>): Promise<CollectorDTO> {
    return this.http.patch(`/collectors/${id}`, data).then(r => r.data)
  }
  batchDisable(ids: string[]): Promise<{ completed: number; total?: number; error?: string }> {
    return this.http.post('/collectors/batch/disable', { ids }).then(r => r.data)
  }

  // ---- Rules ----
  listRules(params: Record<string, unknown>): Promise<Paged<RuleDTO>> {
    return this.http.get('/rules', { params }).then(r => r.data)
  }
  createRule(data: Record<string, unknown>): Promise<RuleDTO> {
    return this.http.post('/rules', data).then(r => r.data)
  }
  updateRule(id: string, data: Record<string, unknown>): Promise<RuleDTO> {
    return this.http.patch(`/rules/${id}`, data).then(r => r.data)
  }
  deleteRule(id: string): Promise<void> {
    return this.http.delete(`/rules/${id}`).then(() => undefined)
  }

  // ---- Ops / Audit / Stats ----
  listAudit(params: Record<string, unknown>): Promise<Paged<AuditDTO>> {
    return this.http.get('/audit', { params }).then(r => r.data)
  }
  listAssignments(params: Record<string, unknown>): Promise<Paged<AssignmentDTO>> {
    return this.http.get('/assignments', { params }).then(r => r.data)
  }
  backlog(): Promise<BacklogSummary> {
    return this.http.get('/backlog').then(r => r.data)
  }
  stats(): Promise<StatsDTO> {
    return this.http.get('/stats').then(r => r.data)
  }
  listFailures(params: Record<string, unknown>): Promise<Paged<FailureDTO>> {
    return this.http.get('/failures', { params }).then(r => r.data)
  }
  reinjectFailure(id: string): Promise<FailureDTO> {
    return this.http.post(`/failures/${id}/reinject`, {}).then(r => r.data)
  }
  resolveFailure(id: string): Promise<void> {
    return this.http.post(`/failures/${id}/resolve`, {}).then(() => undefined)
  }
  shardManifest(): Promise<{ shards: ShardManifestDTO[]; total: string }> {
    return this.http.get('/shards').then(r => r.data)
  }
}

export const api = new ApiClient()

export function extractError(err: unknown): string {
  if (err instanceof AxiosError) {
    const data = err.response?.data as { error?: string; message?: string } | undefined
    return data?.error ?? data?.message ?? err.message
  }
  if (err instanceof Error) return err.message
  return '未知错误'
}
