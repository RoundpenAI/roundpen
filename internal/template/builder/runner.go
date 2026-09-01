package builder

import "context"

// LogFn receives build log lines.
type LogFn func(level, step, message string)

// Runner executes template image builds.
// tags are local image references (name:tag); the first is the primary artifact.
// Kaniko may rewrite them with a registry destination prefix.
type Runner interface {
	Build(ctx context.Context, baseImage string, spec Spec, tags []string, log LogFn) (artifact string, snapshot bool, err error)
}
