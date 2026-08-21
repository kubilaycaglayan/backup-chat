const CACHE_NAME = "backup-chat-shell-v3";
const APP_SHELL = [
  "/",
  "/app.js",
  "/style.css",
  "/manifest.webmanifest",
  "/icon.svg",
  "/service-worker.js"
];

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE_NAME).then((cache) => cache.addAll(APP_SHELL)));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(
      keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
    ))
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  // WebSocket upgrades must bypass the service worker, especially in installed PWAs.
  if (event.request.method !== "GET" || event.request.mode === "websocket") return;
  event.respondWith(
    fetch(event.request).catch(() => caches.match(event.request).then((cached) => cached || caches.match("/")))
  );
});

self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch (_) {
    data = { body: event.data ? event.data.text() : "New message" };
  }
  event.waitUntil(self.registration.showNotification(data.title || "Backup Chat", {
    body: data.body || "New message",
    icon: "/icon.svg",
    badge: "/icon.svg",
    tag: "backup-chat-message",
    data: { url: data.url || "/" }
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = event.notification.data && event.notification.data.url || "/";
  event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => {
    for (const window of windows) {
      if ("focus" in window) return window.focus();
    }
    return clients.openWindow(url);
  }));
});
