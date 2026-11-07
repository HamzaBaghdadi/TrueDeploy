package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"truedeploy/internal/alerts"
	"truedeploy/internal/sitecheck"
	"truedeploy/internal/store"
)

func TestCheckSitesSkipsDomainsNotPointingHere(t *testing.T) {
	origLookup, origGet := lookup, httpsGet
	t.Cleanup(func() { lookup, httpsGet = origLookup, origGet })
	dns := map[string][]string{"vps.example.com": {"1.2.3.4"}, "shop.io": {"1.2.3.4"}, "moved.io": {"5.5.5.5"}, "down.io": {"1.2.3.4"}}
	lookup = func(_ context.Context, h string) ([]string, error) {
		if ips, ok := dns[h]; ok {
			return ips, nil
		}
		return nil, errors.New("no such host")
	}
	var asked []string
	var mu sync.Mutex
	httpsGet = func(_ context.Context, url string) sitecheck.Result {
		mu.Lock()
		asked = append(asked, url)
		mu.Unlock()
		if url == "https://down.io/" {
			return sitecheck.Failed(url, errors.New("connection refused"))
		}
		return sitecheck.Result{URL: url, Status: 200, CertExpires: time.Now().Add(5*24*time.Hour + time.Hour)}
	}

	servers := []store.Server{{ID: "s1", Host: "vps.example.com"}}
	projects := []store.Project{
		{ID: "p1", ServerID: "s1", Domains: []string{"shop.io", "moved.io", "missing.io", "down.io"}},
		{ID: "p2", ServerID: "s1", Domains: []string{"shop.io"}}, // never deployed
	}
	pobs := []alerts.ProjectObs{{ID: "p1", Deployed: true}, {ID: "p2"}}
	checkSites(context.Background(), servers, projects, pobs)

	if len(asked) != 2 {
		t.Errorf("only shop.io and down.io of p1 should be requested, got %v", asked)
	}
	s := pobs[0].Sites
	if len(s) != 2 || s[0].Domain != "down.io" || s[0].Up || s[0].Err == "" || s[1].Domain != "shop.io" || !s[1].Up || s[1].CertDays != 5 {
		t.Errorf("sites: %+v", s)
	}
	if len(pobs[1].Sites) != 0 {
		t.Error("undeployed projects aren't checked")
	}
}
