(() => {
  const nicknameStorageKey = "backup-chat-nickname";
  const nicknameStorageLifetime = 24 * 60 * 60 * 1000;
  const nicknameScreen = document.querySelector("#nickname-screen");
  const chatScreen = document.querySelector("#chat-screen");
  const nicknameForm = document.querySelector("#nickname-form");
  const nicknameInput = document.querySelector("#nickname");
  const nicknameError = document.querySelector("#nickname-error");
  const messageForm = document.querySelector("#message-form");
  const messageInput = document.querySelector("#message");
  const messages = document.querySelector("#messages");
  const chatError = document.querySelector("#chat-error");
  const connectionStatus = document.querySelector("#connection-status");
  let nickname = "";
  let socket;

  if ("serviceWorker" in navigator) navigator.serviceWorker.register("/service-worker.js").catch(() => {});

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
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    socket = new WebSocket(`${protocol}//${location.host}/ws?nickname=${encodeURIComponent(nickname)}`);
    socket.addEventListener("open", () => { connectionStatus.textContent = "Connected"; messageInput.focus(); });
    socket.addEventListener("close", () => { connectionStatus.textContent = "Disconnected"; });
    socket.addEventListener("error", () => { showError(chatError, "Connection failed. Please reload the page."); });
    socket.addEventListener("message", (event) => {
      try {
        const value = JSON.parse(event.data);
        if (value.error) showError(chatError, value.error);
        else if (value.timestamp && typeof value.nickname === "string" && typeof value.message === "string") addMessage(value);
      } catch (_) { showError(chatError, "Received an invalid message."); }
    });
  }

  nicknameInput.value = loadSavedNickname();

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

  messageForm.addEventListener("submit", (event) => {
    event.preventDefault();
    const message = messageInput.value.trim();
    if (!message || [...message].length > 2000 || !socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify({ message }));
    messageInput.value = "";
    showError(chatError, "");
  });
})();
