package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"

	"truedeploy/internal/setup"
	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
)

// Events streamed to the UI while setup steps run.
const (
	EventSetupLog      = "setup:log"
	EventSetupStep     = "setup:step"
	EventSetupFinished = "setup:finished"
)

type SetupLog struct {
	RunID  string `json:"runId"`
	StepID string `json:"stepId"`
	Stream string `json:"stream"` // "stdout" | "stderr"
	Line   string `json:"line"`
}

type SetupStepEvent struct {
	RunID      string `json:"runId"`
	StepID     string `json:"stepId"`
	Status     string `json:"status"` // "running" | "done" | "failed" | "cancelled"
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

type SetupFinished struct {
	RunID  string `json:"runId"`
	Status string `json:"status"` // "done" | "failed" | "cancelled"
	Error  string `json:"error,omitempty"`
}

func init() {
	application.RegisterEvent[SetupLog](EventSetupLog)
	application.RegisterEvent[SetupStepEvent](EventSetupStep)
	application.RegisterEvent[SetupFinished](EventSetupFinished)
}

type SetupOptions struct {
	// Mode is "login" (deploy as the SSH login user) or "create" (make a
	// dedicated user; needs admin rights). Empty picks the best fit.
	Mode       string `json:"mode"`
	DeployUser string `json:"deployUser"`
	// SudoPassword is used when sudo asks for a password. Empty means: use the
	// saved login password, but only if the account is in a sudo group.
	SudoPassword string `json:"sudoPassword,omitempty"`
}

type StepView struct {
	setup.Step
	Done bool `json:"done"`
}

type SetupPlan struct {
	OS         string      `json:"os"`
	Facts      setup.Facts `json:"facts"`
	Mode       string      `json:"mode"`
	DeployUser string      `json:"deployUser"`
	Target     string      `json:"target"` // account deployments will run under
	// NeedsSudoPassword: the account can probably use sudo, but needs a password.
	NeedsSudoPassword bool       `json:"needsSudoPassword"`
	SudoError         string     `json:"sudoError,omitempty"`
	Steps             []StepView `json:"steps"`
}

type SetupService struct {
	store *store.Store
	mu    sync.Mutex
	runs  map[string]context.CancelFunc
}

func NewSetupService(st *store.Store) *SetupService {
	return &SetupService{store: st, runs: map[string]context.CancelFunc{}}
}

// session is an open connection plus what the login account may do on it.
type session struct {
	c          *sshx.Client
	priv       setup.Privilege // how root steps get root
	pw         string          // sudo password for SudoWithPassword
	os         string
	facts      setup.Facts
	mode       setup.Mode
	deployUser string
	sudoErr    string
	steps      []setup.Step
}

// factsScript runs as the login user and never prompts (sudo -n).
const factsScript = `. /etc/os-release 2>/dev/null; echo "id=$ID $ID_LIKE"; echo "name=$PRETTY_NAME"
echo "user=$(id -un)"; echo "uid=$(id -u)"; echo "groups=$(id -nG)"
sudo -n true 2>/dev/null && echo sudo=yes
command -v docker >/dev/null 2>&1 && echo docker=yes
timeout 10 docker info >/dev/null 2>&1 && echo dockeraccess=yes
[ -x "$HOME/bin/docker" ] && DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock" timeout 10 "$HOME/bin/docker" info >/dev/null 2>&1 && echo rootless=yes
command -v newuidmap >/dev/null 2>&1 && echo uidmap=yes
grep -qs "^$(id -un):" /etc/subuid && echo subuid=yes
true`

func (s *SetupService) open(serverID string, opts SetupOptions) (*session, error) {
	t, srv, err := savedTarget(s.store, serverID)
	if err != nil {
		return nil, err
	}
	c, err := sshx.Dial(t, dialTimeout)
	if err != nil {
		return nil, friendly(err)
	}

	out, _ := c.Run(factsScript)
	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = v
		}
	}
	if ids := " " + kv["id"] + " "; !strings.Contains(ids, " debian ") && !strings.Contains(ids, " ubuntu ") {
		c.Close()
		return nil, fmt.Errorf("server setup supports Debian and Ubuntu for now (this server runs %s)", orUnknown(kv["name"]))
	}

	sess := &session{c: c, os: kv["name"]}
	f := setup.Facts{
		LoginUser:       kv["user"],
		DockerInstalled: kv["docker"] == "yes",
		DockerAccess:    kv["dockeraccess"] == "yes",
		RootlessDocker:  kv["rootless"] == "yes",
		UIDMap:          kv["uidmap"] == "yes",
		SubUID:          kv["subuid"] == "yes",
	}
	if f.LoginUser == "" {
		f.LoginUser = srv.User
	}

	groups := " " + kv["groups"] + " "
	inSudoGroup := strings.Contains(groups, " sudo ") || strings.Contains(groups, " wheel ") || strings.Contains(groups, " admin ")
	// Only try sudo with the login password for sudo-group members: a failed
	// sudo by a non-member is logged and often emailed to the admins.
	pw := opts.SudoPassword
	if pw == "" && inSudoGroup {
		pw = t.Password
	}

	switch {
	case kv["uid"] == "0":
		f.Access, f.AdminReady, sess.priv = setup.AccessRoot, true, setup.Direct
	case kv["sudo"] == "yes":
		f.Access, f.AdminReady, sess.priv = setup.AccessSudo, true, setup.SudoNoPassword
	case pw != "":
		switch checkSudo(c, pw) {
		case sudoOK:
			f.Access, f.AdminReady = setup.AccessSudoPassword, true
			sess.priv, sess.pw = setup.SudoWithPassword, pw
		case sudoNotAllowed:
			f.Access = setup.AccessNone
			if opts.SudoPassword != "" {
				sess.sudoErr = f.LoginUser + " isn't allowed to use sudo on this server."
			}
		default:
			f.Access = setup.AccessSudoPassword
			sess.sudoErr = "sudo rejected that password."
		}
	case inSudoGroup:
		f.Access = setup.AccessSudoPassword
	default:
		f.Access = setup.AccessNone
	}
	sess.facts = f

	sess.mode = setup.Mode(opts.Mode)
	if sess.mode != setup.ModeLogin && sess.mode != setup.ModeCreate {
		sess.mode = setup.ModeLogin
		if f.AdminReady && f.LoginUser == "root" {
			sess.mode = setup.ModeCreate
		}
	}
	sess.deployUser = strings.TrimSpace(opts.DeployUser)
	if sess.deployUser == "" {
		sess.deployUser = "deploy"
	}
	if sess.mode == setup.ModeCreate && !setup.ValidUser(sess.deployUser) {
		c.Close()
		return nil, fmt.Errorf("%q is not a valid Linux username (lowercase letters, digits, - and _)", sess.deployUser)
	}

	sess.steps = setup.Steps(setup.Options{
		Mode:       sess.mode,
		DeployUser: sess.deployUser,
		SSHPort:    srv.Port,
		KeyAuth:    srv.Auth == string(sshx.AuthKey),
	}, f)
	return sess, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "an unknown OS"
	}
	return s
}

type sudoResult int

const (
	sudoOK sudoResult = iota
	sudoNotAllowed
	sudoBadPassword
)

func checkSudo(c *sshx.Client, pw string) sudoResult {
	var msgs []string
	err := c.Stream(context.Background(), "sudo -S -p '' -v", setup.PasswordInput(setup.SudoWithPassword, pw),
		func(_, line string) { msgs = append(msgs, strings.ToLower(line)) })
	if err == nil {
		return sudoOK
	}
	all := strings.Join(msgs, "\n")
	if strings.Contains(all, "not in the sudoers") || strings.Contains(all, "may not run sudo") || strings.Contains(all, "not allowed") {
		return sudoNotAllowed
	}
	return sudoBadPassword
}

// privFor returns how to run a step: account steps run as the login user.
func (sess *session) privFor(st setup.Step) (setup.Privilege, string) {
	if st.RunAs == setup.RunAsUser {
		return setup.Direct, ""
	}
	return sess.priv, sess.pw
}

// exec runs a script; ok reports exit status 0.
func (sess *session) exec(ctx context.Context, p setup.Privilege, pw, script string, onLine func(stream, line string)) (ok bool, err error) {
	err = sess.c.Stream(ctx, setup.Wrap(script, p), setup.PasswordInput(p, pw), onLine)
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	return err == nil, err
}

func discard(string, string) {}

// Plan connects to the server and reports what the account can do and which
// steps are already done.
func (s *SetupService) Plan(serverID string, opts SetupOptions) (*SetupPlan, error) {
	sess, err := s.open(serverID, opts)
	if err != nil {
		return nil, err
	}
	defer sess.c.Close()

	plan := &SetupPlan{
		OS:                sess.os,
		Facts:             sess.facts,
		Mode:              string(sess.mode),
		DeployUser:        sess.deployUser,
		Target:            setup.Target(setup.Options{Mode: sess.mode, DeployUser: sess.deployUser}, sess.facts),
		NeedsSudoPassword: sess.facts.Access == setup.AccessSudoPassword && !sess.facts.AdminReady,
		SudoError:         sess.sudoErr,
	}
	ctx := context.Background()
	for _, st := range sess.steps {
		v := StepView{Step: st}
		switch {
		case st.Unavailable == "":
			p, pw := sess.privFor(st)
			v.Done, err = sess.exec(ctx, p, pw, st.Check, discard)
			if err != nil {
				return nil, fmt.Errorf("checking %q: %w", st.Title, err)
			}
		case st.CheckAsUser:
			// Can't run it, but we can still tell whether it's already done.
			v.Done, _ = sess.exec(ctx, setup.Direct, "", st.Check, discard)
		}
		plan.Steps = append(plan.Steps, v)
	}
	return plan, nil
}

// Run starts the given steps in order in the background and returns a run ID.
// Progress arrives as setup:step / setup:log events, ending with setup:finished.
func (s *SetupService) Run(serverID string, stepIDs []string, opts SetupOptions) (string, error) {
	if len(stepIDs) == 0 {
		return "", errors.New("no steps selected")
	}
	sess, err := s.open(serverID, opts)
	if err != nil {
		return "", err
	}
	var steps []setup.Step
	for _, id := range stepIDs {
		st, ok := setup.Find(sess.steps, id)
		if !ok {
			sess.c.Close()
			return "", fmt.Errorf("unknown step %q", id)
		}
		if st.Unavailable != "" {
			sess.c.Close()
			return "", fmt.Errorf("%s: %s", st.Title, st.Unavailable)
		}
		steps = append(steps, st)
	}

	runID := store.NewID()
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.runs[runID] = cancel
	s.mu.Unlock()

	go s.run(ctx, runID, sess, steps)
	return runID, nil
}

func (s *SetupService) run(ctx context.Context, runID string, sess *session, steps []setup.Step) {
	emit := application.Get().Event.Emit
	defer func() {
		sess.c.Close()
		s.mu.Lock()
		if cancel, ok := s.runs[runID]; ok {
			cancel()
			delete(s.runs, runID)
		}
		s.mu.Unlock()
	}()

	for _, st := range steps {
		start := time.Now()
		emit(EventSetupStep, SetupStepEvent{RunID: runID, StepID: st.ID, Status: "running"})
		p, pw := sess.privFor(st)
		ok, err := sess.exec(ctx, p, pw, st.Apply, func(stream, line string) {
			// apt/curl progress bars redraw with \r; keep only the final state.
			if i := strings.LastIndexByte(line, '\r'); i >= 0 {
				line = line[i+1:]
			}
			emit(EventSetupLog, SetupLog{RunID: runID, StepID: st.ID, Stream: stream, Line: line})
		})
		ev := SetupStepEvent{RunID: runID, StepID: st.ID, DurationMs: time.Since(start).Milliseconds()}
		switch {
		case ctx.Err() != nil:
			ev.Status = "cancelled"
			emit(EventSetupStep, ev)
			emit(EventSetupFinished, SetupFinished{RunID: runID, Status: "cancelled"})
			return
		case err != nil:
			ev.Status, ev.Error = "failed", err.Error()
		case !ok:
			ev.Status, ev.Error = "failed", "the step's commands exited with an error (see output)"
		default:
			ev.Status = "done"
		}
		emit(EventSetupStep, ev)
		if ev.Status == "failed" {
			emit(EventSetupFinished, SetupFinished{RunID: runID, Status: "failed", Error: st.Title + " failed"})
			return
		}
	}
	emit(EventSetupFinished, SetupFinished{RunID: runID, Status: "done"})
}

// Cancel stops a running setup. The current command is terminated.
func (s *SetupService) Cancel(runID string) {
	s.mu.Lock()
	cancel := s.runs[runID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
