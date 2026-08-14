"use strict";

const fs = require("fs");
const vm = require("vm");
const assert = require("assert");
const test = require("node:test");

function memoryStorage() {
  const store = new Map();
  return {
    getItem(key) { return store.has(key) ? store.get(key) : null; },
    setItem(key, value) { store.set(key, String(value)); }
  };
}

function loadPilotI18n() {
  const context = {window: {}};
  vm.createContext(context);
  vm.runInContext(fs.readFileSync(require.resolve("./i18n.js"), "utf8"), context);
  return context.window.PilotI18n;
}

const PilotI18n = loadPilotI18n();

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
  assert.strictEqual(zh.t("devices.openTerminal"), "开终端");
  assert.strictEqual(zh.t("topbar.serviceOk"), "服务正常");
  assert.strictEqual(zh.t("topbar.serviceError"), "服务异常");
  assert.strictEqual(en.t("topbar.serviceOk"), "Service OK");
  assert.strictEqual(en.t("topbar.serviceError"), "Service error");
  assert.strictEqual(zh.t("telemetry.failedSegments", {count: 2, segments: "1, 4"}),
    "2 个分段遥测损坏（分段 1, 4），其他分段仍可使用");
  assert.strictEqual(en.t("telemetry.failedSegments", {count: 2, segments: "1, 4"}),
    "Telemetry is damaged in 2 segments (1, 4); other segments remain available");
});

test("dictionaries cover the complete localized admin workflow", () => {
  const required = [
    "login.title", "login.password", "login.passwordPlaceholder", "login.submit",
    "nav.primaryAria", "nav.overview", "nav.devices", "nav.routes", "nav.settings",
    "settings.title", "settings.publicBaseUrlLabel", "settings.listenLabel",
    "settings.allowLan", "settings.mapProvider", "settings.mapNone",
    "settings.mapAmap", "settings.mapTencent", "settings.webKey",
    "settings.securityCode", "settings.mapNote", "settings.mapRefreshNote",
    "settings.save", "settings.configured", "settings.notConfigured",
    "settings.readFailed", "settings.saved", "settings.savedListenChanged",
    "settings.sshKeyTitle", "settings.sshKeyHelp", "settings.sshPublicKey",
    "settings.copySSHKey", "settings.rotateSSHKey", "settings.rotateSSHKeyConfirm",
    "settings.sshKeyCopied",
    "devices.title", "devices.refresh", "devices.empty", "devices.viewRoutes",
    "devices.openSSH", "devices.openTerminal", "devices.dongleId", "devices.status",
    "devices.actions", "ssh.title", "ssh.copyCommand", "ssh.close",
    "ssh.connecting", "ssh.offline", "ssh.publicBaseUnconfigured",
    "ssh.authFailed", "ssh.tunnelFailed",
    "status.online", "status.offline",
    "routes.title", "routes.empty", "routes.loadingFiles", "routes.downloadFailed",
    "routes.loadFilesFailed", "routes.playOnline", "routes.viewTelemetry",
    "routes.replaySummaryFailed", "replay.playerReady", "replay.fragmentFailed",
    "replay.noVideo", "replay.requestingTicket", "replay.ticketFailed",
    "replay.hlsFailed", "replay.hlsUnsupported", "replay.retry",
    "replay.skipSegment", "replay.modeRoute", "replay.modeSegment",
    "replay.previousSegment", "replay.nextSegment", "replay.backToFiles",
    "replay.playMode", "replay.videoSegment", "replay.wholeRoute",
    "replay.singleSegment", "replay.playSegment", "replay.viewSegmentTelemetry",
    "replay.segmentMissingBoth", "replay.telemetryUnavailable",
    "telemetry.failedSegments", "errors.generic", "errors.loadFailed",
    "errors.loginFailed", "errors.saveFailed", "errors.sshFailed"
  ];
  for (const language of ["zh-CN", "en"]) {
    const dictionary = PilotI18n.create({
      storage: memoryStorage(),
      languages: [language]
    }).dictionary();
    for (const key of required) {
      assert.ok(Object.hasOwn(dictionary, key), `${language} missing ${key}`);
      assert.notStrictEqual(dictionary[key], "", `${language} has empty ${key}`);
    }
  }
});

function fakeLocalizedElement(key, attribute = "i18n") {
  return {
    dataset: {[attribute]: key},
    textContent: "",
    placeholder: "",
    attributes: {},
    setAttribute(name, value) { this.attributes[name] = value; }
  };
}

test("observable renderer shows destination language and localized HUD defaults", () => {
  const toggle = fakeLocalizedElement("topbar.languageToggle");
  const speed = fakeLocalizedElement("telemetry.hudSpeed");
  const state = fakeLocalizedElement("telemetry.noData");
  const enabled = fakeLocalizedElement("telemetry.enabled");
  const active = fakeLocalizedElement("telemetry.active");
  const alert = fakeLocalizedElement("telemetry.noAlert");
  const mapStatus = fakeLocalizedElement("telemetry.waitingTrace");
  const chartStatus = fakeLocalizedElement("telemetry.waiting");
  const textElements = [toggle, speed, state, enabled, active, alert, mapStatus, chartStatus];
  const root = {
    querySelectorAll(selector) {
      if (selector === "[data-i18n]") return textElements;
      return [];
    }
  };
  const i18n = PilotI18n.create({storage: memoryStorage(), languages: ["zh-CN"]});

  PilotI18n.render(root, i18n.t);
  assert.deepStrictEqual(textElements.map((element) => element.textContent), [
    "English", "车速", "无数据", "已启用", "活跃", "无告警", "等待轨迹数据", "等待遥测数据"
  ]);

  i18n.setLanguage("en");
  PilotI18n.render(root, i18n.t);
  assert.strictEqual(toggle.textContent, "中文");
  assert.strictEqual(speed.textContent, "VEHICLE SPEED");
  assert.strictEqual(state.textContent, "No data");
});

test("Chinese status and HUD translations are natural language", () => {
  const zh = PilotI18n.create({storage: memoryStorage(), languages: ["zh-CN"]});
  assert.strictEqual(zh.t("status.online"), "在线");
  assert.strictEqual(zh.t("status.offline"), "离线");
  assert.strictEqual(zh.t("status.loading"), "加载中");
  assert.strictEqual(zh.t("telemetry.hudState"), "控制状态");
  assert.strictEqual(zh.t("telemetry.hudFlags"), "系统状态");
  assert.strictEqual(zh.t("telemetry.hudAlert"), "当前告警");
});

test("control state dictionaries use the required localized labels", () => {
  const zh = PilotI18n.create({storage: memoryStorage(), languages: ["zh-CN"]});
  const en = PilotI18n.create({storage: memoryStorage(), languages: ["en"]});
  const expected = {
    disabled: ["未启用", "Disabled"],
    preEnabled: ["预启用", "Pre-enabled"],
    enabled: ["已启用", "Enabled"],
    softDisabling: ["软退出", "Soft disabling"],
    overriding: ["人工接管", "Overriding"],
    unknown: ["未知", "Unknown"]
  };
  for (const [state, labels] of Object.entries(expected)) {
    assert.strictEqual(zh.t(`telemetry.state.${state}`), labels[0]);
    assert.strictEqual(en.t(`telemetry.state.${state}`), labels[1]);
  }
});

test("localized errors map stable metadata without exposing raw details", () => {
  const zh = PilotI18n.create({storage: memoryStorage(), languages: ["zh-CN"]});
  const secret = "upstream says invalid bearer token";
  assert.strictEqual(PilotI18n.localizeError(zh.t, {
    endpoint: "/admin/api/login",
    status: 401,
    message: secret
  }, "errors.loginFailed"), "认证失败，请检查管理密码");
  assert.strictEqual(PilotI18n.localizeError(zh.t, {
    endpoint: "/admin/api/devices",
    status: 503,
    message: secret
  }, "errors.loadFailed"), "服务暂时不可用，请稍后重试");
  assert.strictEqual(PilotI18n.localizeError(zh.t, {
    code: "network_error",
    message: secret
  }, "errors.loadFailed"), "网络连接失败，请稍后重试");
  assert.strictEqual(PilotI18n.localizeError(zh.t, {
    message: secret
  }, "errors.loadFailed"), "加载失败，请重试");
  assert.ok(!PilotI18n.localizeError(zh.t, {message: secret}, "errors.generic").includes(secret));
});

test("create degrades to in-memory language when storage read fails", () => {
  const throwingStorage = {
    getItem() { throw new Error("storage blocked"); },
    setItem() {}
  };
  const i18n = PilotI18n.create({storage: throwingStorage, languages: ["zh-CN"]});
  assert.strictEqual(i18n.language(), "zh-CN");
  i18n.setLanguage("en");
  assert.strictEqual(i18n.language(), "en");
});

test("create without storage uses in-memory fallback when localStorage is unavailable", () => {
  const context = {window: {}};
  vm.createContext(context);
  vm.runInContext(fs.readFileSync(require.resolve("./i18n.js"), "utf8"), context);
  const i18n = context.window.PilotI18n.create({languages: ["zh-CN"]});
  assert.strictEqual(i18n.language(), "zh-CN");
});

test("setLanguage updates state and notifies listeners when storage write fails", () => {
  const storage = {
    getItem() { return null; },
    setItem() { throw new Error("quota exceeded"); }
  };
  const i18n = PilotI18n.create({storage, languages: ["en"]});
  const seen = [];
  i18n.subscribe((language) => seen.push(language));
  i18n.setLanguage("zh-CN");
  assert.strictEqual(i18n.language(), "zh-CN");
  assert.deepStrictEqual(seen, ["zh-CN"]);
});

test("listener notifications capture the language snapshot for each round", () => {
  const i18n = PilotI18n.create({storage: memoryStorage(), languages: ["en"]});
  const outerFirst = [];
  const inner = [];
  i18n.subscribe((language) => {
    if (outerFirst.length === 0) outerFirst.push(language);
    if (language === "zh-CN") {
      i18n.subscribe((lang) => inner.push(lang));
      i18n.setLanguage("en");
    }
  });
  i18n.subscribe((language) => {
    if (outerFirst.length < 2) outerFirst.push(language);
  });
  i18n.setLanguage("zh-CN");
  assert.deepStrictEqual(outerFirst, ["zh-CN", "zh-CN"]);
  assert.deepStrictEqual(inner, ["en"]);
  assert.strictEqual(i18n.language(), "en");
});

test("listener exceptions do not block peers or leave the notify queue stuck", () => {
  const i18n = PilotI18n.create({storage: memoryStorage(), languages: ["en"]});
  const seen = [];
  i18n.subscribe(() => { throw new Error("listener failed"); });
  i18n.subscribe((language) => seen.push(language));
  i18n.setLanguage("zh-CN");
  assert.deepStrictEqual(seen, ["zh-CN"]);

  const after = [];
  i18n.subscribe((language) => after.push(language));
  i18n.setLanguage("en");
  assert.deepStrictEqual(after, ["en"]);
  assert.strictEqual(i18n.language(), "en");
});
