(function () {
  "use strict";

  const supported = new Set(["zh-CN", "en"]);
  const storageKey = "pilotserver_admin_language";

  function normalize(language) {
    return String(language || "").toLowerCase().startsWith("zh") ? "zh-CN" : "en";
  }

  function format(template, params = {}) {
    return template.replace(/\{([a-zA-Z0-9_]+)\}/g,
      (_, key) => Object.hasOwn(params, key) ? String(params[key]) : `{${key}}`);
  }

  const dictionaries = {
    "zh-CN": {
      "common.save": "保存",
      "common.cancel": "取消",
      "common.retry": "重试",
      "common.back": "返回",
      "common.loading": "正在加载…",
      "common.refresh": "刷新",
      "common.logout": "退出",
      "common.yes": "是",
      "common.no": "否",

      "login.title": "Pilotserver Admin",
      "login.password": "管理密码",
      "login.passwordPlaceholder": "请输入管理密码",
      "login.submit": "登录",

      "nav.primaryAria": "主导航",
      "nav.overview": "概览",
      "nav.devices": "设备",
      "nav.routes": "行程",
      "nav.settings": "设置",

      "topbar.serviceStatus": "服务状态",
      "topbar.serviceOk": "服务正常",
      "topbar.serviceError": "服务异常",
      "topbar.onlineDevices": "在线设备",
      "topbar.languageToggle": "English",
      "topbar.logout": "退出",

      "overview.title": "概览",
      "overview.onlineDevices": "在线设备",
      "overview.totalRoutes": "已存行程",
      "overview.lanEnabled": "局域网直连已启用",
      "overview.lanDisabled": "局域网直连未启用",
      "overview.mapProvider": "地图提供商",
      "overview.recentRoutes": "最近行程",

      "devices.title": "设备",
      "devices.refresh": "刷新设备",
      "devices.empty": "暂无设备",
      "devices.viewRoutes": "浏览行程",
      "devices.openSSH": "开 SSH",
      "devices.dongleId": "dongle_id",
      "devices.status": "状态",
      "devices.actions": "操作 / SSH 命令",

      "routes.title": "{dongleID} 行程",
      "routes.empty": "暂无行程",
      "routes.loadFailed": "加载行程失败，请重试",
      "routes.loadingFiles": "正在加载文件与回放摘要…",
      "routes.downloadFailed": "下载失败，请重试",
      "routes.loadFilesFailed": "加载文件失败，请重试",
      "routes.playOnline": "在线播放",
      "routes.viewTelemetry": "查看遥测",
      "routes.replaySummaryFailed": "回放摘要加载失败，请重试",

      "settings.title": "服务配置",
      "settings.publicBaseUrlHelp": "公网地址用于上传/SSH；本机监听仅 127.0.0.1，改端口后进程会热切换，请同步改 DSM 反向代理目标。",
      "settings.publicBaseUrlLabel": "公网基础地址",
      "settings.listenLabel": "监听地址",
      "settings.allowLan": "允许局域网直连（监听 0.0.0.0，取消后自动回到 127.0.0.1）",
      "settings.mapProvider": "地图底图",
      "settings.mapNone": "无底图轨迹",
      "settings.mapAmap": "高德地图",
      "settings.mapTencent": "腾讯地图",
      "settings.webKey": "Web Key",
      "settings.securityCode": "AMap security code",
      "settings.mapNote": "地图 Key 会发送到浏览器；请在地图服务商控制台限制允许域名。未配置 SDK 时使用内置无底图轨迹。",
      "settings.mapRefreshNote": "刷新页面后应用新地图密钥",
      "settings.save": "保存",
      "settings.configured": "公网地址已配置",
      "settings.notConfigured": "公网地址未配置（上传/SSH 对外不可用）",
      "settings.readFailed": "读取配置失败，请重试",
      "settings.saved": "已保存",
      "settings.savedListenChanged": "已保存（监听地址已热切换，请确认反向代理指向新端口）",

      "replay.playerReady": "播放器已就绪",
      "replay.fragmentFailed": "HLS 分段 {segment} 播放失败",
      "replay.noVideo": "该分段没有视频",
      "replay.requestingTicket": "正在申请播放凭证…",
      "replay.ticketFailed": "播放凭证获取失败，请重试",
      "replay.hlsFailed": "HLS 播放失败，请重试",
      "replay.hlsUnsupported": "此浏览器不支持 HLS 播放，请返回文件列表下载视频。",
      "replay.nativeHlsFailed": "原生 HLS 播放失败，请重试。",
      "replay.retry": "重试播放",
      "replay.skipSegment": "跳到下一可播放分段",
      "replay.modeRoute": "整条行程",
      "replay.modeSegment": "单个分段",
      "replay.previousSegment": "上一段",
      "replay.nextSegment": "下一段",
      "replay.backToFiles": "返回文件列表",
      "replay.playMode": "播放模式",
      "replay.videoSegment": "视频分段",
      "replay.segmentLabel": "分段 {number}",
      "replay.segmentTelemetryOnly": "分段 {number} · 仅遥测",
      "replay.wholeRoute": "整条行程",
      "replay.singleSegment": "单个分段 · {number}",
      "replay.playSegment": "播放分段 {number}",
      "replay.viewSegmentTelemetry": "查看分段 {number} 遥测",
      "replay.segmentMissingBoth": "分段 {number} 缺少视频和遥测",
      "replay.telemetryUnavailable": "（遥测不可用：{code}）",

      "telemetry.none": "无遥测数据",
      "telemetry.noGPS": "无 GPS 轨迹",
      "telemetry.noData": "无数据",
      "telemetry.noAlert": "无告警",
      "telemetry.waiting": "等待遥测数据",
      "telemetry.waitingTrace": "等待轨迹数据",
      "telemetry.loading": "正在加载遥测…",
      "telemetry.loadFailed": "{count} 个分段遥测加载失败",
      "telemetry.failedSegments": "{count} 个分段遥测损坏（分段 {segments}），其他分段仍可使用",
      "telemetry.enabled": "已启用",
      "telemetry.active": "活跃",
      "telemetry.state.disabled": "未启用",
      "telemetry.state.preEnabled": "预启用",
      "telemetry.state.enabled": "已启用",
      "telemetry.state.softDisabling": "软退出",
      "telemetry.state.overriding": "人工接管",
      "telemetry.state.unknown": "未知",
      "telemetry.hudSpeed": "车速",
      "telemetry.hudState": "控制状态",
      "telemetry.hudFlags": "系统状态",
      "telemetry.hudAlert": "当前告警",
      "telemetry.mapTitle": "行程轨迹",
      "telemetry.chartTitle": "速度 / 控制",
      "telemetry.chartAriaLabel": "速度与控制时间线",
      "telemetry.chartAriaValueText": "{seconds} 秒",
      "telemetry.segmentStripAria": "分段可播放及遥测状态",
      "telemetry.hudAriaLabel": "当前遥测状态",
      "telemetry.mapCanvasAria": "行程轨迹",
      "telemetry.mapProviderAria": "地图底图",

      "map.refreshKey": "刷新页面后应用新地图密钥",
      "map.sdkLoadFailed": "地图 SDK 加载失败",
      "map.sdkLoadTimeout": "地图 SDK 加载超时",
      "map.runtimeFailure": "地图运行异常，已切换无底图轨迹",
      "map.unavailable": "地图不可用，已切换无底图轨迹",
      "map.fallbackTrace": "无底图轨迹",
      "map.amapTrace": "高德地图轨迹",
      "map.tencentTrace": "腾讯地图轨迹",

      "errors.generic": "操作失败，请重试",
      "errors.loadFailed": "加载失败，请重试",
      "errors.loginFailed": "认证失败，请检查管理密码",
      "errors.saveFailed": "保存失败，请重试",
      "errors.sshFailed": "开启 SSH 失败，请重试",
      "errors.authenticationFailed": "认证失败，请检查管理密码",
      "errors.networkUnavailable": "网络连接失败，请稍后重试",
      "errors.serverUnavailable": "服务暂时不可用，请稍后重试",
      "errors.requestTimedOut": "请求超时，请重试",

      "status.online": "在线",
      "status.offline": "离线",
      "status.loading": "加载中"
    },
    en: {
      "common.save": "Save",
      "common.cancel": "Cancel",
      "common.retry": "Retry",
      "common.back": "Back",
      "common.loading": "Loading…",
      "common.refresh": "Refresh",
      "common.logout": "Log out",
      "common.yes": "Yes",
      "common.no": "No",

      "login.title": "Pilotserver Admin",
      "login.password": "Admin password",
      "login.passwordPlaceholder": "Enter admin password",
      "login.submit": "Log in",

      "nav.primaryAria": "Primary navigation",
      "nav.overview": "Overview",
      "nav.devices": "Devices",
      "nav.routes": "Routes",
      "nav.settings": "Settings",

      "topbar.serviceStatus": "Service status",
      "topbar.serviceOk": "Service OK",
      "topbar.serviceError": "Service error",
      "topbar.onlineDevices": "Online devices",
      "topbar.languageToggle": "中文",
      "topbar.logout": "Log out",

      "overview.title": "Overview",
      "overview.onlineDevices": "Online devices",
      "overview.totalRoutes": "Stored routes",
      "overview.lanEnabled": "LAN direct access enabled",
      "overview.lanDisabled": "LAN direct access disabled",
      "overview.mapProvider": "Map provider",
      "overview.recentRoutes": "Recent routes",

      "devices.title": "Devices",
      "devices.refresh": "Refresh devices",
      "devices.empty": "No devices",
      "devices.viewRoutes": "View routes",
      "devices.openSSH": "Open SSH",
      "devices.dongleId": "dongle_id",
      "devices.status": "Status",
      "devices.actions": "Actions / SSH command",

      "routes.title": "{dongleID} routes",
      "routes.empty": "No routes",
      "routes.loadFailed": "Failed to load routes. Please retry.",
      "routes.loadingFiles": "Loading files and replay summary…",
      "routes.downloadFailed": "Download failed. Please retry.",
      "routes.loadFilesFailed": "Failed to load files. Please retry.",
      "routes.playOnline": "Play online",
      "routes.viewTelemetry": "View telemetry",
      "routes.replaySummaryFailed": "Failed to load replay summary. Please retry.",

      "settings.title": "Service settings",
      "settings.publicBaseUrlHelp": "Public base URL is used for upload/SSH. Listen address defaults to 127.0.0.1; changing the port hot-reloads the process—update your reverse proxy target accordingly.",
      "settings.publicBaseUrlLabel": "Public base URL",
      "settings.listenLabel": "Listen address",
      "settings.allowLan": "Allow LAN direct access (listen on 0.0.0.0; uncheck to return to 127.0.0.1)",
      "settings.mapProvider": "Map base layer",
      "settings.mapNone": "Trace without map",
      "settings.mapAmap": "AMap",
      "settings.mapTencent": "Tencent Map",
      "settings.webKey": "Web Key",
      "settings.securityCode": "AMap security code",
      "settings.mapNote": "Map keys are sent to the browser; restrict allowed domains in your map provider console. Built-in trace rendering is used when no SDK is configured.",
      "settings.mapRefreshNote": "Refresh the page to apply a new map key",
      "settings.save": "Save",
      "settings.configured": "Public base URL configured",
      "settings.notConfigured": "Public base URL not configured (upload/SSH unavailable externally)",
      "settings.readFailed": "Failed to read settings. Please retry.",
      "settings.saved": "Saved",
      "settings.savedListenChanged": "Saved (listen address hot-reloaded—confirm reverse proxy points to the new port)",

      "replay.playerReady": "Player ready",
      "replay.fragmentFailed": "HLS segment {segment} playback failed",
      "replay.noVideo": "This segment has no video",
      "replay.requestingTicket": "Requesting playback ticket…",
      "replay.ticketFailed": "Failed to obtain playback ticket. Please retry.",
      "replay.hlsFailed": "HLS playback failed. Please retry.",
      "replay.hlsUnsupported": "This browser does not support HLS playback. Return to the file list to download video.",
      "replay.nativeHlsFailed": "Native HLS playback failed. Please retry.",
      "replay.retry": "Retry playback",
      "replay.skipSegment": "Skip to next playable segment",
      "replay.modeRoute": "Whole route",
      "replay.modeSegment": "Single segment",
      "replay.previousSegment": "Previous",
      "replay.nextSegment": "Next",
      "replay.backToFiles": "Back to file list",
      "replay.playMode": "Playback mode",
      "replay.videoSegment": "Video segment",
      "replay.segmentLabel": "Segment {number}",
      "replay.segmentTelemetryOnly": "Segment {number} · telemetry only",
      "replay.wholeRoute": "Whole route",
      "replay.singleSegment": "Single segment · {number}",
      "replay.playSegment": "Play segment {number}",
      "replay.viewSegmentTelemetry": "View segment {number} telemetry",
      "replay.segmentMissingBoth": "Segment {number} missing video and telemetry",
      "replay.telemetryUnavailable": " (telemetry unavailable: {code})",

      "telemetry.none": "No telemetry data",
      "telemetry.noGPS": "No GPS trace",
      "telemetry.noData": "No data",
      "telemetry.noAlert": "No alert",
      "telemetry.waiting": "Waiting for telemetry data",
      "telemetry.waitingTrace": "Waiting for trace data",
      "telemetry.loading": "Loading telemetry…",
      "telemetry.loadFailed": "Telemetry load failed for {count} segment(s)",
      "telemetry.failedSegments": "Telemetry is damaged in {count} segments ({segments}); other segments remain available",
      "telemetry.enabled": "ENABLED",
      "telemetry.active": "ACTIVE",
      "telemetry.state.disabled": "Disabled",
      "telemetry.state.preEnabled": "Pre-enabled",
      "telemetry.state.enabled": "Enabled",
      "telemetry.state.softDisabling": "Soft disabling",
      "telemetry.state.overriding": "Overriding",
      "telemetry.state.unknown": "Unknown",
      "telemetry.hudSpeed": "VEHICLE SPEED",
      "telemetry.hudState": "CONTROL STATE",
      "telemetry.hudFlags": "SYSTEM FLAGS",
      "telemetry.hudAlert": "CURRENT ALERT",
      "telemetry.mapTitle": "ROUTE TRACE",
      "telemetry.chartTitle": "SPEED / CONTROL",
      "telemetry.chartAriaLabel": "Speed and control timeline",
      "telemetry.chartAriaValueText": "{seconds} s",
      "telemetry.segmentStripAria": "Segment playback and telemetry status",
      "telemetry.hudAriaLabel": "Current telemetry status",
      "telemetry.mapCanvasAria": "Route trace",
      "telemetry.mapProviderAria": "Map base layer",

      "map.refreshKey": "Refresh the page to apply a new map key",
      "map.sdkLoadFailed": "Map SDK load failed",
      "map.sdkLoadTimeout": "Map SDK load timed out",
      "map.runtimeFailure": "Map runtime error; switched to trace without map",
      "map.unavailable": "Map unavailable; switched to trace without map",
      "map.fallbackTrace": "Trace without map",
      "map.amapTrace": "AMap trace",
      "map.tencentTrace": "Tencent Map trace",

      "errors.generic": "Operation failed. Please retry.",
      "errors.loadFailed": "Load failed. Please retry.",
      "errors.loginFailed": "Authentication failed. Check the admin password.",
      "errors.saveFailed": "Save failed. Please retry.",
      "errors.sshFailed": "Failed to open SSH. Please retry.",
      "errors.authenticationFailed": "Authentication failed. Check the admin password.",
      "errors.networkUnavailable": "Network connection failed. Please retry later.",
      "errors.serverUnavailable": "Service is temporarily unavailable. Please retry later.",
      "errors.requestTimedOut": "Request timed out. Please retry.",

      "status.online": "online",
      "status.offline": "offline",
      "status.loading": "loading"
    }
  };

  function memoryStorage() {
    const store = new Map();
    return {
      getItem(key) { return store.has(key) ? store.get(key) : null; },
      setItem(key, value) { store.set(key, String(value)); }
    };
  }

  function safeStorage(storage) {
    return {
      getItem(key) {
        try { return storage.getItem(key); } catch (_) { return null; }
      },
      setItem(key, value) {
        try { storage.setItem(key, value); } catch (_) {}
      }
    };
  }

  function resolveStorage(options) {
    if (options.storage) return safeStorage(options.storage);
    try {
      if (typeof localStorage !== "undefined") return safeStorage(localStorage);
    } catch (_) {}
    return memoryStorage();
  }

  function resolveLanguage(storage, languages) {
    const saved = storage.getItem(storageKey);
    if (saved && supported.has(saved)) return saved;
    const list = Array.isArray(languages) ? languages : [];
    for (const language of list) {
      const normalized = normalize(language);
      if (supported.has(normalized)) return normalized;
    }
    return "en";
  }

  function render(root, translate) {
    for (const element of root.querySelectorAll("[data-i18n]")) {
      const params = element.dataset.i18nParams
        ? JSON.parse(element.dataset.i18nParams)
        : undefined;
      element.textContent = translate(element.dataset.i18n, params);
    }
    for (const element of root.querySelectorAll("[data-i18n-placeholder]")) {
      element.placeholder = translate(element.dataset.i18nPlaceholder);
    }
    for (const element of root.querySelectorAll("[data-i18n-aria-label]")) {
      element.setAttribute("aria-label", translate(element.dataset.i18nAriaLabel));
    }
    for (const element of root.querySelectorAll("[data-i18n-title]")) {
      const params = element.dataset.i18nTitleParams
        ? JSON.parse(element.dataset.i18nTitleParams)
        : undefined;
      element.title = translate(element.dataset.i18nTitle, params);
      if (element.dataset.i18nTitleSuffix) {
        element.title += translate(
          element.dataset.i18nTitleSuffix,
          JSON.parse(element.dataset.i18nTitleSuffixParams)
        );
      }
    }
  }

  function errorKey(error, fallbackKey) {
    if (error?.endpoint === "/admin/api/login" && error.status === 401) {
      return "errors.authenticationFailed";
    }
    const byCode = {
      network_error: "errors.networkUnavailable",
      request_timeout: "errors.requestTimedOut"
    };
    if (byCode[error?.code]) return byCode[error.code];
    if (Number(error?.status) >= 500) return "errors.serverUnavailable";
    return fallbackKey || "errors.generic";
  }

  function localizeError(translate, error, fallbackKey) {
    return translate(errorKey(error, fallbackKey));
  }

  function create(options = {}) {
    const storage = resolveStorage(options);
    const listeners = [];
    let current = resolveLanguage(storage, options.languages);
    let notifying = false;
    const pendingLanguages = [];

    function t(key, params) {
      const dict = dictionaries[current] || dictionaries.en;
      const fallback = dictionaries.en[key];
      const template = dict[key] ?? fallback ?? key;
      return format(template, params);
    }

    function drainNotifications() {
      notifying = true;
      try {
        while (pendingLanguages.length) {
          const nextLanguage = pendingLanguages.shift();
          for (const listener of listeners.slice()) {
            try {
              listener(nextLanguage);
            } catch (_) {}
          }
        }
      } finally {
        notifying = false;
      }
    }

    return {
      language() { return current; },
      setLanguage(language) {
        if (!supported.has(language)) return;
        current = language;
        storage.setItem(storageKey, language);
        pendingLanguages.push(language);
        if (notifying) return;
        drainNotifications();
      },
      t,
      subscribe(listener) {
        listeners.push(listener);
        return () => {
          const index = listeners.indexOf(listener);
          if (index >= 0) listeners.splice(index, 1);
        };
      },
      dictionary() {
        return {...(dictionaries[current] || dictionaries.en)};
      }
    };
  }

  window.PilotI18n = {create, normalize, format, render, errorKey, localizeError};
})();
