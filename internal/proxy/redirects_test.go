package proxy

import (
	"strings"
	"testing"
)

func TestRedirectsInCaddyfile(t *testing.T) {
	sites := []Site{
		{Project: "web", Domains: []string{"shop.io"}, Port: 3000, Redirects: []string{"www.shop.io", "old-shop.com"}},
		{Project: "api", Domains: []string{"shop.io/api"}, Port: 4000},
		{Project: "docs", Domains: []string{"docs.shop.io/v2"}, Port: 5000, Redirects: []string{"help.shop.io"}},
	}
	if err := Validate(sites); err != nil {
		t.Fatal(err)
	}
	cf := Caddyfile(sites)
	for _, want := range []string{
		"# web (redirects)\nold-shop.com, www.shop.io {\n\tredir https://shop.io{uri} 308\n}\n",
		// The target is the first domain's host; the visitor's path is kept.
		"help.shop.io {\n\tredir https://docs.shop.io{uri} 308\n}\n",
	} {
		if !strings.Contains(cf, want) {
			t.Errorf("Caddyfile missing:\n%s\ngot:\n%s", want, cf)
		}
	}
	if strings.Index(cf, "# docs (redirects)") > strings.Index(cf, "# web (redirects)") {
		t.Error("redirect blocks should be sorted by project")
	}
}

func TestRedirectConflicts(t *testing.T) {
	cases := map[string][]Site{
		"is served by web": {
			{Project: "web", Domains: []string{"shop.io"}, Port: 1},
			{Project: "blog", Domains: []string{"blog.io"}, Port: 2, Redirects: []string{"shop.io"}},
		},
		"already one of web's domains": {
			{Project: "web", Domains: []string{"shop.io/app"}, Port: 1, Redirects: []string{"shop.io"}},
		},
		"redirects for both": {
			{Project: "a", Domains: []string{"a.io"}, Port: 1, Redirects: []string{"www.x.io"}},
			{Project: "b", Domains: []string{"b.io"}, Port: 2, Redirects: []string{"www.x.io"}},
		},
		"not a valid domain": {
			{Project: "a", Domains: []string{"a.io"}, Port: 1, Redirects: []string{"www.a.io/path"}},
		},
		"need a domain": {
			{Project: "a", Port: 1, Redirects: []string{"www.a.io"}},
		},
	}
	for want, sites := range cases {
		if err := Validate(sites); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
}

func TestWWWAlternate(t *testing.T) {
	for in, want := range map[string]string{"shop.io": "www.shop.io", "www.shop.io": "shop.io", "shop.io/api": "www.shop.io"} {
		if got := WWWAlternate(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
