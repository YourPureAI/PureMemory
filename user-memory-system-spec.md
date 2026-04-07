# User Memory System – Component Specification
**Version:** 1.0  
**Určeno pro:** Claude Code / AI-assisted implementation  
**Datum:** 2026-04-04  
**Autor:** Architektonický návrh

---

## 0. Kontext a Záměr

Systém sbírá data o aktivitě uživatele na počítači (macOS + Windows), přenáší je do centrálního serveru (Mac Mini), kde jsou zpracována pomocí AI do strukturované dlouhodobé paměti. Tato paměť je následně dostupná přes MCP server jako kontext pro libovolné AI aplikace.

**Klíčové principy:**
- Minimalistický agent: < 20 MB RAM, < 1% CPU průměr
- Privacy-first: hesla a citlivá data jsou eliminována před zápisem
- Local-buffer-first: data se nikdy neztrácí výpadkem sítě
- Single Go codebase pro oba OS agenty

---

## 1. Přehled Komponent

```
┌─────────────────────────────────────────────────────────────┐
│  KLIENTSKÁ VRSTVA                                           │
│                                                             │
│  collector-agent/     (Go, cross-platform binary)           │
│  browser-extension/   (JS, Chromium Manifest V3)            │
└──────────────────────────────┬──────────────────────────────┘
                               │ HTTP/2 + TLS + gzip
                               │ Local network only
┌──────────────────────────────▼──────────────────────────────┐
│  SERVEROVÁ VRSTVA (Mac Mini, macOS)                         │
│                                                             │
│  ingest-api/          (Python, FastAPI)                     │
│  ai-pipeline/         (Python, Claude API)                  │
│  mcp-server/          (Python, MCP SDK)                     │
│  database/            (PostgreSQL + pgvector)               │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. Collector Agent (Go)

### 2.1 Struktura Projektu

```
collector-agent/
├── main.go
├── go.mod
├── go.sum
├── config/
│   ├── config.go              # Config loader (YAML)
│   └── defaults.go            # Statické blacklisty baseline
├── agent/
│   ├── agent.go               # Hlavní orchestrátor, start/stop
│   └── session.go             # Session management, session_id, device_id
├── watchers/
│   ├── appwindow_darwin.go    # NSWorkspace + Accessibility API (CGo)
│   ├── appwindow_windows.go   # UI Automation COM (CGo/syscall)
│   ├── appwindow_common.go    # Shared types, interface
│   ├── keystroke_darwin.go    # CGEventTap
│   ├── keystroke_windows.go   # SetWindowsHookEx (WH_KEYBOARD_LL)
│   ├── keystroke_common.go    # Aggregátor logika (shared)
│   ├── clipboard_darwin.go    # NSPasteboard polling
│   ├── clipboard_windows.go   # OpenClipboard polling
│   ├── clipboard_common.go    # Dedup logika (shared)
│   ├── filesystem_darwin.go   # FSEvents API (CGo)
│   ├── filesystem_windows.go  # ReadDirectoryChangesW
│   └── filesystem_common.go   # File classifier, content extractor
├── privacy/
│   ├── filter.go              # Hlavní privacy filter, orchestrace
│   ├── password.go            # Heuristiky detekce hesel
│   └── blacklist.go           # Blacklist engine (apps, domains, paths)
├── extractor/
│   ├── pdf.go                 # pdftotext subprocess wrapper
│   └── text.go                # Plain text / code file reader
├── correlation/
│   └── engine.go              # Vazba eventů na focus_session_id
├── buffer/
│   ├── sqlite.go              # SQLite WAL buffer (mattn/go-sqlite3)
│   └── schema.go              # DB schema, migrace
├── transport/
│   ├── client.go              # HTTP/2 client, TLS, gzip
│   ├── retry.go               # Exponential backoff, ack handling
│   └── batch.go               # Batch builder (max 100 eventů nebo 60s)
├── extension/
│   └── server.go              # Lokální HTTP server pro browser extension
└── build/
    ├── build-darwin.sh
    ├── build-windows.bat
    └── install-launchagent.sh # macOS LaunchAgent plist instalace
```

### 2.2 Konfigurace (config.yaml)

Agent hledá config soubor v `~/.config/user-memory/config.yaml` (macOS) nebo `%APPDATA%\user-memory\config.yaml` (Windows).

```yaml
server:
  host: "mac-mini.local"       # nebo IP adresa
  port: 8443
  api_key: ""                  # Generováno při instalaci, nikdy neměnit ručně
  tls_cert_fingerprint: ""     # SHA256 fingerprint self-signed certu serveru

collection:
  keystroke_idle_threshold_ms: 2000     # Pauza = konec typing session
  keystroke_min_word_count: 3           # Méně slov → považuj za heslo/username
  keystroke_min_chars: 20               # Méně znaků → považuj za heslo
  clipboard_poll_interval_ms: 500
  idle_detection_threshold_s: 60        # Bez aktivity = idle
  file_open_confirm_delay_s: 10         # Čekej N sekund před extrakcí
  pdf_max_chars: 10000                  # Kolik znaků z PDF extrahovat
  file_max_size_mb: 50                  # Soubory větší → skip content

buffer:
  flush_interval_s: 60
  flush_max_events: 100
  max_buffer_days: 7                    # Pokud server nedostupný, drž N dní

# Uživatelské rozšíření blacklistů (baseline je v defaults.go, nelze přepsat)
privacy:
  additional_app_blacklist:
    - "com.example.myinternalapp"
  additional_domain_blacklist:
    - "myprivatesite.com"
  additional_path_blacklist:
    - "/Users/jan/Personal"
```

### 2.3 Statická Baseline Blacklistů (defaults.go)

Tyto hodnoty jsou hardcoded a uživatel je NEMŮŽE odebrat, pouze přidávat vlastní.

```go
// App blacklist (bundle IDs pro macOS, process names pro Windows)
var BaselineAppBlacklist = []string{
    // Password managers
    "com.agilebits.onepassword7",
    "com.lastpass.LastPass",
    "com.bitwarden.desktop",
    "org.keepassxc.keepassxc",
    // System auth
    "com.apple.keychainaccess",
    "com.apple.SecurityAgent",
    // Banking (přidávat dle potřeby)
    "com.apple.Passkeys",
    // Windows equivalents
    "1Password.exe",
    "LastPass.exe",
    "Bitwarden.exe",
    "KeePassXC.exe",
}

// Domain blacklist (substring match na url_domain)
var BaselineDomainBlacklist = []string{
    "mail.google.com",
    "outlook.live.com",
    "outlook.office.com",
    "accounts.google.com",
    "login.microsoftonline.com",
    "appleid.apple.com",
    "icloud.com",
    "paypal.com",
    "stripe.com",
}

// File path blacklist (prefix match)
var BaselinePathBlacklist = []string{
    "/.ssh/",
    "/.gnupg/",
    "/Keychain/",
    "/.aws/credentials",
    "/.config/git/credentials",
    "/Library/Keychains/",
    // Windows
    `\AppData\Roaming\Microsoft\Credentials\`,
}

// File extension blacklist (žádná extrakce obsahu)
var BaselineExtensionBlacklist = []string{
    ".key", ".pem", ".p12", ".pfx", ".cer",
    ".kdbx",  // KeePass database
    ".wallet", // crypto wallets
}
```

### 2.4 Event Schema (Go struct → JSON)

```go
// Všechny eventy sdílí tuto obálku
type Event struct {
    EventID       string          `json:"event_id"`        // ULID
    SessionID     string          `json:"session_id"`      // UUID per boot/login
    FocusID       string          `json:"focus_session_id"` // UUID per app focus
    DeviceID      string          `json:"device_id"`       // HMAC(machine-id, api_key)
    SchemaVersion string          `json:"schema_version"`  // "1.0"
    TsStart       time.Time       `json:"ts_start"`
    TsEnd         time.Time       `json:"ts_end"`
    ActiveMs      int64           `json:"active_duration_ms"`
    IdleMs        int64           `json:"idle_duration_ms"`
    Timezone      string          `json:"tz"`
    Type          EventType       `json:"type"`
    Context       EventContext    `json:"context"`
    Payload       json.RawMessage `json:"payload"`         // Type-specific
}

type EventType string
const (
    EventAppFocus    EventType = "app_focus"
    EventURLVisit    EventType = "url_visit"
    EventTextInput   EventType = "text_input"
    EventClipboard   EventType = "clipboard"
    EventFileAccess  EventType = "file_access"
    EventIdleStart   EventType = "idle_start"
    EventIdleEnd     EventType = "idle_end"
)

type EventContext struct {
    AppBundle   string `json:"app_bundle"`    // com.google.Chrome
    AppName     string `json:"app_name"`      // Google Chrome
    WindowTitle string `json:"window_title"`
    URL         string `json:"url,omitempty"`
    URLDomain   string `json:"url_domain,omitempty"`
    OSPlatform  string `json:"os_platform"`   // "darwin" | "windows"
}

// Payload pro EventTextInput
type TextInputPayload struct {
    Text      string `json:"text"`
    WordCount int    `json:"word_count"`
    DurationMs int64 `json:"duration_ms"`
}

// Payload pro EventClipboard
type ClipboardPayload struct {
    ContentType string `json:"content_type"` // "text" | "url" | "skipped"
    Text        string `json:"text,omitempty"`
    CharCount   int    `json:"char_count"`
}

// Payload pro EventFileAccess
type FileAccessPayload struct {
    Path        string `json:"path"`
    Name        string `json:"name"`
    Extension   string `json:"extension"`
    SizeBytes   int64  `json:"size_bytes"`
    ModifiedAt  time.Time `json:"modified_at"`
    OpenedByApp string `json:"opened_by_app"`
    Content     *FileContent `json:"content,omitempty"`
}

type FileContent struct {
    Extracted bool   `json:"extracted"`
    Method    string `json:"method"`    // "plaintext" | "pdftotext" | "skipped"
    Text      string `json:"text,omitempty"`
    PageCount int    `json:"page_count,omitempty"`
}

// Payload pro EventURLVisit (bez web content – ten jde přes extension)
type URLVisitPayload struct {
    URL          string `json:"url"`
    Title        string `json:"title"`
    DwellMs      int64  `json:"dwell_ms"`
    ScrollDepth  int    `json:"scroll_depth_pct,omitempty"`
    WebContent   *WebContent `json:"web_content,omitempty"`
}

type WebContent struct {
    Extracted   bool     `json:"extracted"`
    Method      string   `json:"method"`      // "browser_extension" | "accessibility_fallback"
    TextChunks  []string `json:"text_chunks"` // ~500 slov na chunk
    WordCount   int      `json:"word_count"`
}
```

### 2.5 Password Detection Algorithm (privacy/password.go)

```
Vstup: accumulated_text string, app_context EventContext

Vrstva 1 – SECURE FIELD (macOS: AXSecureTextField, Windows: PasswordBox control)
  → Pokud true: DISCARD, return

Vrstva 2 – APP BLACKLIST
  → Pokud app_bundle v blacklistu: DISCARD, return

Vrstva 3 – DOMAIN BLACKLIST  
  → Pokud url_domain v blacklistu: DISCARD, return

Vrstva 4 – DÉLKA A STRUKTURA
  words = split(accumulated_text, whitespace)
  Pokud len(words) < 3 AND len(accumulated_text) < 20: DISCARD, return

Vrstva 5 – ENTROPIE
  entropy = shannonEntropy(accumulated_text)
  Pokud entropy > 4.0 AND len(words) == 1: DISCARD, return
  (vysoká entropie jednoslovného stringu = typické heslo)

Vrstva 6 – TIMING HEURISTIKA
  Pokud typing_duration_ms < 3000 AND len(words) <= 2: DISCARD, return

→ PASS: emit jako TextInputPayload
```

### 2.6 Idle Detection

```
Sleduj: čas posledního keystrokes NEBO mouse move event

Každých 5 sekund:
  elapsed = now - last_activity_time
  
  Pokud elapsed > idle_threshold (default 60s) AND NOT already_idle:
    emit EventIdleStart
    already_idle = true
    
  Pokud elapsed <= idle_threshold AND already_idle:
    emit EventIdleEnd { idle_duration_ms: ... }
    already_idle = false

Focus session počítá active_duration_ms = total_duration - sum(idle_periods)
```

### 2.7 File Content Extraction (extractor/)

```
Trigger: FSEvent (macOS) nebo ReadDirectoryChangesW (Windows) signalizuje otevření souboru

1. Filtruj: je path v blacklistu? → skip
2. Čekej 10 sekund (confirm_delay)
3. Ověř: je ta samá aplikace stále aktivní? → pokud ne, skip (systémová operace)
4. Klasifikuj příponu:
   
   PLAIN TEXT (.txt, .md, .rst, .csv, .log, .py, .js, .go, .ts, .java, .c, .cpp, .h, .sh, .yaml, .json, .toml, .xml):
     → přečíst prvních min(file_size, 50KB) jako UTF-8
     → emit s method="plaintext"
   
   PDF (.pdf):
     → spustit: pdftotext <path> - | head -c <pdf_max_chars>
     → pokud pdftotext není dostupný: emit bez content, method="skipped_no_tool"
     → emit s method="pdftotext"
   
   OSTATNÍ (.docx, .xlsx, .pptx, .jpg, .png, .mp4, atd.):
     → emit FileAccessPayload BEZ content (jen metadata)
   
   SKIP (> file_max_size_mb NEBO v extension blacklistu):
     → emit FileAccessPayload BEZ content, extracted=false

pdftotext binary path:
  macOS: /opt/homebrew/bin/pdftotext (Homebrew poppler) nebo /usr/local/bin/pdftotext
  Windows: %PROGRAMFILES%\poppler\bin\pdftotext.exe
  → Hledat v PATH, pokud nenalezeno: logovat warning, pokračovat bez extrakce
```

### 2.8 Local SQLite Buffer Schema

```sql
CREATE TABLE events (
    id          TEXT PRIMARY KEY,    -- ULID
    created_at  INTEGER NOT NULL,    -- Unix ms
    payload     TEXT NOT NULL,       -- JSON (celý Event struct)
    sent        INTEGER DEFAULT 0,   -- 0=pending, 1=acked
    sent_at     INTEGER              -- Unix ms kdy server potvrdil
);

CREATE INDEX idx_events_sent ON events(sent, created_at);

-- Auto-cleanup: mazat sent eventy starší 24h (spouštět při startu a každou hodinu)
DELETE FROM events WHERE sent = 1 AND sent_at < (strftime('%s','now') - 86400) * 1000;

-- Emergency cleanup: pokud buffer > 7 dní dat, mazat nejstarší (buffer.max_buffer_days)
```

SQLite pragma nastavení pro výkon:
```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA cache_size = -2000;  -- 2MB cache
PRAGMA temp_store = MEMORY;
```

### 2.9 Transport (transport/)

```
Batch podmínky (OR):
  - uplynulo 60 sekund od posledního flushe
  - buffer obsahuje >= 100 neodeslaných eventů

Batch request:
  POST https://{host}:{port}/api/v1/events
  Content-Type: application/json
  Content-Encoding: gzip
  Authorization: Bearer {api_key}
  
  Body: { "batch_id": "ulid", "device_id": "...", "events": [...] }

Response: 
  200 OK: { "ack_ids": ["event_id_1", ...], "batch_id": "..." }
  → Označit eventy jako sent=1

Retry policy:
  Max attempts: unlimited (při nedostupnosti serveru čekat a zkoušet)
  Backoff: 5s → 15s → 30s → 60s → 300s (cap)
  Jitter: ±20% pro prevenci thundering herd
  
TLS:
  Self-signed certifikát (generovaný při instalaci serveru)
  Agent uloží SHA256 fingerprint při první registraci (TOFU – Trust On First Use)
  Při každém spojení ověřuje fingerprint → odmítne jiný cert
```

### 2.10 Instalace a Spuštění

**macOS (LaunchAgent):**
```xml
<!-- ~/Library/LaunchAgents/com.usermemory.collector.plist -->
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" ...>
<plist version="1.0">
<dict>
    <key>Label</key><string>com.usermemory.collector</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/user-memory-collector</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>/tmp/user-memory-collector.log</string>
    <key>StandardErrorPath</key><string>/tmp/user-memory-collector.err</string>
</dict>
</plist>
```

Vyžadovaná macOS oprávnění (uživatel musí schválit v System Settings):
- Accessibility (pro AppWindow watcher + keystroke)
- Input Monitoring (pro CGEventTap)
- Full Disk Access (pro FSEvents na home directory)

**Windows (Service):**
```
sc create "UserMemoryCollector" binPath="C:\Program Files\UserMemory\collector.exe" start=auto
sc start UserMemoryCollector
```

---

## 3. Browser Extension

### 3.1 Struktura

```
browser-extension/
├── manifest.json
├── background.js          # Service worker
├── content-script.js      # Injektován do každé stránky
├── readability.min.js     # Mozilla Readability (vendored, MIT licence)
└── options/
    ├── options.html
    └── options.js         # Uživatelský blacklist domén
```

### 3.2 manifest.json

```json
{
  "manifest_version": 3,
  "name": "User Memory Collector",
  "version": "1.0.0",
  "permissions": ["tabs", "storage", "scripting", "activeTab"],
  "host_permissions": ["http://localhost/*"],
  "background": { "service_worker": "background.js" },
  "content_scripts": [{
    "matches": ["<all_urls>"],
    "js": ["content-script.js"],
    "run_at": "document_idle"
  }],
  "options_page": "options/options.html"
}
```

### 3.3 Dwell Time Logic (background.js)

```javascript
const DWELL_SKIP = 10_000;       // < 10s: nic
const DWELL_URL_ONLY = 30_000;   // 10-30s: jen URL + title
const DWELL_FULL = 30_000;       // > 30s: plná extrakce textu

const sessions = {};  // tabId → { url, title, startTs, lastActiveTs, textExtracted }

chrome.tabs.onActivated.addListener(({ tabId }) => {
    // Ulož čas opuštění předchozí session
    // Zahaj novou session pro tabId
});

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
    if (changeInfo.status !== 'complete') return;
    // Nová URL → nová session
    sessions[tabId] = { url: tab.url, title: tab.title, startTs: Date.now(), textExtracted: false };
    
    // Naplánuj extrakci po DWELL_FULL ms
    setTimeout(() => extractIfActive(tabId), DWELL_FULL);
});

function extractIfActive(tabId) {
    const session = sessions[tabId];
    if (!session || session.textExtracted) return;
    
    // Je tab stále aktivní?
    chrome.tabs.get(tabId, tab => {
        if (!tab.active) return;
        // Injektuj Readability
        chrome.scripting.executeScript({
            target: { tabId },
            func: extractReadableContent
        }, results => {
            session.textExtracted = true;
            sendToAgent({ ...session, content: results[0].result });
        });
    });
}

function sendToAgent(data) {
    fetch('http://localhost:45678/extension-event', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    }).catch(() => { /* agent neběží nebo nedostupný – silent fail */ });
}
```

### 3.4 Content Extraction (content-script.js)

```javascript
function extractReadableContent() {
    // Readability.js je injektován jako dependency
    const documentClone = document.cloneNode(true);
    const reader = new Readability(documentClone);
    const article = reader.parse();
    
    if (!article) return null;
    
    // Chunking: ~500 slov na chunk
    const words = article.textContent.split(/\s+/).filter(Boolean);
    const chunks = [];
    for (let i = 0; i < words.length; i += 500) {
        chunks.push(words.slice(i, i + 500).join(' '));
    }
    
    return {
        title: article.title,
        byline: article.byline,
        text_chunks: chunks,
        word_count: words.length,
        scroll_depth_pct: Math.round(
            (window.scrollY + window.innerHeight) / document.documentElement.scrollHeight * 100
        )
    };
}
```

### 3.5 Agent-side Extension Server (extension/server.go)

Agent spustí lokální HTTP server na `localhost:45678` (loopback only):

```
POST localhost:45678/extension-event
  Body: ExtensionEvent JSON
  → Privacy filter (domain blacklist)
  → Koreluj s aktuálním focus_session_id
  → Ulož do SQLite buffer jako EventURLVisit s WebContent
  
  Response: 200 OK (vždy, i při filtraci – extension to nepotřebuje vědět)
```

---

## 4. Ingest API (Python/FastAPI – Mac Mini)

### 4.1 Struktura

```
ingest-api/
├── main.py
├── requirements.txt       # fastapi, uvicorn, sqlalchemy, asyncpg, pydantic
├── models/
│   ├── events.py          # Pydantic modely (mirror Go struct)
│   └── database.py        # SQLAlchemy modely
├── api/
│   ├── events.py          # POST /api/v1/events endpoint
│   ├── devices.py         # POST /api/v1/devices/register
│   └── health.py          # GET /health
├── services/
│   ├── validator.py       # Validace eventů, schema version check
│   └── pipeline_trigger.py # Trigger AI pipeline po přijetí batche
├── db/
│   ├── connection.py      # Async PostgreSQL pool
│   └── migrations/        # Alembic migrace
└── config.py              # Env-based konfigurace
```

### 4.2 API Endpoints

```
POST /api/v1/devices/register
  Body: { "device_id": "...", "os": "darwin|windows", "hostname": "..." }
  → Zaregistruje zařízení, vrátí api_key (pokud nové) nebo potvrdí existující
  → api_key je generován serverem při první registraci

POST /api/v1/events
  Header: Authorization: Bearer <api_key>
  Header: Content-Encoding: gzip
  Body: { "batch_id": "ulid", "device_id": "...", "events": [...] }
  → Validuj api_key
  → Ulož raw eventy do PostgreSQL (tabulka raw_events)
  → Triggeruj AI pipeline asynchronně (Celery task nebo asyncio background task)
  → Vrať: { "ack_ids": [...], "batch_id": "..." }

GET /health
  → { "status": "ok", "db": "ok", "pipeline": "ok" }
```

### 4.3 PostgreSQL Schema

```sql
-- Zařízení
CREATE TABLE devices (
    device_id       TEXT PRIMARY KEY,
    api_key_hash    TEXT NOT NULL,       -- bcrypt hash api_key
    os_platform     TEXT NOT NULL,
    hostname        TEXT,
    registered_at   TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ
);

-- Raw eventy (insert only, nikdy update)
CREATE TABLE raw_events (
    id              TEXT PRIMARY KEY,    -- event_id z agenta (ULID)
    device_id       TEXT REFERENCES devices(device_id),
    session_id      TEXT NOT NULL,
    received_at     TIMESTAMPTZ DEFAULT NOW(),
    event_type      TEXT NOT NULL,
    ts_start        TIMESTAMPTZ NOT NULL,
    ts_end          TIMESTAMPTZ,
    active_ms       BIGINT,
    context         JSONB NOT NULL,      -- app, window, url, domain
    payload         JSONB NOT NULL,      -- type-specific data
    processed       BOOLEAN DEFAULT FALSE
);

CREATE INDEX idx_raw_events_processed ON raw_events(processed, received_at);
CREATE INDEX idx_raw_events_device ON raw_events(device_id, ts_start);
CREATE INDEX idx_raw_events_type ON raw_events(event_type, ts_start);

-- Zpracované paměťové záznamy
CREATE TABLE memory_entries (
    id              BIGSERIAL PRIMARY KEY,
    device_id       TEXT REFERENCES devices(device_id),
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    period_start    TIMESTAMPTZ NOT NULL,
    period_end      TIMESTAMPTZ NOT NULL,
    entry_type      TEXT NOT NULL,       -- 'activity_summary' | 'topic' | 'document_read'
    title           TEXT,
    summary         TEXT NOT NULL,       -- AI-generated summary
    tags            TEXT[],              -- extrahovaná témata/klíčová slova
    raw_event_ids   TEXT[],             -- reference na zdrojové raw_events
    embedding       vector(1536),        -- pgvector embedding pro sémantické hledání
    metadata        JSONB               -- flexibilní metadata
);

CREATE INDEX idx_memory_device_time ON memory_entries(device_id, period_start DESC);
CREATE INDEX idx_memory_type ON memory_entries(entry_type, created_at DESC);
CREATE INDEX idx_memory_embedding ON memory_entries USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

-- Denní/týdenní souhrny (konsolidovaná paměť)
CREATE TABLE memory_summaries (
    id              BIGSERIAL PRIMARY KEY,
    device_id       TEXT REFERENCES devices(device_id),
    period_type     TEXT NOT NULL,       -- 'daily' | 'weekly'
    period_date     DATE NOT NULL,
    summary         TEXT NOT NULL,
    key_topics      TEXT[],
    key_apps        JSONB,               -- { "app_name": minutes_active }
    embedding       vector(1536),
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_id, period_type, period_date)
);
```

---

## 5. AI Pipeline (Python – Mac Mini)

### 5.1 Struktura

```
ai-pipeline/
├── main.py                # Celery worker entry point (nebo asyncio scheduler)
├── requirements.txt       # anthropic, celery, redis, asyncpg, pgvector
├── tasks/
│   ├── process_batch.py   # Zpracuj nový batch raw eventů
│   ├── summarize.py       # Generuj AI summaries
│   ├── consolidate.py     # Denní/týdenní konsolidace
│   └── embed.py           # Generuj embeddingy (text-embedding-3-small)
├── prompts/
│   ├── activity_summary.txt
│   ├── topic_extraction.txt
│   └── daily_consolidation.txt
└── config.py
```

### 5.2 Pipeline Flow

```
Trigger: Ingest API přijme batch → publish task do fronty

TASK: process_batch(batch_id)
  1. Načti neprocesované raw_events (processed=false)
  2. Seskup po focus_session_id a časových oknech (max 15 minut na skupinu)
  3. Pro každou skupinu: build_context_string(events)
  4. Claude API call: summarize + extract topics
  5. Ulož memory_entry s embedding
  6. Označ raw_events processed=true

SCHEDULED TASK: daily_consolidation() [každý den v noci]
  1. Načti všechny memory_entries za posledních 24h
  2. Claude API call: high-level denní souhrn
  3. Ulož memory_summary (period_type='daily')

SCHEDULED TASK: weekly_consolidation() [každý týden]
  1. Načti daily_summaries za posledních 7 dní
  2. Claude API call: týdenní souhrn, extrakce dlouhodobých zájmů
  3. Ulož memory_summary (period_type='weekly')
```

### 5.3 Claude API Volání

```python
# Model: claude-sonnet-4-20250514
# Max tokens: 1000 pro summaries, 500 pro topic extraction

ACTIVITY_SUMMARY_PROMPT = """
Analyze this user activity data and provide a concise summary.

Activity window: {period_start} to {period_end} ({duration_minutes} minutes active)

Events:
{events_formatted}

Provide:
1. A 2-3 sentence summary of what the user was doing
2. Key topics/interests identified (max 10 tags)
3. Primary task or goal if identifiable
4. Content type classification: reading|writing|coding|browsing|communication|other

Respond in JSON:
{
  "summary": "...",
  "tags": ["tag1", "tag2"],
  "primary_task": "...",
  "content_type": "..."
}
"""
```

---

## 6. MCP Server (Python – Mac Mini)

### 6.1 Struktura

```
mcp-server/
├── main.py                # MCP server entry point
├── requirements.txt       # mcp, asyncpg, pgvector
├── tools/
│   ├── get_recent_context.py
│   ├── search_memory.py
│   ├── get_current_context.py
│   ├── get_user_interests.py
│   └── get_work_summary.py
└── db/
    └── queries.py         # Async pgvector queries
```

### 6.2 MCP Tools Definice

```python
# Tool: get_recent_context
# Vstup: minutes: int (default 30, max 480)
# Výstup: Sumarizace aktivity za posledních N minut
# Query: memory_entries WHERE period_start > now()-interval ORDER BY period_start DESC

# Tool: search_memory  
# Vstup: query: str, limit: int (default 10), date_from: str (optional)
# Výstup: Sémanticky relevantní memory_entries
# Query: pgvector cosine similarity search na embedding sloupci

# Tool: get_current_context
# Vstup: žádný
# Výstup: Poslední 2 raw_events z každého zařízení (co uživatel PRÁVĚ dělá)
# Query: raw_events ORDER BY received_at DESC LIMIT 2 PER device

# Tool: get_user_interests
# Vstup: days: int (default 30)
# Výstup: Agregovaná témata z posledních N dní
# Query: SELECT unnest(tags), count(*) FROM memory_entries GROUP BY 1 ORDER BY 2 DESC

# Tool: get_work_summary
# Vstup: date: str (YYYY-MM-DD, default today)
# Výstup: memory_summaries WHERE period_type='daily' AND period_date=date
```

### 6.3 MCP Server Konfigurace

MCP server běží jako lokální Unix socket nebo HTTP na localhost:
```
Transport: stdio (pro Claude Desktop integration)
           NEBO HTTP na localhost:8765 (pro ostatní klienty)
```

---

## 7. Deployment (Mac Mini Server)

### 7.1 Adresářová Struktura Serveru

```
/opt/user-memory/
├── ingest-api/
├── ai-pipeline/
├── mcp-server/
├── certs/                 # Self-signed TLS certifikáty
│   ├── server.crt
│   └── server.key
├── .env                   # Secrets (DB password, Anthropic API key)
└── docker-compose.yml
```

### 7.2 docker-compose.yml (Server)

```yaml
version: '3.9'
services:
  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_DB: usermemory
      POSTGRES_USER: usermemory
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "127.0.0.1:5432:5432"   # Pouze localhost

  ingest-api:
    build: ./ingest-api
    environment:
      DATABASE_URL: postgresql+asyncpg://usermemory:${DB_PASSWORD}@postgres/usermemory
    ports:
      - "0.0.0.0:8443:8443"     # Dostupné v lokální síti, TLS
    depends_on:
      - postgres

  ai-pipeline:
    build: ./ai-pipeline
    environment:
      DATABASE_URL: postgresql+asyncpg://usermemory:${DB_PASSWORD}@postgres/usermemory
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
    depends_on:
      - postgres
      - ingest-api

  mcp-server:
    build: ./mcp-server
    environment:
      DATABASE_URL: postgresql+asyncpg://usermemory:${DB_PASSWORD}@postgres/usermemory
    ports:
      - "127.0.0.1:8765:8765"   # Pouze localhost (MCP klienti na stejném stroji)
    depends_on:
      - postgres

volumes:
  postgres_data:
```

---

## 8. Bezpečnost

| Oblast | Opatření |
|---|---|
| Transport | TLS 1.3, self-signed cert, TOFU pinning na agentovi |
| Auth | Per-device API key (generovaný serverem), bcrypt hash v DB |
| Local buffer | SQLite bez šifrování v základu; SQLCipher jako volitelné rozšíření |
| Server přístup | Firewall: port 8443 pouze z lokální sítě (192.168.x.x/24) |
| MCP server | Pouze localhost, žádný síťový přístup |
| Data retention | Raw events: 30 dní; Memory entries: bez limitu (konfigurovatelné) |
| Hesla | Nikdy nezapsána do SQLite ani odeslána na server |
| Logy | Agent loguje pouze metadata eventů, nikdy obsah (text, clipboard) |

---

## 9. Sekvence Implementace (pro Claude Code)

Doporučené pořadí implementace:

```
Fáze 1 – Základ (agent + server příjem)
  1. collector-agent: config, buffer (SQLite), transport, základní event schema
  2. collector-agent: AppWindow watcher (macOS) + IdleDetection
  3. ingest-api: POST /events endpoint, PostgreSQL raw_events tabulka
  4. Test: agent → server pipeline

Fáze 2 – Sběr dat
  5. collector-agent: Keystroke aggregator + password heuristics
  6. collector-agent: Clipboard watcher
  7. collector-agent: File system watcher + content extractor (text + PDF)
  8. collector-agent: AppWindow watcher (Windows)
  9. browser-extension: základní verze (URL + dwell time + Readability)
  10. collector-agent: extension server (localhost:45678)

Fáze 3 – AI zpracování
  11. ai-pipeline: process_batch task
  12. ai-pipeline: daily_consolidation
  13. PostgreSQL: memory_entries tabulka + pgvector

Fáze 4 – MCP
  14. mcp-server: všechny tools
  15. Integrace test s Claude Desktop

Fáze 5 – Windows + Hardening
  16. collector-agent: Windows-specific watchers
  17. Bezpečnostní hardening, TLS certifikáty, API key management
  18. Instalační scripty (macOS LaunchAgent, Windows Service)
```

---

## 10. Otevřené Závislosti (musí být k dispozici)

| Závislost | Účel | Instalace |
|---|---|---|
| Go 1.22+ | Build agenta | brew install go |
| pdftotext (poppler) | PDF extrakce na klientovi | brew install poppler / winget install poppler |
| PostgreSQL 16 + pgvector | Server DB | Docker |
| Anthropic API key | AI pipeline | claude.ai/settings |
| Python 3.12+ | Server komponenty | brew install python |
| Node.js 20+ | Browser extension build | brew install node |

---

*Tento dokument je kompletní specifikací pro implementaci. Každá sekce odpovídá jednomu modulu nebo komponentě. Implementace by měla probíhat v pořadí definovaném v Sekci 9.*
