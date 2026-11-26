/** Chromium can bend the page itself behind a pane (backdrop-filter: url()); Safari cannot, there the
    water bends in the shader and the page is only frosted. */
export const canBend = /Chrome\/\d/.test(navigator.userAgent) && !matchMedia('(prefers-reduced-transparency: reduce)').matches;

const NS = 'http://www.w3.org/2000/svg';
const made = new Map<string, string>();

/** How far, and which way, each point of a pane w by h with corners r looks past itself. */
function lensMap(w: number, h: number, r: number) {
  const c = document.createElement('canvas');
  c.width = w; c.height = h;
  const x = c.getContext('2d')!, img = x.createImageData(w, h);
  const b = Math.min(w / 2, h / 2, 26), most = b * 1.7;
  for (let j = 0; j < h; j++) for (let i = 0; i < w; i++) {
    const px = i + .5 - w / 2, py = j + .5 - h / 2;
    const qx = Math.abs(px) - w / 2 + r, qy = Math.abs(py) - h / 2 + r;
    const mx = Math.max(qx, 0), my = Math.max(qy, 0), l = Math.hypot(mx, my);
    const d = l + Math.min(Math.max(qx, qy), 0) - r;
    let nx = 0, ny = 0;
    if (l > .001) { nx = mx / l; ny = my / l; } else if (qx > qy) nx = 1; else ny = 1;
    const e = 1 - Math.min(1, Math.max(0, -d / b));
    const bend = 1 - Math.sqrt(Math.max(0, 1 - e * e));
    const k = (j * w + i) * 4;
    img.data[k] = Math.round(127.5 - nx * (px < 0 ? -1 : 1) * bend * 127);
    img.data[k + 1] = Math.round(127.5 - ny * (py < 0 ? -1 : 1) * bend * 127);
    img.data[k + 2] = 128;
    img.data[k + 3] = 255;
  }
  x.putImageData(img, 0, 0);
  return { href: c.toDataURL(), scale: most * 2 };
}

function filterFor(svg: SVGSVGElement, w: number, h: number, r: number) {
  const key = `${w}x${h}x${r}`;
  const had = made.get(key);
  if (had) return had;
  const { href, scale } = lensMap(w, h, r);
  const id = `pane-${made.size}`;
  const f = document.createElementNS(NS, 'filter');
  for (const [k, v] of Object.entries({ id, filterUnits: 'userSpaceOnUse', primitiveUnits: 'userSpaceOnUse', x: '0', y: '0', width: `${w}`, height: `${h}`, 'color-interpolation-filters': 'sRGB' })) f.setAttribute(k, v);
  f.innerHTML = `<feImage href="${href}" x="0" y="0" width="${w}" height="${h}" preserveAspectRatio="none" result="map"/><feGaussianBlur in="SourceGraphic" stdDeviation="2.2" edgeMode="duplicate" result="soft"/><feDisplacementMap in="soft" in2="map" scale="${scale}" xChannelSelector="R" yChannelSelector="G"/><feColorMatrix type="saturate" values="1.5"/>`;
  svg.append(f);
  made.set(key, id);
  return id;
}

/** Keeps every pane that stays put bent to its own shape. */
export function bendGlass() {
  if (!canBend) return () => {};
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('aria-hidden', 'true');
  svg.style.cssText = 'position:absolute;width:0;height:0';
  document.body.append(svg);
  const todo = new Set<HTMLElement>();
  let t = 0;
  const run = () => {
    for (const el of todo) {
      const w = el.offsetWidth, h = el.offsetHeight;
      if (!w || !h || !el.isConnected || w * h > 400_000) continue;
      const r = Math.min(parseFloat(getComputedStyle(el).borderTopLeftRadius) || 0, w / 2, h / 2);
      el.style.setProperty('--bend', `url(#${filterFor(svg, w, h, Math.round(r))})`);
    }
    todo.clear();
  };
  const later = (el: Element) => { todo.add(el as HTMLElement); clearTimeout(t); t = window.setTimeout(run, 60); };
  const ro = new ResizeObserver((es) => es.forEach((e) => later(e.target)));
  const seen = new WeakSet<Element>();
  const scan = () => document.querySelectorAll('.dock-glass, dialog.sheet').forEach((el) => { if (!seen.has(el)) { seen.add(el); ro.observe(el); } });
  const mo = new MutationObserver(scan);
  mo.observe(document.body, { childList: true, subtree: true });
  scan();
  document.documentElement.classList.add('bend');
  return () => { mo.disconnect(); ro.disconnect(); clearTimeout(t); svg.remove(); made.clear(); document.documentElement.classList.remove('bend'); };
}
