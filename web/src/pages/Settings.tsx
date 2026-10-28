import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, AppSetting, ConnMode, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'
import { N8NModal } from '../components/CreateModals'

type TabId = 'general' | 'notifications' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'Dates and times, how long the run history is kept, and what web steps may reach.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'Where “Send me a notification” steps go, and a push when a flow fails.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Which Pocket apps Flows hears and acts in for you.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who answers “Ask AI” steps and drafts flows from your words: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for scripts, Homepage widgets and Siri Shortcuts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export or import your flows, import from n8n.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}


const KEEP = [7, 14, 30, 60, 90, 180, 365].map(n => ({ value: String(n), label: `${n} days` }))

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Dates and times" icon="calendar">
        <SettingRow title="Dates"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Times"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Schedules use" help="Schedules run on the server’s clock.">{f.timezone}</SettingRow>
      </Section>
      <Section title="Run history" icon="history">
        <SettingRow title="Keep runs for" help="Older finished runs are deleted once an hour. Events from your apps are kept 30 days."><Select ariaLabel="Keep runs" value={String(f.keep_runs_days)} onChange={v => setF({ ...f, keep_runs_days: Number(v) })} options={KEEP} /></SettingRow>
      </Section>
      {isAdmin && <Section title="This box" icon="server" sub="Admins only; applies to everyone.">
        <SettingRow title="Name" help="Shown in the sidebar and the browser tab."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>
        <SettingRow title="Web steps may reach the local network" help="“Call a web address” steps of members may reach addresses on this network (192.168.x.x, the tailnet, other containers). Admins always can.">
          <Switch checked={f.http_private} onChange={v => setF({ ...f, http_private: v })} label="Local network" />
        </SettingRow>
      </Section>}
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where “Send me a notification” steps and alerts go: an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/flows" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="Alerts" icon="alert">
        <SettingRow title="When a flow fails" help="One push with the error and a link to the run (after retries, if the step was retried)."><Switch checked={f.notify_failures} onChange={v => setF({ ...f, notify_failures: v })} label="Failures" /></SettingRow>
        <SettingRow title="When it works again" help="After a failure, the next good run says so."><Switch checked={f.notify_recover} onChange={v => setF({ ...f, notify_recover: v })} label="Recovered" /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        {status('notify')}
      </div>
    </div>
  )
}

function Integrations() {
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  const [open, setOpen] = useState<string | null>(null)
  if (!settings) return <Loading />
  const coreOn = settings.core_configured && settings.core_registered !== false
  const saveConn = async (slug: string, patch: Record<string, string>) => {
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { connections: { [slug]: patch } }); setSettings(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') }
  }
  const acting = settings.apps.filter(a => a.actions)
  const hearing = settings.apps.filter(a => a.events && !a.actions)
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Flows hears what happens in your Pocket apps and acts in them as you, with nothing to paste: the admin switches each link on once in Pocket Core → <b>Links</b> (<i>Pocket Flows → app</i>), and you can switch any off for yourself in Pocket Core → Settings → Apps acting for me. <b>Direct</b> (for acting only) uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      <Section title="Apps your flows act in" icon="zap" sub="Steps like “Add a task” or “Record a transaction” work through these. Each one also starts flows when something happens in it.">
        {acting.map(a => <AppConn key={a.slug} a={a} coreOn={coreOn} open={open === a.slug} onOpen={() => setOpen(open === a.slug ? null : a.slug)} onSave={p => saveConn(a.slug, p)} />)}
      </Section>
      <Section title="Apps that start flows" icon="activity" sub="Flows listens to their events: what happened, for you. Nothing is changed in them.">
        {hearing.map(a => <AppConn key={a.slug} a={a} coreOn={coreOn} open={open === a.slug} onOpen={() => setOpen(open === a.slug ? null : a.slug)} onSave={p => saveConn(a.slug, p)} />)}
      </Section>
      <Section title="Other Pocket apps" icon="layers" sub="Flows offers your flows to the rest of the box through Pocket Core.">
        <dl className="set-dl">
          <div><dt>flows:read</dt><dd className="muted">Your flows and their run history — for Pocket Day, dashboards and Pocket AI. Never the webhook addresses.</dd></div>
          <div><dt>flows:run</dt><dd className="muted">Start a flow that runs by hand, with some input. It cannot change or create flows.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function AppConn({ a, coreOn, open, onOpen, onSave }: { a: AppSetting; coreOn: boolean; open: boolean; onOpen: () => void; onSave: (p: Record<string, string>) => void }) {
  const { run, status } = useTester()
  const [url, setUrl] = useState(a.url)
  const [token, setToken] = useState(a.token)
  useEffect(() => { setUrl(a.url); setToken(a.token) }, [a.url, a.token])
  const mode: ConnMode = !coreOn && a.mode === 'core' ? 'off' : a.mode
  const ok = a.via !== '' && mode !== 'off'
  const badge = mode === 'off' ? <span className="badge">off</span> : ok ? <span className="badge ok">{a.via === 'core' ? (a.actions ? 'Pocket Core' : 'listening') : 'direct'}</span> : <span className="badge warn">not connected</span>
  return (
    <div className="app-row" style={{ flexDirection: 'column', alignItems: 'stretch' }}>
      <button type="button" className="row link-btn" style={{ color: 'inherit', gap: 10, textAlign: 'left' }} onClick={onOpen} aria-expanded={open}>
        <b className="grow">{a.name}</b>
        {a.events && a.actions && mode !== 'off' && <span className={`small ${a.listening ? 'ok' : 'muted'}`}>{a.listening ? 'hears events' : 'events off'}</span>}
        {badge}<Icon name={open ? 'chevron-up' : 'chevron-down'} size={15} className="muted" />
      </button>
      {open && <div className="mt">
        {a.actions ? (
          <Connection app={a.name} mode={a.mode} via={a.via} detail={a.detail} core={coreOn} hasToken={!!a.token}
            onMode={m => onSave({ mode: m })} onDisconnect={() => onSave({ url: '', token: '', mode: '' })}
            test={<><button type="button" className="btn sm" onClick={() => run(a.slug, url, token)}>Test connection</button>{status(a.slug)}</>}
            direct={<div className="stack">
              <div className="two-col tight">
                <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={url} onChange={e => setUrl(e.target.value)} placeholder="http://host.docker.internal:8184" /></Field>
                <Field label="API token" help={`${a.name} → Settings → API tokens`}><input value={token} onChange={e => setToken(e.target.value)} autoComplete="off" /></Field>
              </div>
              <div><button type="button" className="btn sm primary" onClick={() => onSave({ url, token, mode: 'direct' })}>Save</button></div>
            </div>} />
        ) : (
          <div className="conn-block">
            <div className="conn-mode"><Segmented size="sm" value={mode} onChange={m => onSave({ mode: m === 'core' ? '' : m })} options={[{ value: 'core', label: 'Pocket Core', disabled: !coreOn, title: coreOn ? undefined : 'Needs Pocket Core' }, { value: 'off', label: 'Off' }]} /></div>
            <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{mode === 'off' ? `Off: ${a.name} events do not start your flows.` : ok ? `Flows hears your ${a.name} events through Pocket Core.` : `Not connected: ${a.detail || 'switch on Pocket Flows → ' + a.name + ' in Pocket Core → Links'}.`}</div>
            {mode !== 'off' && coreOn && <div className="row wrap"><button type="button" className="btn sm" onClick={() => run(a.slug, '', '')}>Test</button>{status(a.slug)}</div>}
          </div>
        )}
      </div>}
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Answers “Ask AI” steps in your flows (summarise, sort, write a line), and drafts a flow when you describe it in your own words. Only what a step puts in its question is sent.">
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: your flows do not act in ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own flows, run history and connections. Admins manage accounts; they do not see anyone else’s flows.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their flows and run history." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or log a sit from a Shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>POST /flows/&#123;id&#125;/run</code></dt><dd className="muted"><code>{'{"input": {"text": "Buy milk"}}'}</code> — run a flow now; the input is its <code>{'{{trigger}}'}</code>.</dd></div>
          <div><dt><code>POST /hooks/&lt;secret&gt;</code></dt><dd className="muted">A webhook flow's own address (no token needed: the address is the secret).</dd></div>
          <div><dt><code>GET /overview</code></dt><dd className="muted">Runs today and this week, failures, what runs next — for Homepage.</dd></div>
          <div><dt><code>GET /runs?status=failed</code></dt><dd className="muted">The run history, filtered.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const [n8n, setN8n] = useState(false)
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())); toast('Imported: ' + (Object.entries(r).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')); window.dispatchEvent(new CustomEvent('pfl:changed')) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-flows.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Your flows as one JSON file, or the run history as a CSV. Importing the same file twice adds nothing; imported flows start switched off and webhook flows get a new address.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export flows</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Run history CSV</a><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import flows…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
      <Section title="Coming from n8n" icon="upload" sub="Bring n8n workflows over: the trigger, web requests, waits and notifications are converted; the rest is listed in the flow's description.">
        <div><button type="button" className="btn" onClick={() => setN8n(true)}><Icon name="upload" size={15} />Import from n8n…</button></div>
        {n8n && <N8NModal onClose={() => setN8n(false)} onDone={() => { setN8n(false); window.dispatchEvent(new CustomEvent('pfl:changed')) }} />}
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Flows', counts.flows, 'flow'], ['Runs kept', counts.runs, 'history'], ['Events kept', counts.events, 'activity'], ['Tokens', counts.api_tokens, 'key']]
  const stream = (data.stream || {}) as { connected?: boolean; error?: string; last_event_at?: string }
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Events', core.configured ? (stream.connected ? <span key="s" className="ok">listening{stream.last_event_at ? ` · last ${fmtDateTime(stream.last_event_at)}` : ''}</span> : <span key="s" className="bad">{stream.error || 'connecting…'}</span>) : <span key="s" className="muted">needs Pocket Core</span>],
    ['Workers', `${String(data.workers)} runs at a time · ${String(data.runs_since_start)} runs since start (${String(data.failed_since_start)} failed)`],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Flows</h3>
          <p className="muted small">Automations for your Pocket apps: when something happens — a price drops, a document arrives, a time of day, a webhook — do things for you. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <Link className="btn" to="/recipes"><Icon name="book" size={15} />Recipes</Link>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-flows" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Tasks, Notes, Money, Calendar and more — and Flows ties them together.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
