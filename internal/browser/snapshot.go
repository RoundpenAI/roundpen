package browser

// Shared page-side scripts and UA used by every engine implementation.

const snapshotJS = `(() => {
  const interesting = 'a, button, input, select, textarea, summary, [role="button"], [role="link"], [role="tab"], [role="menuitem"], [role="checkbox"], [role="option"], [role="switch"], [role="textbox"], h1, h2, h3, label';
  const els = Array.from(document.querySelectorAll(interesting));
  window.__rpRefs = {};
  const nodes = [];
  const seen = new Set();
  for (const el of els) {
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') continue;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) continue;
    let name = (el.getAttribute('aria-label') || el.getAttribute('placeholder') || el.getAttribute('title') || '').trim();
    if (!name) {
      name = (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim();
    }
    if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
      const lab = el.labels && el.labels[0] ? el.labels[0].innerText.trim() : '';
      if (lab && !name) name = lab;
    }
    name = name.slice(0, 120);
    const key = el.tagName + '|' + name + '|' + (el.getAttribute('href') || '') + '|' + (el.type || '');
    if (seen.has(key) && !name) continue;
    seen.add(key);
    const ref = 'e' + (nodes.length + 1);
    window.__rpRefs[ref] = el;
    el.setAttribute('data-rp-ref', ref);
    let role = (el.getAttribute('role') || '').toLowerCase();
    if (!role) {
      const tag = el.tagName.toLowerCase();
      if (tag === 'a') role = 'link';
      else if (tag === 'button') role = 'button';
      else if (tag === 'input') role = el.type === 'checkbox' || el.type === 'radio' ? el.type : 'textbox';
      else if (tag === 'select') role = 'combobox';
      else if (tag === 'textarea') role = 'textbox';
      else if (tag === 'h1' || tag === 'h2' || tag === 'h3') role = 'heading';
      else if (tag === 'label') role = 'label';
      else role = tag;
    }
    const node = {
      ref, role, name, tag: el.tagName.toLowerCase(),
      value: ('value' in el && typeof el.value === 'string') ? String(el.value).slice(0, 80) : '',
      href: el.href || '',
      x: Math.round(rect.x + rect.width / 2),
      y: Math.round(rect.y + rect.height / 2),
      w: Math.round(rect.width),
      h: Math.round(rect.height),
    };
    if (el.type === 'checkbox' || el.type === 'radio' || role === 'checkbox' || role === 'switch') {
      node.checked = !!el.checked;
    }
    nodes.push(node);
  }
  return {
    url: location.href,
    title: document.title,
    nodes,
  };
})()`

// desktopChromeUA looks like a normal desktop Chrome (no HeadlessChrome token).
const desktopChromeUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// stealthInitJS strips the most common automation globals before page scripts run.
const stealthInitJS = `(() => {
  try {
    Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
  } catch (e) {}
  try {
    // Chrome automation often leaves an empty chrome.runtime; keep a stub.
    window.chrome = window.chrome || { runtime: {} };
  } catch (e) {}
  try {
    const orig = navigator.permissions && navigator.permissions.query;
    if (orig) {
      navigator.permissions.query = (params) =>
        params && params.name === 'notifications'
          ? Promise.resolve({ state: Notification.permission })
          : orig(params);
    }
  } catch (e) {}
})()`
