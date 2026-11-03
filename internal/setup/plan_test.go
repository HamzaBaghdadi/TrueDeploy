package setup

import (
	"os/exec"
	"strings"
	"testing"
)

func ids(steps []Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func has(steps []Step, id string) bool {
	_, ok := Find(steps, id)
	return ok
}

func TestNoAdminAccess(t *testing.T) {
	f := Facts{LoginUser: "safeer", Access: AccessNone, UIDMap: true, SubUID: true}
	steps := Steps(Options{Mode: ModeLogin}, f)

	for _, s := range steps {
		if s.RunAs == RunAsRoot && s.Unavailable == "" {
			t.Errorf("%s needs root but is available without admin access", s.ID)
		}
		if s.RunAs == RunAsUser && s.Unavailable != "" {
			t.Errorf("%s runs as the user but is unavailable: %s", s.ID, s.Unavailable)
		}
	}
	if has(steps, "deploy-user") {
		t.Error("login mode must not create a deploy user")
	}
	if !has(steps, "rootless-docker") {
		t.Errorf("rootless Docker should be offered without admin or Docker access; got %v", ids(steps))
	}
	ws, _ := Find(steps, "workspace")
	if ws.RunAs != RunAsUser || strings.Contains(ws.Apply, "runuser") {
		t.Error("workspace for the login user must run as that user, not via root")
	}
}

func TestRootlessNeedsAdminPrereqs(t *testing.T) {
	f := Facts{LoginUser: "safeer", Access: AccessNone}
	s, ok := Find(Steps(Options{Mode: ModeLogin}, f), "rootless-docker")
	if !ok || s.Unavailable == "" {
		t.Fatal("rootless Docker without uidmap/subuid must be unavailable")
	}
	for _, want := range []string{"uidmap", "/etc/subuid", "safeer:100000:65536"} {
		if !strings.Contains(s.AdminCommands, want) {
			t.Errorf("admin commands should include %q: %s", want, s.AdminCommands)
		}
	}
}

func TestRootlessNotOfferedWhenDockerUsable(t *testing.T) {
	f := Facts{LoginUser: "safeer", Access: AccessNone, DockerInstalled: true, DockerAccess: true}
	if has(Steps(Options{Mode: ModeLogin}, f), "rootless-docker") {
		t.Error("no rootless Docker when the system Docker is already usable")
	}
	f = Facts{LoginUser: "root", Access: AccessRoot, AdminReady: true}
	if has(Steps(Options{Mode: ModeLogin}, f), "rootless-docker") {
		t.Error("no rootless Docker when admin can install the real one")
	}
}

func TestSudoPasswordPending(t *testing.T) {
	f := Facts{LoginUser: "ops", Access: AccessSudoPassword}
	s, _ := Find(Steps(Options{Mode: ModeLogin}, f), "docker")
	if !strings.Contains(s.Unavailable, "sudo password") {
		t.Errorf("root steps should ask for the sudo password, got %q", s.Unavailable)
	}
}

func TestCreateModeWithAdmin(t *testing.T) {
	f := Facts{LoginUser: "root", Access: AccessRoot, AdminReady: true}
	steps := Steps(Options{Mode: ModeCreate, DeployUser: "deploy", KeyAuth: true}, f)
	for _, s := range steps {
		if s.Unavailable != "" {
			t.Errorf("%s unavailable with root + key auth: %s", s.ID, s.Unavailable)
		}
	}
	if !has(steps, "deploy-user") || has(steps, "docker-access") {
		t.Errorf("create mode: want deploy-user, not docker-access; got %v", ids(steps))
	}
	ws, _ := Find(steps, "workspace")
	if ws.RunAs != RunAsRoot || !strings.Contains(ws.Apply, `runuser -u "$U"`) {
		t.Error("workspace for another user must run via runuser as root")
	}
	if !strings.Contains(ws.Title, "deploy") {
		t.Errorf("workspace title should name the deploy user: %q", ws.Title)
	}
}

func TestLoginModeWithSudoAddsDockerGroup(t *testing.T) {
	f := Facts{LoginUser: "ops", Access: AccessSudo, AdminReady: true}
	s, ok := Find(Steps(Options{Mode: ModeLogin}, f), "docker-access")
	if !ok || s.Unavailable != "" || !strings.Contains(s.Apply, "usermod -aG docker ops") {
		t.Fatalf("expected an available docker-access step for ops, got %+v", s)
	}
}

func TestPasswordLoginCannotDisablePasswords(t *testing.T) {
	f := Facts{LoginUser: "root", Access: AccessRoot, AdminReady: true}
	st, _ := Find(Steps(Options{KeyAuth: false}, f), "ssh-hardening")
	if st.Unavailable == "" {
		t.Fatal("ssh-hardening must be unavailable when connecting with a password")
	}
	st, _ = Find(Steps(Options{KeyAuth: true}, f), "ssh-hardening")
	if st.Unavailable != "" {
		t.Fatal("ssh-hardening should be available with key auth")
	}
}

func TestFirewallKeepsSSHPortOpen(t *testing.T) {
	f := Facts{LoginUser: "root", Access: AccessRoot, AdminReady: true}
	st, _ := Find(Steps(Options{SSHPort: 2222}, f), "firewall")
	allow := strings.Index(st.Apply, "ufw allow 2222/tcp")
	enable := strings.Index(st.Apply, "ufw --force enable")
	if allow < 0 || enable < 0 || allow > enable {
		t.Fatal("firewall must allow the SSH port before enabling ufw")
	}
}

// Every script, in every configuration, must at least be valid bash.
func TestAllScriptsParse(t *testing.T) {
	bash := findBash(t)
	configs := []struct {
		o Options
		f Facts
	}{
		{Options{Mode: ModeLogin}, Facts{LoginUser: "safeer", Access: AccessNone}},
		{Options{Mode: ModeLogin, KeyAuth: true}, Facts{LoginUser: "ops", Access: AccessSudo, AdminReady: true}},
		{Options{Mode: ModeCreate, DeployUser: "deploy", KeyAuth: true}, Facts{LoginUser: "root", Access: AccessRoot, AdminReady: true}},
	}
	for _, c := range configs {
		for _, s := range Steps(c.o, c.f) {
			for kind, script := range map[string]string{"apply": s.Apply, "check": s.Check} {
				out, err := exec.Command(bash, "-n", "-c", script).CombinedOutput()
				if err != nil {
					t.Errorf("%s %s (%s mode): %v\n%s", s.ID, kind, c.o.Mode, err, out)
				}
			}
		}
	}
}
