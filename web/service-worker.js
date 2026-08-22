self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(
      keys.filter((key) => key.startsWith("backup-chat-shell-")).map((key) => caches.delete(key))
    )).then(() => self.clients.claim())
  );
});

self.addEventListener("push", (event) => {
  console.info("[push] service worker received a push event");
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch (_) {
    data = { body: event.data ? event.data.text() : "New message" };
  }
  event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => {
    if (windows.some((window) => window.visibilityState === "visible")) {
      console.info("[push] notification skipped because the app is open");
      return;
    }
    return self.registration.showNotification(data.title || "Backup Chat", {
      body: data.body || "New message",
      icon: "/icon.svg",
      badge: "/icon.svg",
      tag: "backup-chat-message",
      data: { url: data.url || "/" }
    }).then(() => console.info("[push] notification displayed"));
  }).catch((error) => console.error("[push] notification handling failed", error)));
});

self.addEventListener("notificationclick", (event) => {
  console.info("[push] notification opened");
  event.notification.close();
  const url = event.notification.data && event.notification.data.url || "/";
  event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => {
    for (const window of windows) {
      if ("focus" in window) return window.focus();
    }
    return clients.openWindow(url);
  }));
});
