package services

import (
	"strings"
	"testing"
	"time"

	"truedeploy/internal/databases"
	"truedeploy/internal/store"
)

func TestDatabaseLinks(t *testing.T) {
	st := newTestStore(t)
	st.Put(store.Server{ID: "s2", Name: "other", Host: "h2", Port: 22, User: "u", Auth: "password", CreatedAt: time.Now()}, store.Secrets{})
	db := store.Database{ID: "db1", ServerID: "s1", Name: "main", Slug: "main", Engine: "postgres", Version: "17", User: "app", DBName: "main", CreatedAt: time.Now()}
	st.PutDatabase(db, "secretpassword1234567890abcdef")
	far := store.Database{ID: "db2", ServerID: "s2", Name: "far", Slug: "far", Engine: "redis", Version: "7", CreatedAt: time.Now()}
	st.PutDatabase(far, "secretpassword1234567890abcdef")

	svc := NewProjectService(st, nil)
	base := ProjectInput{Name: "api", ServerID: "s1", RepoURL: "git@github.com:me/api.git", Runtime: "compose",
		Env: map[string]string{"NODE_ENV": "production"}}

	cases := map[string][]store.DBLink{
		"different server":   {{DatabaseID: "db2", EnvVar: "REDIS_URL"}},
		"no longer exists":   {{DatabaseID: "nope", EnvVar: "X"}},
		"not a valid":        {{DatabaseID: "db1", EnvVar: "BAD NAME"}},
		"both as a variable": {{DatabaseID: "db1", EnvVar: "NODE_ENV"}},
	}
	for want, links := range cases {
		in := base
		in.Links = links
		if _, err := svc.SaveProject(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}

	in := base
	in.Links = []store.DBLink{{DatabaseID: "db1", EnvVar: "DATABASE_URL"}}
	v, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	p, env, _ := st.GetProject(v.ID)
	if _, stored := env["DATABASE_URL"]; stored {
		t.Fatal("the database URL must be injected at deploy time, not stored in the project's env")
	}
	sp, err := svc.deploySpec(p, env)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Network != databases.Network || sp.Env["NODE_ENV"] != "production" ||
		!strings.HasPrefix(sp.Env["DATABASE_URL"], "postgresql://app:secretpassword") {
		t.Fatalf("deploy spec = %+v", sp)
	}
	if sp.Validate() != nil {
		t.Fatal("merged spec should validate")
	}

	dbs := NewDatabaseService(st)
	if _, err := dbs.DeleteDatabase("db1", false); err == nil || !strings.Contains(err.Error(), "linked to api") {
		t.Fatalf("deleting a linked database must be refused, got %v", err)
	}
	list, _ := dbs.ListDatabases()
	for _, d := range list {
		if d.ID == "db1" && (len(d.LinkedTo) != 1 || d.Host != "td-db-main" || d.EnvVar != "DATABASE_URL") {
			t.Fatalf("view = %+v", d)
		}
	}
}
