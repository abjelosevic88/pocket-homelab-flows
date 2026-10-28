// Overview: today's numbers, what needs attention, the last two weeks, what ran and what runs next.
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Overview, Recipe } from '../lib/types'
import { StatusPill, TRIGGER_ICON, until, when } from '../lib/flows'
import { weekday } from '../lib/format'
import { Card, CoreHint, Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { openCreate, useCatalog, useOnChange } from '../components/Layout'
import { RecipeCard } from './Recipes'

export default function Home() {
  const { user, settings } = useApp()
  const { catalog } = useCatalog()
  const { data, reload } = useAsync(() => api.get<Overview>(`${V1}/overview`), [])
  const { data: recipes } = useAsync(() => api.get<Recipe[]>(`${V1}/recipes`), [])
  useOnChange(reload)
  if (!data) return <Loading />
  const st = data.stats
  const hour = new Date().getHours()
  const hello = hour < 5 ? 'Good night' : hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'
  const max = Math.max(1, ...data.days.map(d => d.ok + d.failed))
  const days = Array.from({ length: 14 }, (_, i) => { const d = new Date(); d.setDate(d.getDate() - 13 + i); const k = d.toLocaleDateString('sv-SE'); return data.days.find(x => x.day === k) || { day: k, ok: 0, failed: 0 } })
  const coreOn = !!settings?.core_configured
  const deaf = catalog?.apps.filter(a => a.events.length > 0 && !a.listening) || []
  return (
    <div className="stack loose">
      <PageHeader title={`${hello}${user?.name ? ', ' + user.name.split(' ')[0] : ''}`}
        sub={st.flows === 0 ? 'Flows do things for you when something happens.' : `${st.enabled} of ${st.flows} flows switched on · ${st.runs_week} runs this week`}
        actions={<button type="button" className="btn primary sm" onClick={() => openCreate('new')}><Icon name="plus" size={14} />New flow</button>} />

      {!coreOn && <div className="callout info"><Icon name="info" size={15} /><div>Without Pocket Core your flows can run on a schedule, from a web address or by hand. Starting flows when something happens in your Pocket apps, and acting in them, comes with Pocket Core.<CoreHint /></div></div>}
      {coreOn && data.stream && !data.stream.connected && st.flows > 0 && <div className="callout warn"><Icon name="alert" size={15} /><div>Not connected to Pocket Core's events right now{data.stream.error ? ` (${data.stream.error})` : ''}. Flows reconnects by itself; nothing is lost.</div></div>}

      {st.flows === 0 ? (
        <>
          <Card>
            <div className="stack">
              <h3 style={{ margin: 0 }}>Make your first flow</h3>
              <p className="muted" style={{ margin: 0 }}>A flow is “when this happens, do that”: when a price drops, send me a push; every Sunday, add a task; when a document arrives, put a line in today's note.</p>
              <div className="row wrap">
                <button type="button" className="btn primary" onClick={() => openCreate('describe')}><Icon name="sparkles" size={15} />Describe it in your words</button>
                <button type="button" className="btn" onClick={() => openCreate('new')}><Icon name="plus" size={15} />Build it step by step</button>
                <button type="button" className="btn ghost" onClick={() => openCreate('n8n')}><Icon name="upload" size={15} />Import from n8n</button>
              </div>
            </div>
          </Card>
          <h3 className="group-h">Or start from a recipe</h3>
          <div className="recipes">{(recipes || []).slice(0, 6).map(r => <RecipeCard key={r.key} r={r} />)}</div>
          <Link to="/recipes" className="btn ghost sm" style={{ justifySelf: 'start' }}>All recipes<Icon name="chevron-right" size={14} /></Link>
        </>
      ) : (
        <>
          <div className="stats-row">
            <div className="stat"><span className="stat-l">Runs today</span><span className="stat-v tnum">{st.runs_today}</span><span className="stat-s small">{st.failed_today ? `${st.failed_today} failed` : 'none failed'}</span></div>
            <div className="stat"><span className="stat-l">This week</span><span className="stat-v tnum">{st.runs_week}</span><span className="stat-s small">{st.failed_week ? `${st.failed_week} failed` : 'all fine'}</span></div>
            <div className="stat"><span className="stat-l">Waiting</span><span className="stat-v tnum">{st.active}</span><span className="stat-s small">runs paused or queued</span></div>
            <div className="stat"><span className="stat-l">Events today</span><span className="stat-v tnum">{st.events_today}</span><span className="stat-s small">from your apps</span></div>
          </div>

          {data.failing.length > 0 && (
            <Card title={<span className="row" style={{ gap: 8 }}><Icon name="alert" size={16} className="bad" />Needs a look</span>} sub="The last run of these flows failed." flush>
              <div className="list">
                {data.failing.map(f => (
                  <Link key={f.id} to={`/runs?flow=${f.id}&status=failed`} className="list-row">
                    <span className="list-ico"><Icon name={TRIGGER_ICON[f.trigger_kind]} size={16} /></span>
                    <span className="grow"><b>{f.name}</b><span className="muted small block">{f.trigger_text}</span></span>
                    <span className="muted small">{when(f.last_run_at)}</span>
                  </Link>
                ))}
              </div>
            </Card>
          )}

          <div className="home-grid">
            <Card title="The last two weeks" sub={<span className="legend"><span><i style={{ background: 'var(--accent)' }} />worked</span><span><i style={{ background: 'var(--danger)' }} />failed</span></span>}>
              <div className="runbars" role="img" aria-label="Runs per day for the last two weeks">
                {days.map(d => (
                  <div key={d.day} className="runbars-col" title={`${d.day}: ${d.ok} worked, ${d.failed} failed`}>
                    {d.failed > 0 && <span className="failed" style={{ height: `${(d.failed / max) * 100}%` }} />}
                    {d.ok > 0 && <span className="ok" style={{ height: `${(d.ok / max) * 100}%` }} />}
                  </div>
                ))}
              </div>
              <div className="runbars-lbls">{days.map((d, i) => <span key={d.day}>{i % 2 === 1 ? weekday(d.day).slice(0, 2) : ''}</span>)}</div>
            </Card>
            <Card title="Coming up" flush>
              {data.upcoming.length === 0 ? <p className="muted small pad">No scheduled flows.</p> : (
                <div className="list">
                  {data.upcoming.map(f => (
                    <Link key={f.id} to={`/flows/${f.id}`} className="list-row">
                      <span className="list-ico"><Icon name="clock" size={16} /></span>
                      <span className="grow"><b>{f.name}</b><span className="muted small block">{f.trigger_text}</span></span>
                      <span className="muted small">{until(f.next_run_at)}</span>
                    </Link>
                  ))}
                </div>
              )}
            </Card>
          </div>

          <Card title="Latest runs" actions={<Link to="/runs" className="btn ghost sm">All runs<Icon name="chevron-right" size={14} /></Link>} flush>
            {data.recent.length === 0 ? <Empty title="Nothing has run yet" icon="history">Runs show up here as your flows start.</Empty> : (
              <div className="list">
                {data.recent.map(r => (
                  <Link key={r.id} to={`/runs/${r.id}`} className="list-row">
                    <span className="list-ico"><Icon name={TRIGGER_ICON[r.trigger_kind] || 'play'} size={16} /></span>
                    <span className="grow" style={{ minWidth: 0 }}><b className="ellipsis block">{r.flow_name}</b><span className="muted small ellipsis block">{r.summary}</span></span>
                    <StatusPill status={r.status} short />
                    <span className="muted small" style={{ minWidth: 70, textAlign: 'right' }}>{when(r.created_at)}</span>
                  </Link>
                ))}
              </div>
            )}
          </Card>

          {coreOn && deaf.length > 0 && st.flows > 0 && (
            <p className="muted small"><Icon name="info" size={13} /> Flows hears events from {catalog!.apps.length - deaf.length} of {catalog!.apps.filter(a => a.events.length).length} apps. To start flows from the others, switch on <b>Flows → app</b> in Pocket Core → Links. <Link to="/settings/apps">Which ones?</Link></p>
          )}
        </>
      )}
    </div>
  )
}
