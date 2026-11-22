// The frame that draws a diagram from text (Mermaid, MIT). It runs in an
// iframe with sandbox="allow-scripts" and without allow-same-origin: it
// cannot reach the wiki, its cookies or its storage, and its own CSP allows
// no requests (connect-src 'none'). The page sends the text of the diagram;
// the frame answers only with its height and whether it worked. A fault in
// the library stays in the frame.
import mermaid from 'mermaid';

interface Ask { source: string; dark: boolean; lang: string }

const box = document.getElementById('d') as HTMLElement;
const MAX = 50_000;

function tell(msg: { ready?: boolean; ok?: boolean; height?: number; error?: string }): void {
  window.parent.postMessage({ ewDiagram: true, ...msg }, '*');
}

let drawn = false;
window.addEventListener('message', (ev: MessageEvent) => {
  if (ev.source !== window.parent || drawn) return;
  const d = ev.data as Partial<Ask> | null;
  if (!d || typeof d.source !== 'string' || d.source.length > MAX) return;
  drawn = true;
  document.documentElement.lang = typeof d.lang === 'string' && /^[a-z]{2,3}(-[A-Za-z]{2,4})?$/.test(d.lang) ? d.lang : 'en';
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    theme: d.dark ? 'dark' : 'default',
    maxTextSize: MAX,
    maxEdges: 500,
    deterministicIds: true,
    fontFamily: 'system-ui, sans-serif',
    layout: 'dagre',
    flowchart: { htmlLabels: false },
  });
  mermaid.render('ewd', d.source).then(({ svg }) => {
    box.innerHTML = svg;
    const size = () => tell({ ok: true, height: Math.ceil(box.getBoundingClientRect().height) });
    size();
    new ResizeObserver(size).observe(box);
  }).catch((e: unknown) => {
    tell({ ok: false, error: String(e instanceof Error ? e.message : e).slice(0, 300) });
  });
});
// ready for the text of the diagram
tell({ ready: true });
