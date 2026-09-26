export const accountPageRoots = [
  '/connexion',
  '/inscription',
  '/verification',
  '/finaliser',
  '/mot-de-passe-oublie',
  '/reinitialiser-mot-de-passe',
  '/compte',
]

export function isAccountPage(path: string): boolean {
  const normalized = normalizeAccountPath(path)
  return accountPageRoots.some(
    (root) => normalized === root || normalized.startsWith(`${root}/`),
  )
}

export function isAccountPrivatePath(path: string): boolean {
  return (
    isAccountPage(path) ||
    /^\/api\/v1\/(auth|account)(\/|$)/.test(normalizeAccountPath(path))
  )
}

function normalizeAccountPath(path: string): string {
  // Vue route matching is case-insensitive; privacy must not depend on casing.
  try {
    return decodeURIComponent(path)
      .toLowerCase()
      .replace(/\\/g, '/')
      .replace(/\/{2,}/g, '/')
  } catch {
    return path.toLowerCase()
  }
}

export const accountPrivacyHeaders = {
  'Cache-Control': 'private, no-store',
  Vary: 'Cookie',
  'X-Robots-Tag': 'noindex, nofollow',
  'Referrer-Policy': 'no-referrer',
}

// Runs first in the document, before Nuxt, analytics, or optional scripts.
// The only retained copy is a one-shot closure, never a payload or storage value.
export const accountFragmentBootstrap = `(function(){
  var fragment=location.hash;
  if(fragment) history.replaceState(history.state,'',location.pathname+location.search);
  var params=new URLSearchParams(fragment.slice(1));
  var value=params.getAll('token');
  var token=value.length===1&&/^[A-Za-z0-9_-]{43}$/.test(value[0])?value[0]:'';
  var path=location.pathname;
  window.__takeAccountToken=function(target){var result=!target||target===path?token:'';token='';delete window.__takeAccountToken;return result;};
})();`
