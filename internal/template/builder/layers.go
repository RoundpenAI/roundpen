package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// CacheKey returns a stable hash for build spec caching.
func CacheKey(spec Spec) (string, error) {
	spec.Force = false
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Dockerfile generates a Dockerfile from a build spec and base image.
func Dockerfile(base string, spec Spec) (string, error) {
	if base == "" {
		return "", fmt.Errorf("base image is required")
	}
	var b strings.Builder
	b.WriteString("FROM ")
	b.WriteString(base)
	b.WriteString("\n")
	for i, step := range spec.Steps {
		line, err := stepLine(step)
		if err != nil {
			return "", fmt.Errorf("step %d: %w", i, err)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if spec.StartCmd != "" {
		escaped := shellEscape(spec.StartCmd)
		b.WriteString("RUN mkdir -p /roundpen && printf '%s\\n' '#!/bin/sh' '")
		b.WriteString(escaped)
		b.WriteString(" &' 'exec sleep infinity' > /roundpen/init.sh && chmod +x /roundpen/init.sh\n")
		b.WriteString("CMD [\"/roundpen/init.sh\"]\n")
	} else {
		b.WriteString("CMD [\"sleep\", \"infinity\"]\n")
	}
	return b.String(), nil
}

func stepLine(step Step) (string, error) {
	typ := strings.ToUpper(strings.TrimSpace(step.Type))
	switch typ {
	case "RUN", "RUNCMD":
		if len(step.Args) == 0 {
			return "", fmt.Errorf("RUN requires args")
		}
		return "RUN " + joinShell(step.Args), nil
	case "COPY":
		if len(step.Args) < 2 {
			return "", fmt.Errorf("COPY requires src and dest")
		}
		return fmt.Sprintf("COPY %s %s", step.Args[0], step.Args[1]), nil
	case "WORKDIR", "SETWORKDIR":
		if len(step.Args) == 0 {
			return "", fmt.Errorf("WORKDIR requires path")
		}
		return "WORKDIR " + step.Args[0], nil
	case "USER", "SETUSER":
		if len(step.Args) == 0 {
			return "", fmt.Errorf("USER requires name")
		}
		return "USER " + step.Args[0], nil
	case "ENV", "SETENVS":
		if len(step.Args) < 2 {
			return "", fmt.Errorf("ENV requires key and value")
		}
		return fmt.Sprintf("ENV %s=%s", step.Args[0], step.Args[1]), nil
	case "APT_INSTALL", "APTINSTALL":
		pkgs := step.Args
		if len(pkgs) == 0 {
			return "", fmt.Errorf("APT_INSTALL requires packages")
		}
		return "RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends " + strings.Join(pkgs, " "), nil
	case "PIP_INSTALL", "PIPINSTALL":
		if len(step.Args) == 0 {
			return "", fmt.Errorf("PIP_INSTALL requires packages")
		}
		return "RUN pip install --no-cache-dir " + strings.Join(step.Args, " "), nil
	case "NPM_INSTALL", "NPMINSTALL":
		if len(step.Args) == 0 {
			return "", fmt.Errorf("NPM_INSTALL requires packages")
		}
		return "RUN npm install -g " + strings.Join(step.Args, " "), nil
	default:
		return "", fmt.Errorf("unsupported step type %q", step.Type)
	}
}

func joinShell(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " && ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\'\''`) + "'"
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", `'\'\''`)
}
