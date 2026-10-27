(() => {
  const $ = (id) => document.getElementById(id);
  const base = "/api/v1/agentx-dev";
  let selected = null, agents = [], bindings = [], active = null, previous = null, busy = false;
  // Optional local personal-server credential, never the AgentX token.
  const localToken = sessionStorage.getItem("agentx.localToken") || "";
  $("save-token").onclick = () => { sessionStorage.setItem("agentx.localToken", $("local-token").value); location.reload(); };
  const headers = () => ({ "Content-Type": "application/json", ...(localToken ? { Authorization: `Bearer ${localToken}` } : {}) });
  async function request(path, method = "GET", body, idempotency = false) {
    const h = headers();
    if (idempotency) h["Idempotency-Key"] = crypto.randomUUID();
    const r = await fetch(base + path, { method, headers: h, body: body === undefined ? undefined : JSON.stringify(body) });
    if (!r.ok) { let e; try { e = await r.json(); } catch { e = {}; } throw new Error(e.error?.message || e.error || `HTTP ${r.status}`); }
    if (r.status === 204) return null;
    return r.json();
  }
  function notice(id, message, error = false) { $(id).textContent = message; $(id).classList.toggle("error", error); }
  function select(a) { if (busy) return; if (selected !== a.id) previous = null; selected = a.id; render(); }
  function render() {
    const list = $("agents"); list.replaceChildren();
    for (const a of agents) {
      const b = document.createElement("button"); b.className = "agent" + (selected === a.id ? " selected" : ""); b.dataset.agentId = a.id; b.disabled = busy;
      const row = document.createElement("div"); row.className = "row spread";
      const name = document.createElement("strong"); name.textContent = a.name;
      const status = document.createElement("span"); status.className = "status " + a.status; status.textContent = a.status === "online" ? (a.busy ? "在线 · 忙碌" : "在线") : a.status === "unhealthy" ? "未就绪" : "离线";
      row.append(name, status); const sub = document.createElement("div"); sub.className = "sub"; sub.textContent = `${a.id} · ${a.owner_id}`; b.append(row, sub); b.onclick = () => select(a); list.append(b);
    }
    if (!agents.length) { const p = document.createElement("p"); p.id = "empty"; p.textContent = "暂无可访问的智能体"; list.append(p); }
    const a = agents.find(a => a.id === selected);
    $("target").textContent = a?.name || "选择远程智能体";
    $("description").textContent = a ? `${a.description || "远程智能体"} · ${a.status_reason || a.status}` : "从左侧目录选择一个在线智能体。";
    $("target-status").textContent = a ? `${a.executor} · ${a.status}` : "";
    $("send").disabled = busy || !a || a.status !== "online" || a.busy;
    $("cancel").disabled = !busy || !active;
    const local = bindings.some(b => b.remote_id === selected);
    $("disconnect").hidden = !local; $("reconnect").hidden = !local;
  }
  async function refresh() {
    agents = (await request(`/agents?scope=${$("scope").value}`)).data;
    const info = await request("/info"); bindings = info.bindings; $("info").textContent = JSON.stringify(info, null, 2); $("origin").textContent = info.local_origin;
    if (!selected && agents.length) selected = agents[0].id; render();
  }
  $("refresh").onclick = () => refresh().catch(e => notice("request-status", e.message, true));
  $("scope").onchange = $("refresh").onclick;
  $("local-agent").onchange = () => {
    const option = $("local-agent").selectedOptions[0];
    $("publish-name").value = option?.dataset.name || "";
    $("publish").disabled = option?.dataset.ready !== "true";
    notice("publish-status", option?.dataset.ready === "true" ? "" : "请先在智能体页面完成配置并启动本地智能体。");
  };
  $("publish").onclick = async () => {
    $("publish").disabled = true;
    try {
      const result = await request("/publish", "POST", { agent_id: $("local-agent").value, name: $("publish-name").value, visibility: $("visibility").value }, true);
      notice("publish-status", `已发布：${result.agent.id}`); selected = result.agent.id; await refresh();
    } catch (e) { notice("publish-status", e.message, true); } finally { $("publish").disabled = false; }
  };
  $("disconnect").onclick = async () => { try { await request(`/connections/${selected}/stop`, "POST", {}); await refresh(); } catch (e) { notice("request-status", e.message, true); } };
  $("reconnect").onclick = async () => { try { await request(`/connections/${selected}/start`, "POST", {}); await refresh(); } catch (e) { notice("request-status", e.message, true); } };
  $("cancel").onclick = async () => { try { await request(`/responses/${active}/cancel`, "POST", {}); notice("request-status", "已发送取消请求，等待执行端确认"); } catch (e) { notice("request-status", e.message, true); } };
  function final(response) {
    active = response.id;
    if (response.status === "completed") { $("output").textContent = response.output.flatMap(o => o.content || []).filter(p => p.type === "output_text").map(p => p.text).join(""); previous = response.id; }
    notice("request-status", `${response.status} · ${response.id}${response.error ? " · " + response.error.message : ""}`, response.status !== "completed");
  }
  $("send").onclick = async () => {
    if (!selected || !$("prompt").value.trim()) return;
    busy = true; active = null; $("output").textContent = ""; $("trace").textContent = ""; notice("request-status", "正在经过 AgentX 派发请求…"); render();
    try {
      const payload = { model: selected, input: $("prompt").value, stream: $("stream").checked, ...(previous ? { previous_response_id: previous } : {}) };
      const r = await fetch(base + "/responses", { method: "POST", headers: { ...headers(), "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(payload) });
      active = r.headers.get("X-Response-Id"); render();
      if (!r.ok) { const e = await r.json(); throw new Error(e.error?.message || `HTTP ${r.status}`); }
      if (!payload.stream) final(await r.json());
      else {
        const reader = r.body.getReader(), decoder = new TextDecoder(); let buffer = "";
        function event(block) {
          const data = block.split("\n").filter(l => l.startsWith("data:")).map(l => l.slice(5).trimStart()).join("\n"); if (!data) return;
          const e = JSON.parse(data);
          if (e.response?.id) { active = e.response.id; render(); }
          if (e.type === "response.output_text.delta") $("output").textContent += e.delta;
          if (e.type === "response.completed" || e.type === "response.failed") final(e.response);
          if (e.type === "error") throw new Error(e.message || e.code);
        }
        while (true) { const { value, done } = await reader.read(); buffer += decoder.decode(value, { stream: !done }); let index; while ((index = buffer.indexOf("\n\n")) >= 0) { const block = buffer.slice(0, index); buffer = buffer.slice(index + 2); event(block); } if (done) break; }
        if (active) final(await request(`/responses/${active}`));
      }
    } catch (e) { notice("request-status", e.message, true); }
    finally { if (active) { try { $("trace").textContent = JSON.stringify((await request(`/diagnostics?response_id=${active}`)).data, null, 2); } catch {} } await refresh().catch(() => {}); busy = false; render(); }
  };
  (async () => {
    try {
      const local = await request("/local-agents"); const select = $("local-agent"); select.replaceChildren();
      for (const a of local) { const option = document.createElement("option"); option.value = a.id; option.textContent = `${a.name} · ${{running:"运行中",stopped:"已停止",profile_incomplete:"待配置",starting:"启动中"}[a.state] || a.state}`; option.dataset.name = a.name; option.dataset.ready = String(a.ready && a.state === "running"); select.append(option); }
      if (!local.length) { const option = document.createElement("option"); option.value = ""; option.textContent = "本实例没有本地智能体"; select.append(option); $("publish").disabled = true; }
      const ready = [...select.options].find(o => o.dataset.ready === "true"); if (ready) select.value = ready.value; select.onchange(); await refresh();
    } catch (e) { notice("request-status", e.message, true); }
    setInterval(() => { if (!document.hidden) refresh().catch(e => notice("request-status", e.message, true)); }, 5000);
  })();
})();
