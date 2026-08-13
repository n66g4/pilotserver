# PilotServer Unified Admin Console and i18n Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fragmented admin page with the approved unified-console layout and provide complete Simplified Chinese / English switching without changing backend APIs or resetting replay state.

**Architecture:** Keep the existing native HTML/CSS/JavaScript application. Add one focused `i18n.js` module for locale selection, dictionaries, interpolation, and subscriptions; retain the existing device/route/replay request code while mounting it inside a persistent shell with overview, devices, routes, settings, and replay views. Pass the same translator into `telemetry.js` so every static and dynamic message uses one language source.

**Tech Stack:** Embedded HTML/CSS/JavaScript, Node `node:test` + `vm`, Go `embed` + `httptest`, hls.js, existing canvas telemetry renderer.

## Global Constraints

- Do not change existing HTTP API paths, request bodies, response structures, authentication, HLS tickets, or telemetry data.
- Do not add a frontend framework, package manager, external font, icon CDN, or runtime dependency.
- First visit follows `navigator.languages` / `navigator.language`; a saved `pilotserver_admin_language` value wins thereafter.
- Supported locales are exactly `zh-CN` and `en`.
- Language switching must not log out, issue a new media ticket, reload the video source, change `video.currentTime`, change replay mode, or change the selected segment.
- Overview may request settings, devices, and each device's route list; it must not request replay summaries or telemetry.
- Keep all existing replay generation guards, cleanup, native-HLS/hls.js behavior, fragment PTS mapping, map fallback, and keyboard chart behavior.
- Desktop uses a left navigation rail; below 760px it becomes a bottom navigation bar.
- Every interactive target remains at least 40px.
- Run `go build -o bin/pilotserver ./cmd/pilotserver` after every production-code edit.
- Do not create a Git commit unless the user separately requests one.
- Release artifact version is `1.0.19-1`.

---

## File Map

- Create `web/admin/i18n.js`: locale detection, persistence, translation dictionaries, interpolation, subscriptions.
- Create `web/admin/i18n_test.js`: deterministic Node tests for locale selection, persistence, dictionary parity, fallback, and interpolation.
- Modify `web/admin/admin.go`: embed and serve `i18n.js` with the same non-immutable policy as `telemetry.js`.
- Modify `web/admin/admin_test.go`: asset, shell, accessibility, and no-untranslated-literal assertions.
- Modify `web/admin/index.html`: approved shell, views, styles, app state, navigation, localized static/dynamic rendering.
- Modify `web/admin/telemetry.js`: consume the shared translator for HUD, map, chart, SDK, and telemetry-load messages.
- Modify `web/admin/telemetry_test.js`: assert both locales and state-preserving translator replacement.
- Modify `synology/build-spk.sh`, `README.md`, `docs/synology-dsm72-spk.md`: release `1.0.19-1`.
- Create `.superpowers/sdd/admin-console-i18n-report.md`: verification record, browser matrix, artifacts, and hashes.

---

### Task 1: Add the Shared i18n Runtime

**Files:**
- Create: `web/admin/i18n.js`
- Create: `web/admin/i18n_test.js`
- Modify: `web/admin/admin.go`
- Modify: `web/admin/admin_test.go`

**Interfaces:**
- Produces: `window.PilotI18n.create(options?)`
- `options.storage`: Storage-compatible `{getItem, setItem}`
- `options.languages`: browser language list
- Returns: `{language(), setLanguage(language), t(key, params?), subscribe(listener), dictionary()}`
- Locale storage key: `pilotserver_admin_language`

- [ ] **Step 1: Write failing locale and dictionary tests**

Create `web/admin/i18n_test.js` using `node:test`, `assert`, and `vm`. Load `i18n.js` into `{window:{}}` and assert:

```js
test("saved language wins and browser language is the first-visit fallback", () => {
  const storage = memoryStorage();
  let i18n = PilotI18n.create({storage, languages: ["en-US", "zh-CN"]});
  assert.strictEqual(i18n.language(), "en");
  i18n.setLanguage("zh-CN");
  assert.strictEqual(storage.getItem("pilotserver_admin_language"), "zh-CN");
  i18n = PilotI18n.create({storage, languages: ["en-US"]});
  assert.strictEqual(i18n.language(), "zh-CN");
});

test("dictionaries have identical keys and interpolate named values", () => {
  const zh = PilotI18n.create({storage: memoryStorage(), languages: ["zh"]});
  const en = PilotI18n.create({storage: memoryStorage(), languages: ["en"]});
  assert.deepStrictEqual(Object.keys(zh.dictionary()).sort(), Object.keys(en.dictionary()).sort());
  assert.strictEqual(zh.t("telemetry.failedSegments", {count: 2, segments: "1, 4"}),
    "2 个分段遥测损坏（分段 1, 4），其他分段仍可使用");
  assert.strictEqual(en.t("telemetry.failedSegments", {count: 2, segments: "1, 4"}),
    "Telemetry is damaged in 2 segments (1, 4); other segments remain available");
});
```

The dictionaries must cover these exact namespaces:

```text
common.* login.* nav.* topbar.* overview.* devices.* routes.* settings.*
replay.* telemetry.* map.* errors.* status.*
```

Required semantic distinctions include `status.online`, `status.offline`, `status.loading`,
`telemetry.none`, `telemetry.noGPS`, `telemetry.failedSegments`, `replay.playerReady`,
`replay.fragmentFailed`, and every map SDK failure/fallback message currently in `telemetry.js`.

- [ ] **Step 2: Run tests and observe RED**

Run:

```bash
node --test web/admin/i18n_test.js
```

Expected: FAIL because `web/admin/i18n.js` / `window.PilotI18n` does not exist.

- [ ] **Step 3: Implement the minimal i18n module**

Implement an IIFE with:

```js
const supported = new Set(["zh-CN", "en"]);
const storageKey = "pilotserver_admin_language";

function normalize(language) {
  return String(language || "").toLowerCase().startsWith("zh") ? "zh-CN" : "en";
}

function format(template, params = {}) {
  return template.replace(/\{([a-zA-Z0-9_]+)\}/g,
    (_, key) => Object.hasOwn(params, key) ? String(params[key]) : `{${key}}`);
}
```

`setLanguage()` accepts only the two supported values, persists the value, and notifies a copied listener list. Missing keys fall back to English, then to the key itself. `dictionary()` returns a shallow copy so tests and callers cannot mutate the source dictionary.

- [ ] **Step 4: Embed and serve the asset**

Add `i18n.js` to `web/admin/admin.go`'s embed declaration and route switch. Serve:

```text
Content-Type: text/javascript; charset=utf-8
Cache-Control: no-cache
```

Update `TestServeHTTPEmbeddedAssets` and `TestServeHTTPCachePolicy` for `/admin/i18n.js`.

- [ ] **Step 5: Verify GREEN and compile**

Run:

```bash
node --test web/admin/i18n_test.js
go test -count=1 ./web/admin
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: all PASS and build exits 0.

---

### Task 2: Build the Persistent Unified Console Shell

**Files:**
- Modify: `web/admin/index.html`
- Modify: `web/admin/admin_test.go`

**Interfaces:**
- Consumes: `window.PilotI18n.create()`
- Produces: `setView("overview"|"devices"|"routes"|"settings"|"replay")`
- Produces state:

```js
const appState = {
  view: "overview",
  devices: [],
  routesByDevice: new Map(),
  selectedDongleID: "",
  selectedRoute: "",
  routeRequestGeneration: 0
};
```

- [ ] **Step 1: Write failing shell structure tests**

Extend `web/admin/admin_test.go` with assertions that embedded HTML contains:

```text
src="/admin/i18n.js"
id="app-shell"
id="primary-nav"
data-view="overview"
data-view="devices"
data-view="routes"
data-view="settings"
id="view-overview"
id="view-devices"
id="view-routes"
id="view-settings"
id="replay-view"
id="language-toggle"
aria-current="page"
@media (max-width: 760px)
prefers-reduced-motion
```

Also assert the existing replay IDs and hooks from current tests remain present.

- [ ] **Step 2: Run shell tests and observe RED**

Run:

```bash
go test -count=1 ./web/admin -run 'TestAdminHTMLContainsUnifiedShell|TestAdminHTMLContainsReplayHooks'
```

Expected: unified-shell test FAILS while replay-hook test remains PASS.

- [ ] **Step 3: Replace the fragmented page structure with the approved shell**

Keep the login card outside the authenticated shell. Inside `#app-shell`, create:

```html
<aside id="primary-nav">
  <div class="brand">PILOT<span>/SERVER</span></div>
  <nav aria-label="Primary">
    <button data-view="overview" aria-current="page"></button>
    <button data-view="devices"></button>
    <button data-view="routes"></button>
    <button data-view="settings"></button>
  </nav>
</aside>
<div class="console">
  <header class="topbar">
    <div id="service-indicator"></div>
    <div id="online-device-count"></div>
    <button id="language-toggle" type="button"></button>
    <button id="logout" type="button"></button>
  </header>
  <main id="workspace">
    <section id="view-overview"></section>
    <section id="view-devices" hidden></section>
    <section id="view-routes" hidden></section>
    <section id="view-settings" hidden></section>
    <section id="replay-view" hidden></section>
  </main>
</div>
```

Move existing settings controls unchanged into `#view-settings`; move device list into `#view-devices`; move route browser/file list/replay entry into `#view-routes`; preserve every existing form-control ID and replay ID.

- [ ] **Step 4: Add the visual system and responsive layout**

Use CSS variables for warm paper, ink, muted, line, cyan, warning, and danger. Implement:

- 224px desktop rail, sticky topbar, maximum 1440px workspace.
- restrained panel borders and shadows; no purple gradient.
- status pills with text plus dot/icon.
- device rows as cards below 760px.
- fixed bottom navigation below 760px with safe-area padding.
- replay video-first grid and one-column mobile telemetry.
- `:focus-visible`, 40px controls, and reduced-motion override.

- [ ] **Step 5: Implement navigation without breaking lifecycle**

`setView()` must:

1. Increment only the request generations whose view is being abandoned.
2. Hide all workspace views and set the chosen view visible.
3. Update each nav button's `aria-current`.
4. Call `leaveReplay()` only when leaving replay, not during language changes.
5. Focus the chosen view's `h1[tabindex="-1"]` for keyboard navigation.

The Routes nav is disabled until a device is selected; selecting “查看行程 / View routes” sets `selectedDongleID` and opens Routes.

- [ ] **Step 6: Build a lightweight overview**

Reuse loaded settings and devices. Fetch each device's `/routes` once through a generation-guarded `Promise.allSettled`, cache successful route arrays in `routesByDevice`, and render:

- online devices / total devices
- total cached route count
- LAN enabled/disabled
- configured map provider
- up to five newest routes with device context

Do not call `/replay`, `/telemetry`, or media-ticket endpoints from overview.

- [ ] **Step 7: Verify shell behavior and compile**

Run:

```bash
go test -count=1 ./web/admin
node --test web/admin/telemetry_test.js web/admin/i18n_test.js
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: PASS.

---

### Task 3: Localize the Admin Workflow and Preserve State

**Files:**
- Modify: `web/admin/index.html`
- Modify: `web/admin/admin_test.go`
- Modify: `web/admin/i18n_test.js`

**Interfaces:**
- Consumes: shared `i18n`
- Produces: `applyLanguage({preserveFocus?: boolean})`
- Produces: `localizedError(error, fallbackKey)`

- [ ] **Step 1: Write failing language-switch and completeness tests**

Add tests that assert:

- dictionary key parity
- `login.title`, every nav label, settings labels/placeholders, device actions, route states, replay controls, and generic errors exist in both dictionaries
- `index.html` references `data-i18n`, `data-i18n-placeholder`, and `data-i18n-aria-label`
- `index.html` contains `i18n.subscribe(applyLanguage)`
- `applyLanguage` does not call `clearReplaySource`, `loadReplaySource`, or `leaveReplay`
- outside inline dictionary-free code, no user-facing Han literals remain in `index.html`; allow technical comments and the locale name “中文”

Use a focused helper in the Go test to strip the embedded `<script src>` tags and assert required localization hooks rather than snapshotting all HTML.

- [ ] **Step 2: Run tests and observe RED**

Run:

```bash
node --test web/admin/i18n_test.js
go test -count=1 ./web/admin -run 'I18n|Localized|Unified'
```

Expected: FAIL on missing keys/hooks and remaining literal text.

- [ ] **Step 3: Localize static nodes**

Add keys to headings, labels, buttons, options, placeholders, `aria-label`, helper text, empty states, and the login page. `applyLanguage()` must:

```js
document.documentElement.lang = i18n.language();
document.querySelectorAll("[data-i18n]").forEach(
  element => { element.textContent = i18n.t(element.dataset.i18n); });
document.querySelectorAll("[data-i18n-placeholder]").forEach(
  element => { element.placeholder = i18n.t(element.dataset.i18nPlaceholder); });
document.querySelectorAll("[data-i18n-aria-label]").forEach(
  element => element.setAttribute("aria-label", i18n.t(element.dataset.i18nAriaLabel)));
renderShell();
renderCurrentView();
```

Language button toggles `zh-CN` / `en`. Its visible label announces the destination language: `English` in Chinese mode and `中文` in English mode.

- [ ] **Step 4: Localize dynamic device, route, settings, and replay text**

Replace literal strings in:

```text
loadSettings, loadDevices, renderDevice, loadRoutes, loadSegments,
openReplay, loadReplaySource, showReplayError, renderSegmentStrip,
login, save settings, SSH response, HLS fatal-error handling
```

Do not translate IDs, environment variable names, URLs, route paths, numeric values, `km/h`, `HLS`, `SSH`, `GPS`, `AMap`, or `Tencent`.

For partial telemetry failures, build the segment list and call:

```js
i18n.t("telemetry.failedSegments", {
  count: failedSegments.length,
  segments: failedSegments.join(", ")
});
```

- [ ] **Step 5: Preserve asynchronous and replay state during translation**

Before and after each language switch assert in browser-oriented code that these values are unchanged:

```js
const replaySnapshot = {
  time: replayVideo.currentTime,
  mode: replayMode.value,
  segment: replaySegment.value,
  source: replayVideo.currentSrc || replayVideo.src,
  requestGeneration: replayRequestGeneration
};
```

`applyLanguage()` may rerender labels and current cards, but must not increment `replayRequestGeneration`, fetch a ticket, assign `video.src`, destroy `hls`, or call telemetry cleanup.

- [ ] **Step 6: Verify localization and compile**

Run:

```bash
node --test web/admin/i18n_test.js
go test -count=1 ./web/admin
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: PASS.

---

### Task 4: Localize Telemetry, Map, and Canvas Accessibility

**Files:**
- Modify: `web/admin/telemetry.js`
- Modify: `web/admin/telemetry_test.js`
- Modify: `web/admin/index.html`
- Modify: `web/admin/admin_test.go`

**Interfaces:**
- Extend `PilotTelemetry.create(options)` with `options.translate`
- `options.translate(key, params?) -> string`
- Add instance method `setTranslate(translate)` to redraw text without clearing data or fragment mapping

- [ ] **Step 1: Write failing telemetry i18n tests**

Extend fake elements and create two translators:

```js
const zh = (key) => ({
  "telemetry.noData": "无数据",
  "telemetry.noAlert": "无告警",
  "telemetry.noGPS": "无 GPS 轨迹"
})[key] || key;
const en = (key) => ({
  "telemetry.noData": "No data",
  "telemetry.noAlert": "No alert",
  "telemetry.noGPS": "No GPS trace"
})[key] || key;
```

Assert initial Chinese text, call `setTranslate(en)`, and assert:

- HUD/map/chart labels change
- loaded speed/GPS/control arrays remain
- current cursor remains
- fragment timing map remains
- provider map is not reconstructed solely for a language change

- [ ] **Step 2: Run telemetry test and observe RED**

Run:

```bash
node --test web/admin/telemetry_test.js
```

Expected: FAIL because `translate` and `setTranslate` are unsupported.

- [ ] **Step 3: Replace telemetry literals with translation keys**

Add a default translator returning the existing English-safe fallback. Replace every user-visible string, including:

```text
refresh-map-key, SDK load failed/timeout, waiting/loading/no data,
partial segment failure, no GPS, fallback trace, provider names,
runtime map failure, no alert, chart aria-valuetext
```

Keep internal error codes unchanged. Store current translator in the instance closure.

- [ ] **Step 4: Implement state-preserving `setTranslate`**

`setTranslate(next)` updates the function, refreshes status/HUD/canvas text and accessibility attributes, and calls provider-map status rendering only. It must not:

```text
increment telemetryGeneration, clear data, refetch telemetry,
destroy/recreate providerMap, clear fragment timing, change video time
```

Wire `i18n.subscribe()` in `index.html` to:

```js
window.PilotTelemetry.active?.setTranslate(i18n.t);
```

Pass `translate: i18n.t` when creating telemetry.

- [ ] **Step 5: Verify telemetry localization and compile**

Run:

```bash
node --test web/admin/telemetry_test.js web/admin/i18n_test.js
go test -count=1 ./web/admin
go build -o bin/pilotserver ./cmd/pilotserver
```

Expected: PASS.

---

### Task 5: Browser Acceptance, Release Build, and Documentation

**Files:**
- Modify: `synology/build-spk.sh`
- Modify: `README.md`
- Modify: `docs/synology-dsm72-spk.md`
- Create: `.superpowers/sdd/admin-console-i18n-report.md`

**Interfaces:**
- Produces: `bin/pilotserver`
- Produces: `dist/pilotserver-1.0.19-1-x64.spk`

- [ ] **Step 1: Run the local server with isolated test data**

Use a temporary data directory and non-production port. Seed or reuse deterministic device/route fixtures that cover:

- online and offline device
- complete route
- partial telemetry failure
- no GPS
- map disabled

Record exact startup and fixture commands in the report; never include production admin passwords or JWTs.

- [ ] **Step 2: Run desktop browser acceptance in both languages**

Verify with browser automation:

1. login
2. overview cards and recent routes
3. devices refresh and SSH button presence
4. settings save validation
5. route selection and files
6. replay opens inside the shell
7. partial telemetry text names failed segments without hiding valid data
8. switch Chinese → English while video is at a non-zero time
9. assert time, source, mode, segment, telemetry cursor, and fragment map are unchanged
10. logout and 401 cleanup preserve saved language

- [ ] **Step 3: Run mobile acceptance**

At 390×844 and 430×932:

- bottom navigation is visible and rail is hidden
- no horizontal page overflow
- all primary controls are at least 40px
- settings and telemetry are single-column
- iPhone-style safe-area padding is present
- chart keyboard behavior remains testable at desktop size

- [ ] **Step 4: Update release version and docs**

Set:

```bash
VERSION="${PILOTSERVER_SPK_VERSION:-1.0.19-1}"
```

Update README and DSM guide artifact names and explain:

- unified Overview / Devices / Routes / Settings console
- Chinese/English switching and persistence
- no API/database migration

- [ ] **Step 5: Run final verification**

Run:

```bash
node --test web/admin/i18n_test.js web/admin/telemetry_test.js
go test -count=1 ./...
go test -race -count=1 ./internal/replay ./internal/adminapi ./internal/store ./internal/upload ./web/admin
go vet ./...
go build -o bin/pilotserver ./cmd/pilotserver
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/pilotserver-linux-amd64 ./cmd/pilotserver
rm -f /tmp/pilotserver-linux-amd64
./synology/build-spk.sh
git diff --check
shasum -a 256 bin/pilotserver dist/pilotserver-1.0.19-1-x64.spk
```

Expected: all tests/builds exit 0, no diff-check output, and both hashes are recorded.

- [ ] **Step 6: Inspect the SPK**

Extract to a temporary directory and confirm:

```text
INFO version="1.0.19-1"
package.tgz contains bin/pilotserver
binary is Linux x86-64
binary serves /admin/i18n.js
no .DS_Store or ._* entries
```

- [ ] **Step 7: Complete the verification report**

Write `.superpowers/sdd/admin-console-i18n-report.md` with:

```text
Status, design/spec path, RED/GREEN evidence per task, automated test output,
desktop/mobile browser assertions, state-preservation evidence,
SPK inspection, artifact hashes, changed files, cleanup, concerns.
```

Do not claim completion unless every listed command and browser assertion has fresh evidence.
