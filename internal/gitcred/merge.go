package gitcred

import (
	"sort"
	"strings"
)

// MergeCreds joins explicit credentials (primary, e.g. hand-entered PATs) with
// fallback ones (e.g. OAuth identities). Primary wins for the same host, so a
// manual token always overrides a token obtained by logging in.
func MergeCreds(primary, fallback []Cred) []Cred {
	seen := make(map[string]bool, len(primary)+len(fallback))
	out := make([]Cred, 0, len(primary)+len(fallback))
	add := func(list []Cred, skipEmpty bool) {
		for _, c := range list {
			key := credKey(c.Host)
			if key == "" || seen[key] {
				continue
			}
			if skipEmpty && strings.TrimSpace(c.Token) == "" {
				continue
			}
			seen[key] = true
			out = append(out, c)
		}
	}
	add(primary, false)
	add(fallback, true)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func credKey(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "/"))
}
