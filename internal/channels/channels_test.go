package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"truedeploy/internal/alerts"
)

var disk = alerts.Alert{Key: "disk:s1", Severity: alerts.Warning, Title: "Disk 92% full on vps", Detail: "Free up space: docker system prune <images>", ServerID: "s1"}

func capture(t *testing.T, status int, reply string) (*httptest.Server, *[]map[string]any, *[]string) {
	t.Helper()
	var bodies []map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &paths
}

func TestPayloadsPerKind(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	srv, bodies, paths := capture(t, 204, "")

	for _, c := range []Channel{
		{Kind: Discord, URL: srv.URL + "/api/webhooks/1/x"},
		{Kind: Slack, URL: srv.URL + "/services/T/B/x"},
		{Kind: Webhook, URL: srv.URL + "/hook"},
	} {
		if err := Send(ctx, srv.Client(), c, Message{Kind: Opened, Alert: disk, At: at}); err != nil {
			t.Fatalf("%s: %v", c.Kind, err)
		}
	}
	TelegramAPI = srv.URL
	tg := Channel{Kind: Telegram, Token: "123456:" + strings.Repeat("a", 35), ChatID: "-100123"}
	if err := Send(ctx, srv.Client(), tg, Message{Kind: Resolved, Alert: disk, At: at}); err != nil {
		t.Fatal(err)
	}

	d := (*bodies)[0]["embeds"].([]any)[0].(map[string]any)
	if d["title"] != "🟠 Disk 92% full on vps" || d["color"].(float64) != 0xF5B84A || d["timestamp"] != "2026-11-05T10:00:00Z" {
		t.Errorf("discord embed: %v", d)
	}
	if s := (*bodies)[1]["text"].(string); s != "*🟠 Disk 92% full on vps*\nFree up space: docker system prune &lt;images&gt;" {
		t.Errorf("slack text: %q", s)
	}
	w := (*bodies)[2]
	if w["event"] != "alert.opened" || w["alert"].(map[string]any)["key"] != "disk:s1" {
		t.Errorf("webhook: %v", w)
	}
	if (*paths)[3] != "/bot"+tg.Token+"/sendMessage" || (*bodies)[3]["chat_id"] != "-100123" ||
		(*bodies)[3]["text"] != "✅ Resolved: Disk 92% full on vps\nBack to normal." {
		t.Errorf("telegram: %s %v", (*paths)[3], (*bodies)[3])
	}
}

func TestErrorsAreReadableAndHideSecrets(t *testing.T) {
	ctx := context.Background()
	srv, _, _ := capture(t, 400, `{"ok":false,"description":"Bad Request: chat not found"}`)
	TelegramAPI = srv.URL
	token := "123456:" + strings.Repeat("s", 35)
	err := Send(ctx, srv.Client(), Channel{Kind: Telegram, Token: token, ChatID: "42424"}, Message{Kind: Test})
	if err == nil || !strings.Contains(err.Error(), "chat not found") || !strings.Contains(err.Error(), "send your bot a message") || strings.Contains(err.Error(), token) {
		t.Errorf("telegram error: %v", err)
	}

	gone, _, _ := capture(t, 404, "Unknown Webhook")
	err = Send(ctx, gone.Client(), Channel{Kind: Discord, URL: gone.URL + "/api/webhooks/1/SECRET"}, Message{Kind: Test})
	if err == nil || !strings.Contains(err.Error(), "doesn't exist anymore") || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("discord error: %v", err)
	}

	err = Send(ctx, http.DefaultClient, Channel{Kind: Webhook, URL: "http://127.0.0.1:1/x?token=SECRET"}, Message{Kind: Test})
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "couldn't reach 127.0.0.1:1") {
		t.Errorf("unreachable error: %v", err)
	}
}

func TestValidateAndWants(t *testing.T) {
	ok := []Channel{
		{Name: "d", Kind: Discord, URL: "https://discord.com/api/webhooks/123/abc"},
		{Name: "s", Kind: Slack, URL: "https://hooks.slack.com/services/T0/B0/xyz"},
		{Name: "w", Kind: Webhook, URL: "https://ntfy.sh/my-topic"},
		{Name: "t", Kind: Telegram, Token: "123456789:" + strings.Repeat("A", 35), ChatID: "-1001234567890"},
		{Name: "t", Kind: Telegram, Token: "123456789:" + strings.Repeat("A", 35), ChatID: "@ops_alerts"},
	}
	for _, c := range ok {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := []Channel{
		{Name: "", Kind: Discord, URL: "https://discord.com/api/webhooks/1/a"},
		{Name: "d", Kind: Discord, URL: "https://example.com/api/webhooks/1/a"},
		{Name: "d", Kind: Discord, URL: "http://discord.com/api/webhooks/1/a"},
		{Name: "s", Kind: Slack, URL: "https://hooks.slack.com/other"},
		{Name: "w", Kind: Webhook, URL: "not a url"},
		{Name: "t", Kind: Telegram, Token: "nope", ChatID: "1234"},
		{Name: "t", Kind: Telegram, Token: "123456789:" + strings.Repeat("A", 35), ChatID: "me"},
		{Name: "x", Kind: "email"},
	}
	for _, c := range bad {
		if c.Validate() == nil {
			t.Errorf("expected %+v to be rejected", c)
		}
	}

	crit := alerts.Alert{Severity: alerts.Critical}
	warn := alerts.Alert{Severity: alerts.Warning}
	c := Channel{Enabled: true, CriticalOnly: true}
	if c.Wants(Message{Kind: Opened, Alert: warn}) || !c.Wants(Message{Kind: Opened, Alert: crit}) || c.Wants(Message{Kind: Resolved, Alert: crit}) || !c.Wants(Message{Kind: Test}) {
		t.Error("critical-only, no recoveries")
	}
	c.Recoveries, c.CriticalOnly = true, false
	if !c.Wants(Message{Kind: Resolved, Alert: warn}) {
		t.Error("recoveries")
	}
	c.Enabled = false
	if c.Wants(Message{Kind: Test}) {
		t.Error("disabled channels get nothing")
	}
	if (Channel{Kind: Discord, URL: "https://discord.com/api/webhooks/1/SECRET"}).Describe() != "discord.com" {
		t.Error("describe must hide the secret path")
	}
}

func TestDeployMessages(t *testing.T) {
	ok := Message{Kind: Deploy, Alert: alerts.Alert{Title: "Deployed shop (a1b2c3d)"}}
	failed := Message{Kind: Deploy, Alert: alerts.Alert{Severity: alerts.Critical, Title: "Deploy of shop failed"}}
	back := Message{Kind: Deploy, Alert: alerts.Alert{Severity: alerts.Warning, Title: "shop was rolled back"}}

	off := Channel{Enabled: true}
	on := Channel{Enabled: true, Deploys: true}
	crit := Channel{Enabled: true, Deploys: true, CriticalOnly: true}
	if off.Wants(ok) || !on.Wants(ok) || !on.Wants(back) || crit.Wants(ok) || crit.Wants(back) || !crit.Wants(failed) {
		t.Error("deploy message routing")
	}
	if icon(ok) != "🚀" || icon(failed) != "🔴" || icon(back) != "↩️" {
		t.Error("icons")
	}
	if eventName(ok) != "deploy.succeeded" || eventName(failed) != "deploy.failed" || eventName(back) != "deploy.rolledback" || eventName(Message{Kind: Opened}) != "alert.opened" {
		t.Error("webhook event names")
	}
}
