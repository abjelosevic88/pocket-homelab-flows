// Run history: every run of every flow, newest first; one run with what each step did.
import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Flow, Run } from '../lib/types'
import { STATUS_LABEL, StatusPill, TRIGGER_ICON, when } from '../lib/flows'
import { fmtDateTime } from '../lib/format'
import { Card, Empty, Loading, PageHeader, Segmented, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { useCrumb, useOnChange } from '../components/Layout'

const PAGE = 50
type StatusFilter = '' | 'ok' | 'failed' | 'stopped' | 'active'

export default function Runs() {
  const [sp, setSp] = useSearchParams()
  const status = (sp.get('status') || '') as StatusFilter
  const flow = sp.get('flow') || ''
  const { data: flows } = useAsync(() => api.get<Flow[]>(`${V1}/flows`), [])
  const [items, setItems] = useState<Run[] | null>(null)
  const [more, setMore] = useState(false)
  const set = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }) }
  const load = (before = 0) => {
    const p = new URLSearchParams({ limit: String(PAGE) })
    if (status) p.set('status', status); if (flow) p.set('flow', flow); if (before) p.set('before', String(before))
    api.get<Run[]>(`${V1}/runs?${p}`).then(r => { setItems(prev => before ? [...(prev || []), ...r] : r); setMore(r.length === PAGE) }).catch(() => setItems([]))
  }
  useEffect(() => { load() }, [status, flow]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(() => load())
  // runs that are still going refresh by themselves
  const live = items?.some(r => ['queued', 'running'].includes(r.status))
  useEffect(() => { if (!live) return; const t = setTimeout(() => load(), 2500); return () => clearTimeout(t) }, [live, items]) // eslint-disable-line react-hooks/exhaustive-deps
  const flowName = flows?.find(f => String(f.id) === flow)?.name
  return (
    <div className="stack loose">
      <PageHeader title="Run history" sub={flowName ? `Runs of “${flowName}”` : 'Every time a flow ran, and what each step did.'}
        actions={<a className="btn ghost sm" href={`${V1}/export.csv`}><Icon name="download" size={14} />CSV</a>} />
      <div className="row wrap filters">
        <Segmented size="sm" value={status} onChange={v => set('status', v)} options={[{ value: '', label: 'All' }, { value: 'failed', label: 'Failed' }, { value: 'ok', label: 'Worked' }, { value: 'active', label: 'Waiting' }, { value: 'stopped', label: 'Stopped' }]} />
        <Select ariaLabel="Flow" value={flow} onChange={v => set('flow', v)} options={[{ value: '', label: 'Every flow' }, ...(flows || []).map(f => ({ value: String(f.id), label: f.name }))]} />
      </div>
      {!items ? <Loading /> : items.length === 0 ? (
        <Empty title={status || flow ? 'No runs match' : 'Nothing has run yet'} icon="history">{status || flow ? 'Try another filter.' : 'Runs show up here as your flows start.'}</Empty>
      ) : (
        <Card flush>
          <div className="list">
            {items.map(r => (
              <Link key={r.id} to={`/runs/${r.id}`} className="list-row">
                <span className="list-ico"><Icon name={TRIGGER_ICON[r.trigger_kind] || 'play'} size={16} /></span>
                <span className="grow" style={{ minWidth: 0 }}>
                  <b className="ellipsis block">{r.flow_name}</b>
                  <span className="muted small ellipsis block">{r.status === 'failed' && r.error ? r.error : r.summary}</span>
                </span>
                <StatusPill status={r.status} short />
                <span className="muted small" style={{ minWidth: 76, textAlign: 'right' }}>{when(r.created_at)}</span>
              </Link>
            ))}
          </div>
          {more && <div className="pad"><button type="button" className="btn ghost sm" onClick={() => load(items[items.length - 1].id)}>Show older</button></div>}
        </Card>
      )}
    </div>
  )
}

const LOG_ICON: Record<string, string> = { ok: 'check-circle', failed: 'x-circle', skipped: 'arrow-right', waiting: 'hourglass', retry: 'refresh', stopped: 'filter' }

export function RunPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { data: run, reload, error } = useAsync(() => api.get<Run>(`${V1}/runs/${id}`), [id])
  useCrumb(run ? `${run.flow_name} #${run.id}` : undefined)
  const going = run && ['queued', 'running'].includes(run.status)
  useEffect(() => { if (!going) return; const t = setTimeout(reload, 1500); return () => clearTimeout(t) }, [going, run]) // eslint-disable-line react-hooks/exhaustive-deps
  const [showData, setShowData] = useState(false)
  if (error) return <Empty title="Run not found" icon="history"><Link to="/runs">Back to the history</Link></Empty>
  if (!run) return <Loading />
  const act = async (what: 'retry' | 'cancel') => { try { await api.post(`${V1}/runs/${run.id}/${what}`); toast(what === 'retry' ? 'Trying again' : 'Cancelled'); reload() } catch (e) { toast(errMsg(e), 'err') } }
  const again = async () => { try { const r = await api.post<Run>(`${V1}/flows/${run.flow_id}/run`, { input: run.input }); navigate(`/runs/${r.id}`) } catch (e) { toast(errMsg(e), 'err') } }
  const dur = run.finished_at && run.started_at ? (new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) / 1000 : 0
  return (
    <div className="stack loose">
      <PageHeader title={<span className="row" style={{ gap: 10 }}>{run.flow_name}<StatusPill status={run.status} /></span>}
        sub={<>{run.summary} · {fmtDateTime(run.created_at)}{dur > 0 ? ` · took ${dur < 1 ? '<1' : Math.round(dur)} s` : ''}</>}
        actions={<>
          {['failed', 'cancelled'].includes(run.status) && <button type="button" className="btn primary sm" onClick={() => act('retry')}><Icon name="refresh" size={14} />Try again from the failed step</button>}
          {['queued', 'waiting'].includes(run.status) && <button type="button" className="btn sm" onClick={() => act('cancel')}><Icon name="x" size={14} />Cancel</button>}
          {['ok', 'stopped', 'failed'].includes(run.status) && <button type="button" className="btn ghost sm" onClick={again}><Icon name="play" size={14} />Run again with the same input</button>}
          <Link to={`/flows/${run.flow_id}`} className="btn ghost sm"><Icon name="edit" size={14} />Open the flow</Link>
        </>} />
      {run.status === 'failed' && run.error && <div className="callout warn"><Icon name="alert" size={15} /><div>{run.error}</div></div>}
      {run.status === 'waiting' && run.resume_at && <div className="callout info"><Icon name="hourglass" size={15} /><div>{run.attempt > 0 ? `Try ${run.attempt + 1} of 4` : 'Waiting'} — goes on {when(run.resume_at) || fmtDateTime(run.resume_at)}{new Date(run.resume_at) > new Date() ? ` (${fmtDateTime(run.resume_at)})` : ''}.</div></div>}
      <Card title="Steps" flush>
        {run.log.length === 0 ? <p className="muted small pad">{STATUS_LABEL[run.status] || run.status}: no step has run yet.</p> : (
          <ol className="log">
            {run.log.map((e, i) => (
              <li key={i} className={`log-row ${e.status}`}>
                <Icon name={LOG_ICON[e.status] || 'circle'} size={18} className={e.status === 'ok' ? 'ok' : e.status === 'failed' ? 'bad' : 'muted'} />
                <div style={{ minWidth: 0 }}>
                  <b>{e.step}. {e.name}</b> <span className="muted small">{STATUS_LABEL[e.status] || e.status}</span>
                  {e.message && <div className="log-msg">{e.message}</div>}
                  {run.outputs[e.step_id] != null && Object.keys(run.outputs[e.step_id] as object).length > 0 && showData && <pre className="code mt">{JSON.stringify(run.outputs[e.step_id], null, 2)}</pre>}
                </div>
                <span className="log-ms">{e.ms >= 1000 ? `${(e.ms / 1000).toFixed(1)} s` : `${e.ms} ms`}</span>
              </li>
            ))}
          </ol>
        )}
      </Card>
      <Card title="What started it" actions={<button type="button" className="btn ghost sm" onClick={() => setShowData(v => !v)}>{showData ? 'Hide step data' : 'Show step data'}</button>}>
        <pre className="code">{JSON.stringify(run.input, null, 2)}</pre>
      </Card>
    </div>
  )
}
