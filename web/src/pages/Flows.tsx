// Every flow, with a switch to turn it on or off, what starts it and how it went last time.
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Flow, TriggerKind } from '../lib/types'
import { FlowIcon, StatusPill, TRIGGER_NAME, appIcon, appName, flowApps, when } from '../lib/flows'
import { Card, Confirm, Empty, Loading, MenuButton, PageHeader, Segmented, Switch, errMsg, useAsync, useDebounce, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { announceChange, openCreate, useCatalog, useOnChange } from '../components/Layout'

export default function Flows() {
  const { catalog } = useCatalog()
  const { toast } = useToast()
  const navigate = useNavigate()
  const { data, reload, setData } = useAsync(() => api.get<Flow[]>(`${V1}/flows`), [])
  useOnChange(reload)
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 150).toLowerCase()
  const [state, setState] = useState<'all' | 'on' | 'off'>('all')
  const [kind, setKind] = useState<'' | TriggerKind>('')
  const [del, setDel] = useState<Flow | null>(null)
  if (!data) return <Loading />
  const list = data.filter(f => (!dq || `${f.name} ${f.description} ${f.trigger_text}`.toLowerCase().includes(dq)) && (state === 'all' || (state === 'on') === f.enabled) && (!kind || f.trigger_kind === kind))
  const toggle = async (f: Flow, on: boolean) => {
    setData(data.map(x => x.id === f.id ? { ...x, enabled: on } : x))
    try { await api.post(`${V1}/flows/${f.id}/enabled`, { enabled: on }); announceChange() } catch (e) { toast(errMsg(e), 'err'); reload() }
  }
  const run = async (f: Flow) => { try { const r = await api.post<{ id: number }>(`${V1}/flows/${f.id}/run`, {}); toast('Started', 'ok', { label: 'Follow it', run: () => navigate(`/runs/${r.id}`) }) } catch (e) { toast(errMsg(e), 'err') } }
  const dup = async (f: Flow) => { try { const c = await api.post<Flow>(`${V1}/flows/${f.id}/duplicate`); navigate(`/flows/${c.id}`) } catch (e) { toast(errMsg(e), 'err') } }
  const remove = async () => { if (!del) return; try { await api.del(`${V1}/flows/${del.id}`); setDel(null); reload(); announceChange() } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <div className="stack loose">
      <PageHeader title="Flows" sub={`${data.filter(f => f.enabled).length} on · ${data.length} in all`}
        actions={<>
          <button type="button" className="btn ghost sm" onClick={() => openCreate('describe')}><Icon name="sparkles" size={14} />Describe</button>
          <button type="button" className="btn primary sm" onClick={() => openCreate('new')}><Icon name="plus" size={14} />New flow</button>
        </>} />
      {data.length > 0 && (
        <div className="row wrap filters">
          <div className="search-box"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a flow…" aria-label="Find a flow" /></div>
          <Segmented size="sm" value={state} onChange={setState} options={[{ value: 'all', label: 'All' }, { value: 'on', label: 'On' }, { value: 'off', label: 'Off' }]} />
          <Select ariaLabel="Started by" value={kind} onChange={v => setKind(v as '' | TriggerKind)} options={[{ value: '', label: 'Any trigger' }, ...(Object.keys(TRIGGER_NAME) as TriggerKind[]).map(k => ({ value: k, label: TRIGGER_NAME[k] }))]} />
        </div>
      )}
      {data.length === 0 ? (
        <Empty title="No flows yet" icon="flow">
          Start from a recipe, describe one in your own words, or build one step by step.
          <div className="row wrap" style={{ justifyContent: 'center', marginTop: 12 }}>
            <Link to="/recipes" className="btn"><Icon name="book" size={15} />Recipes</Link>
            <button type="button" className="btn primary" onClick={() => openCreate('new')}><Icon name="plus" size={15} />New flow</button>
          </div>
        </Empty>
      ) : list.length === 0 ? <Empty title="Nothing matches" icon="search">Try fewer filters.</Empty> : (
        <Card flush>
          <div className="list">
            {list.map(f => (
              <div key={f.id} className={`flow-row ${f.enabled ? '' : 'off'}`}>
                <Link to={`/flows/${f.id}`} className="flow-main">
                  <FlowIcon kind={f.trigger_kind} />
                  <span style={{ minWidth: 0 }}>
                    <span className="flow-name block">{f.name}</span>
                    <span className="flow-sub">
                      <span className="flow-apps">{flowApps(f, catalog).slice(0, 4).map(a => <Icon key={a} name={appIcon(catalog, a)} size={13} title={appName(catalog, a)} />)}</span>
                      <span className="ellipsis">{f.trigger_text} · {f.steps.length} {f.steps.length === 1 ? 'step' : 'steps'}</span>
                    </span>
                  </span>
                </Link>
                <span className="flow-meta">
                  {f.last_status ? <StatusPill status={f.last_status} short /> : <span>never ran</span>}
                  <span>{f.last_run_at ? `${when(f.last_run_at)} · ${f.run_count} runs` : ''}</span>
                </span>
                <Switch checked={f.enabled} onChange={v => toggle(f, v)} label={f.enabled ? 'Switch off' : 'Switch on'} />
                <MenuButton label="More" align="right" items={[
                  { label: 'Run now', icon: 'play', onSelect: () => run(f) },
                  { label: 'Run history', icon: 'history', onSelect: () => navigate(`/runs?flow=${f.id}`) },
                  { label: 'Duplicate', icon: 'copy', onSelect: () => dup(f) },
                  'sep',
                  { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(f) },
                ]}><Icon name="more" size={16} /></MenuButton>
              </div>
            ))}
          </div>
        </Card>
      )}
      {del && <Confirm title={`Delete “${del.name}”?`} message="The flow and its run history are deleted. A webhook address it had stops working." confirmLabel="Delete" onConfirm={remove} onCancel={() => setDel(null)} />}
    </div>
  )
}
