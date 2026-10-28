// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds two months of flows, events and runs):
//
//   DATA_DIR=/tmp/flows-demo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME=Maya PORT=18412 ./pocket-flows
//   CDP=http://127.0.0.1:19271 BASE=http://127.0.0.1:18412 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18412'
const shots = process.env.SHOTS !== '0'
const dir = process.env.OUT || new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1200) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) { if (shots) await page.screenshot({ path: dir + name + '.png' }) }
const flowID = async (page, name) => page.evaluate(n => fetch('/api/v1/flows').then(r => r.json()).then(l => l.find(f => f.name === n)?.id), name)

// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok((await page.content()).includes('Latest runs'), 'the overview shows the latest runs')
  await shot(page, 'desktop-home')
  await go(page, '/flows')
  assert.ok((await page.content()).includes('Wish got cheaper'), 'the list shows the demo flows')
  await shot(page, 'desktop-flows')
  const big = await flowID(page, 'Big expenses need a second look')
  await go(page, `/flows/${big}`)
  await page.locator('.ed-step-title').first().click()
  await page.evaluate(() => document.querySelector('.content')?.scrollTo({ top: 0 }))
  await page.waitForTimeout(600)
  await shot(page, 'desktop-editor')
  await go(page, '/runs')
  await shot(page, 'desktop-runs')
  const run = await page.evaluate(() => fetch('/api/v1/runs?status=ok&limit=50').then(r => r.json()).then(l => (l.find(r => r.flow_name === 'Big expenses need a second look') || l[0]).id))
  await go(page, `/runs/${run}`)
  await shot(page, 'desktop-run')
  await go(page, '/recipes')
  await shot(page, 'desktop-recipes')
  await go(page, '/activity')
  await shot(page, 'desktop-activity')
  for (const p of ['/flows/new', '/settings/general', '/settings/notifications', '/settings/apps', '/settings/ai', '/settings/data', '/settings/about']) await go(page, p, 500)
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true, dark: true })
  await go(page, '/')
  await shot(page, 'phone-home')
  const wish = await flowID(page, 'Wish got cheaper')
  await go(page, `/flows/${wish}`)
  await page.locator('.ed-step-title').first().click()
  await page.evaluate(() => document.querySelector('.content')?.scrollTo({ top: 0 }))
  await page.waitForTimeout(500)
  const wide = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1 || (document.querySelector('.content')?.scrollWidth || 0) > window.innerWidth + 1)
  assert.ok(!wide, 'the editor fits a phone screen')
  await shot(page, 'phone-editor')
  await go(page, '/flows')
  await shot(page, 'phone-flows')
  await ctx.close()
}

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('ok')
