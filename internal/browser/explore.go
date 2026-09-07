package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultMaxClicks = 20
	defaultMaxHovers = 30
	defaultMaxPages  = 6
)

// ExploreOpts bounds a structural walk.
type ExploreOpts struct {
	StartURL  string
	MaxClicks int
	MaxHovers int
	MaxPages  int
}

// ExploreReport is what browser_explore returns.
type ExploreReport struct {
	StartURL string     `json:"startUrl"`
	Pages    []PageSeen `json:"pages"`
	Findings []Finding  `json:"findings"`
	Coverage Coverage   `json:"coverage"`
}

// PageSeen is one URL visited during the walk.
type PageSeen struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Nodes int    `json:"nodes"`
}

// Finding is one surprise: dead click, hover-revealed control, page error.
type Finding struct {
	Kind   string `json:"kind"` // dead_click | hover_reveal | page_error | skipped
	URL    string `json:"url"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail"`
}

// Coverage counts what the walker touched.
type Coverage struct {
	InteractiveSeen int `json:"interactiveSeen"`
	Hovered         int `json:"hovered"`
	Revealed        int `json:"revealed"`
	Clicked         int `json:"clicked"`
	Pages           int `json:"pages"`
}

// Explore walks interactive controls the way a person would: hover to reveal,
// click, and notice when nothing happens.
func Explore(ctx context.Context, eng Engine, opts ExploreOpts) (*ExploreReport, error) {
	if eng == nil {
		return nil, fmt.Errorf("browser engine is required")
	}
	start := strings.TrimSpace(opts.StartURL)
	if err := ValidateStartURL(start); err != nil {
		return nil, err
	}
	if opts.MaxClicks <= 0 {
		opts.MaxClicks = defaultMaxClicks
	}
	if opts.MaxHovers <= 0 {
		opts.MaxHovers = defaultMaxHovers
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = defaultMaxPages
	}

	host, err := urlHost(start)
	if err != nil {
		return nil, err
	}

	rep := &ExploreReport{StartURL: start, Findings: []Finding{}, Pages: []PageSeen{}}
	visited := map[string]bool{}
	if err := walkPage(ctx, eng, start, host, opts, rep, visited, 0); err != nil {
		return nil, err
	}
	rep.Coverage.Pages = len(rep.Pages)
	return rep, nil
}

// ValidateStartURL accepts only http(s) URLs with a host.
func ValidateStartURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("url must be http(s) with a host")
	}
	return nil
}

func walkPage(ctx context.Context, eng Engine, pageURL, host string, opts ExploreOpts, rep *ExploreReport, visited map[string]bool, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := canonURL(pageURL)
	if key == "" || visited[key] || len(rep.Pages) >= opts.MaxPages {
		return nil
	}
	visited[key] = true

	if err := eng.Navigate(ctx, pageURL); err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Kind: "page_error", URL: pageURL, Detail: "navigate: " + err.Error(),
		})
		return nil
	}
	_ = installHooks(ctx, eng)
	time.Sleep(150 * time.Millisecond)

	snap, err := eng.Snapshot(ctx)
	if err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Kind: "page_error", URL: pageURL, Detail: "snapshot: " + err.Error(),
		})
		return nil
	}
	if snap.URL != "" {
		pageURL = snap.URL
	}
	rep.Pages = append(rep.Pages, PageSeen{URL: pageURL, Title: snap.Title, Nodes: len(snap.Nodes)})
	rep.Coverage.InteractiveSeen += len(snap.Nodes)
	drainErrors(ctx, eng, pageURL, rep)

	seen := nodeSet(snap.Nodes)
	for i, n := range snap.Nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rep.Coverage.Hovered >= opts.MaxHovers {
			break
		}
		if skipHover(n) {
			continue
		}
		if err := eng.Hover(ctx, n.Ref); err != nil {
			continue
		}
		rep.Coverage.Hovered++
		after, err := eng.Snapshot(ctx)
		if err != nil {
			continue
		}
		for _, extra := range after.Nodes {
			if seen[nodeKey(extra)] {
				continue
			}
			seen[nodeKey(extra)] = true
			rep.Coverage.Revealed++
			rep.Findings = append(rep.Findings, Finding{
				Kind:   "hover_reveal",
				URL:    pageURL,
				Target: label(n),
				Detail: "revealed " + label(extra),
			})
			snap.Nodes = append(snap.Nodes, extra)
		}
		_ = i
	}

	type clickJob struct {
		node SnapNode
		from string
	}
	var jobs []clickJob
	for _, n := range snap.Nodes {
		if !clickable(n) {
			continue
		}
		if skipClick(n) {
			rep.Findings = append(rep.Findings, Finding{
				Kind: "skipped", URL: pageURL, Target: label(n), Detail: "destructive or sign-out",
			})
			continue
		}
		jobs = append(jobs, clickJob{node: n, from: pageURL})
	}

	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rep.Coverage.Clicked >= opts.MaxClicks {
			break
		}
		before, err := eng.Snapshot(ctx)
		if err != nil {
			continue
		}
		beforeFP := fingerprint(before)
		if err := eng.Click(ctx, job.node.Ref); err != nil {
			rep.Findings = append(rep.Findings, Finding{
				Kind: "page_error", URL: job.from, Target: label(job.node), Detail: "click: " + err.Error(),
			})
			continue
		}
		rep.Coverage.Clicked++
		time.Sleep(200 * time.Millisecond)
		after, err := eng.Snapshot(ctx)
		if err != nil {
			continue
		}
		drainErrors(ctx, eng, job.from, rep)
		afterURL := after.URL
		if afterURL == "" {
			afterURL = eng.URL()
		}
		if fingerprint(after) == beforeFP {
			rep.Findings = append(rep.Findings, Finding{
				Kind:   "dead_click",
				URL:    job.from,
				Target: label(job.node),
				Detail: "click changed nothing (url, title, or controls)",
			})
			continue
		}
		if sameHost(afterURL, host) && !visited[canonURL(afterURL)] && len(rep.Pages) < opts.MaxPages && depth < 2 {
			_ = walkPage(ctx, eng, afterURL, host, opts, rep, visited, depth+1)
			_ = eng.Navigate(ctx, job.from)
			_ = installHooks(ctx, eng)
		}
	}
	return nil
}

func installHooks(ctx context.Context, eng Engine) error {
	_, err := eng.Evaluate(ctx, `(() => {
  if (window.__rpExploreHooked) return {ok:true};
  window.__rpExploreHooked = true;
  window.__rpExploreErrors = [];
  window.addEventListener('error', (e) => {
    window.__rpExploreErrors.push({kind:'page_error', message: String(e.message || e.error || e)});
  });
  window.addEventListener('unhandledrejection', (e) => {
    window.__rpExploreErrors.push({kind:'rejection', message: String(e.reason)});
  });
  const orig = console.error;
  console.error = function() {
    try {
      window.__rpExploreErrors.push({kind:'console', message: Array.from(arguments).map(String).join(' ').slice(0, 300)});
    } catch (e) {}
    return orig.apply(this, arguments);
  };
  return {ok:true};
})()`)
	return err
}

func drainErrors(ctx context.Context, eng Engine, pageURL string, rep *ExploreReport) {
	raw, err := eng.Evaluate(ctx, `(() => {
  const errs = window.__rpExploreErrors || [];
  window.__rpExploreErrors = [];
  return errs;
})()`)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return
	}
	var errs []struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &errs); err != nil {
		return
	}
	for _, e := range errs {
		msg := strings.TrimSpace(e.Message)
		if msg == "" {
			continue
		}
		rep.Findings = append(rep.Findings, Finding{
			Kind: "page_error", URL: pageURL, Detail: e.Kind + ": " + msg,
		})
	}
}

func clickable(n SnapNode) bool {
	switch n.Role {
	case "button", "link", "tab", "menuitem", "checkbox", "switch":
		return true
	}
	switch n.Tag {
	case "button", "a", "summary":
		return true
	}
	return false
}

func skipHover(n SnapNode) bool {
	return n.Role == "heading" || n.Role == "label" || n.Role == "textbox"
}

func skipClick(n SnapNode) bool {
	s := strings.ToLower(strings.TrimSpace(n.Name + " " + n.Href))
	for _, p := range []string{"sign out", "log out", "logout", "sign-out"} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func label(n SnapNode) string {
	name := strings.TrimSpace(n.Name)
	if name == "" {
		name = n.Tag
	}
	if name == "" {
		name = n.Role
	}
	if n.Ref != "" {
		return fmt.Sprintf("%s %q [ref=%s]", n.Role, name, n.Ref)
	}
	return n.Role + " " + name
}

func nodeKey(n SnapNode) string {
	return n.Role + "|" + n.Name + "|" + n.Href + "|" + n.Tag
}

func nodeSet(nodes []SnapNode) map[string]bool {
	out := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		out[nodeKey(n)] = true
	}
	return out
}

func fingerprint(s Snapshot) string {
	var b strings.Builder
	b.WriteString(canonURL(s.URL))
	b.WriteByte('|')
	b.WriteString(s.Title)
	for _, n := range s.Nodes {
		b.WriteByte('|')
		b.WriteString(nodeKey(n))
	}
	return b.String()
}

func canonURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.Fragment = ""
	return u.String()
}

func urlHost(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("url host required")
	}
	return strings.ToLower(u.Hostname()), nil
}

func sameHost(raw, host string) bool {
	h, err := urlHost(raw)
	if err != nil {
		return false
	}
	return h == host
}
