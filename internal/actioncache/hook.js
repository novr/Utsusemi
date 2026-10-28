'use strict';

// Redirects Actions CacheService v2 Twirp calls to the guest-local cache server.
// Artifact and other Results paths stay on ACTIONS_RESULTS_URL.
// Baked into guest node via --require; do not rely on NODE_OPTIONS.

const http = require('http');
const https = require('https');

// Capture before patching so https→http redispatch does not re-enter the hook.
const rawHttpRequest = http.request.bind(http);

const LOCAL = 'http://127.0.0.1:19527';
const TWIRP =
  '/twirp/github.actions.results.api.v1.CacheService/';

function shouldRewrite(url) {
  if (!url) return false;
  const s = typeof url === 'string' ? url : url.href || String(url);
  return s.indexOf(TWIRP) !== -1;
}

function rewrite(url) {
  const s = typeof url === 'string' ? url : url.href || String(url);
  const idx = s.indexOf(TWIRP);
  if (idx === -1) return s;
  return LOCAL + s.slice(idx);
}

function rewriteOptions(options) {
  if (typeof options === 'string' || (options && options.href)) {
    if (!shouldRewrite(options)) return null;
    return rewrite(options);
  }
  if (!options || typeof options !== 'object') return null;
  const path = options.path || options.pathname || '';
  const host = options.host || options.hostname || '';
  const full =
    (options.protocol || 'https:') +
    '//' +
    host +
    (path.startsWith('/') ? path : '/' + path);
  if (!shouldRewrite(full) && !shouldRewrite(path)) return null;
  const u = new URL(rewrite(full.indexOf(TWIRP) !== -1 ? full : LOCAL + path));
  return Object.assign({}, options, {
    protocol: u.protocol,
    hostname: u.hostname,
    host: u.host,
    port: u.port,
    path: u.pathname + u.search,
    href: u.href,
    // Drop any https.Agent; local cache-serve is plain HTTP.
    agent: undefined,
  });
}

function patch(mod) {
  if (!mod || typeof mod.request !== 'function') return;
  const orig = mod.request;
  mod.request = function (options, callback) {
    try {
      const rewritten = rewriteOptions(options);
      if (rewritten !== null) {
        // ACTIONS_RESULTS_URL is https, so callers use https.request. After
        // rewrite the target is http://127.0.0.1 — must dispatch via http,
        // using the unpatched entrypoint to avoid re-entrancy.
        return rawHttpRequest(rewritten, callback);
      }
    } catch (_) {
      // fall through to original
    }
    return orig.call(this, options, callback);
  };
}

try {
  patch(http);
  patch(https);
} catch (_) {
  // never break non-cache actions
}
