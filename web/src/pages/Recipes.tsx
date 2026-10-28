// Recipes: ready-made flows. Using one creates the flow switched off, so you can look it over first.
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Flow, Recipe } from '../lib/types'
import { appName } from '../lib/flows'
import { Loading, PageHeader, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { announceChange, useCatalog } from '../components/Layout'

export function RecipeCard({ r }: { r: Recipe }) {
  const { catalog } = useCatalog()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const use = async () => {
    setBusy(true)
    try { const f = await api.post<Flow>(`${V1}/recipes/${r.key}`); announceChange(); navigate(`/flows/${f.id}`, { state: { fromRecipe: true } }) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const app = (slug: string) => catalog?.apps.find(a => a.slug === slug)
  return (
    <button type="button" className="recipe" onClick={use} disabled={busy}>
      <span className="row" style={{ gap: 10 }}><span className="flow-ico"><Icon name={r.icon} size={17} /></span><b className="grow">{r.name}</b></span>
      <p>{r.description}</p>
      <span className="recipe-apps">
        {r.apps.length === 0 ? <span className="chip">Works on its own</span> : r.apps.map(a => {
          const x = app(a)
          const ready = !catalog?.core ? false : x ? (x.listening || x.acting !== '') : false
          return <span key={a} className={`chip ${catalog && !ready ? 'missing' : ''}`} title={ready ? undefined : 'Not connected yet: switch the link on in Pocket Core'}>{appName(catalog, a).replace('Pocket ', '')}</span>
        })}
      </span>
    </button>
  )
}

export default function Recipes() {
  const { data } = useAsync(() => api.get<Recipe[]>(`${V1}/recipes`), [])
  if (!data) return <Loading />
  const groups: [string, Recipe[]][] = []
  for (const r of data) { const g = groups.find(x => x[0] === r.group); if (g) g[1].push(r); else groups.push([r.group, [r]]) }
  return (
    <div className="stack loose">
      <PageHeader title="Recipes" sub="Ready-made flows. Pick one: it is added switched off, so you can check it (and choose your list or account) before it runs." />
      {groups.map(([g, rs]) => (
        <section key={g} className="stack">
          <h3 className="group-h">{g}</h3>
          <div className="recipes">{rs.map(r => <RecipeCard key={r.key} r={r} />)}</div>
        </section>
      ))}
    </div>
  )
}
