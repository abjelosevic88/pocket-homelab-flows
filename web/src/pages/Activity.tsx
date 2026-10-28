// Activity: what happened in your Pocket apps, as Flows heard it — and a flow made from any of it.
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { FlowEvent } from '../lib/types'
import { appIcon, appName, when } from '../lib/flows'
import { Card, CoreHint, Empty, Loading, Modal, PageHeader } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { useCatalog, useOnChange } from '../components/Layout'

const PAGE = 50

function topicName(c: ReturnType<typeof useCatalog>['catalog'], topic: string) {
  for (const a of c?.apps || []) for (const t of a.events) if (t.topic === topic) return t.name
  return topic
}

function title(p: Record<string, unknown>) {
  for (const k of ['title', 'wish', 'name', 'label', 'summary', 'description', 'habit', 'metric']) if (typeof p[k] === 'string' && p[k]) return p[k] as string
  return ''
}

export default function Activity() {
  const { settings } = useApp()
  const { catalog } = useCatalog()
  const navigate = useNavigate()
  const [app, setApp] = useState('')
  const [items, setItems] = useState<FlowEvent[] | null>(null)
  const [more, setMore] = useState(false)
  const [open, setOpen] = useState<FlowEvent | null>(null)
  const load = (before = 0) => {
    const p = new URLSearchParams({ limit: String(PAGE) })
    if (app) p.set('topic', app + '.*'); if (before) p.set('before', String(before))
    api.get<FlowEvent[]>(`${V1}/events?${p}`).then(r => { setItems(prev => before ? [...(prev || []), ...r] : r); setMore(r.length === PAGE) }).catch(() => setItems([]))
  }
  useEffect(() => { load() }, [app]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(() => load())
  const listening = catalog?.apps.filter(a => a.listening) || []
  const makeFlow = (e: FlowEvent) => navigate('/flows/new', { state: { topic: e.topic } })
  return (
    <div className="stack loose">
      <PageHeader title="Activity" sub="Events from your Pocket apps that Flows heard, kept for 30 days. Any of them can start a flow." />
      {!settings?.core_configured ? <div className="callout info"><Icon name="info" size={15} /><div>Events come from your Pocket apps through Pocket Core.<CoreHint /></div></div> : (
        <p className="muted small">Listening to {listening.length ? listening.map(a => a.name.replace('Pocket ', '')).join(', ') : 'no apps yet'}. <Link to="/settings/apps">Which apps Flows hears</Link></p>
      )}
      <div className="row wrap filters">
        <Select ariaLabel="App" value={app} onChange={setApp} options={[{ value: '', label: 'Every app' }, ...(catalog?.apps || []).filter(a => a.events.length).map(a => ({ value: a.slug, label: a.name }))]} />
      </div>
      {!items ? <Loading /> : items.length === 0 ? <Empty title="Nothing heard yet" icon="activity">When something happens in an app Flows listens to, it shows up here.</Empty> : (
        <Card flush>
          <div className="list">
            {items.map(e => {
              const a = e.topic.split('.')[0]
              return (
                <div key={e.id} className="ev-row">
                  <span className="list-ico" title={appName(catalog, a)}><Icon name={appIcon(catalog, a)} size={16} /></span>
                  <button type="button" className="grow link-btn" style={{ textAlign: 'left', color: 'inherit', minWidth: 0 }} onClick={() => setOpen(e)}>
                    <b className="block ellipsis">{topicName(catalog, e.topic)}{title(e.payload) ? `: ${title(e.payload)}` : ''}</b>
                    <span className="ev-topic">{e.topic}</span>
                  </button>
                  {e.matched > 0 && <span className="st-pill ok" title="Flows it started">{e.matched} {e.matched === 1 ? 'flow' : 'flows'}</span>}
                  <span className="muted small" style={{ minWidth: 70, textAlign: 'right' }}>{when(e.received_at)}</span>
                </div>
              )
            })}
          </div>
          {more && <div className="pad"><button type="button" className="btn ghost sm" onClick={() => load(items[items.length - 1].id)}>Show older</button></div>}
        </Card>
      )}
      {open && (
        <Modal title={topicName(catalog, open.topic)} onClose={() => setOpen(null)} footer={<>
          <button type="button" className="btn" onClick={() => setOpen(null)}>Close</button>
          <button type="button" className="btn primary" onClick={() => makeFlow(open)}><Icon name="plus" size={15} />Make a flow from this</button>
        </>}>
          <p className="muted small">{open.topic} · {when(open.received_at)} · use these fields in a flow as <code>{'{{field}}'}</code></p>
          <pre className="code">{JSON.stringify(open.payload, null, 2)}</pre>
        </Modal>
      )}
    </div>
  )
}
