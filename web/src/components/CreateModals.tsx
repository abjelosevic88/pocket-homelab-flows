// "Describe it to AI" and "Import from n8n" — the two create dialogs.
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Flow } from '../lib/types'
import { Icon } from './icons'
import { CoreHint, Field, Modal, errMsg, useToast } from './ui'
import { useApp } from '../lib/app-context'

const EXAMPLES = [
  'When something on my wishlist gets cheaper, send me a push with the new price',
  'Every weekday at 8:00, add a task to check my inbox',
  'When I finish a book, write it in today’s note',
  'When a document arrives, have AI sum it up in one line and send it to me',
]

export function DescribeModal({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const { settings } = useApp()
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const go = async () => {
    setBusy(true); setErr('')
    try {
      const r = await api.post<{ ok: boolean; error?: string; flow?: Flow }>(`${V1}/ai/draft`, { text })
      if (!r.ok || !r.flow) { setErr(r.error || 'That did not work'); return }
      onClose()
      navigate('/flows/new', { state: { draft: r.flow } })
    } catch (e) { setErr(errMsg(e)) } finally { setBusy(false) }
  }
  return (
    <Modal title="Describe a flow" onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={go} disabled={busy || text.trim().length < 8}>{busy ? 'Drafting…' : 'Draft it'}</button>
    </>}>
      <div className="stack">
        <p className="muted small">Say what should happen, in your own words. AI drafts the flow; you look it over and save it. Nothing runs until you switch it on.</p>
        <Field label="When… then…"><textarea autoFocus rows={4} value={text} onChange={e => setText(e.target.value)} placeholder={EXAMPLES[0]}
          onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) go() }} /></Field>
        <div className="chips">{EXAMPLES.slice(1).map(x => <button key={x} type="button" className="chip" onClick={() => setText(x)}>{x}</button>)}</div>
        {err && <div className="callout warn"><Icon name="alert" size={15} /><div>{err}{!settings?.core_configured && <CoreHint />} <Link to="/settings/ai" onClick={onClose}>AI settings</Link></div></div>}
      </div>
    </Modal>
  )
}

interface N8NResult { name: string; flow_id?: number; skipped?: string[]; status: 'created' | 'exists' | 'failed'; error?: string }

export function N8NModal({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<N8NResult[] | null>(null)
  const pick = async (f: File | undefined) => { if (f) setText(await f.text()) }
  const run = async () => {
    let body: unknown
    try { body = JSON.parse(text) } catch { toast('That is not JSON: export the workflow from n8n (⋯ → Download)', 'err'); return }
    setBusy(true)
    try { const r = await api.post<{ results: N8NResult[] }>(`${V1}/import/n8n`, body); setRes(r.results) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Import from n8n" onClose={res ? onDone : onClose} size="sm" footer={res
      ? <button type="button" className="btn primary" onClick={onDone}>Done</button>
      : <><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={run} disabled={busy || !text.trim()}>{busy ? 'Importing…' : 'Import'}</button></>}>
      {!res ? (
        <div className="stack">
          <p className="muted small">In n8n open a workflow, then ⋯ → <b>Download</b> (or export all with <code>n8n export:workflow --all</code>). The trigger (schedule, webhook, manual), web requests, waits and notifications come across; anything else is listed in the flow's description so you can rebuild it. Imported flows start switched off.</p>
          <label className="btn"><Icon name="upload" size={15} />Choose a file<input type="file" accept=".json,application/json" hidden onChange={e => pick(e.target.files?.[0])} /></label>
          <Field label="…or paste the JSON"><textarea rows={6} value={text} onChange={e => setText(e.target.value)} placeholder='{"name": "My workflow", "nodes": [...]}' /></Field>
        </div>
      ) : (
        <div className="stack">
          {res.map((r, i) => (
            <div key={i} className="rule">
              <Icon name={r.status === 'failed' ? 'x-circle' : 'check-circle'} size={16} className={r.status === 'failed' ? 'bad' : 'ok'} />
              <div className="grow">
                {r.flow_id ? <Link to={`/flows/${r.flow_id}`} onClick={onDone}><b>{r.name}</b></Link> : <b>{r.name}</b>}
                <div className="muted small">{r.status === 'created' ? 'Imported, switched off' : r.status === 'exists' ? 'Already imported' : r.error}
                  {r.skipped && r.skipped.length > 0 && <> · not converted: {r.skipped.join(', ')}</>}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </Modal>
  )
}
