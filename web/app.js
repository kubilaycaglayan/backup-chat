(() => {
  const nicknameStorageKey = "backup-chat-nickname";
  const appVersion = "20";
  const nicknameScreen = document.querySelector("#nickname-screen");
  const chatScreen = document.querySelector("#chat-screen");
  const nicknameForm = document.querySelector("#nickname-form");
  const nicknameInput = document.querySelector("#nickname");
  const nicknameError = document.querySelector("#nickname-error");
  const notificationPrompt = document.querySelector("#notification-prompt");
  const enableNotifications = document.querySelector("#enable-notifications");
  const dismissNotifications = document.querySelector("#dismiss-notifications");
  const notificationStatus = document.querySelector("#notification-status");
  const updateAvailable = document.querySelector("#update-available");
  const messageForm = document.querySelector("#message-form");
  const messageInput = document.querySelector("#message");
  const messages = document.querySelector("#messages");
  const chatError = document.querySelector("#chat-error");
  const connectionStatus = document.querySelector("#connection-status");
  const app = document.querySelector(".app");
  let nickname = "";
  let socket;
  let pushPublicKey = "";
  let reconnectTimer;
  let reconnectAttempts = 0;
  let serviceWorkerRegistration;
  let updateRequested = false;

  function setupLayoutDebugger() {
    const debugStorageKey = "backup-chat-layout-debug";
    const requestedMode = new URLSearchParams(window.location.search).get("debug");
    try {
      if (requestedMode === "layout") localStorage.setItem(debugStorageKey, "1");
      if (requestedMode === "off") localStorage.removeItem(debugStorageKey);
      if (requestedMode !== "layout" && localStorage.getItem(debugStorageKey) !== "1") return;
    } catch (_) {
      if (requestedMode !== "layout") return;
    }

    document.documentElement.classList.add("layout-debug");
    const panel = document.createElement("pre");
    panel.className = "layout-debug-panel";
    panel.setAttribute("aria-live", "off");
    document.body.append(panel);
    let lastEvent = "debugger started";
    let lastTouchY = 0;

    function nameOf(target) {
      if (target === window) return "window";
      if (target === document) return "document";
      if (!(target instanceof Element)) return "unknown";
      if (target.id) return `#${target.id}`;
      return target.className ? `.${String(target.className).replace(/\s+/g, ".")}` : target.tagName.toLowerCase();
    }

    function rounded(value) { return Math.round(Number(value) || 0); }

    function renderLayoutDebug() {
      const viewport = window.visualViewport;
      const chatRect = chatScreen.getBoundingClientRect();
      const composerRect = messageForm.getBoundingClientRect();
      panel.textContent = [
        `LAYOUT DEBUG · ${window.matchMedia("(display-mode: standalone)").matches || navigator.standalone ? "standalone" : "browser"}`,
        "blue=app  pink=chat  green=messages  orange=composer",
        `screen ${screen.width}×${screen.height}  inner ${innerWidth}×${innerHeight}`,
        `visual h:${rounded(viewport && viewport.height)} top:${rounded(viewport && viewport.offsetTop)} pageTop:${rounded(viewport && viewport.pageTop)}`,
        `doc clientH:${document.documentElement.clientHeight} scrollY:${rounded(scrollY)} html:${rounded(document.documentElement.scrollTop)} body:${rounded(document.body.scrollTop)}`,
        `app scroll:${rounded(app.scrollTop)} h:${rounded(app.clientHeight)}/${rounded(app.scrollHeight)}`,
        `chat top:${rounded(chatRect.top)} bottom:${rounded(chatRect.bottom)} h:${rounded(chatRect.height)}`,
        `composer top:${rounded(composerRect.top)} bottom:${rounded(composerRect.bottom)}`,
        `messages scroll:${rounded(messages.scrollTop)} h:${rounded(messages.clientHeight)}/${rounded(messages.scrollHeight)}`,
        `active ${nameOf(document.activeElement)}  keyboard:${document.documentElement.classList.contains("keyboard-open")}`,
        `last ${lastEvent}`
      ].join("\n");
    }

    window.addEventListener("scroll", () => { lastEvent = "WINDOW SCROLL"; }, { passive: true });
    document.addEventListener("scroll", (event) => { lastEvent = `scroll ${nameOf(event.target)}`; }, { capture: true, passive: true });
    document.addEventListener("touchstart", (event) => {
      const touch = event.touches[0];
      lastTouchY = touch ? touch.clientY : 0;
      lastEvent = `touchstart ${nameOf(event.target)} y:${rounded(lastTouchY)}`;
    }, { capture: true, passive: true });
    document.addEventListener("touchmove", (event) => {
      const touch = event.touches[0];
      const nextY = touch ? touch.clientY : lastTouchY;
      lastEvent = `touchmove ${nameOf(event.target)} Δ:${rounded(nextY - lastTouchY)} prevented:${event.defaultPrevented}`;
      lastTouchY = nextY;
    }, { passive: true });
    window.visualViewport?.addEventListener("resize", () => { lastEvent = "visual viewport resize"; }, { passive: true });
    window.visualViewport?.addEventListener("scroll", () => { lastEvent = "VISUAL VIEWPORT SCROLL"; }, { passive: true });
    setInterval(renderLayoutDebug, 100);
    renderLayoutDebug();
  }

  function logPush(message, details) {
    if (details === undefined) console.info(`[push] ${message}`);
    else console.info(`[push] ${message}`, details);
  }

  function logConnection(message, details) {
    if (details === undefined) console.info(`[websocket] ${message}`);
    else console.info(`[websocket] ${message}`, details);
  }

  async function checkForUpdate() {
    try {
      const response = await fetch("/version.json", { cache: "no-store" });
      if (!response.ok) throw new Error("version request failed");
      const version = (await response.json()).version;
      updateAvailable.hidden = version === appVersion;
    } catch (error) {
      console.warn("[update] version check failed", error);
    }
  }

  async function refreshUpdateCheck() {
    if (serviceWorkerRegistration) {
      try {
        await serviceWorkerRegistration.update();
      } catch (error) {
        console.warn("[update] service worker update check failed", error);
      }
    }
    await checkForUpdate();
  }

  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/service-worker.js", { updateViaCache: "none" })
      .then((registration) => {
        serviceWorkerRegistration = registration;
        return registration.update();
      })
      .then(() => {
        logPush("service worker registered and checked for updates");
        return refreshUpdateCheck();
      })
      .catch((error) => console.warn("[push] service worker registration failed", error));

    navigator.serviceWorker.addEventListener("controllerchange", () => {
      if (updateRequested) window.location.reload();
    });
  }

  // Keep an open chat aware of a deployment without requiring a reload or
  // foreground/background transition first.
  setInterval(() => {
    if (!document.hidden) refreshUpdateCheck();
  }, 30000);

  function setNotificationStatus(message) { notificationStatus.textContent = message || ""; }

  function base64ToBytes(value) {
    const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized + "=".repeat((4 - normalized.length % 4) % 4);
    const binary = atob(padded);
    return Uint8Array.from(binary, (character) => character.charCodeAt(0));
  }

  function sameBytes(first, second) {
    if (first.length !== second.length) return false;
    return first.every((value, index) => value === second[index]);
  }

  async function subscribeToPush() {
    if (!nickname || [...nickname].length > 32) {
      setNotificationStatus("Choose a nickname first, then enable notifications.");
      return;
    }
    try {
      const permission = Notification.permission === "granted" ? "granted" : await Notification.requestPermission();
      logPush("notification permission", permission);
      if (permission !== "granted") {
        setNotificationStatus("Notifications are not enabled.");
        notificationPrompt.hidden = true;
        return;
      }
      const registration = await navigator.serviceWorker.ready;
      let subscription = await registration.pushManager.getSubscription();
    const subscriptionKey = subscription && subscription.options && subscription.options.applicationServerKey;
    if (subscriptionKey && !sameBytes(new Uint8Array(subscriptionKey), base64ToBytes(pushPublicKey))) {
    logPush("replacing browser push subscription after VAPID key change");
    await subscription.unsubscribe();
    subscription = null;
    }
      if (!subscription) {
        subscription = await registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: base64ToBytes(pushPublicKey)
        });
        logPush("created browser push subscription");
      } else {
        logPush("using existing browser push subscription");
      }
      const serialized = subscription.toJSON();
      const response = await fetch("/push/subscribe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          nickname,
          subscription: { endpoint: serialized.endpoint, keys: serialized.keys }
        })
      });
      logPush("subscription API response", response.status);
      if (!response.ok) throw new Error("subscription request failed");
      notificationPrompt.hidden = true;
      logPush("push subscription saved");
    } catch (error) {
      console.warn("[push] subscription failed", error);
      setNotificationStatus("Notifications could not be enabled. Try again later.");
    }
  }

  async function updateNotificationPrompt() {
    if (sessionStorage.getItem("backup-chat-notification-prompt-dismissed") === "1") {
      notificationPrompt.hidden = true;
      return;
    }
    if (!nickname || !pushPublicKey) {
      notificationPrompt.hidden = true;
      return;
    }
    notificationPrompt.hidden = false;
    if ("Notification" in window && Notification.permission === "denied") {
      enableNotifications.disabled = true;
      setNotificationStatus("Notifications are blocked. Allow them for this app in iOS Settings.");
      return;
    }
    if (!window.isSecureContext) {
      enableNotifications.disabled = true;
      setNotificationStatus("Notifications require HTTPS. Open the installed app from an HTTPS address.");
      return;
    }
    if (!("Notification" in window) || !("PushManager" in window) || !("serviceWorker" in navigator)) {
      enableNotifications.disabled = true;
      setNotificationStatus("Push notifications are unavailable here. Use iOS 16.4+ and open the installed Home Screen app.");
      return;
    }
    try {
      const registration = await navigator.serviceWorker.ready;
      const subscription = await registration.pushManager.getSubscription();
      if (Notification.permission === "granted" && subscription) {
        notificationPrompt.hidden = true;
        setNotificationStatus("");
        return;
      }
    } catch (error) {
      console.warn("[push] could not check existing subscription", error);
    }
    enableNotifications.disabled = false;
    setNotificationStatus("");
  }

  async function setupNotifications() {
    try {
      const response = await fetch("/push/config");
      logPush("configuration API response", response.status);
      if (!response.ok) throw new Error("configuration request failed");
      const config = await response.json();
      pushPublicKey = config.publicKey || "";
      logPush("configuration received", { hasPublicKey: Boolean(pushPublicKey) });
      if (!pushPublicKey) return;
      updateNotificationPrompt();
    } catch (error) {
      console.warn("[push] setup failed", error);
    }
  }

  function loadSavedNickname() {
    try {
      const saved = JSON.parse(localStorage.getItem(nicknameStorageKey));
      if (saved && typeof saved.nickname === "string") {
        return saved.nickname;
      }
      localStorage.removeItem(nicknameStorageKey);
    } catch (_) {
      return "";
    }
    return "";
  }

  function saveNickname(value) {
    try {
      localStorage.setItem(nicknameStorageKey, JSON.stringify({ nickname: value, savedAt: Date.now() }));
    } catch (_) {}
  }

  function showError(element, message) { element.textContent = message || ""; }

  function clearReconnectTimer() {
    if (!reconnectTimer) return;
    clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
  }

  function scheduleReconnect() {
    if (!nickname || reconnectTimer) return;
    const delay = Math.min(1000 * 2 ** reconnectAttempts, 15000);
    reconnectAttempts += 1;
    logConnection("reconnect scheduled", { attempt: reconnectAttempts, delay });
    connectionStatus.textContent = "Reconnecting…";
    reconnectTimer = setTimeout(() => {
      reconnectTimer = undefined;
      connect();
    }, delay);
  }

  function addMessage(message) {
    const item = document.createElement("article");
    item.className = message.nickname === nickname ? "message own" : "message";
    const meta = document.createElement("div");
    meta.className = "meta";
    const time = new Date(message.timestamp);
    meta.textContent = `${message.nickname} · ${Number.isNaN(time.getTime()) ? "" : time.toLocaleString()}`;
    const body = document.createElement("p");
    body.textContent = message.message;
    item.append(meta, body);
    const wasNearBottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight < 80;
    messages.append(item);
    if (wasNearBottom) messages.scrollTop = messages.scrollHeight;
  }

  function connect(focusComposer = false) {
    if (!nickname || (socket && (socket.readyState === WebSocket.CONNECTING || socket.readyState === WebSocket.OPEN))) {
      logConnection("connection attempt skipped", { hasNickname: Boolean(nickname), readyState: socket && socket.readyState });
      return;
    }
    clearReconnectTimer();
    connectionStatus.textContent = "Connecting…";
    messages.textContent = "";
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    logConnection("connecting");
    const connection = new WebSocket(`${protocol}//${location.host}/ws?nickname=${encodeURIComponent(nickname)}`);
    socket = connection;
    connection.addEventListener("open", () => {
      if (socket !== connection) return;
      reconnectAttempts = 0;
      logConnection("connected");
      connectionStatus.textContent = "Connected";
      if (focusComposer && !document.hidden) messageInput.focus();
    });
    connection.addEventListener("close", (event) => {
      logConnection("closed", { code: event.code, reason: event.reason || "none", wasClean: event.wasClean });
      if (socket === connection) scheduleReconnect();
    });
    connection.addEventListener("error", () => {
      logConnection("connection error");
      if (socket === connection) connectionStatus.textContent = "Connection failed";
    });
    connection.addEventListener("message", (event) => {
      if (socket !== connection) return;
      try {
        const value = JSON.parse(event.data);
        if (value.error) showError(chatError, value.error);
        else if (value.timestamp && typeof value.nickname === "string" && typeof value.message === "string") addMessage(value);
      } catch (_) { showError(chatError, "Received an invalid message."); }
    });
  }

  function enterChat(value, focusComposer) {
    nickname = value.trim();
    if (!nickname || [...nickname].length > 32) {
      showError(nicknameError, "Use a nickname between 1 and 32 characters.");
      return false;
    }
    saveNickname(nickname);
    showError(nicknameError, "");
    nicknameScreen.hidden = true;
    chatScreen.hidden = false;
    document.body.classList.add("chat-open");
    updateNotificationPrompt();
    connect(focusComposer);
    return true;
  }

  nicknameInput.value = loadSavedNickname();
  setupLayoutDebugger();
  setupNotifications();

  enableNotifications.addEventListener("click", subscribeToPush);
  dismissNotifications.addEventListener("click", () => {
    sessionStorage.setItem("backup-chat-notification-prompt-dismissed", "1");
    notificationPrompt.hidden = true;
  });
  updateAvailable.addEventListener("click", () => {
    updateRequested = true;
    if (serviceWorkerRegistration && serviceWorkerRegistration.waiting) {
      serviceWorkerRegistration.waiting.postMessage({ type: "SKIP_WAITING" });
      return;
    }
    window.location.reload();
  });

  nicknameForm.addEventListener("submit", (event) => {
    event.preventDefault();
    enterChat(nicknameInput.value, true);
  });

  if (nicknameInput.value.trim()) enterChat(nicknameInput.value, false);

  document.addEventListener("visibilitychange", () => {
    logConnection("visibility changed", document.hidden ? "hidden" : "visible");
    if (!document.hidden) {
      connect();
      refreshUpdateCheck();
    }
  });
  window.addEventListener("online", () => {
    logConnection("network online");
    connect();
  });

  function updateVisualViewport() {
    const viewport = window.visualViewport;
    if (!viewport) return;
    const keyboardOpen = window.innerHeight - viewport.height > 120;
    document.documentElement.style.setProperty("--app-viewport-height", `${viewport.height}px`);
    document.documentElement.style.setProperty("--app-viewport-top", `${viewport.offsetTop}px`);
    document.documentElement.classList.toggle("keyboard-open", keyboardOpen);
  }

  if (window.visualViewport) {
    window.visualViewport.addEventListener("resize", updateVisualViewport);
    window.visualViewport.addEventListener("scroll", updateVisualViewport);
    updateVisualViewport();
  }

  messageForm.querySelector("button").addEventListener("pointerdown", (event) => {
    // iOS moves focus to a tapped button unless its default focus behavior is prevented.
    event.preventDefault();
  });

  let messageListTouch = false;
  chatScreen.addEventListener("touchstart", (event) => {
    messageListTouch = Boolean(event.target.closest("#messages"));
  }, { passive: true });
  chatScreen.addEventListener("touchmove", (event) => {
    // Safari can otherwise scroll the fixed app shell while the keyboard is open.
    if (!messageListTouch) event.preventDefault();
  }, { passive: false });
  chatScreen.addEventListener("touchend", () => { messageListTouch = false; }, { passive: true });
  chatScreen.addEventListener("touchcancel", () => { messageListTouch = false; }, { passive: true });

  messageForm.addEventListener("submit", (event) => {
    event.preventDefault();
    const keepKeyboardOpen = document.activeElement === messageInput;
    const message = messageInput.value.trim();
    if (!message || [...message].length > 2000 || !socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify({ message }));
    messageInput.value = "";
    showError(chatError, "");
    if (keepKeyboardOpen) requestAnimationFrame(() => messageInput.focus({ preventScroll: true }));
  });
})();
