package builder

import "context"

// LogFn receives build log lines.
type LogFn func(level, step, message string)

// Runner executes template image builds.
type Runner interface {
	Build(ctx context.Context, baseImage string, spec Spec, tag string, log LogFn) (artifact string, snapshot bool, err error)
}
