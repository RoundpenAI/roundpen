// internal/browser/playwright.go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const (
	pwNavTimeout    = 45 * time.Second
	pwActionTimeout = 5 * time.Second
)

// pwActionTimeoutMS is pwActionTimeout in the millisecond unit the Playwright
// option fields expect.
var pwActionTimeoutMS = float64(pwActionTimeout.Milliseconds())

// pwDriver starts the Playwright driver process once per roundpend process.
type pwDriver struct {
	once sync.Once
	pw   *playwright.Playwright
	err  error
}

func (d *pwDriver) get() (*playwright.Playwright, error) {
	d.once.Do(func() {
		if err := playwright.Install(&playwright.RunOptions{SkipInstallBrowsers: true}); err != nil {
			d.err = fmt.Errorf("playwright driver install: %w", err)
			return
		}
		d.pw, d.err = playwright.Run()
		if d.err != nil {
			d.err = fmt.Errorf("playwright driver start: %w", d.err)
		}
	})
	return d.pw, d.err
}

// stop must run after every engine using this driver has been closed.
func (d *pwDriver) stop() {
	if d != nil && d.pw != nil {
		_ = d.pw.Stop()
	}
}

// pwAttach describes one engine attachment.
type pwAttach struct {
	Endpoint      string // ws(s):// or http(s):// origin/member path (empty for host mode)
	Token         string
	LaunchPath    string // host mode: Chrome executable
	Width, Height int
}

// PlaywrightEngine implements Engine over a Playwright browser.
type PlaywrightEngine struct {
	driver  *pwDriver
	browser playwright.Browser
	bctx    playwright.BrowserContext
	page    playwright.Page
	url     string
	width   int
	height  int
	mu      sync.Mutex
}

var _ Engine = (*PlaywrightEngine)(nil)

func newPlaywrightEngine(d *pwDriver, att pwAttach) (*PlaywrightEngine, error) {
	pw, err := d.get()
	if err != nil {
		return nil, err
	}
	if att.Width <= 0 {
		att.Width = 1280
	}
	if att.Height <= 0 {
		att.Height = 800
	}
	var browser playwright.Browser
	if strings.TrimSpace(att.LaunchPath) != "" {
		headless := true
		browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
			ExecutablePath: playwright.String(att.LaunchPath),
			Headless:       &headless,
			Args: []string{
				fmt.Sprintf("--window-size=%d,%d", att.Width, att.Height),
				"--hide-scrollbars", "--mute-audio", "--disable-dev-shm-usage",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("launch host chrome: %w", err)
		}
	} else {
		browser, err = connectCandidate(pw, att.Endpoint, att.Token)
		if err != nil {
			return nil, fmt.Errorf("cdp attach: %w", err)
		}
	}
	bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport:  &playwright.Size{Width: att.Width, Height: att.Height},
		UserAgent: playwright.String(desktopChromeUA),
	})
	if err != nil {
		_ = browser.Close()
		return nil, fmt.Errorf("browser context: %w", err)
	}
	if err := bctx.AddInitScript(playwright.Script{Content: playwright.String(stealthInitJS)}); err != nil {
		_ = bctx.Close()
		_ = browser.Close()
		return nil, fmt.Errorf("stealth init script: %w", err)
	}
	page, err := bctx.NewPage()
	if err != nil {
		_ = bctx.Close()
		_ = browser.Close()
		return nil, fmt.Errorf("new page: %w", err)
	}
	return &PlaywrightEngine{
		driver: d, browser: browser, bctx: bctx, page: page,
		width: att.Width, height: att.Height,
	}, nil
}

// cdpPathCache remembers the working ws path per endpoint origin.
var cdpPathCache sync.Map // string -> string

// connectCandidate connects over CDP, trying candidate paths in order. A
// cached path is trusted first, but a failure evicts the entry and falls back
// to the full sweep so a stale cache cannot poison the process for good.
func connectCandidate(pw *playwright.Playwright, endpoint, token string) (playwright.Browser, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("cdp endpoint is required")
	}
	cacheKey := strings.TrimRight(endpoint, "/")
	if v, ok := cdpPathCache.Load(cacheKey); ok {
		if wsURL, err := buildWSURL(endpoint, v.(string), token); err == nil {
			if b, err := pw.Chromium.ConnectOverCDP(wsURL); err == nil {
				return b, nil
			}
		}
		cdpPathCache.Delete(cacheKey) // stale: sweep the candidates again
	}
	var lastErr error
	for _, p := range candidatePaths(endpointPathOf(endpoint)) {
		wsURL, err := buildWSURL(endpoint, p, token)
		if err != nil {
			return nil, err
		}
		b, err := pw.Chromium.ConnectOverCDP(wsURL)
		if err == nil {
			cdpPathCache.Store(cacheKey, p)
			return b, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no candidate path connected")
	}
	return nil, lastErr
}

func (e *PlaywrightEngine) Navigate(ctx context.Context, target string) error {
	timeout := float64(pwNavTimeout.Milliseconds())
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline).Milliseconds(); remain > 0 && float64(remain) < timeout {
			timeout = float64(remain)
		}
	}
	if _, err := e.page.Goto(strings.TrimSpace(target), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(timeout),
	}); err != nil {
		return err
	}
	e.noteURL()
	return nil
}

// noteURL refreshes the cached page URL. Page.URL is driver-side state, so
// this costs no round trip.
func (e *PlaywrightEngine) noteURL() {
	e.mu.Lock()
	e.url = e.page.URL()
	e.mu.Unlock()
}

func (e *PlaywrightEngine) URL() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.url
}

func (e *PlaywrightEngine) Title(ctx context.Context) (string, error) {
	_ = ctx
	return e.page.Title()
}

func (e *PlaywrightEngine) Snapshot(ctx context.Context) (Snapshot, error) {
	_ = ctx
	raw, err := e.page.Evaluate(snapshotJS)
	if err != nil {
		return Snapshot{}, err
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return Snapshot{}, err
	}
	var out Snapshot
	if err := json.Unmarshal(body, &out); err != nil {
		return Snapshot{}, err
	}
	e.mu.Lock()
	if out.URL != "" {
		e.url = out.URL
	}
	out.Width = e.width
	out.Height = e.height
	e.mu.Unlock()
	out.Text = snapshotText(out)
	return out, nil
}

func (e *PlaywrightEngine) locator(ref string) playwright.Locator {
	return e.page.Locator(fmt.Sprintf(`[data-rp-ref=%q]`, ref))
}

// actionLocator resolves a snapshot ref, failing fast when it is unknown.
func (e *PlaywrightEngine) actionLocator(ref string) (playwright.Locator, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("ref is required")
	}
	loc := e.locator(ref)
	n, err := loc.Count()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("%s: unknown ref", ref)
	}
	return loc, nil
}

func (e *PlaywrightEngine) Hover(ctx context.Context, ref string) error {
	_ = ctx
	loc, err := e.actionLocator(ref)
	if err != nil {
		return err
	}
	return loc.Hover(playwright.LocatorHoverOptions{Timeout: playwright.Float(pwActionTimeoutMS)})
}

func (e *PlaywrightEngine) Click(ctx context.Context, ref string) error {
	_ = ctx
	loc, err := e.actionLocator(ref)
	if err != nil {
		return err
	}
	if err := loc.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(pwActionTimeoutMS)}); err != nil {
		return err
	}
	e.noteURL()
	return nil
}

func (e *PlaywrightEngine) Type(ctx context.Context, ref, text string, submit bool) error {
	_ = ctx
	loc, err := e.actionLocator(ref)
	if err != nil {
		return err
	}
	if err := loc.Fill(text, playwright.LocatorFillOptions{Timeout: playwright.Float(pwActionTimeoutMS)}); err != nil {
		return err
	}
	if submit {
		if err := loc.Press("Enter", playwright.LocatorPressOptions{Timeout: playwright.Float(pwActionTimeoutMS)}); err != nil {
			return err
		}
	}
	e.noteURL()
	return nil
}

func (e *PlaywrightEngine) Press(ctx context.Context, key string) error {
	_ = ctx
	if err := e.page.Keyboard().Press(normalizeKey(key)); err != nil {
		return err
	}
	e.noteURL()
	return nil
}

func (e *PlaywrightEngine) Screenshot(ctx context.Context) ([]byte, error) {
	_ = ctx
	return e.page.Screenshot()
}

func (e *PlaywrightEngine) SetViewport(ctx context.Context, width, height int) error {
	_ = ctx
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid viewport %dx%d", width, height)
	}
	if err := e.page.SetViewportSize(width, height); err != nil {
		return err
	}
	e.mu.Lock()
	e.width, e.height = width, height
	e.mu.Unlock()
	return nil
}

func (e *PlaywrightEngine) Evaluate(ctx context.Context, expression string) (json.RawMessage, error) {
	_ = ctx
	v, err := e.page.Evaluate(expression)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (e *PlaywrightEngine) InputClick(ctx context.Context, x, y float64) error {
	_ = ctx
	return e.page.Mouse().Click(x, y)
}

func (e *PlaywrightEngine) InputMove(ctx context.Context, x, y float64) error {
	_ = ctx
	return e.page.Mouse().Move(x, y)
}

// InputWheel moves the pointer to (x,y) first: Playwright's Wheel has no
// coordinates, so the wheel event lands wherever the pointer last was.
func (e *PlaywrightEngine) InputWheel(ctx context.Context, x, y, deltaX, deltaY float64) error {
	_ = ctx
	if err := e.page.Mouse().Move(x, y); err != nil {
		return err
	}
	return e.page.Mouse().Wheel(deltaX, deltaY)
}

func (e *PlaywrightEngine) InputType(ctx context.Context, text string) error {
	_ = ctx
	return e.page.Keyboard().Type(text)
}

func (e *PlaywrightEngine) InputKey(ctx context.Context, key string) error {
	_ = ctx
	return e.page.Keyboard().Press(normalizeKey(key))
}

func (e *PlaywrightEngine) Close() error {
	if e.page != nil {
		_ = e.page.Close()
	}
	if e.bctx != nil {
		_ = e.bctx.Close()
	}
	if e.browser != nil {
		return e.browser.Close()
	}
	return nil
}
