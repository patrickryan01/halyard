// Waiting on the real event, never for a fixed time. Every wait has a timeout and says what it waited for.

// idle: the page has no request, debounced refresh or suggestion lookup in flight, the translation pass has run, and two
// frames have been drawn since.
export async function idle(pg, ms = 20000) {
  const rest = () => pg.waitForFunction(() => (window.__busy || 0) === 0 && !(window.H && H.i18n && H.i18n.pending()), null, { timeout: ms });
  await rest();
  await pg.evaluate(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))));
  await rest();
}

// until: poll a condition inside the page (a call to the core, a DOM test) until it is true, with a timeout. The
// condition may be async; it is called with arg.
export async function until(pg, what, fn, arg, ms = 30000) {
  const ok = await pg.evaluate(async ({ src, arg, ms }) => {
    const f = (0, eval)('(' + src + ')');
    const end = Date.now() + ms;
    for (;;) {
      if (await f(arg)) return true;
      if (Date.now() > end) return false;
      await new Promise(r => setTimeout(r, 100));
    }
  }, { src: fn.toString(), arg: arg === undefined ? null : arg, ms });
  if (!ok) throw new Error('timed out waiting for ' + what);
}
