import { useSyncExternalStore } from 'react';

interface Prompt extends Event { prompt: () => Promise<unknown> }
let prompt: Prompt | null = null;
const heard = new Set<() => void>();
const tell = (p: Prompt | null) => { prompt = p; heard.forEach((f) => f()); };

// Browsers allow both on HTTPS and on localhost only.
addEventListener('beforeinstallprompt', (e) => { e.preventDefault(); tell(e as Prompt); });
addEventListener('appinstalled', () => tell(null));
if (import.meta.env.PROD && 'serviceWorker' in navigator && isSecureContext) addEventListener('load', () => { navigator.serviceWorker.register('/sw.js').catch(() => {}); });

// Installed, it moves like an app: no zooming. In a browser tab zooming stays.
const standalone = matchMedia('(display-mode: standalone)');
function settle() {
  const on = standalone.matches || (navigator as { standalone?: boolean }).standalone === true;
  document.documentElement.classList.toggle('installed', on);
  document.querySelector('meta[name=viewport]')?.setAttribute('content', `width=device-width, initial-scale=1.0${on ? ', maximum-scale=1.0, user-scalable=no' : ''}, viewport-fit=cover`);
}
settle();
standalone.addEventListener('change', settle);

// Installed on iOS, the page is sometimes drawn from the top of the screen, under the status bar, yet
// keeps the height it would have below it: the status bar's height goes missing at the bottom
// (393x852 screen, 59 safe top, 793 viewport). A scroll makes iOS measure again; 此刻 does not scroll.
// --vp-short is what is missing, 0 whenever the page already fills the screen.
const top = document.createElement('div');
top.style.cssText = 'position:fixed;left:-9999px;top:0;width:1px;height:0;padding-top:env(safe-area-inset-top);visibility:hidden;pointer-events:none';
function measure() {
  const root = document.documentElement;
  if (!top.isConnected) document.body?.appendChild(top);
  const tall = innerHeight >= innerWidth ? Math.max(screen.width, screen.height) : Math.min(screen.width, screen.height);
  const short = root.classList.contains('installed') ? Math.max(0, Math.min(top.offsetHeight, tall - innerHeight)) : 0;
  if (short) root.style.setProperty('--vp-short', `${short}px`);
  else root.style.removeProperty('--vp-short');
  if (short) nudge();
}
// iOS measures the page again after a scroll, but a page shorter than the screen cannot be scrolled, so the
// missing strip stayed black under the dock. Make the page one pixel taller, scroll it by that pixel and
// back, then drop the pixel. At most once a second, so a phone where it does not help is not kept busy.
let nudged = 0;
function nudge() {
  if (Date.now() - nudged < 1000) return;
  nudged = Date.now();
  const root = document.documentElement, y = scrollY;
  root.style.minHeight = 'calc(100% + 1px)';
  requestAnimationFrame(() => {
    scrollTo(0, y + 1);
    requestAnimationFrame(() => {
      scrollTo(0, y);
      root.style.minHeight = '';
      requestAnimationFrame(measure);
    });
  });
}
for (const e of ['resize', 'orientationchange', 'pageshow', 'DOMContentLoaded']) addEventListener(e, measure);
visualViewport?.addEventListener('resize', measure);
standalone.addEventListener('change', measure);
new ResizeObserver(measure).observe(top); // the safe area can change with no resize
if (document.body) measure();
// iOS may ignore the above.
for (const g of ['gesturestart', 'gesturechange']) document.addEventListener(g, (e) => { if (document.documentElement.classList.contains('installed')) e.preventDefault(); }, { passive: false });

/** Asks the browser to install the page; null when it cannot be asked. */
export function useInstall() {
  const p = useSyncExternalStore((f) => { heard.add(f); return () => { heard.delete(f); }; }, () => prompt);
  return p && (() => { tell(null); p.prompt().catch(() => {}); });
}
