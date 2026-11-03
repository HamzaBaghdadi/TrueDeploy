// Package setup defines the server-preparation steps TrueDeploy can run
// over SSH on Debian/Ubuntu hosts. Each step has a Check script (exit 0 =
// already done) and an Apply script, both idempotent.
//
// Steps adapt to what the login account is allowed to do: "server" steps
// need root (directly or via sudo); "account" steps run as the login user
// and work even without any admin access.
package setup

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

// Access is how much power the login account has on the server.
type Access string

const (
	AccessRoot         Access = "root"          // logged in as root
	AccessSudo         Access = "sudo"          // passwordless sudo
	AccessSudoPassword Access = "sudo-password" // sudo after entering a password
	AccessNone         Access = "none"          // no admin rights
)

// Mode decides which account deployments run under.
type Mode string

const (
	ModeLogin  Mode = "login"  // use the account TrueDeploy connects with
	ModeCreate Mode = "create" // create a dedicated deploy user (needs admin)
)

// Facts are what TrueDeploy learned about the server before planning.
type Facts struct {
	LoginUser       string `json:"loginUser"`
	Access          Access `json:"access"`
	AdminReady      bool   `json:"adminReady"` // root steps can run right now
	DockerInstalled bool   `json:"dockerInstalled"`
	DockerAccess    bool   `json:"dockerAccess"` // login user can use the system Docker
	RootlessDocker  bool   `json:"rootlessDocker"`
	UIDMap          bool   `json:"uidmap"` // newuidmap present (rootless prerequisite)
	SubUID          bool   `json:"subuid"` // /etc/subuid entry for the login user
}

type Options struct {
	Mode       Mode
	DeployUser string // used with ModeCreate
	SSHPort    int    // port the firewall must keep open
	KeyAuth    bool   // TrueDeploy logs in with a key, so disabling passwords is safe
}

const (
	GroupServer  = "server"
	GroupAccount = "account"
	RunAsRoot    = "root"
	RunAsUser    = "user"
)

type Step struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Group       string `json:"group"` // "server" | "account"
	RunAs       string `json:"runAs"` // "root" | "user"
	Apply       string `json:"script"`
	Check       string `json:"-"`
	// CheckAsUser: the check works without root, so "already done" can be
	// reported even when the step itself can't run.
	CheckAsUser bool `json:"-"`
	// Unavailable explains why the step can't run here ("" = available).
	Unavailable string `json:"unavailable,omitempty"`
	// AdminCommands an admin can run to make the step available.
	AdminCommands string `json:"adminCommands,omitempty"`
	Recommended   bool   `json:"recommended"`
}

var userRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func ValidUser(u string) bool { return userRe.MatchString(u) }

const aptEnv = `export DEBIAN_FRONTEND=noninteractive
APT="apt-get -o DPkg::Lock::Timeout=300 -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold"
`

// Target is the account deployments will run under.
func Target(o Options, f Facts) string {
	if o.Mode == ModeCreate && o.DeployUser != "" {
		return o.DeployUser
	}
	return f.LoginUser
}

// Steps returns the steps for this server, in run order.
func Steps(o Options, f Facts) []Step {
	if o.SSHPort == 0 {
		o.SSHPort = 22
	}
	if o.Mode == "" {
		o.Mode = ModeLogin
	}
	target := Target(o, f)

	adminReason := ""
	switch {
	case f.AdminReady:
	case f.Access == AccessNone:
		adminReason = fmt.Sprintf("Needs root or sudo, and %s has no admin rights. Ask your server admin to do this.", f.LoginUser)
	default:
		adminReason = "Enter the sudo password above to enable this step."
	}

	var steps []Step
	add := func(s Step) {
		if s.RunAs == "" {
			s.RunAs = RunAsRoot
		}
		if s.Group == "" {
			s.Group = GroupServer
		}
		if s.RunAs == RunAsRoot && s.Unavailable == "" {
			s.Unavailable = adminReason
		} else if s.Unavailable == "" {
			s.AdminCommands = "" // only shown for blocked steps
		}
		steps = append(steps, s)
	}

	add(Step{
		ID:          "update",
		Title:       "Update system packages",
		Description: "Refreshes package lists and installs all pending updates.",
		Recommended: true,
		Check:       `exit 1`,
		Apply: aptEnv + `$APT update
$APT upgrade
echo "System packages are up to date."`,
	})
	add(Step{
		ID:          "essentials",
		Title:       "Install essential tools",
		Description: "curl, git, ca-certificates and ufw: needed by the other steps and by deployments.",
		Recommended: true,
		CheckAsUser: true,
		Check:       `for p in curl git ufw; do command -v "$p" >/dev/null || exit 1; done`,
		Apply: aptEnv + `$APT update
$APT install curl git ca-certificates ufw
echo "Essential tools installed."`,
	})
	add(Step{
		ID:          "docker",
		Title:       "Install Docker",
		Description: "Docker Engine and the Compose plugin from Docker's official repository, started on boot.",
		Recommended: true,
		CheckAsUser: true,
		Check:       `command -v docker >/dev/null && docker compose version >/dev/null 2>&1`,
		Apply: aptEnv + `command -v curl >/dev/null || { $APT update && $APT install curl ca-certificates; }
curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
sh /tmp/get-docker.sh
rm -f /tmp/get-docker.sh
systemctl enable --now docker
docker --version
docker compose version`,
	})

	// Without admin rights, rootless Docker is the way to get containers.
	if o.Mode == ModeLogin && !f.AdminReady && !f.DockerAccess {
		s := Step{
			ID:          "rootless-docker",
			Title:       "Install rootless Docker",
			Description: rootlessDescription(f),
			Group:       GroupAccount,
			RunAs:       RunAsUser,
			Recommended: true,
			Check:       `[ -x "$HOME/bin/docker" ] && DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock" "$HOME/bin/docker" info >/dev/null 2>&1`,
			Apply: `command -v curl >/dev/null || { echo "curl is not installed; ask your admin to install it." >&2; exit 1; }
curl -fsSL https://get.docker.com/rootless | sh
if ! loginctl enable-linger "$(id -un)" 2>/dev/null; then
  echo "Note: couldn't enable lingering, so Docker stops when you log out."
  echo "Ask your admin to run: sudo loginctl enable-linger $(id -un)"
fi
if ! grep -q 'TrueDeploy rootless docker' "$HOME/.bashrc" 2>/dev/null; then
  cat >> "$HOME/.bashrc" <<'EOF'
# TrueDeploy rootless docker
export PATH="$HOME/bin:$PATH"
export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
EOF
fi
DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock" "$HOME/bin/docker" info --format 'Rootless Docker {{.ServerVersion}} is running.'`,
		}
		var missing []string
		if !f.UIDMap {
			missing = append(missing, "sudo apt-get install -y uidmap dbus-user-session")
		}
		if !f.SubUID {
			missing = append(missing, fmt.Sprintf("echo '%[1]s:100000:65536' | sudo tee -a /etc/subuid /etc/subgid", f.LoginUser))
		}
		if len(missing) > 0 {
			s.Unavailable = "Rootless Docker needs a one-time change by your server admin. Send them this command:"
			s.AdminCommands = strings.Join(missing, " && ")
		}
		add(s)
	}

	if o.Mode == ModeCreate {
		add(Step{
			ID:    "deploy-user",
			Title: fmt.Sprintf("Create %q user", target),
			Description: "A non-root account in the docker group for running deployments. " +
				"Your current SSH keys are copied to it so it can log in the same way.",
			Recommended: true,
			CheckAsUser: true,
			Check:       fmt.Sprintf(`id -u %[1]s >/dev/null 2>&1 && id -nG %[1]s | grep -qw docker`, target),
			Apply: fmt.Sprintf(`U=%s
id -u "$U" >/dev/null 2>&1 || adduser --disabled-password --gecos "" "$U"
getent group docker >/dev/null || groupadd docker
usermod -aG docker "$U"
HOME_DIR=$(getent passwd "$U" | cut -d: -f6)
install -d -m 700 -o "$U" -g "$U" "$HOME_DIR/.ssh"
SRC_HOME=$(getent passwd "${SUDO_USER:-root}" | cut -d: -f6)
if [ -s "$SRC_HOME/.ssh/authorized_keys" ]; then
  touch "$HOME_DIR/.ssh/authorized_keys"
  sort -u "$SRC_HOME/.ssh/authorized_keys" "$HOME_DIR/.ssh/authorized_keys" -o "$HOME_DIR/.ssh/authorized_keys"
  echo "Copied SSH keys from ${SUDO_USER:-root}."
else
  echo "No SSH keys to copy from ${SUDO_USER:-root}; add one later to log in as $U."
fi
chown "$U:$U" "$HOME_DIR/.ssh/authorized_keys" 2>/dev/null || true
chmod 600 "$HOME_DIR/.ssh/authorized_keys" 2>/dev/null || true
echo "User $U is ready (groups: $(id -nG "$U"))."`, target),
		})
	} else if target != "root" {
		add(Step{
			ID:            "docker-access",
			Title:         fmt.Sprintf("Let %s use Docker", target),
			Description:   fmt.Sprintf("Adds %s to the docker group so deployments don't need sudo.", target),
			Recommended:   true,
			CheckAsUser:   true,
			Check:         fmt.Sprintf(`id -nG %s | grep -qw docker`, target),
			AdminCommands: fmt.Sprintf("sudo usermod -aG docker %s", target),
			Apply: fmt.Sprintf(`getent group docker >/dev/null || groupadd docker
usermod -aG docker %[1]s
echo "%[1]s is now in the docker group (takes effect on the next login)."`, target),
		})
	}

	add(workspaceStep(target, f.LoginUser))

	add(Step{
		ID:    "firewall",
		Title: "Enable firewall",
		Description: fmt.Sprintf("Allows SSH (port %d), HTTP (80) and HTTPS (443) and blocks everything else. "+
			"Note: ports published by Docker containers bypass ufw.", o.SSHPort),
		Recommended: true,
		Check:       fmt.Sprintf(`ufw status 2>/dev/null | grep -q "Status: active" && ufw status | grep -qE "^%d(/tcp)? +ALLOW"`, o.SSHPort),
		Apply: aptEnv + fmt.Sprintf(`command -v ufw >/dev/null || { $APT update && $APT install ufw; }
ufw allow %d/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable
ufw status verbose`, o.SSHPort),
	})
	add(Step{
		ID:          "fail2ban",
		Title:       "Block brute-force logins",
		Description: "Installs fail2ban, which temporarily bans IPs after repeated failed SSH logins.",
		Recommended: true,
		CheckAsUser: true,
		Check:       `systemctl is-active --quiet fail2ban`,
		Apply: aptEnv + `$APT install fail2ban
systemctl enable --now fail2ban
fail2ban-client status sshd || fail2ban-client status`,
	})
	add(Step{
		ID:          "auto-updates",
		Title:       "Automatic security updates",
		Description: "Installs security patches daily with unattended-upgrades.",
		Recommended: true,
		CheckAsUser: true,
		Check:       `dpkg -s unattended-upgrades >/dev/null 2>&1 && grep -qs 'Unattended-Upgrade "1"' /etc/apt/apt.conf.d/20auto-upgrades`,
		Apply: aptEnv + `$APT install unattended-upgrades
cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
systemctl enable --now unattended-upgrades
echo "Automatic security updates enabled."`,
	})

	hardening := Step{
		ID:    "ssh-hardening",
		Title: "Disable password logins",
		Description: "Only SSH keys can log in afterwards, which stops password-guessing attacks. " +
			"The config is validated before SSH is reloaded.",
		// Named 01- so it wins over 50-cloud-init.conf (sshd uses the first value it reads).
		Check: `sshd -T 2>/dev/null | grep -qi '^passwordauthentication no'`,
		Apply: `cat > /etc/ssh/sshd_config.d/01-truedeploy.conf <<'EOF'
# Managed by TrueDeploy
PasswordAuthentication no
KbdInteractiveAuthentication no
EOF
if ! sshd -t; then
  rm -f /etc/ssh/sshd_config.d/01-truedeploy.conf
  echo "sshd config test failed; change reverted." >&2
  exit 1
fi
systemctl reload ssh 2>/dev/null || systemctl reload sshd
echo "Password logins disabled."`,
	}
	if !o.KeyAuth {
		hardening.Unavailable = "TrueDeploy connects to this server with a password, so this would lock it out. Switch the server to SSH key login first."
	}
	add(hardening)

	return steps
}

func rootlessDescription(f Facts) string {
	d := "Runs Docker inside your own account, no admin rights needed. " +
		"Containers can't use ports below 1024 (put a proxy on 80/443 later, or use high ports)."
	if f.DockerInstalled {
		d = fmt.Sprintf("Docker is installed but %s can't use it. Ask your admin to add you to the docker group "+
			"(see \"Let %[1]s use Docker\"), or run your own rootless Docker instead. ", f.LoginUser) + d
	}
	return d
}

const workspaceScript = `mkdir -p "$HOME/apps"
install -d -m 700 "$HOME/.ssh"
if [ ! -f "$HOME/.ssh/truedeploy_ed25519" ]; then
  ssh-keygen -q -t ed25519 -N "" -C "truedeploy@$(hostname)" -f "$HOME/.ssh/truedeploy_ed25519"
fi
echo "Apps folder: $HOME/apps"
echo "Deploy key public part (add it to GitHub/GitLab as a read-only deploy key):"
cat "$HOME/.ssh/truedeploy_ed25519.pub"`

// workspaceStep prepares the deploy account's apps folder and Git deploy key.
// It runs as the login user when that's the target, otherwise as root via runuser.
func workspaceStep(target, login string) Step {
	s := Step{
		ID:    "workspace",
		Title: fmt.Sprintf("Prepare %s's workspace", target),
		Description: "Creates ~/apps for deployed projects and an SSH deploy key " +
			"(~/.ssh/truedeploy_ed25519) for pulling code from GitHub/GitLab.",
		Group:       GroupAccount,
		Recommended: true,
	}
	if target == login {
		s.RunAs = RunAsUser
		s.Check = `[ -d "$HOME/apps" ] && [ -f "$HOME/.ssh/truedeploy_ed25519" ]`
		s.Apply = workspaceScript
		return s
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("set -euo pipefail\n" + workspaceScript + "\n"))
	s.RunAs = RunAsRoot
	s.Check = fmt.Sprintf(`H=$(getent passwd %[1]s | cut -d: -f6) && [ -d "$H/apps" ] && [ -f "$H/.ssh/truedeploy_ed25519" ]`, target)
	s.Apply = fmt.Sprintf(`U=%s
id -u "$U" >/dev/null 2>&1 || { echo "User $U does not exist yet: create it first." >&2; exit 1; }
H=$(getent passwd "$U" | cut -d: -f6)
cd "$H"
# Runs this script as $U:
#%s
runuser -u "$U" -- env HOME="$H" bash -c "$(echo %s | base64 -d)"`,
		target, strings.ReplaceAll(workspaceScript, "\n", "\n#"), b64)
	return s
}

// Find returns the step with the given id.
func Find(steps []Step, id string) (Step, bool) {
	for _, s := range steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// Privilege describes how a command is run on the server.
type Privilege int

const (
	Direct           Privilege = iota // as the login user (root if logged in as root)
	SudoNoPassword                    // passwordless sudo
	SudoWithPassword                  // sudo -S, password on stdin
)

// Wrap turns a bash script into a single remote command.
// The script travels base64-encoded so no shell quoting can break it, and runs
// with stdin from /dev/null so nothing inside can swallow the sudo password.
func Wrap(script string, p Privilege) string {
	b64 := base64.StdEncoding.EncodeToString([]byte("set -euo pipefail\n" + script + "\n"))
	inner := fmt.Sprintf(`bash -c "$(echo %s | base64 -d)" </dev/null`, b64)
	switch p {
	case SudoNoPassword:
		return "sudo -n bash -c '" + inner + "'"
	case SudoWithPassword:
		return "sudo -S -p '' bash -c '" + inner + "'"
	}
	return "bash -c '" + inner + "'"
}

// PasswordInput is the stdin to send for SudoWithPassword.
func PasswordInput(p Privilege, password string) string {
	if p != SudoWithPassword {
		return ""
	}
	return strings.TrimRight(password, "\r\n") + "\n"
}
