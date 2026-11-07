package databases

import (
	"net/url"
	"os/exec"
	"strings"
	"testing"
)

func spec(engine, version string) Spec {
	return Spec{Slug: "main-db", Engine: engine, Version: version, User: "app", Database: "app", Password: GeneratePassword()}
}

func TestURLs(t *testing.T) {
	s := spec("postgres", "17")
	s.Password = "pw-with/odd+chars123"
	u, err := url.Parse(URL(s))
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u.User.Password()
	if u.Scheme != "postgresql" || u.Host != "td-db-main-db:5432" || u.Path != "/app" || pw != s.Password {
		t.Fatalf("postgres URL = %s (password %q)", URL(s), pw)
	}
	for engine, prefix := range map[string]string{
		"mysql": "mysql://app:", "redis": "redis://:", "mongo": "mongodb://app:",
	} {
		v := map[string]string{"mysql": "8.4", "redis": "7", "mongo": "8"}[engine]
		if got := URL(spec(engine, v)); !strings.HasPrefix(got, prefix) || !strings.Contains(got, "td-db-main-db") {
			t.Errorf("%s URL = %s", engine, got)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	a, b := GeneratePassword(), GeneratePassword()
	if len(a) != 32 || a == b || url.QueryEscape(a) != a {
		t.Fatalf("passwords should be 32 URL-safe random chars: %q %q", a, b)
	}
}

func TestValidate(t *testing.T) {
	if err := spec("postgres", "17").Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []Spec{
		{Slug: "x", Engine: "oracle", Version: "1", Password: GeneratePassword()},
		{Slug: "Bad Name", Engine: "redis", Version: "7", Password: GeneratePassword()},
		{Slug: "x", Engine: "postgres", Version: "9", User: "app", Database: "app", Password: GeneratePassword()},
		{Slug: "x", Engine: "postgres", Version: "17", User: "app; drop", Database: "app", Password: GeneratePassword()},
		{Slug: "x", Engine: "redis", Version: "7", Password: "short"},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("expected %+v to be rejected", s)
		}
	}
	// Redis has no user/database names to validate.
	if err := (Spec{Slug: "cache", Engine: "redis", Version: "7", Password: GeneratePassword()}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordNeverOnCommandLine(t *testing.T) {
	for _, engine := range []string{"postgres", "mysql", "redis", "mongo"} {
		v := map[string]string{"postgres": "17", "mysql": "8.4", "redis": "7", "mongo": "8"}[engine]
		s := spec(engine, v)
		if strings.Contains(CreateScript(s), s.Password) {
			t.Errorf("%s: password appears in the create script in clear text", engine)
		}
		if b, err := BackupScript(s); err == nil && strings.Contains(b, s.Password) {
			t.Errorf("%s: password appears in the backup script in clear text", engine)
		}
	}
	if _, err := BackupScript(spec("redis", "7")); err == nil {
		t.Error("redis has no dump backup")
	}
}

func TestScriptsParse(t *testing.T) {
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && !strings.Contains(strings.ToLower(p), `\system32\`) {
		bash = p
	}
	if exec.Command(bash, "-c", "true").Run() != nil {
		t.Skip("no bash")
	}
	scripts := map[string]string{
		"status": StatusScript("x"), "delete": DeleteScript("x", true), "list": ListBackupsScript("x"), "network": NetworkScript,
	}
	for _, e := range Engines {
		s := spec(e.ID, e.Versions[0])
		scripts["create-"+e.ID] = CreateScript(s)
		if b, err := BackupScript(s); err == nil {
			scripts["backup-"+e.ID] = b
		}
	}
	for name, s := range scripts {
		if out, err := exec.Command(bash, "-n", "-c", s).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}
