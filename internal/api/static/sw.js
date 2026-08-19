"use strict";

const shellCache = "dmtx-console-shell-v1";
const shellAssets = ["/", "/static/console.css", "/static/console.js", "/static/icon.svg", "/manifest.webmanifest"];

self.addEventListener("install", event => event.waitUntil(caches.open(shellCache).then(cache => cache.addAll(shellAssets))));
self.addEventListener("activate", event => event.waitUntil(self.clients.claim()));
self.addEventListener("fetch", event => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith("/api/v1/")) return;
  event.respondWith(fetch(event.request).then(response => {
    // A 401 is an authorization decision, not a transient network outage. It
    // must never fall back to an old authenticated shell.
    if (!response.ok) return response;
    const copy = response.clone();
    caches.open(shellCache).then(cache => cache.put(event.request, copy)).catch(() => {});
    return response;
  }).catch(() => caches.match(event.request).then(cached => cached || Response.error())));
});
