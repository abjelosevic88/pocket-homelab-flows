// Words, icons and small pieces every Flows page shares.
import type { Catalog, Flow, RunStatus, Step, StepKind, TriggerKind } from './types'
import { ago, fmtDateTime } from './format'
import { Icon } from '../components/icons'

export const STATUS_LABEL: Record<string, string> = {
  ok: 'Worked', failed: 'Failed', stopped: 'Stopped by a condition', cancelled: 'Cancelled',
  waiting: 'Waiting', queued: 'Queued', running: 'Running', skipped: 'Skipped', retry: 'Will try again',
}

export function StatusPill({ status, short }: { status: RunStatus | string; short?: boolean }) {
  const label = short && status === 'stopped' ? 'Stopped' : STATUS_LABEL[status] || status
  return <span className={`st-pill ${status}`}><span className={`st-dot ${status}`} />{label}</span>
}

export const TRIGGER_ICON: Record<TriggerKind, string> = { event: 'zap', schedule: 'clock', webhook: 'link', manual: 'play' }
export const TRIGGER_NAME: Record<TriggerKind, string> = { event: 'Something happens', schedule: 'On a schedule', webhook: 'A web address is called', manual: 'When I run it' }

export function FlowIcon({ kind, size = 17 }: { kind: TriggerKind; size?: number }) {
  return <span className={`flow-ico kind-${kind}`}><Icon name={TRIGGER_ICON[kind] || 'zap'} size={size} /></span>
}

export function stepKind(c: Catalog | null, kind: string): StepKind | undefined {
  return c?.steps.find(k => k.kind === kind)
}

/** A step in a few words, for collapsed cards and lists. */
export function stepSummary(c: Catalog | null, s: Step): string {
  const k = stepKind(c, s.kind)
  const cfg = s.config || {}
  const first = cfg.title || cfg.text || cfg.prompt || cfg.url || cfg.description || ''
  if (s.kind === 'wait') return `${cfg.amount || '?'} ${cfg.unit || 'minutes'}`
  if (s.kind === 'filter') return (s.only_if || []).map(c => `${c.field} ${c.op.replace('_', ' ')} ${c.value}`).join(s.match === 'any' ? ' or ' : ' and ') || 'No conditions yet'
  return first ? first.replace(/\s+/g, ' ').slice(0, 90) : k?.description.split('.')[0] || ''
}

/** The apps a flow touches: its trigger's and its steps'. */
export function flowApps(f: Pick<Flow, 'trigger_kind' | 'trigger' | 'steps'>, c: Catalog | null): string[] {
  const out: string[] = []
  if (f.trigger_kind === 'event' && f.trigger.topic) out.push(f.trigger.topic.split('.')[0])
  for (const s of f.steps) { const a = stepKind(c, s.kind)?.app; if (a && !out.includes(a)) out.push(a) }
  return out
}

export function appName(c: Catalog | null, slug: string): string {
  return c?.apps.find(a => a.slug === slug)?.name || `Pocket ${slug[0]?.toUpperCase()}${slug.slice(1)}`
}
export function appIcon(c: Catalog | null, slug: string): string {
  return c?.apps.find(a => a.slug === slug)?.icon || 'layers'
}

export function when(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Date.now() - d.getTime() < 6 * 86400e3 && d.getTime() < Date.now()) return ago(iso)
  return fmtDateTime(iso)
}

/** "in 3 hours", "tomorrow 07:30". */
export function until(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso), ms = d.getTime() - Date.now()
  if (ms < 60e3) return 'any moment'
  if (ms < 3600e3) return `in ${Math.round(ms / 60e3)} min`
  if (ms < 12 * 3600e3) return `in ${Math.round(ms / 3600e3)} h`
  return fmtDateTime(iso)
}

export function newStepID(steps: Step[]): string {
  let n = steps.length + 1
  while (steps.some(s => s.id === `s${n}`)) n++
  return `s${n}`
}
