// tsession's service worker exists only to satisfy the browser's PWA
// installability checks (Chrome/Edge require a registered service worker
// with a fetch handler before showing an install prompt). It intentionally
// does very little: the app itself is inherently dynamic (live session
// list, live PTY output over WebSocket, SSE notifications), so there is no
// meaningful "offline" mode to build here — this only gives the static
// shell (HTML/CSS/JS/vendor/icons) a network-first path so application
// updates take effect immediately while cached assets remain available if
// the server is briefly unreachable. Every other
// request (notably /api/* and the /api/terminal WebSocket upgrade) passes
// straight through to the network, untouched and uncached.
const CACHE_NAME = "tsession-shell-v7";
const SHELL_PATHS = [
  "/",
  "/app.css",
  "/app.js",
  "/manifest.json",
  "/vendor/xterm.css",
  "/vendor/xterm.js",
  "/vendor/addon-fit.js",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(SHELL_PATHS))
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((names) =>
      Promise.all(
        names
          .filter((name) => name !== CACHE_NAME)
          .map((name) => caches.delete(name))
      )
    )
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);

  // Only ever intervene for same-origin GETs of the static shell itself.
  // Everything else — /api/sessions polling, /api/events (SSE), and
  // especially the /api/terminal WebSocket upgrade — must reach the
  // network unmodified; caching or intercepting those would break live
  // session data and PTY streaming.
  const isShellRequest =
    event.request.method === "GET" &&
    url.origin === self.location.origin &&
    (SHELL_PATHS.includes(url.pathname) || url.pathname.startsWith("/icons/"));

  if (!isShellRequest) return;

  event.respondWith(
    caches.match(event.request).then((cached) => {
      const network = fetch(event.request)
        .then((resp) => {
          if (resp.ok) {
            const copy = resp.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
          }
          return resp;
        });
      return network.catch(() => cached);
    })
  );
});
