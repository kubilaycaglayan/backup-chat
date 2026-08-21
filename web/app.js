(() => {
  const nicknameStorageKey = "backup-chat-nickname";
  const nicknameStorageLifetime = 24 * 60 * 60 * 1000;
  const nicknameScreen = document.querySelector("#nickname-screen");
  const chatScreen = document.querySelector("#chat-screen");
  const nicknameForm = document.querySelector("#nickname-form");
  const nicknameInput = document.querySelector("#nickname");
  const nicknameError = document.querySelector("#nickname-error");
  const notificationPrompt = document.querySelector("#notification-prompt");
  const enableNotifications = document.querySelector("#enable-notifications");
  const notificationStatus = document.querySelector("#notification-status");
  const messageForm = document.querySelector("#message-form");
  const messageInput = document.querySelector("#message");
  const messages = document.querySelector("#messages");
  const chatError = document.querySelector("#chat-error");
  const connectionStatus = document.querySelector("#connection-status");
  let nickname = "";
  let socket;
  let pushPublicKey = "";
  let reconnectTimer;
  let reconnectAttempts = 0;

  function logPush(message, details) {
    if (details === undefined) console.info(`[push] ${message}`);
    else console.info(`[push] ${message}`, details);
  }

  function logConnection(message, details) {
    if (details === undefined) console.info(`[websocket] ${message}`);
    else console.info(`[websocket] ${message}`, details);
  }

  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/service-worker.js")
      .then(() => logPush("service worker registered"))
      .catch((error) => console.warn("[push] service worker registration failed", error));
  }

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
    const chosenNickname = nicknameInput.value.trim();
    if (!chosenNickname || [...chosenNickname].length > 32) {
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
          nickname: chosenNickname,
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

  async function setupNotifications() {
    try {
      const response = await fetch("/push/config");
      logPush("configuration API response", response.status);
      if (!response.ok) throw new Error("configuration request failed");
      const config = await response.json();
      pushPublicKey = config.publicKey || "";
      logPush("configuration received", { hasPublicKey: Boolean(pushPublicKey) });
      if (!pushPublicKey) return;
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
      if (Notification.permission === "granted" && nicknameInput.value.trim()) await subscribeToPush();
    } catch (error) {
      console.warn("[push] setup failed", error);
    }
  }

  function loadSavedNickname() {
    try {
      const saved = JSON.parse(localStorage.getItem(nicknameStorageKey));
      if (saved && typeof saved.nickname === "string" && typeof saved.savedAt === "number" &&
          Date.now() - saved.savedAt < nicknameStorageLifetime) {
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

  function connect() {
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
      if (!document.hidden) messageInput.focus();
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

  nicknameInput.value = loadSavedNickname();
  setupNotifications();

  enableNotifications.addEventListener("click", subscribeToPush);

  nicknameForm.addEventListener("submit", (event) => {
    event.preventDefault();
    nickname = nicknameInput.value.trim();
    if (!nickname || [...nickname].length > 32) { showError(nicknameError, "Use a nickname between 1 and 32 characters."); return; }
    saveNickname(nickname);
    showError(nicknameError, "");
    nicknameScreen.hidden = true;
    chatScreen.hidden = false;
    connect();
  });

  document.addEventListener("visibilitychange", () => {
    logConnection("visibility changed", document.hidden ? "hidden" : "visible");
    if (!document.hidden) connect();
  });
  window.addEventListener("online", () => {
    logConnection("network online");
    connect();
  });

  messageForm.addEventListener("submit", (event) => {
    event.preventDefault();
    const message = messageInput.value.trim();
    if (!message || [...message].length > 2000 || !socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify({ message }));
    messageInput.value = "";
    showError(chatError, "");
  });
})();
