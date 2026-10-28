export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface AppSetting {
  slug: string; name: string; events: boolean; actions: boolean
  mode: ConnMode; via: ConnVia; detail: string; listening: boolean
  url: string; token: string; public_url: string; open_url: string
}

export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number; http_private: boolean
  notify_url: string; notify_format: string; notify_configured: boolean; notify_failures: boolean; notify_recover: boolean; keep_runs_days: number
  apps: AppSetting[]
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string; timezone: string
}

// ---- flows ----

export type TriggerKind = 'event' | 'schedule' | 'webhook' | 'manual'
export interface Condition { field: string; op: string; value: string }
export interface Trigger {
  topic?: string; conditions?: Condition[]; match?: '' | 'any'
  repeat?: 'minutes' | 'hourly' | 'daily' | 'weekly' | 'monthly' | 'once'; every?: number; time?: string; days?: string; day_of_month?: number; at?: string
}
export interface Step { id: string; kind: string; name?: string; config: Record<string, string>; only_if?: Condition[]; match?: '' | 'any'; on_error?: '' | 'continue' }
export interface Flow {
  id: number; name: string; description: string; enabled: boolean; trigger_kind: TriggerKind; trigger: Trigger; steps: Step[]
  next_run_at: string; last_run_at: string; last_status: string; run_count: number; fail_count: number
  recipe: string; source: string; created_at: string; updated_at: string
  hook_url?: string; trigger_text: string
}
export type RunStatus = 'queued' | 'running' | 'waiting' | 'ok' | 'failed' | 'stopped' | 'cancelled'
export interface LogEntry { step: number; step_id: string; kind: string; name: string; status: string; message: string; at: string; ms: number }
export interface Run {
  id: number; flow_id: number; flow_name: string; trigger_kind: TriggerKind; trigger_ref: string; summary: string
  input: Record<string, unknown>; status: RunStatus; step: number; attempt: number; outputs: Record<string, unknown>; log: LogEntry[]
  error: string; resume_at: string; started_at: string; finished_at: string; created_at: string
}
export interface FlowEvent { id: number; core_id: number; topic: string; source: string; subject: string; payload: Record<string, unknown>; matched: number; received_at: string }

// ---- the catalog ----

export interface Option { value: string; label: string }
export interface FieldDef {
  key: string; label: string; type: 'text' | 'textarea' | 'number' | 'select' | 'picker' | 'bool' | 'keyvalue'
  help?: string; placeholder?: string; default?: string; required?: boolean; options?: Option[]; picker?: string; no_template?: boolean
}
export interface StepKind { kind: string; app?: string; name: string; description: string; icon: string; fields: FieldDef[]; scopes?: string[]; output?: string[] }
export interface Topic { topic: string; name: string }
export interface CatalogApp { slug: string; name: string; icon: string; events: Topic[]; listening: boolean; acting: ConnVia; detail: string; actions: boolean; open_url: string }
export interface StreamStatus { connected: boolean; error: string; last_event_at?: string }
export interface Catalog { apps: CatalogApp[]; steps: StepKind[]; ops: Option[]; core: boolean; stream: StreamStatus }

export interface Recipe { key: string; name: string; description: string; icon: string; group: string; apps: string[]; flow: Flow }

export interface RunStats { flows: number; enabled: number; runs_today: number; failed_today: number; runs_week: number; failed_week: number; active: number; events_today: number }
export interface Overview {
  stats: RunStats; days: { day: string; ok: number; failed: number }[]; recent: Run[]; failing: Flow[]; upcoming: Flow[]; stream: StreamStatus
}
