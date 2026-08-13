"use strict";

// This is deliberately a shell-only cache. Do not add /api/v1 routes here:
// their data is authenticated, time-sensitive operator state.
const shellCache = "dmtx-console-shell-v1";
const shellAssets = [
  "/static/console.css",
  "/static/console.js",
  "/static/manifest.webmanifest",
  "/static/icon-192.png",
  "/static/icon-512.png",
  "/static/icon.svg"
];

self.addEventListener("install", event => {
  event.waitUntil(caches.open(shellCache).then(cache => cache.addAll(shellAssets)));
  self.skipWaiting();
});

self.addEventListener("activate", event => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("fetch", event => {
  // API requests always bypass Cache Storage. This applies to all methods and
  // protects credential-bearing as well as ordinary API responses.
  if (new URL(event.request.url).pathname.startsWith("/api/v1/")) return;
  if (event.request.method !== "GET") return;
  // The document response is authenticated too. It is deliberately fetched
  // live rather than cached: this cache is only for static, credential-free
  // shell bytes.
  if (new URL(event.request.url).pathname === "/") return;
  event.respondWith(caches.match(event.request).then(cached => cached || fetch(event.request)));
});
