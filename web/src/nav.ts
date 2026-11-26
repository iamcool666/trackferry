import { flushSync } from 'react-dom';

export type Page = 'welcome' | 'home' | 'search' | 'flow' | 'say' | 'library' | 'speakers' | 'account' | 'more' | 'tv';
export interface Place { page: Page; kind?: 'album' | 'artist' | 'playlist' | 'favorites' | 'playlists' | 'missing'; id?: string }

const pages: Page[] = ['welcome', 'home', 'search', 'flow', 'say', 'library', 'speakers', 'account', 'more', 'tv'];

export function readPlace(): Place | null {
  const [p, kind, ...rest] = location.hash.replace(/^#\/?/, '').split('/');
  const page = p === 'commands' ? 'home' : (p as Page);
  if (!pages.includes(page)) return null;
  if (page === 'library' && (kind === 'album' || kind === 'artist' || kind === 'playlist') && rest.length) {
    try { return { page, kind, id: decodeURIComponent(rest.join('/')) }; } catch { return { page }; }
  }
  if (page === 'library' && (kind === 'missing' || kind === 'favorites' || kind === 'playlists')) return { page, kind };
  if (page === 'tv' && /^[a-f0-9]{32}$/.test(kind ?? '')) return { page: 'home', id: kind };
  if (page === 'flow' && /^r?\d+$/.test(kind ?? '')) return { page, id: kind };
  return { page };
}

export function href(p: Place) {
  if (p.page === 'flow' && p.id) return `#/flow/${p.id}`;
  return `#/${p.page}${p.kind ? `/${p.kind}${p.id ? `/${encodeURIComponent(p.id)}` : ''}` : ''}`;
}

type VT = Document & { startViewTransition?: (f: () => void) => { finished: Promise<void>; ready: Promise<void>; updateCallbackDone: Promise<void> } };

/** Runs a state change as a view transition; `from` morphs into the element named "hero". */
export function morph(change: () => void, from?: Element | null) {
  const d = document as VT;
  const el = from as HTMLElement | null | undefined;
  // On a phone a transition freezes the whole screen to photograph it; only a cover that flies is worth that.
  // The press is painted first, then the page changes.
  if (!el && matchMedia('(max-width: 760px)').matches && !document.hidden) {
    let done = false;
    const go = () => { if (!done) { done = true; flushSync(change); } };
    requestAnimationFrame(() => setTimeout(go));
    setTimeout(go, 80); // frames may be held back
    return;
  }
  if (!d.startViewTransition || document.hidden || matchMedia('(prefers-reduced-motion: reduce)').matches) return change();
  if (el) el.style.viewTransitionName = 'hero';
  const t = d.startViewTransition(() => {
    if (el) el.style.viewTransitionName = '';
    flushSync(change);
  });
  for (const p of [t.ready, t.finished, t.updateCallbackDone]) p.catch(() => {});
}
