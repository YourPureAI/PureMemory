package settings

import (
	"encoding/json"
	"log"
	"net/http"

	"user-memory-collector/buffer"
	"user-memory-collector/privacy"
)

const settingsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>PureMemory — Settings</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Segoe UI',system-ui,sans-serif;background:#0d0d14;color:#e0e0e0;min-height:100vh}
header{background:linear-gradient(135deg,#1e1b4b 0%,#312e81 50%,#1e40af 100%);padding:24px 32px}
header h1{font-size:22px;font-weight:600;color:#fff;display:flex;align-items:center;gap:10px}
header p{font-size:13px;color:rgba(255,255,255,.6);margin-top:4px}
.tabs{display:flex;border-bottom:1px solid #2a2a3a;padding:0 32px;background:#12121f}
.tab{padding:12px 24px;cursor:pointer;font-size:14px;color:#888;border-bottom:2px solid transparent;transition:all .2s;user-select:none}
.tab.active{color:#818cf8;border-bottom-color:#818cf8}
.tab:hover:not(.active){color:#a5b4fc}
.content{padding:32px;max-width:740px}
.card{background:#1a1a2e;border:1px solid #252545;border-radius:12px;padding:24px;margin-bottom:20px}
.card h2{font-size:15px;font-weight:600;color:#a5b4fc;margin-bottom:6px;display:flex;align-items:center;gap:10px}
.card p{font-size:13px;color:#777;margin-bottom:16px;line-height:1.7}
code{font-family:'Cascadia Code','Consolas',monospace;background:#0f0f1a;padding:1px 5px;border-radius:4px;font-size:12px;color:#c4b5fd}
.badge{font-size:11px;background:#312e81;color:#a5b4fc;padding:2px 8px;border-radius:99px;font-weight:500}
.tags{display:flex;flex-wrap:wrap;gap:7px;margin-bottom:16px;min-height:32px}
.tag{display:inline-flex;align-items:center;gap:6px;background:#1e1e3a;border:1px solid #333360;border-radius:6px;padding:5px 11px;font-size:13px;font-family:'Cascadia Code','Consolas',monospace;color:#c4b5fd}
.tag.builtin{opacity:.55}
.tag .x{cursor:pointer;color:#555;font-size:16px;line-height:1;transition:color .15s;margin-left:2px}
.tag .x:hover{color:#f87171}
.empty{color:#444;font-size:13px;padding:6px 0}
.row{display:flex;gap:8px}
input[type=text]{flex:1;background:#0f0f1a;border:1px solid #333;border-radius:8px;padding:10px 14px;font-size:13px;color:#e0e0e0;outline:none;font-family:'Cascadia Code','Consolas',monospace;transition:border-color .2s}
input[type=text]:focus{border-color:#818cf8}
input[type=text]::placeholder{color:#333}
.btn{padding:10px 20px;border-radius:8px;border:none;font-size:13px;cursor:pointer;font-family:inherit;font-weight:500;transition:all .15s}
.btn-add{background:#252545;color:#a5b4fc;border:1px solid #333360}
.btn-add:hover{background:#2d2d58}
.btn-save{background:#4f46e5;color:#fff;margin-top:18px}
.btn-save:hover{background:#4338ca}
.btn-save:active{transform:scale(.97)}
.toast{display:inline-flex;align-items:center;gap:6px;margin-top:18px;color:#4ade80;font-size:13px;opacity:0;transition:opacity .3s}
.toast.show{opacity:1}
.tab-panel{display:none}
.tab-panel.active{display:block}
.divider{height:1px;background:#1e1e35;margin:20px 0}

/* Buffer list styles */
.buf-item{border-bottom:1px solid #2a2a3a;padding:12px 0;transition:background .1s}
.buf-item:last-child{border-bottom:none}
.buf-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:6px}
.buf-title{color:#818cf8;font-weight:600;font-size:13px;font-family:'Cascadia Code',monospace}
.buf-time{font-size:11px;color:#555}
.buf-body{font-size:12px;color:#999;word-break:break-all}
</style>
</head>
<body>
<header>
  <h1>🧠 PureMemory Settings</h1>
  <p>Local memory collection agent — changes apply immediately, no restart needed</p>
</header>

<div class="tabs">
  <div class="tab" onclick="showTab('identity',this)">👤 Identity</div>
  <div class="tab active" onclick="showTab('privacy',this)">🔒 Password Protection</div>
  <div class="tab" onclick="showTab('domains',this)">🚫 Excluded Domains</div>
  <div class="tab" onclick="showTab('tags',this)">🏷️ Tags</div>
  <div class="tab" onclick="showTab('buffer',this)">📊 Buffer Preview</div>
</div>

<div class="content">
  <!-- IDENTITY TAB -->
  <div id="panel-identity" class="tab-panel">
    <div class="card">
      <h2>👤 Device Identity</h2>
      <p>Configure how this device is identified when sending data to the server.</p>
      <div class="row" style="margin-bottom: 12px;">
        <div style="flex:1;">
          <label style="display:block;font-size:12px;margin-bottom:4px;color:#a5b4fc;">User ID</label>
          <input type="text" id="input-userid" placeholder="e.g. martin@example.com" />
        </div>
        <div style="flex:1;">
          <label style="display:block;font-size:12px;margin-bottom:4px;color:#a5b4fc;">Device ID</label>
          <input type="text" id="input-deviceid" placeholder="e.g. martins-macbook" />
        </div>
      </div>
      <button class="btn btn-save" onclick="saveAll()">💾 Save Changes</button>
      <div class="toast" id="toast-identity">✓ Saved and applied instantly</div>
    </div>
  </div>

  <!-- PASSWORDS TAB -->
  <div id="panel-privacy" class="tab-panel active">
    <div class="card">
      <h2>Built-in API Key Patterns <span class="badge">always active</span></h2>
      <p>These common secret prefixes are always redacted in captured text. They cannot be disabled.</p>
      <div class="tags" id="builtin-tags"></div>
    </div>
    <div class="card">
      <h2>✏️ Custom Secret Prefixes</h2>
      <p>
        Add any string prefix. Words starting with it will be replaced with <code>*******</code>
        in all captured text — keystrokes, clipboard, files, and quick notes.
        <br>Example: add <code>abc!</code> to redact words like <code>abc!MyP@ssw0rd</code>.
      </p>
      <div class="tags" id="custom-tags"></div>
      <div class="row">
        <input type="text" id="new-prefix" placeholder='e.g.  abc!   mypass_   Bearer sk-' />
        <button class="btn btn-add" onclick="addPrefix()">+ Add</button>
      </div>
      <br>
      <button class="btn btn-save" onclick="saveAll()">💾 Save Changes</button>
      <div class="toast" id="toast-privacy">✓ Saved and applied instantly</div>
    </div>
  </div>

  <!-- EXCLUDED DOMAINS TAB -->
  <div id="panel-domains" class="tab-panel">
    <div class="card">
      <h2>Built-in Exclusions <span class="badge">always active</span></h2>
      <p>These international domains are permanently excluded from ALL logging. If the domain appears in your active window title or URL, nothing is captured.</p>
      <div class="tags" id="builtin-dom-tags"></div>
    </div>
    <div class="card">
      <h2>✏️ Custom Excluded Domains</h2>
      <p>
        Add domains you want completely ignored by the PureMemory agent (e.g. banks, sensitive portals).
        <br>Matches substrings in URL and Window Titles. Example: <code>ib.fio.cz</code>
      </p>
      <div class="tags" id="custom-dom-tags"></div>
      <div class="row">
        <input type="text" id="new-domain" placeholder='e.g.  internetbanking.kb.cz' />
        <button class="btn btn-add" onclick="addDomain()">+ Add</button>
      </div>
      <br>
      <button class="btn btn-save" onclick="saveAll()">💾 Save Changes</button>
      <div class="toast" id="toast-domains">✓ Saved and applied instantly</div>
    </div>
  </div>

  <!-- TAGS TAB -->
  <div id="panel-tags" class="tab-panel">
    <div class="card">
      <h2>🏷️ Note Tags</h2>
      <p>
        Define tags that you can attach to quick notes (Ctrl+Shift+Space or Ctrl+C+C).
      </p>
      <div class="tags" id="custom-note-tags"></div>
      <div class="row">
        <input type="text" id="new-tag" placeholder='e.g.  work  idea' />
        <button class="btn btn-add" onclick="addTag()">+ Add</button>
      </div>
      <br>
      <button class="btn btn-save" onclick="saveAll()">💾 Save Changes</button>
      <div class="toast" id="toast-tags">✓ Saved and applied instantly</div>
    </div>
  </div>

  <!-- BUFFER PREVIEW TAB -->
  <div id="panel-buffer" class="tab-panel">
    <div class="card">
      <h2>📊 Unsent Event Buffer</h2>
      <p>
        A snapshot of up to 50 currently queued events waiting to be safely shipped to the Ingestion server.
        Use this to verify data correctly logs or redacts.
        <button class="btn btn-add" style="float:right; padding:5px 12px; margin-top:-8px" onclick="loadBuffer()">↻ Refresh</button>
      </p>
      <div class="divider"></div>
      <div id="buffer-events">
        <span class="empty">Loading buffer...</span>
      </div>
    </div>
  </div>
</div>

<script>
const BUILTIN_PREF = ["sk-","ghp_","ghs_","xoxb-","xoxp-","AIza","ya29.","Bearer ","AKIA"];
const BUILTIN_DOM = ["paypal.com"];

let configUserID = "";
let configDeviceID = "";
let customPrefixes = [];
let customDomains = [];
let customTags = [];

async function load() {
  try {
    const r = await fetch('/api/privacy');
    const d = await r.json();
    configUserID = d.user_id || "";
    configDeviceID = d.device_id || "";
    customPrefixes = d.custom_prefixes || [];
    customDomains = d.custom_domains || [];
    customTags = d.tags || [];
    
    document.getElementById('input-userid').value = configUserID;
    document.getElementById('input-deviceid').value = configDeviceID;
  } catch(e) { 
    customPrefixes = []; 
    customDomains = []; 
    customTags = [];
  }
  renderBuiltin();
  renderCustom();
  
  // also initially load buffer if active
  loadBuffer();
}

async function loadBuffer() {
  const el = document.getElementById('buffer-events');
  el.innerHTML = '<span class="empty">Fetching locally...</span>';
  try {
    const r = await fetch('/api/buffer');
    if (!r.ok) throw new Error();
    const evts = await r.json();
    
    if (!evts || evts.length === 0) {
      el.innerHTML = '<span class="empty">✓ Buffer is completely empty (everything successfully shipped to server).</span>';
      return;
    }
    
    let ht = '';
    evts.forEach(e => {
       ht += '<div class="buf-item">';
       let dateStr = new Date(e.ts_start).toLocaleTimeString();
       ht += '<div class="buf-header"><span class="buf-title">' + esc(e.type) + '</span><span class="buf-time">' + dateStr + '</span></div>';
       
       let p = (typeof e.payload === 'object' && e.payload !== null) ? JSON.stringify(e.payload) : (e.payload || "");
       
       let c = e.context || {};
       let ctxStr = [];
       if (c.window_title) ctxStr.push('Title: ' + c.window_title);
       if (c.url) ctxStr.push('URL: ' + c.url);
       if (c.app_bundle) ctxStr.push('App: ' + c.app_bundle);
       
       if (p !== "") {
         if (p.length > 250) p = p.substring(0, 250) + '...';
         ht += '<div class="buf-body">' + esc(p) + '</div>';
       } else if (ctxStr.length > 0) {
         ht += '<div class="buf-body" style="color:#ccc">' + esc(ctxStr.join(' | ')) + '</div>';
       } else {
         ht += '<div class="buf-body" style="color:#444">(no payload / filtered)</div>';
       }
       ht += '</div>';
    });
    el.innerHTML = ht;
  } catch(e) {
    el.innerHTML = '<span class="empty" style="color:#f87171">Failed to fetch. Is the agent running correctly?</span>';
  }
}

function esc(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

function renderBuiltin() {
  document.getElementById('builtin-tags').innerHTML =
    BUILTIN_PREF.map(function(p) {
      return '<span class="tag builtin"><code>' + esc(p) + '</code></span>';
    }).join('');
    
  document.getElementById('builtin-dom-tags').innerHTML =
    BUILTIN_DOM.map(function(d) {
      return '<span class="tag builtin"><code>' + esc(d) + '</code></span>';
    }).join('');
}

function renderCustom() {
  const elPref = document.getElementById('custom-tags');
  if (!customPrefixes.length) {
    elPref.innerHTML = '<span class="empty">No custom prefixes yet. Add one below.</span>';
  } else {
    elPref.innerHTML = customPrefixes.map(function(p, i) {
      return '<span class="tag"><code>' + esc(p) + '</code><span class="x" onclick="removePref(' + i + ')">&times;</span></span>';
    }).join('');
  }

  const elDom = document.getElementById('custom-dom-tags');
  if (!customDomains.length) {
    elDom.innerHTML = '<span class="empty">No custom domains yet. Add one below.</span>';
  } else {
    elDom.innerHTML = customDomains.map(function(d, i) {
      return '<span class="tag"><code>' + esc(d) + '</code><span class="x" onclick="removeDom(' + i + ')">&times;</span></span>';
    }).join('');
  }

  const elTags = document.getElementById('custom-note-tags');
  if (!customTags.length) {
    elTags.innerHTML = '<span class="empty">No tags defined yet. Add one below.</span>';
  } else {
    elTags.innerHTML = customTags.map(function(t, i) {
      return '<span class="tag"><code>' + esc(t) + '</code><span class="x" onclick="removeTag(' + i + ')">&times;</span></span>';
    }).join('');
  }
}

function addPrefix() {
  const inp = document.getElementById('new-prefix');
  const val = inp.value.trim();
  if (!val) { inp.focus(); return; }
  if (customPrefixes.includes(val) || BUILTIN_PREF.includes(val)) {
    inp.select(); return;
  }
  customPrefixes.push(val);
  inp.value = '';
  renderCustom();
  inp.focus();
}

function addDomain() {
  const inp = document.getElementById('new-domain');
  var val = inp.value.trim().toLowerCase();
  val = val.replace(/^https?:\/\//, '').split('/')[0];

  if (!val) { inp.focus(); return; }
  if (customDomains.includes(val) || BUILTIN_DOM.includes(val)) {
    inp.select(); return;
  }
  customDomains.push(val);
  inp.value = '';
  renderCustom();
  inp.focus();
}

function removePref(i) {
  customPrefixes.splice(i, 1);
  renderCustom();
}

function removeDom(i) {
  customDomains.splice(i, 1);
  renderCustom();
}

function addTag() {
  const inp = document.getElementById('new-tag');
  var val = inp.value.trim();
  if (val && !val.startsWith('#')) val = '#' + val;
  if (!val) { inp.focus(); return; }
  if (customTags.includes(val)) {
    inp.select(); return;
  }
  customTags.push(val);
  inp.value = '';
  renderCustom();
  inp.focus();
}

function removeTag(i) {
  customTags.splice(i, 1);
  renderCustom();
}

async function saveAll() {
  const inpUserID = document.getElementById('input-userid').value.trim();
  const inpDeviceID = document.getElementById('input-deviceid').value.trim();

  const inpPref = document.getElementById('new-prefix');
  if (inpPref && inpPref.value.trim() !== '') addPrefix();
  
  const inpDom = document.getElementById('new-domain');
  if (inpDom && inpDom.value.trim() !== '') addDomain();

  const inpTag = document.getElementById('new-tag');
  if (inpTag && inpTag.value.trim() !== '') addTag();

  const bodyData = {
    user_id: inpUserID,
    device_id: inpDeviceID,
    secret_prefixes: customPrefixes,
    excluded_domains: customDomains,
    tags: customTags
  };

  const r = await fetch('/api/privacy', {
    method: 'POST',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify(bodyData)
  });
  
  const activeTab = document.querySelector('.tab-panel.active').id;
  let t = null;
  if (activeTab === 'panel-identity') t = document.getElementById('toast-identity');
  else if (activeTab === 'panel-privacy') t = document.getElementById('toast-privacy');
  else if (activeTab === 'panel-domains') t = document.getElementById('toast-domains');
  else if (activeTab === 'panel-tags') t = document.getElementById('toast-tags');
  
  if (r.ok) {
    if(t) {
      t.style.color = '#4ade80';
      t.innerHTML = '✓ Saved and applied instantly';
    }
  } else {
    if(t) {
      t.style.color = '#f87171';
      t.innerHTML = '✗ Save failed — check console';
    }
  }
  if(t) {
    t.classList.add('show');
    setTimeout(() => t.classList.remove('show'), 3000);
  }
}

function showTab(name, el) {
  document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.getElementById('panel-' + name).classList.add('active');
  el.classList.add('active');
  if (name === 'buffer') {
     loadBuffer();
  }
}

document.getElementById('new-prefix').addEventListener('keydown', e => {
  if (e.key === 'Enter') addPrefix();
});
document.getElementById('new-domain').addEventListener('keydown', e => {
  if (e.key === 'Enter') addDomain();
});
document.getElementById('new-tag').addEventListener('keydown', e => {
  if (e.key === 'Enter') addTag();
});

load();
</script>
</body>
</html>`

// Server serves the settings web UI and REST API on localhost:45679
type Server struct {
	filter *privacy.Filter
	buf    *buffer.SQLiteBuffer
	server *http.Server
}

func NewServer(filter *privacy.Filter, buf *buffer.SQLiteBuffer) *Server {
	return &Server{
		filter: filter,
		buf:    buf,
	}
}

func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(settingsHTML))
	})
	mux.HandleFunc("/api/privacy", s.handlePrivacyConfig)
	mux.HandleFunc("/api/buffer", s.handleBufferPreview)

	s.server = &http.Server{Addr: "127.0.0.1:45679", Handler: mux}
	log.Println("[Settings] UI available at http://127.0.0.1:45679")
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("[Settings] Server error: %v", err)
	}
}

func (s *Server) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

// REST Handlers
func (s *Server) handlePrivacyConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "http://127.0.0.1:45679")

	switch r.Method {
	case http.MethodGet:
		uid, did, prefixes, domains, tags := s.filter.GetConfig()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id":         uid,
			"device_id":       did,
			"custom_prefixes": prefixes,
			"custom_domains":  domains,
			"tags":            tags,
		})

	case http.MethodPost:
		var body struct {
			UserID          string   `json:"user_id"`
			DeviceID        string   `json:"device_id"`
			SecretPrefixes  []string `json:"secret_prefixes"`
			ExcludedDomains []string `json:"excluded_domains"`
			Tags            []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := s.filter.SetConfig(body.UserID, body.DeviceID, body.SecretPrefixes, body.ExcludedDomains, body.Tags); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBufferPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "http://127.0.0.1:45679")

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Fetch max 50 events from local SQLite to preview (O(1) CPU/RAM hit)
	events, err := s.buf.GetPendingBatch(50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(events)
}
