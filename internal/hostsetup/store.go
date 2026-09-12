package hostsetup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend/qemu"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
)

const (
	StatusPendingConfirm = "pending_confirm"
	StatusPendingManual  = "pending_manual"
	StatusQueued         = "queued"
	StatusRunning        = "running"
	StatusSucceeded      = "succeeded"
	StatusFailed         = "failed"
	StatusSkipped        = "skipped"

	maxLogBytes = 256 * 1024
)

// ActionRun is one action's live status inside a plan.
type ActionRun struct {
	ActionID  string    `json:"actionId"`
	Title     string    `json:"title"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	Privilege Privilege `json:"privilege,omitempty"`
	Command   string    `json:"command"`
	Error     string    `json:"error,omitempty"`
	Log       string    `json:"log,omitempty"`
}

// PlanRecord is an in-memory setup plan owned by a user.
type PlanRecord struct {
	ID        string        `json:"id"`
	User      string        `json:"-"`
	Summary   string        `json:"summary"`
	Context   WizardContext `json:"context"`
	Actions   []ActionRun   `json:"actions"`
	CreatedAt time.Time     `json:"createdAt"`

	mu sync.Mutex
}

// Service owns plans and runs whitelist actions.
type Service struct {
	Probe   *runtime.Probe
	Runner  *Runner
	Cfg     *config.Config
	Facts   func() HostFacts
	PlanLLM func(ctx context.Context, w WizardContext, f HostFacts) (Plan, error)
	// Privilege overrides ProbePrivilege in tests; nil uses ProbePrivilege.
	Privilege func() Privilege

	plans sync.Map // id → *PlanRecord
}

func (s *Service) facts() HostFacts {
	if s.Facts != nil {
		return s.Facts()
	}
	return defaultFacts(s.Probe)
}

func defaultFacts(p *runtime.Probe) HostFacts {
	f := HostFacts{}
	if p != nil {
		f.DockerReady = p.DockerReady
		if p.DockerCheck != nil {
			f.DockerReady = p.DockerCheck()
		}
	}
	f.BinariesOK = qemu.BinariesAvailable() == nil
	browserImg := "images/browser-qemu/out/browser.qcow2"
	if p != nil && p.Cfg != nil {
		if v := strings.TrimSpace(p.Cfg.BrowserImage); v != "" {
			browserImg = v
		}
	}
	f.BrowserImageOK = qemu.ValidateImage(browserImg) == nil
	return f
}

// CreatePlan builds and starts a plan for the user.
func (s *Service) CreatePlan(ctx context.Context, user string, w WizardContext) (*PlanRecord, error) {
	f := s.facts()
	priv := ProbePrivilege()
	if s.Privilege != nil {
		priv = s.Privilege()
	}
	var plan Plan
	var err error
	if s.PlanLLM != nil {
		plan, err = s.PlanLLM(ctx, w, f)
		if err != nil {
			plan = PlanFromFacts(w, f, priv)
		} else {
			plan = Reconcile(plan, f, priv, w)
		}
	} else {
		plan = PlanFromFacts(w, f, priv)
	}

	rec := &PlanRecord{
		ID:        newID(),
		User:      user,
		Summary:   plan.Summary,
		Context:   w,
		CreatedAt: time.Now().UTC(),
		Actions:   []ActionRun{},
	}
	for _, a := range plan.Actions {
		ar := ActionRun{
			ActionID:  a.ID,
			Title:     a.Title,
			Reason:    a.Reason,
			Privilege: a.Privilege,
			Command:   a.Command,
		}
		def, _ := LookupAction(a.ID)
		switch {
		case def.Sensitive && a.Privilege == PrivilegeManual:
			ar.Status = StatusPendingManual
		case def.Sensitive && a.Privilege == PrivilegeAuto:
			ar.Status = StatusPendingConfirm
		default:
			ar.Status = StatusQueued
		}
		rec.Actions = append(rec.Actions, ar)
	}
	s.plans.Store(rec.ID, rec)
	s.kickAuto(rec)
	return s.snapshot(rec), nil
}

func (s *Service) Get(user, id string) (*PlanRecord, error) {
	rec, err := s.load(user, id)
	if err != nil {
		return nil, err
	}
	return s.snapshot(rec), nil
}

// Ready reports whether all actions succeeded or were skipped (or plan empty).
func (s *Service) Ready(user, id string) (bool, error) {
	rec, err := s.load(user, id)
	if err != nil {
		return false, err
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, a := range rec.Actions {
		if a.Status != StatusSucceeded && a.Status != StatusSkipped {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) Confirm(user, id, actionID string) (*PlanRecord, error) {
	rec, err := s.load(user, id)
	if err != nil {
		return nil, err
	}
	rec.mu.Lock()
	idx := -1
	for i := range rec.Actions {
		if rec.Actions[i].ActionID == actionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		rec.mu.Unlock()
		return nil, fmt.Errorf("action not found")
	}
	a := &rec.Actions[idx]
	if a.Status != StatusPendingConfirm {
		rec.mu.Unlock()
		return nil, fmt.Errorf("action is not awaiting confirm (status=%s)", a.Status)
	}
	if a.Privilege != PrivilegeAuto {
		rec.mu.Unlock()
		return nil, fmt.Errorf("use recheck for manual privilege actions")
	}
	a.Status = StatusQueued
	rec.mu.Unlock()
	s.kickAuto(rec)
	return s.snapshot(rec), nil
}

func (s *Service) Recheck(user, id, actionID string) (*PlanRecord, error) {
	rec, err := s.load(user, id)
	if err != nil {
		return nil, err
	}
	f := s.facts()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i := range rec.Actions {
		a := &rec.Actions[i]
		if a.ActionID != actionID {
			continue
		}
		ok := false
		switch a.ActionID {
		case ActionInstallDocker:
			ok = f.DockerReady
		case ActionInstallQEMU:
			ok = f.BinariesOK
		case ActionBuildBrowserImage:
			ok = f.BrowserImageOK
		}
		if ok {
			a.Status = StatusSucceeded
			a.Error = ""
		} else {
			a.Error = "仍未检测到所需组件，请确认命令已成功执行"
			if a.Status != StatusPendingManual && a.Status != StatusFailed {
				a.Status = StatusPendingManual
			}
		}
		return s.snapshotLocked(rec), nil
	}
	return nil, fmt.Errorf("action not found")
}

func (s *Service) Retry(user, id, actionID string) (*PlanRecord, error) {
	rec, err := s.load(user, id)
	if err != nil {
		return nil, err
	}
	rec.mu.Lock()
	idx := -1
	for i := range rec.Actions {
		if rec.Actions[i].ActionID == actionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		rec.mu.Unlock()
		return nil, fmt.Errorf("action not found")
	}
	a := &rec.Actions[idx]
	if a.Status != StatusFailed {
		rec.mu.Unlock()
		return nil, fmt.Errorf("only failed actions can be retried")
	}
	def, _ := LookupAction(a.ActionID)
	if def.Sensitive {
		if a.Privilege == PrivilegeManual {
			a.Status = StatusPendingManual
		} else {
			a.Status = StatusPendingConfirm
		}
	} else {
		a.Status = StatusQueued
	}
	a.Error = ""
	rec.mu.Unlock()
	s.kickAuto(rec)
	return s.snapshot(rec), nil
}

func (s *Service) kickAuto(rec *PlanRecord) {
	rec.mu.Lock()
	var toRun []string
	for i := range rec.Actions {
		if rec.Actions[i].Status == StatusQueued {
			toRun = append(toRun, rec.Actions[i].ActionID)
		}
	}
	rec.mu.Unlock()
	for _, id := range toRun {
		actionID := id
		go s.runAction(rec, actionID)
	}
}

func (s *Service) runAction(rec *PlanRecord, actionID string) {
	rec.mu.Lock()
	var priv Privilege
	var idx int = -1
	for i := range rec.Actions {
		if rec.Actions[i].ActionID == actionID && rec.Actions[i].Status == StatusQueued {
			rec.Actions[i].Status = StatusRunning
			priv = rec.Actions[i].Privilege
			idx = i
			break
		}
	}
	rec.mu.Unlock()
	if idx < 0 {
		return
	}
	if s.Runner == nil {
		rec.mu.Lock()
		rec.Actions[idx].Status = StatusFailed
		rec.Actions[idx].Error = "runner not configured"
		rec.mu.Unlock()
		return
	}
	var buf limitedBuffer
	err := s.Runner.Run(context.Background(), actionID, priv, &buf)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	// re-find index in case slice changed (shouldn't)
	for i := range rec.Actions {
		if rec.Actions[i].ActionID == actionID {
			rec.Actions[i].Log = buf.String()
			if err != nil {
				rec.Actions[i].Status = StatusFailed
				rec.Actions[i].Error = err.Error()
			} else {
				rec.Actions[i].Status = StatusSucceeded
				rec.Actions[i].Error = ""
			}
			return
		}
	}
}

func (s *Service) load(user, id string) (*PlanRecord, error) {
	v, ok := s.plans.Load(id)
	if !ok {
		return nil, fmt.Errorf("plan not found")
	}
	rec := v.(*PlanRecord)
	if rec.User != user {
		return nil, fmt.Errorf("plan not found")
	}
	return rec, nil
}

func (s *Service) snapshot(rec *PlanRecord) *PlanRecord {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return s.snapshotLocked(rec)
}

func (s *Service) snapshotLocked(rec *PlanRecord) *PlanRecord {
	actions := append([]ActionRun{}, rec.Actions...)
	if actions == nil {
		actions = []ActionRun{}
	}
	out := &PlanRecord{
		ID:        rec.ID,
		Summary:   rec.Summary,
		Context:   rec.Context,
		CreatedAt: rec.CreatedAt,
		Actions:   actions,
	}
	return out
}

type limitedBuffer struct {
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len() >= maxLogBytes {
		return len(p), nil
	}
	remain := maxLogBytes - b.buf.Len()
	if len(p) > remain {
		p = p[:remain]
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string { return b.buf.String() }

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
