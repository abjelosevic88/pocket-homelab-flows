// The flow editor: "When" (what starts it) and "Then" (the steps, in order), with placeholders,
// conditions, a test run and the flow's own run history.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Catalog, CatalogApp, Condition, FieldDef, Flow, Option, Run, Step, StepKind, Trigger, TriggerKind } from '../lib/types'
import { StatusPill, TRIGGER_ICON, TRIGGER_NAME, newStepID, stepKind, stepSummary, when, until } from '../lib/flows'
import { Confirm, CopyField, CoreHint, Empty, Field, Loading, Menu, type MenuItem, Popover, Segmented, Switch, errMsg, rectOf, type Anchor, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { announceChange, useCatalog, useCrumb } from '../components/Layout'

type Draft = Pick<Flow, 'name' | 'description' | 'enabled' | 'trigger_kind' | 'trigger' | 'steps'>

const blank: Draft = { name: '', description: '', enabled: true, trigger_kind: 'event', trigger: { topic: '' }, steps: [] }

const DAYS: [string, string][] = [['1', 'Mon'], ['2', 'Tue'], ['3', 'Wed'], ['4', 'Thu'], ['5', 'Fri'], ['6', 'Sat'], ['7', 'Sun']]

/** Fields placeholders can name, for the picker. */
interface Fields { trigger: string[]; always: string[]; steps: { id: string; name: string; out: string[] }[] }

export default function FlowEditor() {
  const { id } = useParams()
  const isNew = !id || id === 'new'
  const loc = useLocation()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { catalog } = useCatalog()
  const { settings } = useApp()
  const [flow, setFlow] = useState<Flow | null>(null)
  const [d, setD] = useState<Draft | null>(null)
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const [leave, setLeave] = useState<string | null>(null)
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [notFound, setNotFound] = useState(false)
  const state = loc.state as { draft?: Flow; topic?: string; fromRecipe?: boolean } | null

  useEffect(() => {
    setNotFound(false)
    if (isNew) {
      const draft = state?.draft
      const base: Draft = draft ? { name: draft.name, description: draft.description, enabled: true, trigger_kind: draft.trigger_kind, trigger: draft.trigger, steps: draft.steps }
        : state?.topic ? { ...blank, trigger: { topic: state.topic } } : { ...blank }
      setFlow(null); setD(base); setDirty(!!draft)
      setOpen(Object.fromEntries(base.steps.map(s => [s.id, true])))
      return
    }
    api.get<Flow>(`${V1}/flows/${id}`).then(f => { setFlow(f); setD({ name: f.name, description: f.description, enabled: f.enabled, trigger_kind: f.trigger_kind, trigger: f.trigger, steps: f.steps }); setDirty(false); setOpen(state?.fromRecipe ? Object.fromEntries(f.steps.map(s => [s.id, true])) : {}) })
      .catch(() => setNotFound(true))
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useCrumb(isNew ? 'New flow' : flow?.name)

  const set = (patch: Partial<Draft>) => { setD(cur => cur ? { ...cur, ...patch } : cur); setDirty(true) }
  const setTrigger = (t: Trigger) => set({ trigger: t })
  const setStep = (i: number, s: Step) => set({ steps: d!.steps.map((x, j) => j === i ? s : x) })

  // the fields the trigger offers (from the newest matching event or webhook call)
  const [tfields, setTfields] = useState<{ fields: string[]; always: string[]; sample: Record<string, unknown> | null }>({ fields: [], always: [], sample: null })
  const topic = d?.trigger.topic || ''
  useEffect(() => {
    if (!d) return
    const p = new URLSearchParams({ kind: d.trigger_kind }); if (topic) p.set('topic', topic)
    api.get<{ fields: string[]; always: string[]; sample: Record<string, unknown> | null }>(`${V1}/flows/${flow?.id || 0}/fields?${p}`).then(setTfields).catch(() => {})
  }, [d?.trigger_kind, topic, flow?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  const save = async (andRun = false) => {
    if (!d) return
    setBusy(true)
    try {
      const body = { ...d, name: d.name.trim() || autoName(d, catalog) }
      const f = isNew ? await api.post<Flow>(`${V1}/flows`, body) : await api.put<Flow>(`${V1}/flows/${flow!.id}`, body)
      setFlow(f); setD({ name: f.name, description: f.description, enabled: f.enabled, trigger_kind: f.trigger_kind, trigger: f.trigger, steps: f.steps }); setDirty(false)
      announceChange()
      if (isNew) navigate(`/flows/${f.id}`, { replace: true })
      if (andRun) await runNow(f.id)
      else toast(f.enabled ? 'Saved — the flow is on' : 'Saved — switched off for now')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const runNow = async (fid = flow?.id, eventID?: number) => {
    if (!fid) return
    try { const r = await api.post<Run>(`${V1}/flows/${fid}/run`, eventID ? { event_id: eventID } : { input: {} }); toast('Started', 'ok', { label: 'Follow it', run: () => navigate(`/runs/${r.id}`) }); reloadRuns() } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async () => { try { await api.del(`${V1}/flows/${flow!.id}`); announceChange(); navigate('/flows') } catch (e) { toast(errMsg(e), 'err') } }
  const rotate = async () => { try { const f = await api.post<Flow>(`${V1}/flows/${flow!.id}/hook`); setFlow(f); toast('New address made; the old one no longer works') } catch (e) { toast(errMsg(e), 'err') } }

  const { data: runs, reload: reloadRuns } = useAsync(() => flow ? api.get<Run[]>(`${V1}/runs?flow=${flow.id}&limit=6`) : Promise.resolve([] as Run[]), [flow?.id])
  const go = (to: string) => { if (dirty) setLeave(to); else navigate(to) }

  // ⌘S saves, esc goes back
  const saveRef = useRef(save); saveRef.current = save
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') { e.preventDefault(); saveRef.current() }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])
  useEffect(() => {
    if (!dirty) return
    const h = (e: BeforeUnloadEvent) => { e.preventDefault() }
    window.addEventListener('beforeunload', h)
    return () => window.removeEventListener('beforeunload', h)
  }, [dirty])

  if (notFound) return <Empty title="Flow not found" icon="flow"><Link to="/flows">Back to your flows</Link></Empty>
  if (!d || !catalog) return <Loading />
  const coreOn = catalog.core || !!settings?.core_configured
  const fields: Fields = {
    trigger: tfields.fields, always: tfields.always,
    steps: d.steps.map(s => ({ id: s.id, name: s.name || stepKind(catalog, s.kind)?.name || s.kind, out: stepKind(catalog, s.kind)?.output || [] })),
  }
  const addStep = (k: StepKind, at = d.steps.length) => {
    const s: Step = { id: newStepID(d.steps), kind: k.kind, config: Object.fromEntries(k.fields.map(f => [f.key, f.default || ''])) }
    if (k.kind === 'filter') s.only_if = [{ field: '', op: 'equals', value: '' }]
    const steps = [...d.steps]; steps.splice(at, 0, s)
    set({ steps }); setOpen(o => ({ ...o, [s.id]: true }))
  }
  const move = (i: number, by: number) => { const steps = [...d.steps]; const [s] = steps.splice(i, 1); steps.splice(i + by, 0, s); set({ steps }) }
  return (
    <div className="ed">
      <div className="row" style={{ gap: 10, alignItems: 'flex-start' }}>
        <div className="grow">
          <input className="ed-name" value={d.name} onChange={e => set({ name: e.target.value })} placeholder={autoName(d, catalog) || 'Name this flow'} aria-label="Name" />
          <input className="muted" style={{ border: 0, background: 'transparent', padding: '2px 0', height: 'auto', fontSize: 13.5 }} value={d.description} onChange={e => set({ description: e.target.value })} placeholder="What it is for (optional)" aria-label="Description" />
        </div>
        <label className="row" style={{ gap: 8, paddingTop: 8 }} title={d.enabled ? 'On: it runs when its trigger happens' : 'Off: it only runs when you press Run now'}>
          <span className="small muted">{d.enabled ? 'On' : 'Off'}</span><Switch checked={d.enabled} onChange={v => set({ enabled: v })} label="On" />
        </label>
      </div>
      {state?.fromRecipe && !flow?.enabled && <div className="callout info"><Icon name="info" size={15} /><div>Added from a recipe and switched off. Check each step (pick your list or account where asked), then switch it on and save.</div></div>}
      {isNew && state?.draft && <div className="callout info"><Icon name="sparkles" size={15} /><div>AI drafted this flow. Look it over, then save.</div></div>}

      <section className="ed-block ed-when">
        <div className="ed-block-head"><span className="ed-num"><Icon name={TRIGGER_ICON[d.trigger_kind]} size={14} /></span><div className="grow"><div className="ed-label">When</div><h3>{TRIGGER_NAME[d.trigger_kind]}</h3></div></div>
        <div className="ed-block-body">
          <div className="kind-grid">
            {(Object.keys(TRIGGER_NAME) as TriggerKind[]).map(k => (
              <button key={k} type="button" className={`kind-btn ${d.trigger_kind === k ? 'on' : ''}`} disabled={k === 'event' && !coreOn} title={k === 'event' && !coreOn ? 'Needs Pocket Core' : undefined}
                onClick={() => set({ trigger_kind: k, trigger: k === 'schedule' ? { repeat: 'daily', time: '08:00' } : k === 'event' ? { topic: d.trigger.topic || '' } : { conditions: d.trigger.conditions } })}>
                <Icon name={TRIGGER_ICON[k]} size={17} />
                <span><b>{TRIGGER_NAME[k]}</b><small>{TRIGGER_HELP[k]}</small></span>
              </button>
            ))}
          </div>
          {!coreOn && <CoreHint />}
          {d.trigger_kind === 'event' && <EventTrigger t={d.trigger} onChange={setTrigger} catalog={catalog} fields={tfields.fields} flowID={flow?.id} onTry={eid => runNow(flow?.id, eid)} dirty={dirty} />}
          {d.trigger_kind === 'schedule' && <ScheduleTrigger t={d.trigger} onChange={setTrigger} next={flow && !dirty ? flow.next_run_at : ''} enabled={d.enabled} />}
          {d.trigger_kind === 'webhook' && <>
            {flow?.hook_url && !dirty ? <WebhookBox url={flow.hook_url} onRotate={rotate} /> : <p className="ed-hint">Save the flow to get its private web address.</p>}
            <Conditions title="Only when the call…" list={d.trigger.conditions || []} match={d.trigger.match || ''} fields={tfields.fields} onChange={(c, m) => setTrigger({ ...d.trigger, conditions: c, match: m })} catalog={catalog} optional />
          </>}
          {d.trigger_kind === 'manual' && <p className="ed-hint">It runs when you press <b>Run now</b> — here, in the list, or from the command palette. Other Pocket apps you allow (the <code>flows:run</code> link) can start it too.</p>}
        </div>
      </section>

      {d.steps.map((s, i) => (
        <React.Fragment key={s.id}>
          <div className="ed-connector" />
          <StepCard step={s} index={i} count={d.steps.length} catalog={catalog} fields={{ ...fields, steps: fields.steps.slice(0, i) }} coreOn={coreOn}
            open={!!open[s.id]} onToggle={() => setOpen(o => ({ ...o, [s.id]: !o[s.id] }))}
            onChange={x => setStep(i, x)} onMove={by => move(i, by)} onRemove={() => set({ steps: d.steps.filter((_, j) => j !== i) })} />
        </React.Fragment>
      ))}
      <div className="ed-connector" />
      <div className="ed-add"><AddStep catalog={catalog} coreOn={coreOn} onAdd={k => addStep(k)} first={d.steps.length === 0} /></div>

      <div className="ed-sticky">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy || (!dirty && !isNew)}>{busy ? 'Saving…' : isNew ? 'Save flow' : dirty ? 'Save changes' : 'Saved'}</button>
        {flow && <button type="button" className="btn" onClick={() => dirty ? save(true) : runNow()} disabled={busy || d.steps.length === 0} title={dirty ? 'Save, then run' : 'Run it now, whatever starts it'}><Icon name="play" size={15} />{dirty ? 'Save and run' : 'Run now'}</button>}
        <span className="spacer" />
        {flow && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}><Icon name="trash" size={15} />Delete</button>}
        <button type="button" className="btn ghost" onClick={() => go('/flows')}>Close</button>
      </div>

      {flow && (
        <section className="ed-block">
          <div className="ed-block-head"><Icon name="history" size={16} /><h3 className="grow">Recent runs</h3>{flow.run_count > 0 && <Link to={`/runs?flow=${flow.id}`} className="btn ghost sm">All {flow.run_count}<Icon name="chevron-right" size={14} /></Link>}</div>
          {!runs || runs.length === 0 ? <p className="ed-hint" style={{ padding: '0 14px 14px' }}>It has not run yet.</p> : (
            <div className="list">
              {runs.map(r => (
                <Link key={r.id} to={`/runs/${r.id}`} className="list-row">
                  <span className="grow" style={{ minWidth: 0 }}><span className="ellipsis block small">{r.status === 'failed' && r.error ? r.error : r.summary}</span></span>
                  <StatusPill status={r.status} short /><span className="muted small" style={{ minWidth: 70, textAlign: 'right' }}>{when(r.created_at)}</span>
                </Link>
              ))}
            </div>
          )}
        </section>
      )}
      {del && <Confirm title={`Delete “${flow?.name}”?`} message="The flow and its run history are deleted. A webhook address it had stops working." confirmLabel="Delete" onConfirm={remove} onCancel={() => setDel(false)} />}
      {leave && <Confirm title="Leave without saving?" message="Your changes to this flow are not saved." confirmLabel="Leave" onConfirm={() => { setDirty(false); navigate(leave) }} onCancel={() => setLeave(null)} />}
    </div>
  )
}

const TRIGGER_HELP: Record<TriggerKind, string> = {
  event: 'Something in one of your Pocket apps: a price drop, a new document, a finished task.',
  schedule: 'Every day at 8, every Monday, every 15 minutes, once on a date.',
  webhook: 'A private address for iOS Shortcuts, Home Assistant, scripts, other services.',
  manual: 'Only when you press Run now.',
}

function autoName(d: Draft, c: Catalog | null): string {
  const first = d.steps[0] ? stepKind(c, d.steps[0].kind)?.name : ''
  let when = ''
  if (d.trigger_kind === 'event' && d.trigger.topic) for (const a of c?.apps || []) for (const t of a.events) if (t.topic === d.trigger.topic) when = t.name
  if (!when && !first) return ''
  return [when, first?.toLowerCase()].filter(Boolean).join(' → ')
}

// ---- When ----

function EventTrigger({ t, onChange, catalog, fields, flowID, onTry, dirty }: { t: Trigger; onChange: (t: Trigger) => void; catalog: Catalog; fields: string[]; flowID?: number; onTry: (eventID: number) => void; dirty: boolean }) {
  const apps = catalog.apps.filter(a => a.events.length > 0)
  const slug = (t.topic || '').split('.')[0]
  const app = apps.find(a => a.slug === slug)
  const event = t.topic || ''
  const { data: lastEvents } = useAsync(() => event ? api.get<{ id: number; topic: string }[]>(`${V1}/events?topic=${encodeURIComponent(event)}&limit=1`) : Promise.resolve([]), [event])
  return (
    <>
      <div className="two-col tight">
        <Field group label="In"><Select ariaLabel="App" value={slug} placeholder="Choose an app…" onChange={v => { const a = apps.find(x => x.slug === v); onChange({ ...t, topic: a?.events[0]?.topic || v + '.*' }) }}
          options={apps.map(a => ({ value: a.slug, label: a.name, hint: a.listening ? undefined : 'not connected' }))} /></Field>
        <Field group label="When">{app ? <Select ariaLabel="Event" value={event} onChange={v => onChange({ ...t, topic: v })}
          options={[...app.events.map(e => ({ value: e.topic, label: e.name })), { value: app.slug + '.*', label: `Anything in ${app.name.replace('Pocket ', '')}` }]} /> : <Select ariaLabel="Event" value="" onChange={() => {}} options={[]} placeholder="Choose an app first" disabled />}</Field>
      </div>
      {app && catalog.core && !app.listening && <AppNotListening app={app} />}
      <Conditions title="Only if…" list={t.conditions || []} match={t.match || ''} fields={fields} onChange={(c, m) => onChange({ ...t, conditions: c, match: m })} catalog={catalog} optional />
      {fields.length > 0 ? <p className="ed-hint">This event brings: {fields.slice(0, 14).map(f => <code key={f} style={{ marginRight: 6 }}>{f}</code>)}{fields.length > 14 && '…'} — use them in steps as <code>{'{{title}}'}</code>.</p>
        : event && <p className="ed-hint">The fields this event brings show up once it has happened once.</p>}
      {flowID && !dirty && lastEvents && lastEvents.length > 0 && <div><button type="button" className="btn sm" onClick={() => onTry(lastEvents[0].id)}><Icon name="play" size={14} />Try it with the last {app?.name.replace('Pocket ', '')} event</button></div>}
    </>
  )
}

function AppNotListening({ app }: { app: CatalogApp }) {
  return <div className="callout warn"><Icon name="alert" size={15} /><div>Flows does not hear {app.name} yet{app.detail ? ` (${app.detail})` : ''}. In Pocket Core → <b>Links</b>, switch on <b>Pocket Flows → {app.name}: Read activity events</b>.</div></div>
}

function ScheduleTrigger({ t, onChange, next, enabled }: { t: Trigger; onChange: (t: Trigger) => void; next: string; enabled: boolean }) {
  const r = t.repeat || 'daily'
  const set = (p: Partial<Trigger>) => onChange({ ...t, ...p })
  return (
    <>
      <Field group label="Repeat">
        <Segmented size="sm" value={r} onChange={v => set({ repeat: v, every: v === 'minutes' ? 15 : v === 'hourly' ? 1 : undefined, days: v === 'weekly' ? (t.days || '12345') : t.days, day_of_month: v === 'monthly' ? (t.day_of_month || 1) : t.day_of_month })}
          options={[{ value: 'minutes', label: 'Minutes' }, { value: 'hourly', label: 'Hourly' }, { value: 'daily', label: 'Daily' }, { value: 'weekly', label: 'Weekly' }, { value: 'monthly', label: 'Monthly' }, { value: 'once', label: 'Once' }]} />
      </Field>
      <div className="row wrap" style={{ gap: 12, alignItems: 'flex-end' }}>
        {r === 'minutes' && <Field label="Every (minutes)"><input type="number" min={5} max={720} value={t.every || 15} onChange={e => set({ every: Number(e.target.value) })} style={{ width: 110 }} /></Field>}
        {r === 'hourly' && <>
          <Field label="Every (hours)"><input type="number" min={1} max={24} value={t.every || 1} onChange={e => set({ every: Number(e.target.value) })} style={{ width: 110 }} /></Field>
          <Field label="At minute"><input type="number" min={0} max={59} value={Number((t.time || '00:00').split(':')[1])} onChange={e => set({ time: `00:${String(Math.min(59, Math.max(0, Number(e.target.value)))).padStart(2, '0')}` })} style={{ width: 110 }} /></Field>
        </>}
        {['daily', 'weekly', 'monthly'].includes(r) && <Field label="At"><input type="time" value={t.time || '08:00'} onChange={e => set({ time: e.target.value })} style={{ width: 130 }} /></Field>}
        {r === 'monthly' && <Field label="Day of the month" help="31 = the last day"><input type="number" min={1} max={31} value={t.day_of_month || 1} onChange={e => set({ day_of_month: Number(e.target.value) })} style={{ width: 110 }} /></Field>}
        {r === 'once' && <Field label="On"><input type="datetime-local" value={t.at || ''} onChange={e => set({ at: e.target.value })} /></Field>}
      </div>
      {r === 'weekly' && <div className="row wrap">{DAYS.map(([n, l]) => <button key={n} type="button" className={`chip day-chip ${(t.days || '').includes(n) ? 'on' : ''}`} onClick={() => set({ days: (t.days || '').includes(n) ? (t.days || '').replace(n, '') : [...(t.days || ''), n].sort().join('') })}>{l}</button>)}</div>}
      <p className="ed-hint">{next ? <>Next run {until(next)}.</> : enabled ? 'Saved times use the server’s clock.' : 'Switched off: it does not run on its own.'}</p>
    </>
  )
}

function WebhookBox({ url, onRotate }: { url: string; onRotate: () => void }) {
  const [rot, setRot] = useState(false)
  return (
    <div className="hook-box">
      <span className="small muted">Send a POST (or GET) to this private address. A JSON body's fields become <code>{'{{field}}'}</code>; so do <code>?query</code> values.</span>
      <CopyField value={url} label="Webhook address" />
      <pre className="code">{`curl -X POST ${url} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"text": "Buy milk tomorrow"}'`}</pre>
      <div><button type="button" className="btn ghost sm" onClick={() => setRot(true)}><Icon name="refresh" size={14} />Make a new address</button></div>
      {rot && <Confirm title="Make a new address?" message="The current address stops working at once; anything that calls it needs the new one." confirmLabel="Make a new one" onConfirm={() => { setRot(false); onRotate() }} onCancel={() => setRot(false)} />}
    </div>
  )
}

// ---- conditions ----

function Conditions({ title, list, match, fields, onChange, catalog, optional }: { title: string; list: Condition[]; match: '' | 'any'; fields: string[]; onChange: (c: Condition[], m: '' | 'any') => void; catalog: Catalog; optional?: boolean }) {
  const [shown, setShown] = useState(list.length > 0 || !optional)
  const listID = useMemo(() => 'f' + Math.random().toString(36).slice(2), [])
  if (!shown) return <div><button type="button" className="btn ghost sm" onClick={() => { setShown(true); if (list.length === 0) onChange([{ field: fields[0] || '', op: 'equals', value: '' }], match) }}><Icon name="filter" size={14} />{title.replace('…', '')}: add a condition</button></div>
  const upd = (i: number, c: Condition) => onChange(list.map((x, j) => j === i ? c : x), match)
  return (
    <div className="stack" style={{ gap: 8 }}>
      <div className="row" style={{ gap: 8 }}>
        <span className="small"><b>{title}</b></span>
        {list.length > 1 && <Segmented size="sm" value={match || 'all'} onChange={v => onChange(list, v === 'any' ? 'any' : '')} options={[{ value: 'all', label: 'all hold' }, { value: 'any', label: 'any holds' }]} />}
      </div>
      <datalist id={listID}>{fields.map(f => <option key={f} value={f} />)}</datalist>
      {list.map((c, i) => (
        <div key={i} className="ed-cond">
          <input list={listID} value={c.field} onChange={e => upd(i, { ...c, field: e.target.value })} placeholder="field, e.g. price" aria-label="Field" />
          <Select ariaLabel="Comparison" value={c.op} onChange={v => upd(i, { ...c, op: v })} options={catalog.ops} />
          {['exists', 'missing'].includes(c.op) ? <span /> : <input value={c.value} onChange={e => upd(i, { ...c, value: e.target.value })} placeholder="value" aria-label="Value" />}
          <button type="button" className="btn ghost icon-only sm" aria-label="Remove the condition" onClick={() => { const n = list.filter((_, j) => j !== i); onChange(n, match); if (n.length === 0 && optional) setShown(false) }}><Icon name="x" size={14} /></button>
        </div>
      ))}
      <div><button type="button" className="btn ghost sm" onClick={() => onChange([...list, { field: '', op: 'equals', value: '' }], match)}><Icon name="plus" size={14} />Another condition</button></div>
    </div>
  )
}

// ---- Then ----

function AddStep({ catalog, coreOn, onAdd, first }: { catalog: Catalog; coreOn: boolean; onAdd: (k: StepKind) => void; first: boolean }) {
  const [menu, setMenu] = useState<Anchor | null>(null)
  const app = (slug?: string) => catalog.apps.find(a => a.slug === slug)
  const items: MenuItem[] = [{ heading: 'Built in' }]
  for (const k of catalog.steps.filter(k => !k.app)) items.push({ label: k.name, icon: k.icon, onSelect: () => onAdd(k) })
  items.push({ heading: 'In your Pocket apps' })
  for (const k of catalog.steps.filter(k => k.app)) {
    const a = app(k.app)
    items.push({ label: k.name, icon: k.icon, disabled: !coreOn && !a?.acting, hint: !coreOn && !a?.acting ? 'needs Pocket Core' : a && !a.acting ? 'not connected' : a?.name.replace('Pocket ', ''), onSelect: () => onAdd(k) })
  }
  return (
    <>
      <button type="button" className={`btn ${first ? 'primary' : ''}`} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))} aria-haspopup="menu"><Icon name="plus" size={15} />{first ? 'Then do this…' : 'Add a step'}</button>
      {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={290} />}
    </>
  )
}

function StepCard({ step, index, count, catalog, fields, coreOn, open, onToggle, onChange, onMove, onRemove }: {
  step: Step; index: number; count: number; catalog: Catalog; fields: Fields; coreOn: boolean; open: boolean
  onToggle: () => void; onChange: (s: Step) => void; onMove: (by: number) => void; onRemove: () => void
}) {
  const k = stepKind(catalog, step.kind)
  const app = k?.app ? catalog.apps.find(a => a.slug === k.app) : undefined
  const setCfg = (key: string, v: string) => onChange({ ...step, config: { ...step.config, [key]: v } })
  const [cond, setCond] = useState((step.only_if || []).length > 0)
  return (
    <section className={`ed-block ed-step ${open ? '' : 'collapsed'}`}>
      <div className="ed-block-head">
        <span className="ed-num">{index + 1}</span>
        <button type="button" className="ed-step-title" onClick={onToggle} aria-expanded={open}>
          <b>{step.name || k?.name || step.kind}</b>
          <span>{k?.app ? `${app?.name || k.app} · ` : ''}{stepSummary(catalog, step)}</span>
        </button>
        <div className="ed-tools">
          <button type="button" className="btn ghost icon-only sm" aria-label="Move up" disabled={index === 0} onClick={() => onMove(-1)}><Icon name="chevron-up" size={15} /></button>
          <button type="button" className="btn ghost icon-only sm" aria-label="Move down" disabled={index === count - 1} onClick={() => onMove(1)}><Icon name="chevron-down" size={15} /></button>
          <button type="button" className="btn ghost icon-only sm" aria-label="Remove the step" onClick={onRemove}><Icon name="trash" size={15} /></button>
        </div>
      </div>
      <div className="ed-block-body">
        {!k ? <p className="bad small">Unknown step “{step.kind}”.</p> : <>
          <p className="ed-hint" style={{ margin: 0 }}>{k.description}</p>
          {app && !app.acting && <div className="callout warn"><Icon name="alert" size={15} /><div>{coreOn ? <>Flows cannot reach {app.name} for you yet{app.detail ? ` (${app.detail})` : ''}. Switch on <b>Pocket Flows → {app.name}</b> in Pocket Core → Links, or connect it directly in <Link to="/settings/apps">Settings → Connected apps</Link>.</> : <>Connect {app.name} in <Link to="/settings/apps">Settings → Connected apps</Link>.<CoreHint /></>}</div></div>}
          {step.kind === 'filter'
            ? <Conditions title="Continue only if…" list={step.only_if || []} match={step.match || ''} fields={allFields(fields)} onChange={(c, m) => onChange({ ...step, only_if: c, match: m })} catalog={catalog} />
            : k.fields.map(f => <StepField key={f.key} f={f} value={step.config[f.key] ?? ''} onChange={v => setCfg(f.key, v)} fields={fields} />)}
          {step.kind !== 'filter' && <>
            {cond ? <Conditions title="Only do this step if…" list={step.only_if || []} match={step.match || ''} fields={allFields(fields)} onChange={(c, m) => { onChange({ ...step, only_if: c, match: m }); if (c.length === 0) setCond(false) }} catalog={catalog} optional />
              : <div className="row wrap" style={{ gap: 8 }}>
                <button type="button" className="btn ghost sm" onClick={() => { setCond(true); onChange({ ...step, only_if: [{ field: '', op: 'equals', value: '' }] }) }}><Icon name="filter" size={14} />Only if…</button>
              </div>}
            <div className="row" style={{ gap: 10 }}>
              <Switch size="sm" checked={step.on_error === 'continue'} onChange={v => onChange({ ...step, on_error: v ? 'continue' : '' })} label="Carry on if this step fails" />
              <span className="small muted">Carry on with the next step if this one fails</span>
            </div>
            <Field label="Step name (optional)"><input value={step.name || ''} onChange={e => onChange({ ...step, name: e.target.value })} placeholder={k.name} /></Field>
            {k.output && k.output.length > 0 && <p className="ed-hint" style={{ margin: 0 }}>Later steps can use {k.output.map(o => <code key={o} style={{ marginRight: 6 }}>{`{{steps.${step.id}.${o}}}`}</code>)}</p>}
          </>}
        </>}
      </div>
    </section>
  )
}

function allFields(f: Fields): string[] {
  return [...f.trigger, ...f.steps.flatMap(s => s.out.map(o => `steps.${s.id}.${o}`)), ...f.always]
}

function StepField({ f, value, onChange, fields }: { f: FieldDef; value: string; onChange: (v: string) => void; fields: Fields }) {
  const ref = useRef<HTMLInputElement & HTMLTextAreaElement>(null)
  const insert = (ph: string) => {
    const el = ref.current
    const tok = `{{${ph}}}`
    if (!el) { onChange(value + tok); return }
    const a = el.selectionStart ?? value.length, b = el.selectionEnd ?? value.length
    onChange(value.slice(0, a) + tok + value.slice(b))
    requestAnimationFrame(() => { el.focus(); el.setSelectionRange(a + tok.length, a + tok.length) })
  }
  const label = <>{f.label}{f.required && <span className="muted"> *</span>}</>
  switch (f.type) {
    case 'select':
      return <Field group label={label} help={f.help}><Select ariaLabel={f.label} value={value || f.default || ''} onChange={onChange} options={f.options || []} /></Field>
    case 'bool':
      return <div className="row" style={{ gap: 10 }}><Switch size="sm" checked={value === 'true'} onChange={v => onChange(v ? 'true' : 'false')} label={f.label} /><span className="small">{f.label}</span>{f.help && <span className="small muted">— {f.help}</span>}</div>
    case 'picker':
      return <Field group label={label} help={f.help}><PickerSelect name={f.picker!} value={value} onChange={onChange} label={f.label} required={f.required} /></Field>
    case 'number':
      return <Field label={label} help={f.help}><input type="number" value={value} onChange={e => onChange(e.target.value)} placeholder={f.placeholder} style={{ width: 140 }} /></Field>
  }
  const multi = f.type === 'textarea' || f.type === 'keyvalue'
  return (
    <Field group label={label} help={f.help}>
      <div className="field-wrap">
        {multi ? <textarea ref={ref} value={value} onChange={e => onChange(e.target.value)} placeholder={f.type === 'keyvalue' ? 'Authorization: Bearer …' : f.placeholder} rows={f.type === 'keyvalue' ? 2 : 3} />
          : <input ref={ref} value={value} onChange={e => onChange(e.target.value)} placeholder={f.placeholder} />}
        {!f.no_template && <Placeholders fields={fields} onPick={insert} />}
      </div>
    </Field>
  )
}

function Placeholders({ fields, onPick }: { fields: Fields; onPick: (ph: string) => void }) {
  const [anchor, setAnchor] = useState<Anchor | null>(null)
  const pick = (p: string) => { setAnchor(null); onPick(p) }
  return (
    <>
      <button type="button" className="btn ghost icon-only sm ph-btn" title="Insert a value from the trigger or an earlier step" aria-label="Insert a value" onClick={e => setAnchor(anchor ? null : rectOf(e.currentTarget))}><Icon name="code" size={14} /></button>
      {anchor && (
        <Popover anchor={anchor} onClose={() => setAnchor(null)} prefer="below" width={340} label="Insert a value">
          <div className="ph-list">
            <span className="ph-group">From what started it</span>
            {fields.trigger.length === 0 ? <span className="small muted">Shows up once the trigger has happened once. You can still type <code>{'{{name}}'}</code>.</span>
              : fields.trigger.map(f => <button key={f} type="button" className="ph-chip" onClick={() => pick(f)}>{f}</button>)}
            {fields.steps.filter(s => s.out.length).map(s => (
              <React.Fragment key={s.id}>
                <span className="ph-group">From “{s.name}”</span>
                {s.out.map(o => <button key={o} type="button" className="ph-chip" onClick={() => pick(`steps.${s.id}.${o}`)}>{o}</button>)}
              </React.Fragment>
            ))}
            <span className="ph-group">Always there</span>
            {fields.always.map(f => <button key={f} type="button" className="ph-chip" onClick={() => pick(f)}>{f}</button>)}
          </div>
        </Popover>
      )}
    </>
  )
}

function PickerSelect({ name, value, onChange, label, required }: { name: string; value: string; onChange: (v: string) => void; label: string; required?: boolean }) {
  const { data, reload } = useAsync(() => api.get<{ ok: boolean; error?: string; options: Option[] }>(`${V1}/pickers?name=${encodeURIComponent(name)}`), [name])
  if (!data) return <Select ariaLabel={label} value="" onChange={() => {}} options={[]} placeholder="Loading…" disabled />
  if (!data.ok) return (
    <div className="stack" style={{ gap: 6 }}>
      <span className="small bad">{data.error}</span>
      <div className="row" style={{ gap: 8 }}><input value={value} onChange={e => onChange(e.target.value)} placeholder="or type the id" style={{ width: 160 }} /><button type="button" className="btn ghost sm" onClick={reload}>Try again</button></div>
    </div>
  )
  const opts = [...(required ? [] : [{ value: '', label: 'Default' }]), ...data.options]
  if (value && !opts.some(o => o.value === value)) opts.push({ value, label: `#${value} (not found)` })
  return <Select ariaLabel={label} value={value} onChange={onChange} options={opts} placeholder={`Choose ${label.toLowerCase()}…`} />
}

