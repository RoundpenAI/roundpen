package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	reSxxExx = regexp.MustCompile(`(?i)[.\-_ ]S(\d{1,2})E(\d{1,3})`)
	reNxNN   = regexp.MustCompile(`(?i)(?:^|[.\-_ ])(\d{1,2})x(\d{1,3})(?:[.\-_ ]|$)`)
)

// RegisterMediaLib adds ScanTVGaps for library completion workflows.
func RegisterMediaLib(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "ScanTVGaps",
		Description: "Scan a TV show folder under a granted host path for present SxxExx / NxNN episodes " +
			"and report missing episode numbers within the observed season ranges. " +
			"Pass the show root (containing season files or season subfolders).",
		Parameters: objectSchema(map[string]any{
			"path":          map[string]any{"type": "string", "description": "Absolute granted path to the show directory"},
			"expected_last": map[string]any{"type": "integer", "description": "Optional last episode number to expect per season (fills trailing gaps)"},
		}, "path"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Path         string `json:"path"`
				ExpectedLast int    `json:"expected_last"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Path) == "" {
				return "", fmt.Errorf("path is required")
			}
			return binder.scanTVGaps(ctx, actor, in.Path, in.ExpectedLast)
		},
	})
}

func (b *AgentBinder) scanTVGaps(ctx context.Context, actor Actor, root string, expectedLast int) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	abs, err := requireHostAbs(root)
	if err != nil {
		return "", err
	}
	// Authorize by opening the directory (read grant).
	rc, fi, err := host.OpenHost(ctx, actor, abs)
	if err != nil {
		return "", err
	}
	_ = rc.Close()
	if fi != nil && !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	present := map[int]map[int]string{} // season -> ep -> path
	_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		s, e, ok := parseEpisode(name)
		if !ok {
			return nil
		}
		if present[s] == nil {
			present[s] = map[int]string{}
		}
		present[s][e] = p
		return nil
	})
	type gapSeason struct {
		Season   int   `json:"season"`
		Present  []int `json:"present"`
		Missing  []int `json:"missing"`
		MaxSeen  int   `json:"maxSeen"`
	}
	var seasons []gapSeason
	keys := make([]int, 0, len(present))
	for s := range present {
		keys = append(keys, s)
	}
	sort.Ints(keys)
	for _, s := range keys {
		eps := present[s]
		var list []int
		max := 0
		for e := range eps {
			list = append(list, e)
			if e > max {
				max = e
			}
		}
		sort.Ints(list)
		end := max
		if expectedLast > end {
			end = expectedLast
		}
		var missing []int
		for e := 1; e <= end; e++ {
			if _, ok := eps[e]; !ok {
				missing = append(missing, e)
			}
		}
		seasons = append(seasons, gapSeason{Season: s, Present: list, Missing: missing, MaxSeen: max})
	}
	raw, err := json.Marshal(map[string]any{
		"path":    abs,
		"seasons": seasons,
		"note":    "Use WebSearch/Browser to find sources for missing episodes, then SubmitDownload after confirmation.",
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func parseEpisode(name string) (season, ep int, ok bool) {
	base := path.Base(name)
	if m := reSxxExx.FindStringSubmatch(base); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, s > 0 && e > 0
	}
	if m := reNxNN.FindStringSubmatch(base); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, s > 0 && e > 0
	}
	return 0, 0, false
}
