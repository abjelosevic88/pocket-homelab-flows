// Left column: the create button, sections with today's numbers — the Pocket Calendar sidebar.
import { useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Overview } from '../lib/types'
import { until } from '../lib/flows'
import { Icon } from './icons'
import { createItems, type CreateKind } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onCreate, onNavigate, counts }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; counts: Overview | null }) {
  const { status, settings } = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Flows'
  const st = counts?.stats
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string; badge?: string }[] = [
    { to: '/', label: 'Overview', icon: 'home', end: true },
    { to: '/flows', label: 'Flows', icon: 'flow', count: st ? `${st.enabled}/${st.flows}` : undefined },
    { to: '/runs', label: 'Run history', icon: 'history', count: st?.failed_today ? `${st.failed_today} failed` : st?.runs_today ? String(st.runs_today) : undefined, badge: st?.failed_today ? 'bad' : undefined },
    { to: '/activity', label: 'Activity', icon: 'activity', count: st?.events_today ? String(st.events_today) : undefined },
    { to: '/recipes', label: 'Recipes', icon: 'book' },
  ]
  const coreOn = !!settings?.core_configured
  const stream = counts?.stream
  const next = counts?.upcoming?.[0]
  return (
    <aside className="sidebar" aria-label="Flows">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Flows' ? title : <>Pocket <b>Flows</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={15} strokeWidth={2.2} />New flow<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className={`sb-count ${n.badge ? 'bad' : ''}`}>{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      {counts && (
        <section className="sb-sec" aria-label="Listening">
          <h4 className="sb-h"><span className="sb-h-btn">Listening</span></h4>
          <div className="sb-live" title={stream?.error || undefined}>
            <span className={`st-dot ${!coreOn ? '' : stream?.connected ? 'ok' : 'waiting'}`} />
            {!coreOn ? <span>Events need Pocket Core</span> : stream?.connected ? <span><b>Connected</b> to your apps</span> : <span>Connecting to Pocket Core…</span>}
          </div>
          {next && <div className="sb-live"><Icon name="clock" size={13} /><span className="ellipsis">Next: <b>{next.name}</b> {until(next.next_run_at)}</span></div>}
        </section>
      )}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pfl:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
