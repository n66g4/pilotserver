package admin

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestServeHTTPEmbeddedAssets(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
		body        string
	}{
		{path: "/admin/", contentType: "text/html; charset=utf-8", body: "<title>Pilotserver Admin</title>"},
		{path: "/admin/index.html", contentType: "text/html; charset=utf-8", body: "<title>Pilotserver Admin</title>"},
		{path: "/admin/telemetry.js", contentType: "text/javascript; charset=utf-8", body: "window.PilotTelemetry"},
		{path: "/admin/i18n.js", contentType: "text/javascript; charset=utf-8", body: "window.PilotI18n"},
		{path: "/admin/vendor/hls.min.js", contentType: "text/javascript; charset=utf-8", body: "Hls"},
		{path: "/admin/vendor/hls.js.LICENSE.txt", contentType: "text/plain; charset=utf-8", body: "Apache License"},
		{path: "/admin/vendor/xterm.min.js", contentType: "text/javascript; charset=utf-8", body: "Terminal"},
		{path: "/admin/vendor/xterm.css", contentType: "text/css; charset=utf-8", body: ".xterm"},
		{path: "/admin/vendor/xterm.LICENSE.txt", contentType: "text/plain; charset=utf-8", body: "Permission is hereby granted"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.contentType)
			}
			if !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("body does not contain %q", tt.body)
			}
		})
	}
}

func TestServeHTTPUnknownAssetReturnsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/missing.js", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestServeHTTPEmbeddedCerealLicenses(t *testing.T) {
	tests := []struct {
		path string
		hash string
	}{
		{
			path: "/admin/licenses/dragonpilot.txt",
			hash: "d2c0b49249de153c87a29eff48c99149466b13c9db30b7cafa0c57a7c5524f98",
		},
		{
			path: "/admin/licenses/openpilot.txt",
			hash: "716ce815a0467219c59ec2433e6bce7f32efc45240725c6d3141a52b111d2558",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/plain", got)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(rec.Body.Bytes())); got != tt.hash {
				t.Errorf("body SHA-256 = %s, want %s", got, tt.hash)
			}
		})
	}
}

func TestServeHTTPCachePolicy(t *testing.T) {
	for _, path := range []string{
		"/admin/vendor/hls.min.js",
		"/admin/vendor/hls.js.LICENSE.txt",
		"/admin/vendor/xterm.min.js",
		"/admin/vendor/xterm.css",
		"/admin/vendor/xterm.LICENSE.txt",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
				t.Errorf("Cache-Control = %q, want immutable vendor cache", got)
			}
		})
	}

	for _, path := range []string{"/admin/telemetry.js", "/admin/i18n.js"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
				t.Errorf("Cache-Control = %q, want %q", got, "no-cache")
			}
		})
	}

	for _, path := range []string{"/admin/", "/admin/index.html"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
				t.Errorf("Cache-Control = %q, must not be immutable", rec.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestTelemetryAssetContainsRequiredSafetyAndSynchronizationHooks(t *testing.T) {
	js := string(telemetryJS)
	for _, want := range []string{
		`window.PilotTelemetry`,
		`telemetryGeneration`,
		`playableOffset`,
		`/telemetry`,
		`latestSample`,
		`video.currentTime`,
		`has_video`,
		`wgs84ToGcj02`,
		`outOfChina`,
		`https://webapi.amap.com/maps`,
		`https://map.qq.com/api/gljs`,
		`_AMapSecurityConfig`,
		`setTimeout`,
		`onerror`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("telemetry asset does not contain %q", want)
		}
	}
	for _, forbidden := range []string{`innerHTML`, `eval(`, `cdn.jsdelivr`, `unpkg.com`} {
		if strings.Contains(js, forbidden) {
			t.Errorf("telemetry asset contains forbidden %q", forbidden)
		}
	}
}

func TestAdminHTMLContainsMapAndTelemetryHooks(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`id="map-provider"`,
		`id="map-web-key"`,
		`id="map-security-code"`,
		`src="/admin/telemetry.js"`,
		`id="telemetry-speed"`,
		`id="telemetry-state"`,
		`id="telemetry-enabled"`,
		`id="telemetry-active"`,
		`id="telemetry-alert"`,
		`id="telemetry-map"`,
		`id="telemetry-map-status"`,
		`id="telemetry-chart" tabindex="0" role="slider"`,
		`aria-valuemin="0"`,
		`aria-valuemax="0"`,
		`aria-valuenow="0"`,
		`id="telemetry-chart-status"`,
		`#telemetry-chart:focus-visible`,
		`min-height: 40px`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain %q", want)
		}
	}
}

func TestAdminHTMLMakesCheckboxTouchTargetAtLeast40Pixels(t *testing.T) {
	if !strings.Contains(string(indexHTML), `input[type="checkbox"] { min-width: 40px; }`) {
		t.Fatal("admin HTML does not give checkboxes a 40px minimum width")
	}
}

func TestAdminHTMLMakesFileLinksTouchTargetsAtLeast40Pixels(t *testing.T) {
	if !strings.Contains(string(indexHTML), `#file-list a { display: inline-flex; min-height: 40px; align-items: center; }`) {
		t.Fatal("admin HTML does not give file links a 40px minimum height")
	}
}

func TestTelemetryAssetContainsReviewSafetyHooks(t *testing.T) {
	js := string(telemetryJS)
	for _, want := range []string{
		`pendingSDKConfig`,
		`buildStaticLayer`,
		`drawImage`,
		`data.maxSpeed`,
		`downsampleGPS`,
		`downsampleSpeeds`,
		`segmentStart`,
		`segmentEnd`,
		`onSegmentLoadState`,
		`runtime_load_failed`,
		`ensureCanvasSize`,
		`if (metrics.changed)`,
		`event.key === "ArrowLeft"`,
		`event.key === "ArrowRight"`,
		`event.key === "Home"`,
		`event.key === "End"`,
		`aria-valuenow`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("telemetry asset does not contain review hook %q", want)
		}
	}
	if strings.Contains(js, `Math.max(1, ...`) || strings.Contains(js, `Math.max(...`) {
		t.Error("telemetry asset must not spread arrays into Math.max")
	}
	if strings.Contains(js, `overview.telemetry_error = "telemetry_load_failed"`) {
		t.Error("runtime load failures must not overwrite server telemetry_error")
	}
	if !strings.Contains(string(indexHTML), `runtimeTelemetryErrors`) {
		t.Error("admin HTML must keep runtime telemetry failures separate from summary errors")
	}
	if strings.Contains(string(indexHTML), `segment.telemetry_error = errorCode`) {
		t.Error("admin HTML must not overwrite summary telemetry_error")
	}
}

func TestAdminHTMLContainsUnifiedShell(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`src="/admin/i18n.js"`,
		`id="app-shell"`,
		`id="primary-nav"`,
		`data-view="overview"`,
		`data-view="devices"`,
		`data-view="routes"`,
		`data-view="settings"`,
		`id="view-overview"`,
		`id="view-devices"`,
		`id="view-routes"`,
		`id="view-settings"`,
		`id="upload-max-gb"`,
		`id="ssh-audit-list"`,
		`api("/admin/api/ssh-audit")`,
		`id="replay-view"`,
		`id="language-toggle"`,
		`aria-current="page"`,
		`@media (max-width: 760px)`,
		`prefers-reduced-motion`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain unified shell hook %q", want)
		}
	}
}

func TestAdminHTMLErrorLiveRegionsStayWithTheirWorkspaces(t *testing.T) {
	html := string(indexHTML)
	login := strings.Index(html, `id="login"`)
	message := strings.Index(html, `id="message" aria-live="polite"`)
	appShell := strings.Index(html, `id="app-shell"`)
	workspace := strings.Index(html, `id="workspace"`)
	workspaceStatus := strings.Index(html, `id="workspace-status" aria-live="polite"`)
	workspaceEnd := strings.Index(html[workspace:], `</main>`)
	if login < 0 || message <= login || appShell <= message {
		t.Fatal("login message must appear after #login and before #app-shell")
	}
	if workspace < 0 || workspaceStatus <= workspace || workspaceEnd < 0 ||
		workspaceStatus >= workspace+workspaceEnd {
		t.Fatal("workspace status must be a descendant of #workspace")
	}
}

func TestAdminHTMLErrorPathsUseLocalLiveRegions(t *testing.T) {
	html := string(indexHTML)
	saveSettings := adminFunction(t, html,
		`document.querySelector("#save-settings").addEventListener`,
		`document.querySelector("#allow-lan").addEventListener`)
	if !strings.Contains(saveSettings,
		`setLocalizedError(document.querySelector("#settings-status"), error, "errors.saveFailed");`) {
		t.Error("save settings failures must use #settings-status")
	}
	if strings.Contains(saveSettings, `setLocalizedError(message, error, "errors.saveFailed")`) {
		t.Error("save settings failures must not use the login message")
	}

	for _, want := range []string{
		`const workspaceStatus = document.querySelector("#workspace-status");`,
		`setLocalizedError(workspaceStatus, error, "errors.loadFailed");`,
		`setLocalizedError(workspaceStatus, error, "errors.sshFailed");`,
		`setLocalizedError(workspaceStatus, error, "routes.loadFailed");`,
		`setLocalizedError(workspaceStatus, error, "routes.downloadFailed");`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not route logged-in failure through workspace status %q", want)
		}
	}
	for _, forbidden := range []string{
		`setLocalizedError(message, error, "errors.loadFailed");`,
		`setLocalizedError(message, error, "errors.sshFailed");`,
		`setLocalizedError(message, error, "routes.loadFailed");`,
		`setLocalizedError(message, error, "routes.downloadFailed");`,
	} {
		if strings.Contains(html, forbidden) {
			t.Errorf("admin HTML routes logged-in failure through login message %q", forbidden)
		}
	}
}

func TestAdminHTMLServiceIndicatorReflectsAPIReachability(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`id="service-indicator" class="status-pill" data-on="false" data-i18n="topbar.serviceError"`,
		`#service-indicator[data-on="false"]::before`,
		`function setServiceStatus(ok)`,
		`serviceIndicator.dataset.on = String(ok);`,
		`setLocalizedText(serviceIndicator, ok ? "topbar.serviceOk" : "topbar.serviceError");`,
		`setServiceStatus(false);`,
		`setServiceStatus(response.status < 500);`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain service reachability hook %q", want)
		}
	}
}

func adminInlineHTML(t *testing.T) string {
	t.Helper()
	externalScript := regexp.MustCompile(`<script\s+src="[^"]+"></script>`)
	return externalScript.ReplaceAllString(string(indexHTML), "")
}

func adminFunction(t *testing.T, html, start, end string) string {
	t.Helper()
	startIndex := strings.Index(html, start)
	if startIndex < 0 {
		t.Fatalf("admin HTML does not contain bounded %s", start)
	}
	endIndex := strings.Index(html[startIndex+len(start):], end)
	if endIndex < 0 {
		t.Fatalf("admin HTML does not contain end boundary for %s", start)
	}
	return html[startIndex : startIndex+len(start)+endIndex]
}

func TestAdminHTMLContainsCompleteI18nHooks(t *testing.T) {
	html := adminInlineHTML(t)
	for _, want := range []string{
		`data-i18n="login.title"`,
		`data-i18n-placeholder="login.passwordPlaceholder"`,
		`data-i18n-aria-label="nav.primaryAria"`,
		`languages: [...(navigator.languages || []), navigator.language]`,
		`window.PilotI18n.render(document, i18n.t)`,
		`i18n.subscribe(applyLanguage)`,
		`function localizedError(error, fallbackKey)`,
		`i18n.t("telemetry.failedSegments", {`,
		`segments: failedSegments.join(", ")`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain i18n hook %q", want)
		}
	}
	withoutLocaleName := strings.ReplaceAll(html, "中文", "")
	if match := regexp.MustCompile(`[\p{Han}]`).FindString(withoutLocaleName); match != "" {
		t.Errorf("admin HTML contains untranslated Han literal %q", match)
	}
}

func TestAdminHTMLLanguageSwitchPreservesReplayState(t *testing.T) {
	html := adminInlineHTML(t)
	applyLanguage := adminFunction(t, html, "function applyLanguage(", "\n    async function api")
	for _, want := range []string{
		`time: replayVideo.currentTime`,
		`mode: replayMode.value`,
		`segment: replaySegment.value`,
		`source: replayVideo.currentSrc || replayVideo.src`,
		`requestGeneration: replayRequestGeneration`,
		`renderShell();`,
		`renderCurrentView();`,
	} {
		if !strings.Contains(applyLanguage, want) {
			t.Errorf("applyLanguage does not contain replay-preservation hook %q", want)
		}
	}
	for _, forbidden := range []string{
		`clearReplaySource`,
		`loadReplaySource`,
		`leaveReplay`,
		`telemetry.cleanup`,
		`replayVideo.src =`,
		`hlsInstance.destroy`,
		`api(`,
	} {
		if strings.Contains(applyLanguage, forbidden) {
			t.Errorf("applyLanguage contains state-changing call %q", forbidden)
		}
	}
}

func TestAdminHTMLLocalizesToggleHUDAndSanitizesErrors(t *testing.T) {
	html := adminInlineHTML(t)
	for _, want := range []string{
		`id="language-toggle" type="button" data-i18n="topbar.languageToggle"`,
		`class="hud-label" data-i18n="telemetry.hudSpeed"`,
		`id="telemetry-state" data-i18n="telemetry.noData"`,
		`id="telemetry-enabled" class="telemetry-badge" data-on="false" data-i18n="telemetry.enabled"`,
		`id="telemetry-active" class="telemetry-badge" data-on="false" data-i18n="telemetry.active"`,
		`id="telemetry-alert" data-i18n="telemetry.noAlert"`,
		`id="telemetry-map-status" aria-live="polite" data-i18n="telemetry.waitingTrace"`,
		`id="telemetry-chart-status" aria-live="polite" data-i18n="telemetry.waiting"`,
		`translate: i18n.t`,
		`window.PilotTelemetry.active?.setTranslate(i18n.t);`,
		`window.PilotI18n.localizeError(i18n.t, error, fallbackKey)`,
		`endpoint: path`,
		`status: response.status`,
		`console.error`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain observable localization hook %q", want)
		}
	}
	for _, forbidden := range []string{
		`message: error.message`,
		`message: filesResult.reason.message`,
		`message: replayResult.reason.message`,
		`details: data.details`,
		`new Error(data.details`,
	} {
		if strings.Contains(html, forbidden) {
			t.Errorf("admin HTML exposes raw error detail through %q", forbidden)
		}
	}
}

func TestAdminHTMLContainsUnifiedShellBehavior(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`const appState = {`,
		`view: "overview"`,
		`devices: []`,
		`routesByDevice: new Map()`,
		`selectedDongleID: ""`,
		`selectedRoute: ""`,
		`routeRequestGeneration: 0`,
		`function setView(view)`,
		`leaveReplay();`,
		`heading.focus();`,
		`Promise.allSettled`,
		`appState.routesByDevice.set`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain unified shell behavior %q", want)
		}
	}

	start := strings.Index(html, "async function loadOverviewRoutes")
	end := strings.Index(html, "function renderOverview")
	if start < 0 || end <= start {
		t.Fatal("admin HTML does not contain bounded overview route loader")
	}
	overviewLoader := html[start:end]
	for _, forbidden := range []string{"/replay", "/telemetry", "media-ticket"} {
		if strings.Contains(overviewLoader, forbidden) {
			t.Errorf("overview route loader contains forbidden endpoint %q", forbidden)
		}
	}
}

func TestAdminHTMLUsesCompleteSessionCleanupForLogoutAndUnauthorized(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`function clearSession()`,
		`authGeneration++;`,
		`appState.devices = [];`,
		`appState.routesByDevice.clear();`,
		`appState.selectedDongleID = "";`,
		`appState.selectedRoute = "";`,
		`mapSettings = {map_provider: "none", map_web_key: "", map_security_code: ""};`,
		`currentSettings = {};`,
		`document.querySelector("#map-web-key").value = "";`,
		`document.querySelector("#map-security-code").value = "";`,
		`document.querySelector('#primary-nav [data-view="routes"]').disabled = true;`,
		`localStorage.removeItem(tokenKey);`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain complete session cleanup hook %q", want)
		}
	}
	if !strings.Contains(html, `document.querySelector("#logout").addEventListener("click", clearSession);`) {
		t.Error("logout must use the same clearSession path as 401")
	}
}

func TestAdminHTMLGuardsAsyncResponsesWithAuthGeneration(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`let authGeneration = 0;`,
		`const sessionGeneration = authGeneration;`,
		`if (!isLogin && sessionGeneration !== authGeneration) throw staleSession;`,
		`result = await response.json();`,
		`if (error === staleSession) return;`,
		`const downloadGeneration = authGeneration;`,
		`if (downloadGeneration !== authGeneration) throw staleSession;`,
		`throw requestError({endpoint: path, status: 0, code: "network_error"});`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain auth race guard %q", want)
		}
	}
	if got := strings.Count(html, `if (!isLogin && sessionGeneration !== authGeneration) throw staleSession;`); got < 3 {
		t.Errorf("auth generation response guards = %d, want fetch, parse, and rejection guards", got)
	}
}

func TestAdminHTMLLoginUnauthorizedClearsSessionAndShowsAuthError(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`const {isLogin = false, ...requestOptions} = options;`,
		`if (!isLogin) throw staleSession;`,
		`api("/admin/api/login", {`,
		`isLogin: true,`,
		`setLocalizedError(message, error, "errors.loginFailed");`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain login 401 behavior %q", want)
		}
	}
}

func TestAdminHTMLDownloadUnauthorizedClearsSession(t *testing.T) {
	html := string(indexHTML)
	start := strings.Index(html, "async function download")
	end := strings.Index(html, `loginForm.addEventListener("submit"`)
	if start < 0 || end <= start {
		t.Fatal("admin HTML does not contain bounded download function")
	}
	download := html[start:end]
	for _, want := range []string{
		`if (response.status === 401)`,
		`clearSession();`,
		`throw staleSession;`,
	} {
		if !strings.Contains(download, want) {
			t.Errorf("download function does not contain 401 cleanup hook %q", want)
		}
	}
}

func TestAdminHTMLClearsPasswordAfterLoginAndSessionCleanup(t *testing.T) {
	html := string(indexHTML)
	if got := strings.Count(html, `document.querySelector("#password").value = "";`); got < 2 {
		t.Errorf("password clear hooks = %d, want login success and session cleanup", got)
	}
}

func TestAdminHTMLPrunesRemovedDeviceState(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`function reconcileDevices(devices)`,
		`const deviceIDs = new Set(devices.map((device) => device.dongle_id));`,
		`appState.routesByDevice.delete(dongleID);`,
		`if (appState.selectedDongleID && !deviceIDs.has(appState.selectedDongleID))`,
		`clearRouteBrowserState();`,
		`setView("devices");`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain removed-device cleanup hook %q", want)
		}
	}
}

func TestAdminHTMLSortsRecentRoutesByCreatedAtStably(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`createdAt: Number(route.created_at) || 0`,
		`order: routes.length`,
		`(b.createdAt - a.createdAt) || (a.order - b.order)`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain created_at ordering hook %q", want)
		}
	}
	if strings.Contains(html, `b.route.localeCompare(a.route)`) {
		t.Error("recent routes must not sort by route name")
	}
}

func TestAdminHTMLContainsReplayHooks(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`src="/admin/vendor/hls.min.js"`,
		`id="replay-view"`,
		`id="replay-video"`,
		`id="replay-mode"`,
		`id="replay-segment"`,
		`id="replay-retry"`,
		`id="back-to-files"`,
		`canPlayType("application/vnd.apple.mpegurl")`,
		`Hls.isSupported()`,
		`JSON.stringify({mode: "route"})`,
		`JSON.stringify({mode: "segment", segment: segmentNumber})`,
		`clearReplaySource`,
		`clearTicketRefresh`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain %q", want)
		}
	}
}

func TestAdminHTMLContainsSSHKeyAndTerminalHooks(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`id="ssh-private-key"`,
		`id="ssh-key-state"`,
		`id="save-ssh-key"`,
		`id="clear-ssh-key"`,
		`devices.openTerminal`,
		`id="view-ssh"`,
		`src="/admin/vendor/xterm.min.js"`,
		`/admin/api/devices/${encodeURIComponent(device.dongle_id)}/ssh/pty?access_token=`,
		`key_unconfigured: "ssh.keyUnconfigured"`,
		`await loadSSHKey(device.dongle_id)`,
		"/admin/api/devices/${encodeURIComponent(sshKeyDongleID)}/ssh-key",
		`{method: "PUT"`,
		`{method: "DELETE"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain SSH hook %q", want)
		}
	}
	for _, forbidden := range []string{"PRIVATE KEY", "id_ed25519"} {
		if strings.Contains(html, forbidden) {
			t.Errorf("admin HTML exposes private key material marker %q", forbidden)
		}
	}
}

func TestAdminHTMLLoadsSettingsWithoutSSHKey(t *testing.T) {
	html := adminInlineHTML(t)
	loadSettings := adminFunction(t, html, "async function loadSettings()", "\n    async function loadSSHKey")
	if strings.Contains(loadSettings, "ssh-key") {
		t.Error("loadSettings must not load an SSH key")
	}
	loadSSHKey := adminFunction(t, html, "async function loadSSHKey(", "\n    function applyMapSettings")
	if !strings.Contains(loadSSHKey, "`/admin/api/devices/${encodeURIComponent(dongleID)}/ssh-key`") {
		t.Error("loadSSHKey must request the device SSH key")
	}
	if !strings.Contains(html, `settings.sshKeyCorrupt`) {
		t.Error("admin HTML does not contain the corrupt SSH key status")
	}
}

func TestAdminHTMLPreservesUnsavedSSHKeyOnLanguageSwitch(t *testing.T) {
	html := adminInlineHTML(t)
	renderState := adminFunction(t, html, "function renderSSHKeyState()", "\n    function localizedError")
	if strings.Contains(renderState, `input.value = ""`) {
		t.Error("renderSSHKeyState must not clear an unsaved private key")
	}

	saveHandler := adminFunction(t, html,
		`document.querySelector("#save-ssh-key").addEventListener`,
		`document.querySelector("#clear-ssh-key").addEventListener`)
	clearHandler := adminFunction(t, html,
		`document.querySelector("#clear-ssh-key").addEventListener`,
		`document.querySelector("#allow-lan").addEventListener`)
	for name, handler := range map[string]string{"save": saveHandler, "clear": clearHandler} {
		if !strings.Contains(handler, `document.querySelector("#ssh-private-key").value = "";`) {
			t.Errorf("%s handler must clear the private key after success", name)
		}
	}
}

func TestAdminHTMLLanguageSwitchPreservesTerminalSession(t *testing.T) {
	html := adminInlineHTML(t)
	applyLanguage := adminFunction(t, html, "function applyLanguage(", "\n    async function api")
	if strings.Contains(applyLanguage, "closeSSH") {
		t.Error("applyLanguage must not close the SSH terminal")
	}
	for _, want := range []string{
		`previous === "ssh" && view !== "ssh"`,
		`closeSSH();`,
		`JSON.stringify({cols: sshTerminal.cols, rows: sshTerminal.rows})`,
		`sshTerminal.onData`,
		`sshTerminal.onResize`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain terminal lifecycle hook %q", want)
		}
	}
}

func TestAdminHTMLReportsRemoteSSHCloseWithoutFlashingOnLocalClose(t *testing.T) {
	html := adminInlineHTML(t)
	closeSSH := adminFunction(t, html, "function closeSSH()", "\n    async function openSSHDevice")
	openSSH := adminFunction(t, html, "async function openSSHDevice", "\n    function renderDevice")
	if strings.Index(closeSSH, `sshSocket = null;`) > strings.Index(closeSSH, `socket.close();`) {
		t.Error("closeSSH must clear sshSocket before closing the WebSocket")
	}
	for _, want := range []string{
		`closeSSH();`,
		`let errorShown = false;`,
		`errorShown = true;`,
		`socket.addEventListener("close", () => {`,
		`if (socket !== sshSocket) return;`,
		`if (!errorShown) setLocalizedText(status, "ssh.tunnelFailed");`,
		`sshSocket = null;`,
	} {
		if !strings.Contains(openSSH, want) {
			t.Errorf("openSSHDevice does not contain remote-close hook %q", want)
		}
	}
}

func TestAdminHTMLDisablesSSHKeySaveUntilRequestFinishes(t *testing.T) {
	html := adminInlineHTML(t)
	handler := adminFunction(t, html,
		`document.querySelector("#save-ssh-key").addEventListener`,
		`document.querySelector("#clear-ssh-key").addEventListener`)
	disabled := strings.Index(handler, `button.disabled = true;`)
	request := strings.Index(handler, "`/admin/api/devices/${encodeURIComponent(sshKeyDongleID)}/ssh-key`")
	finally := strings.Index(handler, `} finally {`)
	enabled := strings.Index(handler, `button.disabled = false;`)
	if disabled < 0 || request < 0 || disabled > request {
		t.Error("save handler must disable the button before starting the request")
	}
	if finally < 0 || enabled < finally {
		t.Error("save handler must re-enable the button in finally")
	}
}

func TestAdminHTMLContainsFatalFragmentSkipHooks(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`id="replay-skip-segment"`,
		`data.frag`,
		`failedSegmentFromFragment`,
		`data-i18n="replay.skipSegment"`,
		`replayMode.value = "segment"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain fatal fragment hook %q", want)
		}
	}
}

func TestAdminHTMLConnectsFragmentTimeMappingLifecycle(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`Hls.Events.BUFFER_APPENDED`,
		`telemetry.updateFragmentTiming`,
		`fragmentPlayableSegments()`,
		`telemetry.clearFragmentMapping`,
		`generation === replayRequestGeneration`,
		`window.PilotTelemetry.active = telemetry`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain fragment mapping hook %q", want)
		}
	}
	if strings.Contains(html, `Hls.Events.LEVEL_LOADED`) {
		t.Error("manifest LEVEL_LOADED timing must not install media mapping")
	}
}

func TestAdminHTMLContainsRouteStateLifecycleHooks(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`clearRouteBrowserState();`,
		`routeBrowser.replaceChildren();`,
		`fileList.replaceChildren();`,
		`replayEntry.replaceChildren();`,
		`clearLocalizedText(fileStatus);`,
		`const routeGeneration = routeRequestGeneration;`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain %q", want)
		}
	}
	if got := strings.Count(html, `if (routeGeneration !== routeRequestGeneration) return;`); got < 2 {
		t.Errorf("route generation guards = %d, want at least 2", got)
	}
	if got := strings.Count(html, `clearRouteBrowserState();`); got < 3 {
		t.Errorf("route state cleanup calls = %d, want at least 3", got)
	}
}

func TestAdminHTMLContainsReplayFocusHooks(t *testing.T) {
	html := string(indexHTML)
	for _, want := range []string{
		`id="replay-title" tabindex="-1"`,
		`replayTrigger = trigger;`,
		`document.querySelector("#replay-title").focus();`,
		`if (trigger && trigger.isConnected) trigger.focus();`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin HTML does not contain %q", want)
		}
	}
}
