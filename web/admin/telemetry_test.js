"use strict";

const fs = require("fs");
const vm = require("vm");
const assert = require("assert");
const test = require("node:test");

const context = {window: {}};
vm.createContext(context);
vm.runInContext(fs.readFileSync(require.resolve("./telemetry.js"), "utf8"), context);

const telemetry = context.window.PilotTelemetry;
assert.strictEqual(telemetry.mediaToCanonicalTime(55, 120, 110), 60);
assert.strictEqual(
  telemetry.latestSampleForTest([
    {t: 0, segment: 0},
    {t: 60, segment: 1}
  ], telemetry.mediaToCanonicalTime(55, 120, 110)).segment,
  1
);
assert.strictEqual(telemetry.canonicalToMediaTime(90, 120, 110), 82.5);
assert.strictEqual(telemetry.mediaToCanonicalTime(7, 120, NaN), 7);
assert.strictEqual(telemetry.canonicalToMediaTime(7, 0, 110), 7);

const segments = [
  {number: 0, duration: 60},
  {number: 2, duration: 60}
];
const state = telemetry.createFragmentTimingState();
const first = {
  type: "main",
  sn: 0,
  url: "https://example/media/0.ts?ticket=1",
  start: 0,
  duration: 60,
  elementaryStreams: {video: {startPTS: 0, endPTS: 50}}
};
const second = {
  type: "main",
  sn: 1,
  url: "2.ts",
  start: 60,
  duration: 60,
  elementaryStreams: {video: {startPTS: 50, endPTS: 110}}
};
assert.strictEqual(state.update(segments, first), true);
assert.strictEqual(state.knownCount(), 1);
let fragmentMap = state.current();
assert.strictEqual(
  telemetry.fragmentMediaToCanonicalTime(50, fragmentMap, 120, 110),
  60
);
assert.strictEqual(state.update(segments, second), true);
assert.strictEqual(state.update(segments, second), true);
assert.strictEqual(state.knownCount(), 2);
fragmentMap = state.current();
assert.strictEqual(
  telemetry.fragmentMediaToCanonicalTime(50, fragmentMap, 120, 110),
  60
);
assert.strictEqual(
  telemetry.fragmentCanonicalToMediaTime(90, fragmentMap, 120, 110),
  80
);

const discontinuousState = telemetry.createFragmentTimingState();
assert.strictEqual(discontinuousState.update(segments, {
  type: "main", sn: 0, url: "0.ts", startPTS: 5, endPTS: 55
}), true);
assert.strictEqual(discontinuousState.update(segments, {
  type: "main", sn: 1, url: "2.ts", startPTS: 75, endPTS: 135
}), true);
const discontinuousMap = discontinuousState.current();
assert.strictEqual(
  telemetry.fragmentMediaToCanonicalTime(75, discontinuousMap, 120, 135),
  60
);
assert.strictEqual(
  telemetry.fragmentCanonicalToMediaTime(90, discontinuousMap, 120, 135),
  105
);
assert.strictEqual(
  telemetry.fragmentMediaToCanonicalTime(60, discontinuousMap, 120, 135),
  telemetry.mediaToCanonicalTime(60, 120, 135)
);

for (const invalid of [
  {type: "audio", sn: 0, url: "0.ts", startPTS: 0, endPTS: 50},
  {type: "main", sn: "initSegment", url: "0.ts", startPTS: 0, endPTS: 50},
  {type: "main", sn: 0, url: "0.ts", start: 0, duration: 50},
  {type: "main", sn: 0, url: "0.ts", startPTS: NaN, endPTS: 50},
  {type: "main", sn: 0, url: "0.ts", startPTS: 50, endPTS: 50}
]) {
  assert.strictEqual(state.update(segments, invalid), false);
  assert.strictEqual(state.knownCount(), 2);
}

state.clear();
assert.strictEqual(state.knownCount(), 0);
assert.strictEqual(state.current(), null);
assert.strictEqual(state.update([{number: 2, duration: 60}], second), true);
assert.strictEqual(state.knownCount(), 1);
assert.strictEqual(
  telemetry.fragmentMediaToCanonicalTime(50, state.current(), 60, 60),
  0
);

function fakeCanvas() {
  const fillTexts = [];
  const context2d = {
    setTransform() {}, clearRect() {}, fillRect() {}, beginPath() {},
    moveTo() {}, lineTo() {}, stroke() {}, drawImage() {}, arc() {},
    fill() {}, fillText(text) { fillTexts.push(text); }
  };
  return {
    width: 0, height: 0, hidden: false, parentElement: {},
    attributes: {},
    fillTexts,
    getContext() { return context2d; },
    getBoundingClientRect() { return {left: 0, width: 640, height: 360}; },
    addEventListener() {},
    setAttribute(name, value) { this.attributes[name] = value; }
  };
}

function fakeElements() {
  return {
    mapCanvas: fakeCanvas(),
    chart: fakeCanvas(),
    providerLayer: {
      hidden: true,
      replaceChildren() {}
    },
    speed: {textContent: ""},
    state: {textContent: "", dataset: {}},
    enabled: {textContent: "", dataset: {}},
    active: {textContent: "", dataset: {}},
    alert: {textContent: "", dataset: {}},
    mapStatus: {textContent: "", dataset: {}},
    chartStatus: {textContent: "", dataset: {}}
  };
}

test("renders HUD defaults and waiting states through the translator", async () => {
  context.document = {
    createElement(name) {
      if (name === "canvas") return fakeCanvas();
      return {remove() {}};
    },
    head: {append() {}}
  };
  context.window.devicePixelRatio = 1;
  context.window.ResizeObserver = context.ResizeObserver = class {
    observe() {}
    disconnect() {}
  };
  let resolveTelemetry;
  const telemetryResult = new Promise((resolve) => { resolveTelemetry = resolve; });
  const elements = fakeElements();
  const instance = telemetry.create({
    video: {currentTime: 0, duration: 60},
    elements,
    api: async () => telemetryResult,
    getMapSettings: () => ({map_provider: "none"}),
    translate: (key) => `translated:${key}`
  });
  const opening = instance.open({
    base: "/api",
    summary: {
      segments: [{number: 0, duration: 60, has_video: true, has_telemetry: true}]
    }
  }, "route", 0);

  assert.strictEqual(elements.state.textContent, "translated:telemetry.noData");
  assert.strictEqual(elements.enabled.textContent, "translated:telemetry.enabled");
  assert.strictEqual(elements.active.textContent, "translated:telemetry.active");
  assert.strictEqual(elements.alert.textContent, "translated:telemetry.noAlert");
  assert.strictEqual(elements.mapStatus.textContent, "translated:telemetry.waitingTrace");
  assert.strictEqual(elements.chartStatus.textContent, "translated:telemetry.loading");
  assert.strictEqual(elements.mapStatus.dataset.i18n, "telemetry.waitingTrace");
  assert.strictEqual(elements.chartStatus.dataset.i18n, "telemetry.loading");

  resolveTelemetry({speeds: [], gps: [], controls: []});
  await opening;
  assert.strictEqual(elements.mapStatus.textContent, "translated:telemetry.noGPS");
  assert.strictEqual(elements.chartStatus.textContent, "translated:telemetry.none");
  assert.strictEqual(elements.mapStatus.dataset.i18n, "telemetry.noGPS");
  assert.strictEqual(elements.chartStatus.dataset.i18n, "telemetry.none");
  instance.cleanup();
});

test("setTranslate redraws localized telemetry without changing loaded state", async () => {
  const createdCanvases = [];
  context.document = {
    createElement(name) {
      if (name === "canvas") {
        const canvas = fakeCanvas();
        createdCanvases.push(canvas);
        return canvas;
      }
      return {remove() {}};
    },
    head: {append() {}}
  };
  context.window.devicePixelRatio = 1;
  context.window.ResizeObserver = context.ResizeObserver = class {
    observe() {}
    disconnect() {}
  };
  let mapCalls = 0;
  context.window.AMap = {
    Map: class {
      constructor() { mapCalls++; }
      add() {}
      setFitView() {}
      resize() {}
      destroy() {}
    },
    Polyline: class { constructor() {} },
    Marker: class { setPosition() {} }
  };
  const translations = {
    zh: {
      "telemetry.enabled": "已启用",
      "telemetry.active": "活跃",
      "telemetry.noAlert": "无告警",
      "telemetry.state.enabled": "已启用",
      "telemetry.chartTitle": "速度 / 控制",
      "telemetry.chartAriaValueText": ({seconds}) => `${seconds} 秒`,
      "map.fallbackTrace": "无底图轨迹",
      "map.amapTrace": "高德地图轨迹"
    },
    en: {
      "telemetry.enabled": "ENABLED",
      "telemetry.active": "ACTIVE",
      "telemetry.noAlert": "No alert",
      "telemetry.state.enabled": "Enabled",
      "telemetry.chartTitle": "SPEED / CONTROL",
      "telemetry.chartAriaValueText": ({seconds}) => `${seconds} s`,
      "map.fallbackTrace": "Trace without map",
      "map.amapTrace": "AMap trace"
    }
  };
  const translator = (language) => (key, params) => {
    const value = translations[language][key];
    return typeof value === "function" ? value(params) : (value || key);
  };
  const video = {currentTime: 10, duration: 60};
  const elements = fakeElements();
  let apiCalls = 0;
  const instance = telemetry.create({
    video,
    elements,
    api: async () => {
      apiCalls++;
      return {
        speeds: [{t: 0, v: 10}],
        gps: [{t: 0, lat: 23.1291, lon: 113.2644}],
        controls: [{
          t: 0, state: "enabled", enabled: true, active: true,
          alert_text_1: "DEVICE ALERT"
        }]
      };
    },
    getMapSettings: () => ({map_provider: "amap", map_web_key: "test"}),
    translate: translator("zh")
  });
  const summary = {
    segments: [{number: 0, duration: 60, has_video: true, has_telemetry: true}]
  };
  await instance.open({summary, base: "/api"}, "route", 0);
  instance.updateFragmentTiming({
    type: "main", sn: 0, url: "0.ts", startPTS: 5, endPTS: 55
  }, [{number: 0, duration: 60}]);

  assert.strictEqual(elements.speed.textContent, "36.0");
  assert.strictEqual(elements.state.textContent, "已启用");
  assert.strictEqual(elements.mapStatus.textContent, "高德地图轨迹");
  assert.strictEqual(elements.alert.textContent, "DEVICE ALERT");
  assert.strictEqual(elements.chart.attributes["aria-valuetext"], "10.0 秒");
  const mapping = JSON.stringify(instance.fragmentMapping());
  const timingCount = instance.parsedTimingCount();
  const mapCount = mapCalls;
  const videoTime = video.currentTime;

  instance.setTranslate(translator("en"));

  assert.strictEqual(elements.speed.textContent, "36.0");
  assert.strictEqual(elements.state.textContent, "Enabled");
  assert.strictEqual(elements.enabled.textContent, "ENABLED");
  assert.strictEqual(elements.active.textContent, "ACTIVE");
  assert.strictEqual(elements.alert.textContent, "DEVICE ALERT");
  assert.strictEqual(elements.mapStatus.textContent, "AMap trace");
  assert.strictEqual(elements.chart.attributes["aria-valuetext"], "10.0 s");
  assert.strictEqual(createdCanvases[1].fillTexts.at(-1), "SPEED / CONTROL");
  assert.strictEqual(instance.parsedTimingCount(), timingCount);
  assert.strictEqual(JSON.stringify(instance.fragmentMapping()), mapping);
  assert.strictEqual(mapCalls, mapCount);
  assert.strictEqual(apiCalls, 1);
  assert.strictEqual(video.currentTime, videoTime);
  instance.cleanup();
});

test("translates known control states and falls back to unknown", async () => {
  context.document = {
    createElement(name) {
      if (name === "canvas") return fakeCanvas();
      return {remove() {}};
    },
    head: {append() {}}
  };
  context.window.devicePixelRatio = 1;
  context.window.ResizeObserver = context.ResizeObserver = class {
    observe() {}
    disconnect() {}
  };
  const elements = fakeElements();
  const states = ["disabled", "preEnabled", "enabled", "softDisabling", "overriding", "futureState"];
  const instance = telemetry.create({
    video: {currentTime: 0, duration: 60},
    elements,
    api: async () => ({
      controls: states.map((state, t) => ({t, state}))
    }),
    getMapSettings: () => ({map_provider: "none"}),
    translate: (key) => key
  });
  await instance.open({
    base: "/api",
    summary: {
      segments: [{number: 0, duration: 60, has_video: true, has_telemetry: true}]
    }
  }, "route", 0);

  for (let index = 0; index < states.length; index++) {
    instance.sync(index);
    const expected = index === states.length - 1 ? "unknown" : states[index];
    assert.strictEqual(elements.state.textContent, `telemetry.state.${expected}`);
    assert.strictEqual(elements.state.dataset.i18n, `telemetry.state.${expected}`);
  }
  instance.cleanup();
});

test("filters invalid API GPS before constructing a provider map", async () => {
  context.document = {
    createElement(name) {
      if (name === "canvas") return fakeCanvas();
      return {remove() {}};
    },
    head: {append() {}}
  };
  context.window.devicePixelRatio = 1;
  context.window.ResizeObserver = context.ResizeObserver = class {
    observe() {}
    disconnect() {}
  };

  let mapCalls = 0;
  let drawnPath = null;
  context.window.AMap = {
    Map: class {
      constructor() { mapCalls++; }
      add() {}
      setFitView() {}
      resize() {}
      destroy() {}
    },
    Polyline: class {
      constructor(options) { drawnPath = options.path; }
    },
    Marker: class {
      setPosition() {}
    }
  };

  const summary = {
    segments: [{number: 0, duration: 60, has_video: true, has_telemetry: true}]
  };
  const mixed = telemetry.create({
    video: {currentTime: 0, duration: 60},
    elements: fakeElements(),
    api: async () => ({gps: [
      {t: 0, lat: 0, lon: 0},
      {t: 1, lat: NaN, lon: 113},
      {t: 2, lat: 23, lon: Infinity},
      {t: 3, lat: 91, lon: 113},
      {t: 4, lat: 23, lon: 181},
      {t: 5, lat: null, lon: 113},
      {t: 6, lat: "", lon: 113},
      {t: 7, lat: true, lon: 113},
      {t: 8, lat: [], lon: 113},
      {t: 9, lat: "23.1291", lon: 113.2644},
      {t: 10, lat: 23, lon: null},
      {t: 11, lat: 23, lon: ""},
      {t: 12, lat: 23, lon: false},
      {t: 13, lat: 23, lon: [113]},
      {t: 14, lat: 23.1291, lon: "113.2644"},
      {t: 15, lat: 23.1291, lon: 113.2644}
    ]}),
    getMapSettings: () => ({map_provider: "amap", map_web_key: "test"})
  });
  await mixed.open({summary, base: "/api"}, "route", 0);
  assert.strictEqual(mapCalls, 1);
  assert.strictEqual(drawnPath.length, 1);
  assert.ok(drawnPath[0][0] > 113 && drawnPath[0][0] < 114);
  assert.ok(drawnPath[0][1] > 23 && drawnPath[0][1] < 24);
  mixed.cleanup();

  const allInvalidElements = fakeElements();
  const allInvalid = telemetry.create({
    video: {currentTime: 0, duration: 60},
    elements: allInvalidElements,
    api: async () => ({gps: [
      {t: 0, lat: 0, lon: 0},
      {t: 1, lat: -91, lon: 113},
      {t: 2, lat: 23, lon: -181}
    ]}),
    getMapSettings: () => ({map_provider: "amap", map_web_key: "test"})
  });
  await allInvalid.open({summary, base: "/api"}, "route", 0);
  assert.strictEqual(mapCalls, 1, "all-invalid GPS must not construct another map");
  assert.strictEqual(allInvalidElements.mapStatus.textContent, "无 GPS 轨迹");
  allInvalid.cleanup();
});
