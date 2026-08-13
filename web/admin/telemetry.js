(function () {
  "use strict";

  const sdkLoads = new Map();
  const loadedSDKConfig = new Map();
  const pendingSDKConfig = new Map();
  const MAX_DRAW_POINTS = 8192;
  const chinaBounds = {west: 72.004, east: 137.8347, south: 0.8293, north: 55.8271};

  function outOfChina(lat, lon) {
    return lon < chinaBounds.west || lon > chinaBounds.east ||
      lat < chinaBounds.south || lat > chinaBounds.north;
  }

  function transformLat(x, y) {
    let value = -100 + 2 * x + 3 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x));
    value += (20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2 / 3;
    value += (20 * Math.sin(y * Math.PI) + 40 * Math.sin(y / 3 * Math.PI)) * 2 / 3;
    value += (160 * Math.sin(y / 12 * Math.PI) + 320 * Math.sin(y * Math.PI / 30)) * 2 / 3;
    return value;
  }

  function transformLon(x, y) {
    let value = 300 + x + 2 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x));
    value += (20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2 / 3;
    value += (20 * Math.sin(x * Math.PI) + 40 * Math.sin(x / 3 * Math.PI)) * 2 / 3;
    value += (150 * Math.sin(x / 12 * Math.PI) + 300 * Math.sin(x / 30 * Math.PI)) * 2 / 3;
    return value;
  }

  function wgs84ToGcj02(point) {
    if (outOfChina(point.lat, point.lon)) return {lat: point.lat, lon: point.lon};
    const a = 6378245;
    const ee = 0.006693421622965943;
    let dLat = transformLat(point.lon - 105, point.lat - 35);
    let dLon = transformLon(point.lon - 105, point.lat - 35);
    const radLat = point.lat / 180 * Math.PI;
    let magic = Math.sin(radLat);
    magic = 1 - ee * magic * magic;
    const sqrtMagic = Math.sqrt(magic);
    dLat = dLat * 180 / ((a * (1 - ee)) / (magic * sqrtMagic) * Math.PI);
    dLon = dLon * 180 / (a / sqrtMagic * Math.cos(radLat) * Math.PI);
    return {lat: point.lat + dLat, lon: point.lon + dLon};
  }

  function validGPSCoordinates(lat, lon) {
    return Number.isFinite(lat) && Number.isFinite(lon) &&
      lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 &&
      (lat !== 0 || lon !== 0);
  }

  function latestSample(samples, time) {
    let low = 0;
    let high = samples.length - 1;
    let result = null;
    while (low <= high) {
      const middle = (low + high) >> 1;
      if (samples[middle].t <= time) {
        result = samples[middle];
        low = middle + 1;
      } else {
        high = middle - 1;
      }
    }
    return result;
  }

  function mediaToCanonicalTime(time, canonicalDuration, mediaDuration) {
    if (!Number.isFinite(mediaDuration) || mediaDuration <= 0 ||
        !Number.isFinite(canonicalDuration) || canonicalDuration <= 0) {
      return time;
    }
    return time * canonicalDuration / mediaDuration;
  }

  function canonicalToMediaTime(time, canonicalDuration, mediaDuration) {
    if (!Number.isFinite(mediaDuration) || mediaDuration <= 0 ||
        !Number.isFinite(canonicalDuration) || canonicalDuration <= 0) {
      return time;
    }
    return time * mediaDuration / canonicalDuration;
  }

  function fragmentSegmentNumber(fragment, playableSegments) {
    const match = String(fragment?.url || "").match(/(?:^|\/)(\d+)\.ts(?:$|[?#])/);
    if (match) return Number(match[1]);
    if (Number.isInteger(fragment?.sn) && fragment.sn >= 0 &&
        fragment.sn < playableSegments.length) {
      return Number(playableSegments[fragment.sn]?.number);
    }
    return null;
  }

  function parsedFragmentTiming(playableSegments, fragment) {
    if (!Array.isArray(playableSegments) || !playableSegments.length ||
        fragment?.type !== "main" || fragment?.sn === "initSegment") {
      return null;
    }
    const number = fragmentSegmentNumber(fragment, playableSegments);
    if (!playableSegments.some((segment) => Number(segment.number) === number)) {
      return null;
    }
    const video = fragment?.elementaryStreams?.video;
    let start = Number(video?.startPTS);
    let end = Number(video?.endPTS);
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
      start = Number(fragment?.startPTS);
      end = Number(fragment?.endPTS);
    }
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return null;
    return {number, start, end};
  }

  function buildFragmentTimeMap(playableSegments, timings) {
    let canonicalStart = 0;
    let priorMediaEnd = -1;
    const result = [];
    for (const segment of playableSegments) {
      const number = Number(segment?.number);
      const canonicalDuration = Number(segment?.duration);
      if (!Number.isInteger(number) || number < 0 ||
          !Number.isFinite(canonicalDuration) || canonicalDuration <= 0) {
        return null;
      }
      const timing = timings.get(number);
      if (timing) {
        if (timing.start < priorMediaEnd) return null;
        result.push({
          number,
          canonicalStart,
          canonicalEnd: canonicalStart + canonicalDuration,
          mediaStart: timing.start,
          mediaEnd: timing.end
        });
        priorMediaEnd = timing.end;
      }
      canonicalStart += canonicalDuration;
    }
    return result.length ? result : null;
  }

  function createFragmentTimingState() {
    let timings = new Map();
    let currentMap = null;
    return {
      update(playableSegments, fragment) {
        const timing = parsedFragmentTiming(playableSegments, fragment);
        if (!timing) return false;
        const next = new Map(timings);
        next.set(timing.number, timing);
        const nextMap = buildFragmentTimeMap(playableSegments, next);
        if (!nextMap) return false;
        timings = next;
        currentMap = nextMap;
        return true;
      },
      clear() {
        timings = new Map();
        currentMap = null;
      },
      current() { return currentMap; },
      knownCount() { return timings.size; }
    };
  }

  function fragmentMediaToCanonicalTime(time, fragmentMap, canonicalDuration, mediaDuration) {
    if (Array.isArray(fragmentMap)) {
      for (const segment of fragmentMap) {
        if (time >= segment.mediaStart && time < segment.mediaEnd) {
          return segment.canonicalStart +
            (time - segment.mediaStart) *
            (segment.canonicalEnd - segment.canonicalStart) /
            (segment.mediaEnd - segment.mediaStart);
        }
      }
      const last = fragmentMap[fragmentMap.length - 1];
      if (last && time === last.mediaEnd) return last.canonicalEnd;
    }
    return mediaToCanonicalTime(time, canonicalDuration, mediaDuration);
  }

  function fragmentCanonicalToMediaTime(time, fragmentMap, canonicalDuration, mediaDuration) {
    if (Array.isArray(fragmentMap)) {
      for (const segment of fragmentMap) {
        if (time >= segment.canonicalStart && time < segment.canonicalEnd) {
          return segment.mediaStart +
            (time - segment.canonicalStart) *
            (segment.mediaEnd - segment.mediaStart) /
            (segment.canonicalEnd - segment.canonicalStart);
        }
      }
      const last = fragmentMap[fragmentMap.length - 1];
      if (last && time === last.canonicalEnd) return last.mediaEnd;
    }
    return canonicalToMediaTime(time, canonicalDuration, mediaDuration);
  }

  function downsampleGPS(points, requestedLimit) {
    const limit = Math.max(2, Math.min(MAX_DRAW_POINTS, Math.floor(requestedLimit) || 2));
    if (points.length <= limit) return points;
    const result = [points[0]];
    const step = (points.length - 1) / (limit - 1);
    for (let index = 1; index < limit - 1; index++) {
      result.push(points[Math.min(points.length - 2, Math.floor(index * step))]);
    }
    result.push(points[points.length - 1]);
    return result;
  }

  function downsampleSpeeds(samples, requestedLimit) {
    const limit = Math.max(2, Math.min(MAX_DRAW_POINTS, Math.floor(requestedLimit) || 2));
    if (samples.length <= limit) return samples;
    const result = [samples[0]];
    const bucketSize = (samples.length - 2) / (limit - 2);
    for (let bucket = 0; bucket < limit - 2; bucket++) {
      const start = 1 + Math.floor(bucket * bucketSize);
      const end = Math.min(samples.length - 1, 1 + Math.floor((bucket + 1) * bucketSize));
      let peak = samples[start];
      for (let index = start + 1; index < end; index++) {
        if (Number(samples[index].v) > Number(peak.v)) peak = samples[index];
      }
      result.push(peak);
    }
    result.push(samples[samples.length - 1]);
    return result;
  }

  function ensureCanvasSize(canvases, rect, ratio) {
    const width = Math.max(1, Math.round(rect.width * ratio));
    const height = Math.max(1, Math.round(rect.height * ratio));
    let changed = false;
    for (const canvas of canvases) {
      if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width;
        canvas.height = height;
        changed = true;
      }
    }
    return changed;
  }

  function mapSDKError(i18nKey) {
    const error = new Error(i18nKey);
    error.i18nKey = i18nKey;
    return error;
  }

  function loadSDK(settings) {
    const provider = settings.map_provider;
    const key = settings.map_web_key;
    const security = settings.map_security_code || "";
    const configKey = `${provider}\u0000${key}\u0000${security}`;
    const prior = loadedSDKConfig.get(provider);
    if (prior && prior !== configKey) {
      return Promise.reject(mapSDKError("map.refreshKey"));
    }
    if (sdkLoads.has(configKey)) return sdkLoads.get(configKey);
    const pending = pendingSDKConfig.get(provider);
    if (pending) {
      return pending.configKey === configKey
        ? pending.promise
        : Promise.reject(mapSDKError("map.refreshKey"));
    }

    const globalReady = provider === "amap" ? window.AMap : window.TMap;
    if (globalReady) {
      loadedSDKConfig.set(provider, configKey);
      return Promise.resolve(globalReady);
    }

    let promise;
    let start;
    promise = new Promise((resolve, reject) => {
      const callback = `__pilotMapReady_${Date.now()}_${Math.random().toString(36).slice(2)}`;
      const script = document.createElement("script");
      const url = new URL(provider === "amap"
        ? "https://webapi.amap.com/maps"
        : "https://map.qq.com/api/gljs");
      if (provider === "amap") {
        window._AMapSecurityConfig = {securityJsCode: security};
        url.searchParams.set("v", "2.0");
      } else {
        url.searchParams.set("v", "1.exp");
      }
      url.searchParams.set("key", key);
      url.searchParams.set("callback", callback);
      script.src = url.toString();
      script.async = true;
      script.charset = "utf-8";

      let timer;
      const finish = (error) => {
        clearTimeout(timer);
        script.onerror = null;
        script.onload = null;
        delete window[callback];
        if (error) {
          script.remove();
          reject(error);
          return;
        }
        loadedSDKConfig.set(provider, configKey);
        resolve(provider === "amap" ? window.AMap : window.TMap);
      };
      window[callback] = () => finish(null);
      script.onerror = () => finish(mapSDKError("map.sdkLoadFailed"));
      timer = setTimeout(() => finish(mapSDKError("map.sdkLoadTimeout")), 10000);
      start = () => {
        try {
          document.head.append(script);
        } catch (_) {
          finish(mapSDKError("map.sdkLoadFailed"));
        }
      };
    });
    pendingSDKConfig.set(provider, {configKey, promise});
    start();
    promise.then((sdk) => {
      sdkLoads.set(configKey, Promise.resolve(sdk));
    }, () => {}).finally(() => {
      const current = pendingSDKConfig.get(provider);
      if (current?.promise === promise) pendingSDKConfig.delete(provider);
    });
    return promise;
  }

  function createCanvasMap(canvas) {
    const staticLayer = document.createElement("canvas");
    let points = [];
    let current = null;
    let project = null;

    function currentMetrics() {
      const rect = canvas.getBoundingClientRect();
      const ratio = window.devicePixelRatio || 1;
      const changed = ensureCanvasSize([canvas, staticLayer], rect, ratio);
      return {rect, ratio, changed};
    }

    function renderStaticLayer(metrics) {
      const {rect, ratio} = metrics;
      const ctx = staticLayer.getContext("2d");
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      ctx.clearRect(0, 0, rect.width, rect.height);
      ctx.fillStyle = "#071013";
      ctx.fillRect(0, 0, rect.width, rect.height);
      ctx.strokeStyle = "#17343b";
      ctx.lineWidth = 1;
      for (let x = 20; x < rect.width; x += 40) {
        ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, rect.height); ctx.stroke();
      }
      for (let y = 20; y < rect.height; y += 40) {
        ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(rect.width, y); ctx.stroke();
      }
      project = null;
      if (!points.length) return;

      let minLat = points[0].lat, maxLat = points[0].lat;
      let minLon = points[0].lon, maxLon = points[0].lon;
      for (const point of points) {
        minLat = Math.min(minLat, point.lat); maxLat = Math.max(maxLat, point.lat);
        minLon = Math.min(minLon, point.lon); maxLon = Math.max(maxLon, point.lon);
      }
      const latRange = Math.max(maxLat - minLat, 0.00001);
      const lonRange = Math.max(maxLon - minLon, 0.00001);
      const pad = 24;
      project = (point) => ({
        x: pad + (point.lon - minLon) / lonRange * Math.max(1, rect.width - pad * 2),
        y: rect.height - pad - (point.lat - minLat) / latRange * Math.max(1, rect.height - pad * 2)
      });
      const drawPoints = downsampleGPS(points, Math.max(2, rect.width * 2));
      ctx.strokeStyle = "#27d3c2";
      ctx.lineWidth = 3;
      ctx.lineJoin = "round";
      ctx.beginPath();
      drawPoints.forEach((point, index) => {
        const p = project(point);
        if (index) ctx.lineTo(p.x, p.y); else ctx.moveTo(p.x, p.y);
      });
      ctx.stroke();
    }

    function paintCurrent(metrics) {
      const {ratio} = metrics;
      const ctx = canvas.getContext("2d");
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.drawImage(staticLayer, 0, 0);
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      if (current && project) {
        const marker = project(current);
        ctx.fillStyle = "#ffb224";
        ctx.beginPath();
        ctx.arc(marker.x, marker.y, 6, 0, Math.PI * 2);
        ctx.fill();
        ctx.strokeStyle = "#fff4d6";
        ctx.lineWidth = 2;
        ctx.stroke();
      }
    }

    function buildStaticLayer() {
      const metrics = currentMetrics();
      renderStaticLayer(metrics);
      paintCurrent(metrics);
    }

    function drawCurrent() {
      const metrics = currentMetrics();
      if (metrics.changed) renderStaticLayer(metrics);
      paintCurrent(metrics);
    }

    return {
      setData(nextPoints) { points = nextPoints; buildStaticLayer(); },
      setCurrent(point) { current = point; drawCurrent(); },
      resize: buildStaticLayer,
      destroy() { points = []; current = null; buildStaticLayer(); }
    };
  }

  function createProviderMap(provider, target, rawPoints) {
    const points = rawPoints.map(wgs84ToGcj02);
    if (provider === "amap") {
      const map = new window.AMap.Map(target, {zoom: 13, viewMode: "2D"});
      const path = points.map((point) => [point.lon, point.lat]);
      const line = new window.AMap.Polyline({path, strokeColor: "#16d9c4", strokeWeight: 5});
      const marker = new window.AMap.Marker({position: path[0]});
      map.add([line, marker]);
      if (path.length > 1) map.setFitView([line], false, [36, 36, 36, 36]);
      return {
        setCurrent(point) {
          const converted = wgs84ToGcj02(point);
          marker.setPosition([converted.lon, converted.lat]);
        },
        resize() { map.resize?.(); },
        destroy() { map.destroy?.(); }
      };
    }

    const center = new window.TMap.LatLng(points[0].lat, points[0].lon);
    const map = new window.TMap.Map(target, {center, zoom: 13});
    const path = points.map((point) => new window.TMap.LatLng(point.lat, point.lon));
    const line = new window.TMap.MultiPolyline({
      map,
      styles: {route: new window.TMap.PolylineStyle({color: "#16d9c4", width: 5})},
      geometries: [{id: "route", styleId: "route", paths: path}]
    });
    const marker = new window.TMap.MultiMarker({
      map,
      styles: {vehicle: new window.TMap.MarkerStyle({width: 18, height: 18})},
      geometries: [{id: "vehicle", styleId: "vehicle", position: path[0]}]
    });
    if (path.length > 1 && window.TMap.LatLngBounds) {
      const bounds = new window.TMap.LatLngBounds(path[0], path[0]);
      path.slice(1).forEach((point) => bounds.extend(point));
      map.fitBounds(bounds, {padding: 36});
    }
    return {
      setCurrent(point) {
        const converted = wgs84ToGcj02(point);
        marker.updateGeometries([{
          id: "vehicle",
          styleId: "vehicle",
          position: new window.TMap.LatLng(converted.lat, converted.lon)
        }]);
      },
      resize() {},
      destroy() { marker.setMap(null); line.setMap(null); map.destroy?.(); }
    };
  }

  function create(options) {
    const video = options.video;
    const elements = options.elements;
    const fallbackTranslations = {
      "telemetry.noData": "无数据",
      "telemetry.enabled": "已启用",
      "telemetry.active": "活跃",
      "telemetry.noAlert": "无告警",
      "telemetry.waitingTrace": "等待轨迹数据",
      "telemetry.waiting": "等待遥测数据",
      "telemetry.loading": "正在加载遥测…",
      "telemetry.none": "无遥测数据",
      "telemetry.noGPS": "无 GPS 轨迹",
      "telemetry.loadFailed": "遥测加载失败",
      "telemetry.state.disabled": "未启用",
      "telemetry.state.preEnabled": "预启用",
      "telemetry.state.enabled": "已启用",
      "telemetry.state.softDisabling": "软退出",
      "telemetry.state.overriding": "人工接管",
      "telemetry.state.unknown": "未知",
      "telemetry.chartTitle": "速度 / 控制",
      "telemetry.chartAriaValueText": "{seconds} 秒",
      "map.refreshKey": "刷新页面后应用新地图密钥",
      "map.sdkLoadFailed": "地图 SDK 加载失败",
      "map.sdkLoadTimeout": "地图 SDK 加载超时",
      "map.runtimeFailure": "地图运行异常，已切换无底图轨迹",
      "map.unavailable": "地图不可用，已切换无底图轨迹",
      "map.fallbackTrace": "无底图轨迹",
      "map.amapTrace": "高德地图轨迹",
      "map.tencentTrace": "腾讯地图轨迹"
    };
    const fallbackTranslate = (key, params = {}) => {
      const template = fallbackTranslations[key] || key;
      return template.replace(/\{([a-zA-Z0-9_]+)\}/g,
        (_, name) => Object.hasOwn(params, name) ? String(params[name]) : `{${name}}`);
    };
    let translate = options.translate || fallbackTranslate;
    const canvasMap = createCanvasMap(elements.mapCanvas);
    const chartStaticLayer = document.createElement("canvas");
    let telemetryGeneration = 0;
    let context = null;
    let mode = "route";
    let segmentNumber = 0;
    let cursor = 0;
    let providerMap = null;
    let resizeObserver = null;
    const fragmentTimingState = createFragmentTimingState();
    let data = emptyData();

    function emptyData() {
      return {duration: 0, maxSpeed: 0, speeds: [], gps: [], controls: [], overview: []};
    }

    function setTranslatedText(element, key, params) {
      element.dataset.i18n = key;
      if (params) element.dataset.i18nParams = JSON.stringify(params);
      else delete element.dataset.i18nParams;
      element.textContent = translate(key, params);
    }

    function setRawText(element, text) {
      delete element.dataset.i18n;
      delete element.dataset.i18nParams;
      element.textContent = text;
    }

    function refreshTranslatedText(element) {
      if (!element.dataset.i18n) return;
      const params = element.dataset.i18nParams
        ? JSON.parse(element.dataset.i18nParams)
        : undefined;
      setTranslatedText(element, element.dataset.i18n, params);
    }

    function resetUI() {
      data = emptyData();
      cursor = 0;
      elements.speed.textContent = "--";
      setTranslatedText(elements.state, "telemetry.noData");
      setBadge(elements.enabled, false, translate("telemetry.enabled"));
      setBadge(elements.active, false, translate("telemetry.active"));
      setTranslatedText(elements.alert, "telemetry.noAlert");
      setTranslatedText(elements.mapStatus, "telemetry.waitingTrace");
      setTranslatedText(elements.chartStatus, "telemetry.waiting");
      canvasMap.setData([]);
      destroyProviderMap();
      buildStaticLayer();
    }

    function setBadge(element, active, label) {
      element.textContent = label;
      element.dataset.on = active ? "true" : "false";
    }

    function destroyProviderMap() {
      try { providerMap?.destroy(); } catch (_) {}
      providerMap = null;
      elements.providerLayer.replaceChildren();
      elements.providerLayer.hidden = true;
      elements.mapCanvas.hidden = false;
    }

    function installResize() {
      if (resizeObserver) return;
      const resize = () => {
        canvasMap.resize();
        buildStaticLayer();
        try { providerMap?.resize(); } catch (_) { fallbackMap("map.runtimeFailure"); }
      };
      if (window.ResizeObserver) {
        resizeObserver = new ResizeObserver(resize);
        resizeObserver.observe(elements.mapCanvas.parentElement);
        resizeObserver.observe(elements.chart);
      } else {
        window.addEventListener("resize", resize);
        resizeObserver = {disconnect() { window.removeEventListener("resize", resize); }};
      }
    }

    async function open(nextContext, nextMode, nextSegment) {
      context = nextContext;
      mode = nextMode;
      segmentNumber = Number(nextSegment);
      installResize();
      await loadSelection();
    }

    async function selectionChanged(nextMode, nextSegment) {
      mode = nextMode;
      segmentNumber = Number(nextSegment);
      await loadSelection();
    }

    async function loadSelection() {
      const generation = ++telemetryGeneration;
      fragmentTimingState.clear();
      resetUI();
      if (!context) return;
      setTranslatedText(elements.chartStatus, "telemetry.loading");
      const sorted = [...context.summary.segments].sort((a, b) => a.number - b.number);
      const jobs = [];
      let playableOffset = 0;

      if (mode === "route") {
        for (const segment of sorted) {
          const duration = Number(segment.duration) || 0;
          data.overview.push({
            number: segment.number,
            start: playableOffset,
            duration,
            has_video: segment.has_video,
            has_telemetry: segment.has_telemetry,
            telemetry_error: segment.telemetry_error,
            runtime_load_failed: false
          });
          if (segment.has_video && segment.has_telemetry) {
            jobs.push({
              segment,
              playableOffset,
              segmentStart: playableOffset,
              segmentEnd: playableOffset + duration
            });
          }
          if (segment.has_video) playableOffset += duration;
        }
        data.duration = playableOffset;
      } else {
        const selected = sorted.find((segment) => segment.number === segmentNumber);
        if (!selected) return;
        data.duration = Number(selected.duration) || 0;
        data.overview.push({
          number: selected.number,
          start: 0,
          duration: data.duration,
          has_video: selected.has_video,
          has_telemetry: selected.has_telemetry,
          telemetry_error: selected.telemetry_error,
          runtime_load_failed: false
        });
        if (selected.has_telemetry) {
          jobs.push({
            segment: selected,
            playableOffset: 0,
            segmentStart: 0,
            segmentEnd: data.duration
          });
        }
      }

      let failed = 0;
      let nextJob = 0;
      async function worker() {
        while (nextJob < jobs.length) {
          const job = jobs[nextJob++];
          try {
            const result = await options.api(
              `${context.base}/segments/${encodeURIComponent(job.segment.number)}/telemetry`
            );
            if (generation !== telemetryGeneration || !context) return;
            const overview = data.overview.find((item) => item.number === job.segment.number);
            if (overview) overview.runtime_load_failed = false;
            options.onSegmentLoadState?.(job.segment.number, false);
            appendTelemetry(result, job);
          } catch (_) {
            if (generation !== telemetryGeneration) return;
            failed++;
            const overview = data.overview.find((item) => item.number === job.segment.number);
            if (overview) overview.runtime_load_failed = true;
            options.onSegmentLoadState?.(job.segment.number, true);
          }
        }
      }
      await Promise.all(Array.from({length: Math.min(3, jobs.length)}, worker));
      if (generation !== telemetryGeneration || !context) return;

      data.speeds.sort((a, b) => a.t - b.t);
      data.gps.sort((a, b) => a.t - b.t);
      data.controls.sort((a, b) => a.t - b.t);
      canvasMap.setData(data.gps);
      if (failed) {
        setTranslatedText(elements.chartStatus, "telemetry.loadFailed", {count: failed});
      } else if (data.speeds.length || data.controls.length) {
        setRawText(elements.chartStatus, "");
      } else {
        setTranslatedText(elements.chartStatus, "telemetry.none");
      }
      setTranslatedText(elements.mapStatus,
        data.gps.length ? "map.fallbackTrace" : "telemetry.noGPS");
      buildStaticLayer();
      sync(currentTime());
      if (data.gps.length) await enableConfiguredMap(generation);
    }

    function appendTelemetry(result, job) {
      for (const sample of result.speeds || []) {
        const value = Number(sample.v) * 3.6;
        if (Number.isFinite(value) && value > data.maxSpeed) data.maxSpeed = value;
        data.speeds.push({...sample, t: job.playableOffset + Number(sample.t)});
      }
      for (const sample of result.gps || []) {
        const lat = sample.lat;
        const lon = sample.lon;
        if (typeof lat !== "number" || typeof lon !== "number" ||
            !validGPSCoordinates(lat, lon)) continue;
        data.gps.push({...sample, lat, lon, t: job.playableOffset + Number(sample.t)});
      }
      for (const sample of result.controls || []) {
        data.controls.push({
          ...sample,
          t: job.playableOffset + Number(sample.t),
          segmentNumber: job.segment.number,
          segmentStart: job.segmentStart,
          segmentEnd: job.segmentEnd
        });
      }
    }

    async function enableConfiguredMap(generation) {
      const settings = options.getMapSettings();
      if (!settings || settings.map_provider === "none" || !settings.map_web_key) return;
      try {
        await loadSDK(settings);
        if (generation !== telemetryGeneration || !context) return;
        destroyProviderMap();
        elements.providerLayer.hidden = false;
        providerMap = createProviderMap(
          settings.map_provider,
          elements.providerLayer,
          downsampleGPS(data.gps, MAX_DRAW_POINTS)
        );
        elements.mapCanvas.hidden = true;
        setTranslatedText(elements.mapStatus,
          settings.map_provider === "amap" ? "map.amapTrace" : "map.tencentTrace");
        const point = latestSample(data.gps, cursor);
        if (point) providerMap.setCurrent(point);
      } catch (error) {
        if (generation !== telemetryGeneration) return;
        fallbackMap(error.i18nKey || "map.unavailable");
      }
    }

    function fallbackMap(key) {
      destroyProviderMap();
      setTranslatedText(elements.mapStatus, key);
      canvasMap.setCurrent(latestSample(data.gps, cursor));
    }

    function currentTime() {
      if (mode === "segment" && !selectedHasVideo()) return cursor;
      return fragmentMediaToCanonicalTime(
        video.currentTime, fragmentTimingState.current(), data.duration, video.duration
      );
    }

    function selectedHasVideo() {
      if (mode === "route") return context?.summary.segments.some((segment) => segment.has_video);
      return context?.summary.segments.some((segment) =>
        segment.number === segmentNumber && segment.has_video);
    }

    function sync(time) {
      cursor = Math.max(0, Number(time) || 0);
      const speed = latestSample(data.speeds, cursor);
      const control = latestSample(data.controls, cursor);
      const gps = latestSample(data.gps, cursor);
      elements.speed.textContent = speed ? (Number(speed.v) * 3.6).toFixed(1) : "--";
      if (control) setTranslatedText(elements.state, controlStateKey(control.state));
      else setTranslatedText(elements.state, "telemetry.noData");
      setBadge(elements.enabled, !!control?.enabled, translate("telemetry.enabled"));
      setBadge(elements.active, !!control?.active, translate("telemetry.active"));
      const alert = [control?.alert_text_1, control?.alert_text_2].filter(Boolean).join(" · ");
      if (alert) setRawText(elements.alert, alert);
      else setTranslatedText(elements.alert, "telemetry.noAlert");
      canvasMap.setCurrent(gps);
      if (providerMap && gps) {
        try { providerMap.setCurrent(gps); } catch (_) { fallbackMap("map.runtimeFailure"); }
      }
      drawChartCursor();
      options.onTime?.(cursor, data.duration);
    }

    function controlStateKey(state) {
      const known = new Set(["disabled", "preEnabled", "enabled", "softDisabling", "overriding"]);
      return `telemetry.state.${known.has(state) ? state : "unknown"}`;
    }

    function currentChartMetrics() {
      const rect = elements.chart.getBoundingClientRect();
      const ratio = window.devicePixelRatio || 1;
      const changed = ensureCanvasSize([elements.chart, chartStaticLayer], rect, ratio);
      return {rect, ratio, changed};
    }

    function renderChartStatic(metrics) {
      const {rect, ratio} = metrics;
      const ctx = chartStaticLayer.getContext("2d");
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      ctx.clearRect(0, 0, rect.width, rect.height);
      ctx.fillStyle = "#071013";
      ctx.fillRect(0, 0, rect.width, rect.height);
      const left = 36, right = 12, top = 16, bottom = 28;
      const plotWidth = Math.max(1, rect.width - left - right);
      const plotHeight = Math.max(1, rect.height - top - bottom);
      const duration = Math.max(data.duration, 0.001);
      const xAt = (time) => left + Math.max(0, Math.min(duration, time)) / duration * plotWidth;

      ctx.fillStyle = "#193139";
      for (const segment of data.overview) {
        if (!segment.has_video) {
          const x = xAt(segment.start);
          ctx.fillRect(Math.max(left, x - 3), top, 6, plotHeight);
        } else if (!segment.has_telemetry || segment.telemetry_error || segment.runtime_load_failed) {
          ctx.fillRect(xAt(segment.start), top, Math.max(2, xAt(segment.start + segment.duration) - xAt(segment.start)), plotHeight);
        }
      }

      const bandBins = Math.max(1, Math.min(4096, Math.ceil(plotWidth)));
      const enabledDiff = new Int32Array(bandBins + 1);
      const activeDiff = new Int32Array(bandBins + 1);
      for (let index = 0; index < data.controls.length; index++) {
        const sample = data.controls[index];
        const next = data.controls[index + 1];
        let end = sample.segmentEnd;
        if (next?.segmentNumber === sample.segmentNumber) end = Math.min(end, next.t);
        const startBin = Math.max(0, Math.min(bandBins - 1, Math.floor(sample.t / duration * bandBins)));
        const endBin = Math.max(startBin + 1, Math.min(bandBins, Math.ceil(end / duration * bandBins)));
        if (sample.enabled) {
          enabledDiff[startBin]++;
          enabledDiff[endBin]--;
        }
        if (sample.active) {
          activeDiff[startBin]++;
          activeDiff[endBin]--;
        }
      }
      const enabledBins = materializeBandBins(enabledDiff);
      const activeBins = materializeBandBins(activeDiff);
      drawBandBins(ctx, enabledBins, left, plotWidth, top + plotHeight - 16, "#1d6b5f66");
      drawBandBins(ctx, activeBins, left, plotWidth, top + plotHeight - 8, "#d7922466");

      if (data.speeds.length) {
        const maxSpeed = Math.max(1, data.maxSpeed);
        const drawSpeeds = downsampleSpeeds(data.speeds, Math.max(2, plotWidth * 2));
        ctx.strokeStyle = "#22d3c5";
        ctx.lineWidth = 2;
        ctx.beginPath();
        drawSpeeds.forEach((sample, index) => {
          const x = xAt(sample.t);
          const y = top + plotHeight - Number(sample.v) * 3.6 / maxSpeed * Math.max(1, plotHeight - 22);
          if (index) ctx.lineTo(x, y); else ctx.moveTo(x, y);
        });
        ctx.stroke();
      }
      ctx.fillStyle = "#91a8ae";
      ctx.font = "11px monospace";
      ctx.fillText(translate("telemetry.chartTitle"), left, rect.height - 8);
      elements.chart.setAttribute("aria-valuemax", String(Math.max(0, data.duration)));
    }

    function materializeBandBins(diff) {
      const bins = new Uint8Array(diff.length - 1);
      let active = 0;
      for (let index = 0; index < bins.length; index++) {
        active += diff[index];
        bins[index] = active > 0 ? 1 : 0;
      }
      return bins;
    }

    function drawBandBins(ctx, bins, left, plotWidth, y, color) {
      ctx.fillStyle = color;
      let start = -1;
      for (let index = 0; index <= bins.length; index++) {
        if (index < bins.length && bins[index] && start < 0) start = index;
        if ((index === bins.length || !bins[index]) && start >= 0) {
          const x = left + start / bins.length * plotWidth;
          const width = (index - start) / bins.length * plotWidth;
          ctx.fillRect(x, y, Math.max(1, width), 7);
          start = -1;
        }
      }
    }

    function paintChartCursor(metrics) {
      const canvas = elements.chart;
      const {rect, ratio} = metrics;
      const ctx = canvas.getContext("2d");
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.drawImage(chartStaticLayer, 0, 0);
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      const left = 36, right = 12, top = 16, bottom = 28;
      const plotWidth = Math.max(1, rect.width - left - right);
      const plotHeight = Math.max(1, rect.height - top - bottom);
      const duration = Math.max(data.duration, 0.001);
      const cursorX = left + Math.max(0, Math.min(duration, cursor)) / duration * plotWidth;
      ctx.strokeStyle = "#ffb224";
      ctx.lineWidth = 2;
      ctx.beginPath(); ctx.moveTo(cursorX, top); ctx.lineTo(cursorX, top + plotHeight); ctx.stroke();
      elements.chart.setAttribute("aria-valuenow", String(Math.max(0, Math.min(data.duration, cursor))));
      elements.chart.setAttribute("aria-valuetext",
        translate("telemetry.chartAriaValueText", {seconds: cursor.toFixed(1)}));
    }

    function buildStaticLayer() {
      const metrics = currentChartMetrics();
      renderChartStatic(metrics);
      paintChartCursor(metrics);
    }

    function drawChartCursor() {
      const metrics = currentChartMetrics();
      if (metrics.changed) renderChartStatic(metrics);
      paintChartCursor(metrics);
    }

    function seekFromPointer(event) {
      if (!context || data.duration <= 0) return;
      const rect = elements.chart.getBoundingClientRect();
      const left = 36;
      const width = Math.max(1, rect.width - left - 12);
      const time = Math.max(0, Math.min(data.duration, (event.clientX - rect.left - left) / width * data.duration));
      seekTo(time);
    }

    function seekTo(time) {
      const nextTime = Math.max(0, Math.min(data.duration, Number(time) || 0));
      if (selectedHasVideo()) {
        video.currentTime = fragmentCanonicalToMediaTime(
          nextTime, fragmentTimingState.current(), data.duration, video.duration
        );
      } else {
        cursor = nextTime;
      }
      sync(nextTime);
    }

    function seekFromKeyboard(event) {
      let time = cursor;
      if (event.key === "ArrowLeft") time -= 1;
      else if (event.key === "ArrowRight") time += 1;
      else if (event.key === "Home") time = 0;
      else if (event.key === "End") time = data.duration;
      else return;
      event.preventDefault();
      seekTo(time);
    }

    elements.chart.addEventListener("pointerdown", seekFromPointer);
    elements.chart.addEventListener("keydown", seekFromKeyboard);

    function cleanup() {
      telemetryGeneration++;
      context = null;
      fragmentTimingState.clear();
      data = emptyData();
      resizeObserver?.disconnect();
      resizeObserver = null;
      destroyProviderMap();
      canvasMap.destroy();
      const ctx = elements.chart.getContext("2d");
      ctx.clearRect(0, 0, elements.chart.width, elements.chart.height);
      resetUI();
    }

    function setTranslate(next) {
      translate = typeof next === "function" ? next : fallbackTranslate;
      const control = latestSample(data.controls, cursor);
      if (control) setTranslatedText(elements.state, controlStateKey(control.state));
      else setTranslatedText(elements.state, "telemetry.noData");
      setBadge(elements.enabled, !!control?.enabled, translate("telemetry.enabled"));
      setBadge(elements.active, !!control?.active, translate("telemetry.active"));
      const alert = [control?.alert_text_1, control?.alert_text_2].filter(Boolean).join(" · ");
      if (alert) setRawText(elements.alert, alert);
      else setTranslatedText(elements.alert, "telemetry.noAlert");
      refreshTranslatedText(elements.mapStatus);
      refreshTranslatedText(elements.chartStatus);
      buildStaticLayer();
    }

    return {
      open,
      selectionChanged,
      sync,
      setTranslate,
      syncVideoTime() { sync(currentTime()); },
      updateFragmentTiming(fragment, playableSegments) {
        const segments = playableSegments || data.overview
          .filter((segment) => segment.has_video)
          .map((segment) => ({number: segment.number, duration: segment.duration}));
        return fragmentTimingState.update(segments, fragment);
      },
      clearFragmentMapping() { fragmentTimingState.clear(); },
      mediaToCanonical(time) {
        return fragmentMediaToCanonicalTime(
          time, fragmentTimingState.current(), data.duration, video.duration
        );
      },
      canonicalToMedia(time) {
        return fragmentCanonicalToMediaTime(
          time, fragmentTimingState.current(), data.duration, video.duration
        );
      },
      parsedTimingCount() { return fragmentTimingState.knownCount(); },
      fragmentMapping() {
        return fragmentTimingState.current()?.map((segment) => ({...segment})) || null;
      },
      cleanup,
      latestSample,
      hasLoadedMapSDK() { return loadedSDKConfig.size > 0 || pendingSDKConfig.size > 0; }
    };
  }

  window.PilotTelemetry = {
    create,
    mediaToCanonicalTime,
    canonicalToMediaTime,
    createFragmentTimingState,
    fragmentMediaToCanonicalTime,
    fragmentCanonicalToMediaTime,
    latestSampleForTest: latestSample
  };
})();
