import { useEffect, useMemo, useState } from 'react'

const api = async (path, options = {}, token = '') => {
  const response = await fetch(path, {
    ...options,
    headers: {
      'content-type': 'application/json',
      ...(token ? { authorization: `Bearer ${token}` } : {}),
      ...(options.headers || {}),
    },
  })
  const body = response.status === 204 ? null : await response.json()
  if (!response.ok) throw Object.assign(new Error(body?.error || `HTTP ${response.status}`), { body })
  return body
}

const short = value => value && value !== 'unknown' ? value.slice(0, 8) : 'unknown'

function App() {
  const [repo, setRepo] = useState(null)
  const [roles, setRoles] = useState([])
  const [profile, setProfile] = useState(null)
  const [mode, setMode] = useState('ask')
  const [input, setInput] = useState('分片上传完成后，视频转码任务是如何触发的？')
  const [result, setResult] = useState(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [plans, setPlans] = useState([])
  const [selectedPlan, setSelectedPlan] = useState(null)
  const [adminToken, setAdminToken] = useState(() => sessionStorage.getItem('codeatlas-admin') || '')
  const [approvalToken, setApprovalToken] = useState('')
  const [execution, setExecution] = useState(null)
  const [knowledge, setKnowledge] = useState([])

  const load = async () => {
    const [repoData, roleData, profileData, planData, reportData] = await Promise.all([
      api('/api/v1/repository'), api('/api/v1/harness/roles'), api('/api/v1/profile'),
      api('/api/v1/plans'), api('/api/v1/knowledge/reports'),
    ])
    setRepo(repoData); setRoles(roleData); setProfile(profileData); setPlans(planData); setKnowledge(reportData)
  }

  useEffect(() => { load().catch(e => setError(e.message)) }, [])
  useEffect(() => { sessionStorage.setItem('codeatlas-admin', adminToken) }, [adminToken])

  const submit = async () => {
    if (!input.trim()) return
    setBusy(true); setError(''); setResult(null); setExecution(null)
    try {
      const data = mode === 'ask'
        ? await api('/api/v1/ask', { method: 'POST', body: JSON.stringify({ question: input }) })
        : await api('/api/v1/plan', { method: 'POST', body: JSON.stringify({ requirement: input }) })
      setResult(data)
      if (data.record) { setSelectedPlan(data.record); setPlans(current => [data.record, ...current.filter(p => p.id !== data.record.id)]) }
    } catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  const approve = async () => {
    if (!selectedPlan) return
    setBusy(true); setError('')
    try {
      const data = await api(`/api/v1/plans/${selectedPlan.id}/approve`, {
        method: 'POST', body: JSON.stringify({
          plan_digest: selectedPlan.digest, base_revision: selectedPlan.base_revision,
          allowed_paths: selectedPlan.candidate_paths, test_suite_ids: selectedPlan.test_suite_ids,
        }),
      }, adminToken)
      setApprovalToken(data.approval_token)
      const updated = { ...selectedPlan, status: 'approved' }; setSelectedPlan(updated)
      setPlans(items => items.map(p => p.id === updated.id ? updated : p))
    } catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  const execute = async () => {
    if (!selectedPlan || !approvalToken) return
    setBusy(true); setError('')
    try {
      const data = await api(`/api/v1/plans/${selectedPlan.id}/execute`, {
        method: 'POST', body: JSON.stringify({ approval_token: approvalToken }),
      }, adminToken)
      setExecution(data); await load()
    } catch (e) { setError(e.message); if (e.body?.execution) setExecution(e.body.execution) } finally { setBusy(false) }
  }

  const refreshKnowledge = async () => {
    setBusy(true); setError('')
    try { const report = await api('/api/v1/knowledge/refresh', { method: 'POST', body: '{}' }, adminToken); setKnowledge(items => report.id ? [report, ...items] : items); setRepo(await api('/api/v1/repository')) }
    catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  const roleFlow = useMemo(() => roles.map(role => role.name).join(' → '), [roles])

  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><span className="brand-mark">CA</span><div><strong>CodeAtlas</strong><small>Repository-grounded engineering agent</small></div></div>
      <div className="status"><i className="live" /> {repo ? `${repo.name} · ${short(repo.revision)}` : 'connecting'}</div>
    </header>

    <main>
      <section className="hero">
        <div><p className="eyebrow">HARNESS-CONTROLLED · MCP-NATIVE · REVIEWABLE</p><h1>让 Agent 用证据理解代码，<br/><em>用审批约束修改。</em></h1><p className="lead">围绕视频微服务仓库完成调查、计划、隔离修改、独立测试与知识更新。模型负责语义判断，Harness 负责权限和完成条件。</p></div>
        <div className="repo-card">
          <span>ACTIVE REPOSITORY</span><strong>{repo?.name || '—'}</strong>
          <div className="metrics"><Metric value={repo?.files} label="files"/><Metric value={repo?.go_modules} label="Go modules"/><Metric value={repo?.services?.length} label="top-level"/></div>
          <code>{repo?.revision || 'unknown revision'}</code>
        </div>
      </section>

      {error && <div className="error"><strong>Request stopped</strong><span>{error}</span><button onClick={() => setError('')}>×</button></div>}

      <section className="workspace-grid">
        <div className="composer panel">
          <div className="panel-head"><div><span className="index">01</span><h2>Agent workspace</h2></div><div className="segmented"><button className={mode==='ask'?'active':''} onClick={()=>setMode('ask')}>Ask</button><button className={mode==='plan'?'active':''} onClick={()=>setMode('plan')}>Plan</button></div></div>
          <textarea value={input} onChange={e=>setInput(e.target.value)} placeholder={mode==='ask'?'询问架构、调用链或实现细节':'描述一个边界清晰的修改需求'} />
          <div className="composer-foot"><span>{input.length} / 4000</span><button className="primary" disabled={busy} onClick={submit}>{busy ? 'Running harness…' : mode==='ask' ? 'Run evidence research' : 'Create grounded plan'} <b>↗</b></button></div>
        </div>
        <aside className="panel harness-card"><div className="panel-head"><div><span className="index">02</span><h2>Harness</h2></div><span className="pill">deterministic gates</span></div><div className="flow">{roleFlow || 'loading roles…'}</div>{roles.map(role=><div className="role" key={role.name}><strong>{role.name}</strong><span>{role.responsibility}</span></div>)}</aside>
      </section>

      <section className="result-grid">
        <div className="panel output-panel"><div className="panel-head"><div><span className="index">03</span><h2>{mode==='ask'?'Grounded answer':'Change plan'}</h2></div>{result?.revision && <code>{short(result.revision)}</code>}</div>
          {!result ? <Empty text="Run Ask or Plan to inspect repository-grounded output."/> : <>
            <pre className="answer">{result.answer || result.plan}</pre>
            {!!result.citations?.length && <div className="citation-list">{result.citations.map(c=><article key={c.id}><header><b>{c.id}</b><code>{c.path}:{c.start_line}-{c.end_line}</code></header><pre>{c.snippet}</pre></article>)}</div>}
            {!!result.trace?.length && <details><summary>{result.trace.length} trace events</summary><div className="timeline">{result.trace.map((e,i)=><div key={i}><b>{e.role}</b><span>{e.type} · {e.name} · {e.success?'pass':'fail'} · {e.duration_ms||0} ms</span></div>)}</div></details>}
          </>}
        </div>
        <aside className="panel control-panel"><div className="panel-head"><div><span className="index">04</span><h2>Approval control</h2></div><span className={`pill ${repo?.write_enabled?'warn':''}`}>{selectedPlan?.status || 'no plan'}</span></div>
          <label>Admin bearer token<input type="password" value={adminToken} onChange={e=>setAdminToken(e.target.value)} placeholder="Required only for write workflow"/></label>
          {selectedPlan ? <>
            <small>Plan</small><code className="block">{selectedPlan.id}</code>
            <small>Allowed scope candidates</small><div className="chips">{selectedPlan.candidate_paths.map(x=><span key={x}>{x}</span>)}</div>
            <small>Independent suites</small><div className="chips">{selectedPlan.test_suite_ids.map(x=><span key={x}>{x}</span>)}</div>
            <button className="secondary" disabled={busy || selectedPlan.status!=='pending_approval'} onClick={approve}>Bind approval to digest</button>
            {approvalToken && <><label>One-time approval capability<input value={approvalToken} readOnly/></label><button className="primary wide" disabled={busy} onClick={execute}>Execute in isolated worktree</button></>}
          </> : <Empty text="A structured Plan is required before write tools can become visible."/>}
        </aside>
      </section>

      {execution && <section className="panel execution"><div className="panel-head"><div><span className="index">05</span><h2>Execution review</h2></div><span className={`pill ${execution.status==='completed'?'success':'warn'}`}>{execution.status}</span></div><p>{execution.review || execution.error}</p><div className="test-grid">{execution.tests?.map(test=><article key={test.suite_id}><b>{test.passed?'PASS':'FAIL'} · {test.suite_id}</b><code>{test.command}</code><pre>{test.output || 'No output'}</pre></article>)}</div><h3>Reviewable diff</h3><pre className="diff">{execution.diff || 'No diff produced.'}</pre></section>}

      <section className="bottom-grid">
        <div className="panel"><div className="panel-head"><div><span className="index">06</span><h2>Plans</h2></div><button className="ghost" onClick={load}>Refresh</button></div><div className="list">{plans.length?plans.map(plan=><button key={plan.id} onClick={()=>setSelectedPlan(plan)}><span><b>{plan.summary}</b><small>{plan.id} · {short(plan.base_revision)}</small></span><em>{plan.status}</em></button>):<Empty text="No persisted plans."/>}</div></div>
        <div className="panel"><div className="panel-head"><div><span className="index">07</span><h2>Knowledge updates</h2></div><button className="ghost" disabled={busy} onClick={refreshKnowledge}>Scan revision</button></div><div className="list">{knowledge.length?knowledge.map(report=><article key={report.id}><b>{short(report.from_revision)} → {short(report.to_revision)}</b><span>{report.summary}</span><em>{report.status}</em></article>):<Empty text="No revision change reports."/>}</div></div>
      </section>

      <footer><span>Read MCP</span><code>POST /mcp</code><span>Admin MCP</span><code>POST /mcp/admin</code><span>Protocol</span><code>2026-07-28</code></footer>
    </main>
  </div>
}

function Metric({value,label}) { return <div><b>{value ?? '—'}</b><small>{label}</small></div> }
function Empty({text}) { return <div className="empty">{text}</div> }

export default App
