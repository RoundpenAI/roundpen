package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const (
	bgJobDir        = "/tmp/roundpen-jobs"
	bgLaunchTimeout = 30 * time.Second
	bgStatusTimeout = 30 * time.Second
	bgKillTimeout   = 60 * time.Second
	bgMaxReadBytes  = 32 << 10
)

// bgIDPattern guards every id that reaches the shell scripts below.
var bgIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

var errBGUnknownJob = errors.New("unknown background job")

// bgLaunchScript starts a job in its own session, detached from the exec that
// launched it: the wrapper survives the exec stream closing, and the whole
// process group (wrapper + command + descendants) can be signalled later by
// its group id. The wrapper's own stdio is detached so the exec returns as
// soon as the launcher exits instead of waiting on the inherited pipe.
const bgLaunchScript = `set -u
d=@DIR@
mkdir -p "$d" || { echo "cannot create job dir" >&2; exit 1; }
printf '%s' "$ROUNDPEN_BG_CMD" > "$d/cmd" || exit 1
cd "$ROUNDPEN_BG_WORKDIR" || { echo "workdir not found: $ROUNDPEN_BG_WORKDIR" >&2; exit 1; }
wrap='echo $$ > "$0/pid"
unset ROUNDPEN_BG_CMD ROUNDPEN_BG_WORKDIR
/bin/sh "$0/cmd" > "$0/log" 2>&1
echo $? > "$0/exit"'
if command -v setsid >/dev/null 2>&1; then
  setsid /bin/sh -c "$wrap" "$d" </dev/null >/dev/null 2>&1 &
else
  /bin/sh -c "$wrap" "$d" </dev/null >/dev/null 2>&1 &
fi
i=0
while [ ! -s "$d/pid" ] && [ "$i" -lt 50 ]; do sleep 0.05; i=$((i+1)); done
if [ ! -s "$d/pid" ]; then echo "background job did not start" >&2; exit 1; fi`

// bgStatusScript reports the job state and returns output produced since the
// previous read. Liveness is group-based and ignores zombies: a killed job
// lingers as a zombie when the container init does not reap.
const bgStatusScript = `set -u
d=@DIR@
alive() {
  g=${1:-}
  [ -n "$g" ] || return 1
  for f in /proc/[0-9]*/stat; do
    s=$(cat "$f" 2>/dev/null) || continue
    s=${s##*) }
    set -- $s
    [ "${3:-}" = "$g" ] || continue
    [ "${1:-Z}" = "Z" ] || return 0
  done
  return 1
}
if [ ! -d "$d" ]; then echo "STATUS missing"; exit 0; fi
if [ -f "$d/exit" ]; then
  echo "STATUS exited"
  echo "EXIT $(cat "$d/exit" 2>/dev/null)"
else
  if alive "$(cat "$d/pid" 2>/dev/null)"; then
    echo "STATUS running"
  else
    echo "STATUS exited"
  fi
  echo "EXIT -"
fi
size=$(wc -c < "$d/log" 2>/dev/null || echo 0)
off=$(cat "$d/offset" 2>/dev/null || echo 0)
case "$size" in ''|*[!0-9]*) size=0 ;; esac
case "$off" in ''|*[!0-9]*) off=0 ;; esac
[ "$off" -gt "$size" ] && off=$size
echo "SIZE $size"
echo "OFFSET $off"
echo "---"
tmp="$d/read.tmp"
tail -c "+$((off + 1))" "$d/log" 2>/dev/null | head -c 32768 > "$tmp" || : > "$tmp"
n=$(wc -c < "$tmp" 2>/dev/null || echo 0)
case "$n" in ''|*[!0-9]*) n=0 ;; esac
printf '%s' "$((off + n))" > "$d/offset"
cat "$tmp"
rm -f "$tmp"`

// bgKillScript signals the job's whole process group, escalating to SIGKILL
// when the group outlives the grace period.
const bgKillScript = `set -u
d=@DIR@
alive() {
  g=${1:-}
  [ -n "$g" ] || return 1
  for f in /proc/[0-9]*/stat; do
    s=$(cat "$f" 2>/dev/null) || continue
    s=${s##*) }
    set -- $s
    [ "${3:-}" = "$g" ] || continue
    [ "${1:-Z}" = "Z" ] || return 0
  done
  return 1
}
if [ ! -d "$d" ]; then echo "STATUS missing"; exit 0; fi
p=$(cat "$d/pid" 2>/dev/null || true)
if ! alive "$p"; then
  echo "STATUS exited"
  echo "EXIT $(cat "$d/exit" 2>/dev/null || echo -)"
  exit 0
fi
kill -TERM -"$p" 2>/dev/null || kill -TERM "$p" 2>/dev/null || true
i=0
while [ "$i" -lt 20 ] && alive "$p"; do sleep 0.2; i=$((i+1)); done
if alive "$p"; then
  kill -KILL -"$p" 2>/dev/null || kill -KILL "$p" 2>/dev/null || true
  sleep 0.5
fi
if alive "$p"; then
  echo "STATUS running"
  echo "EXIT -"
else
  echo "STATUS exited"
  echo "EXIT $(cat "$d/exit" 2>/dev/null || echo -)"
fi`

func bgNewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func bgJobDirFor(id string) string { return bgJobDir + "/" + id }

// bgWorkdir normalizes a relative workdir against the workspace root the same
// way the rest of the workspace tools resolve paths.
func bgWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return WorkspaceRoot
	}
	if !strings.HasPrefix(workdir, "/") {
		return strings.TrimSuffix(WorkspaceRoot, "/") + "/" + workdir
	}
	return workdir
}

func bgScript(script, id string) []string {
	return []string{"/bin/sh", "-c", strings.ReplaceAll(script, "@DIR@", bgJobDirFor(id))}
}

func bgJSON(payload map[string]any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return `{"error":"encode failure"}`
	}
	return string(raw)
}

type bgStatus struct {
	status string
	exit   *int
	size   int
	offset int
	output string
}

// parseBGStatus decodes the header the status/kill scripts print before "---".
func parseBGStatus(raw string) (bgStatus, error) {
	var st bgStatus
	if strings.HasPrefix(raw, "STATUS missing") {
		return st, errBGUnknownJob
	}
	head, body, found := strings.Cut(raw, "---\n")
	if found {
		st.output = body
	} else {
		head = raw
	}
	for _, line := range strings.Split(head, "\n") {
		key, val, _ := strings.Cut(line, " ")
		val = strings.TrimSpace(val)
		switch key {
		case "STATUS":
			st.status = val
		case "EXIT":
			if val != "" && val != "-" {
				if n, err := strconv.Atoi(val); err == nil {
					st.exit = &n
				}
			}
		case "SIZE":
			st.size, _ = strconv.Atoi(val)
		case "OFFSET":
			st.offset, _ = strconv.Atoi(val)
		}
	}
	if st.status != "running" && st.status != "exited" {
		return st, fmt.Errorf("unexpected background job status %q", st.status)
	}
	return st, nil
}

// runBGScript runs one of the job scripts against the caller's agent workspace.
func (b *AgentBinder) runBGScript(ctx context.Context, actor Actor, id, script string, timeout time.Duration) (*sandbox.ExecResult, error) {
	if !bgIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid background job id %q", id)
	}
	sbID, err := b.ensureID(ctx, actor)
	if err != nil {
		return nil, err
	}
	res, err := b.execRequest(ctx, sbID, bgScript(script, id), WorkspaceRoot, nil, timeout)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = fmt.Sprintf("exit %d", res.ExitCode)
		}
		return nil, fmt.Errorf("background job %s: %s", id, msg)
	}
	return res, nil
}

func (b *AgentBinder) startBackground(ctx context.Context, actor Actor, command, workdir string) (string, error) {
	id := bgNewID()
	sbID, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	res, err := b.execRequest(ctx, sbID, bgScript(bgLaunchScript, id), WorkspaceRoot, map[string]string{
		"ROUNDPEN_BG_CMD":     command,
		"ROUNDPEN_BG_WORKDIR": bgWorkdir(workdir),
	}, bgLaunchTimeout)
	if err != nil {
		return "", fmt.Errorf("start background job: %w", err)
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = fmt.Sprintf("exit %d", res.ExitCode)
		}
		return "", fmt.Errorf("start background job: %s", msg)
	}
	return bgJSON(map[string]any{
		"id":     id,
		"status": "running",
		"log":    bgJobDirFor(id) + "/log",
		"note":   "Running in the background. Use BashOutput with this bash_id to read new output and status; KillShell stops it.",
	}), nil
}

// registerBackground adds BashOutput and KillShell for jobs started by Bash
// with run_in_background.
func registerBackground(r *Registry, binder *AgentBinder) {
	r.Register(Tool{
		Name: "BashOutput",
		Description: "Read new output and status from a background job started by Bash with run_in_background. " +
			"Each call returns output produced since the previous call, the job status, and the exit code once it has finished.",
		Parameters: objectSchema(map[string]any{
			"bash_id": map[string]any{"type": "string", "description": "Job id returned by Bash"},
		}, "bash_id"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				BashID string `json:"bash_id"`
			}
			if err := json.Unmarshal(args, &in); err != nil || !bgIDPattern.MatchString(strings.TrimSpace(in.BashID)) {
				return "", fmt.Errorf("bash_id is required (the id returned by Bash)")
			}
			id := strings.TrimSpace(in.BashID)
			res, err := binder.runBGScript(ctx, actor, id, bgStatusScript, bgStatusTimeout)
			if err != nil {
				return "", err
			}
			st, err := parseBGStatus(string(res.Stdout))
			if errors.Is(err, errBGUnknownJob) {
				return "", fmt.Errorf("unknown background job %q (it may not exist in this workspace)", id)
			}
			if err != nil {
				return "", err
			}
			payload := map[string]any{
				"id":     id,
				"status": st.status,
				"output": truncateRunes(st.output, bgMaxReadBytes),
			}
			if st.exit != nil {
				payload["exitCode"] = *st.exit
			}
			remaining := st.size - (st.offset + len(st.output))
			if remaining < 0 {
				remaining = 0
			}
			payload["bytesRemaining"] = remaining
			var notes []string
			if st.status == "running" && st.output == "" {
				notes = append(notes, "No new output yet.")
			}
			if st.status == "exited" && st.exit == nil {
				notes = append(notes, "The job ended without recording an exit code (terminated or crashed).")
			}
			if remaining > 0 {
				notes = append(notes, "More output is buffered; call BashOutput again to read it.")
			}
			if len(notes) > 0 {
				payload["note"] = strings.Join(notes, " ")
			}
			return bgJSON(payload), nil
		},
	})
	r.Register(Tool{
		Name: "KillShell",
		Description: "Stop a background job started by Bash with run_in_background. " +
			"Sends SIGTERM to the job, then SIGKILL if it is still running.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"shell_id": map[string]any{"type": "string", "description": "Job id returned by Bash"},
		}, "shell_id"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				ShellID string `json:"shell_id"`
			}
			if err := json.Unmarshal(args, &in); err != nil || !bgIDPattern.MatchString(strings.TrimSpace(in.ShellID)) {
				return "", fmt.Errorf("shell_id is required (the id returned by Bash)")
			}
			id := strings.TrimSpace(in.ShellID)
			res, err := binder.runBGScript(ctx, actor, id, bgKillScript, bgKillTimeout)
			if err != nil {
				return "", err
			}
			st, err := parseBGStatus(string(res.Stdout))
			if errors.Is(err, errBGUnknownJob) {
				return "", fmt.Errorf("unknown background job %q (it may not exist in this workspace)", id)
			}
			if err != nil {
				return "", err
			}
			payload := map[string]any{"id": id, "status": st.status}
			if st.exit != nil {
				payload["exitCode"] = *st.exit
			}
			if st.status == "running" {
				payload["note"] = "The job is still running; it may ignore termination signals."
			} else {
				payload["note"] = "The job is no longer running."
			}
			return bgJSON(payload), nil
		},
	})
}
