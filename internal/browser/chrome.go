package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

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

type chromeEngine struct {
	mu     sync.Mutex
	alloc  context.CancelFunc
	cancel context.CancelFunc
	ctx    context.Context
	url    string
	width  int
	height int
}

// ChromeOnPATH reports whether a Chrome/Chromium binary is available to this process.
func ChromeOnPATH() bool {
	return lookupChrome() != ""
}

func lookupChrome() string {
	if p := strings.TrimSpace(os.Getenv("CHROME_PATH")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func newChromeEngine(userDataDir string, width, height int) (*chromeEngine, error) {
	bin := lookupChrome()
	if bin == "" {
		return nil, fmt.Errorf("chrome not found (set CHROME_PATH)")
	}
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 800
	}
	if err := os.MkdirAll(userDataDir, 0o700); err != nil {
		return nil, err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(bin),
		chromedp.UserDataDir(userDataDir),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("hide-scrollbars", false),
		chromedp.WindowSize(width, height),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	eng := &chromeEngine{
		alloc:  allocCancel,
		cancel: cancel,
		ctx:    ctx,
		width:  width,
		height: height,
	}
	// The first Run owns the browser lifetime — do not wrap it in a cancellable
	// timeout or chromedp will tear Chrome down when that context ends.
	if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(width), int64(height), 1, false)); err != nil {
		eng.Close()
		return nil, fmt.Errorf("chrome start: %w", err)
	}
	return eng, nil
}

func (e *chromeEngine) run(ctx context.Context, actions ...chromedp.Action) error {
	if e.ctx == nil {
		return fmt.Errorf("browser closed")
	}
	timeout := 45 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline); remain > 0 && remain < timeout {
			timeout = remain
		}
	}
	runCtx, cancel := context.WithTimeout(e.ctx, timeout)
	defer cancel()
	return chromedp.Run(runCtx, actions...)
}

func (e *chromeEngine) Navigate(ctx context.Context, url string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.run(ctx, chromedp.Navigate(url), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		return err
	}
	e.url = url
	time.Sleep(250 * time.Millisecond)
	var href string
	_ = e.run(ctx, chromedp.Location(&href))
	if href != "" {
		e.url = href
	}
	return nil
}

func (e *chromeEngine) URL() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.url
}

func (e *chromeEngine) Title(ctx context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var title string
	if err := e.run(ctx, chromedp.Title(&title)); err != nil {
		return "", err
	}
	return title, nil
}

type snapPayload struct {
	URL   string           `json:"url"`
	Title string           `json:"title"`
	Nodes []map[string]any `json:"nodes"`
}

func (e *chromeEngine) Snapshot(ctx context.Context) (Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var raw snapPayload
	if err := e.run(ctx, chromedp.Evaluate(snapshotJS, &raw)); err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{
		URL:    raw.URL,
		Title:  raw.Title,
		Width:  e.width,
		Height: e.height,
		Nodes:  make([]SnapNode, 0, len(raw.Nodes)),
	}
	if raw.URL != "" {
		e.url = raw.URL
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- Page: %s\n- URL: %s\n", raw.Title, raw.URL)
	for _, n := range raw.Nodes {
		node := SnapNode{
			Ref:   strField(n, "ref"),
			Role:  strField(n, "role"),
			Name:  strField(n, "name"),
			Tag:   strField(n, "tag"),
			Value: strField(n, "value"),
			Href:  strField(n, "href"),
		}
		if v, ok := n["checked"].(bool); ok {
			node.Checked = &v
		}
		out.Nodes = append(out.Nodes, node)
		name := node.Name
		if name == "" {
			name = node.Tag
		}
		fmt.Fprintf(&b, "- %s %q [ref=%s]", node.Role, name, node.Ref)
		if node.Value != "" && node.Role == "textbox" {
			fmt.Fprintf(&b, " value=%q", node.Value)
		}
		b.WriteByte('\n')
	}
	out.Text = b.String()
	return out, nil
}

func strField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func (e *chromeEngine) Click(ctx context.Context, ref string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	js := fmt.Sprintf(`(() => {
      const el = window.__rpRefs && window.__rpRefs[%q];
      if (!el) return { ok: false, err: "unknown ref" };
      el.scrollIntoView({block: "center", inline: "center"});
      el.click();
      return { ok: true };
    })()`, ref)
	var res map[string]any
	if err := e.run(ctx, chromedp.Evaluate(js, &res)); err != nil {
		return err
	}
	if ok, _ := res["ok"].(bool); !ok {
		errMsg, _ := res["err"].(string)
		if errMsg == "" {
			errMsg = "click failed"
		}
		return fmt.Errorf("%s: %s", ref, errMsg)
	}
	time.Sleep(200 * time.Millisecond)
	var href string
	_ = e.run(ctx, chromedp.Location(&href))
	if href != "" {
		e.url = href
	}
	return nil
}

func (e *chromeEngine) Type(ctx context.Context, ref, text string, submit bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	payload, _ := json.Marshal(text)
	js := fmt.Sprintf(`(() => {
      const el = window.__rpRefs && window.__rpRefs[%q];
      if (!el) return { ok: false, err: "unknown ref" };
      el.scrollIntoView({block: "center"});
      el.focus();
      const proto = el.tagName === "TEXTAREA" ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
      const desc = Object.getOwnPropertyDescriptor(proto, "value");
      if (desc && desc.set) desc.set.call(el, %s);
      else el.value = %s;
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
      return { ok: true };
    })()`, ref, payload, payload)
	var res map[string]any
	if err := e.run(ctx, chromedp.Evaluate(js, &res)); err != nil {
		return err
	}
	if ok, _ := res["ok"].(bool); !ok {
		errMsg, _ := res["err"].(string)
		if errMsg == "" {
			errMsg = "type failed"
		}
		return fmt.Errorf("%s: %s", ref, errMsg)
	}
	if submit {
		_ = e.run(ctx, chromedp.KeyEvent("\r"))
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}

func (e *chromeEngine) Press(ctx context.Context, key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := strings.TrimSpace(key)
	switch strings.ToLower(k) {
	case "enter", "return":
		k = "\r"
	case "tab":
		k = "\t"
	case "escape", "esc":
		return e.run(ctx, chromedp.KeyEvent("\u001b"))
	case "backspace":
		k = "\b"
	}
	return e.run(ctx, chromedp.KeyEvent(k))
}

func (e *chromeEngine) Screenshot(ctx context.Context) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var buf []byte
	if err := e.run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		return nil, err
	}
	return buf, nil
}

func (e *chromeEngine) SetViewport(ctx context.Context, width, height int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid viewport")
	}
	e.width, e.height = width, height
	return e.run(ctx, emulation.SetDeviceMetricsOverride(int64(width), int64(height), 1, false))
}

func (e *chromeEngine) Evaluate(ctx context.Context, expression string) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var raw any
	if err := e.run(ctx, chromedp.Evaluate(expression, &raw)); err != nil {
		return nil, err
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (e *chromeEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.alloc != nil {
		e.alloc()
		e.alloc = nil
	}
	return nil
}
