// Package channels sends alerts to places outside the app: Discord, Slack,
// Telegram or any webhook. It only formats and posts; the monitor decides
// what to send and when.
package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"truedeploy/internal/alerts"
)

const (
	Discord  = "discord"
	Slack    = "slack"
	Telegram = "telegram"
	Webhook  = "webhook"
)

// Channel is one destination. URL is the webhook URL (Discord, Slack,
// webhook); Telegram uses Token and ChatID. Secrets live encrypted.
type Channel struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
	Token   string `json:"token,omitempty"`
	ChatID  string `json:"chatId,omitempty"`
	// CriticalOnly skips warnings (like a disk at 90%).
	CriticalOnly bool `json:"criticalOnly,omitempty"`
	// Recoveries also sends a message when a problem goes away.
	Recoveries bool `json:"recoveries"`
	// Deploys also sends a message when a deploy from TrueDeploy finishes.
	Deploys bool `json:"deploys,omitempty"`
}

// Event kinds.
const (
	Opened   = "opened"
	Resolved = "resolved"
	Test     = "test"
	// Deploy reports a finished deploy: Alert.Severity is "" when it
	// succeeded, Critical when it failed, Warning when it was rolled back.
	Deploy = "deploy"
)

type Message struct {
	Kind  string
	Alert alerts.Alert
	At    time.Time
}

// Wants reports whether the channel should get this message.
func (c Channel) Wants(m Message) bool {
	if !c.Enabled {
		return false
	}
	switch m.Kind {
	case Test:
		return true
	case Deploy:
		return c.Deploys && (!c.CriticalOnly || m.Alert.Severity == alerts.Critical)
	case Resolved:
		if !c.Recoveries {
			return false
		}
	}
	return !c.CriticalOnly || m.Alert.Severity == alerts.Critical
}

var (
	telegramTokenRe = regexp.MustCompile(`^\d{3,}:[A-Za-z0-9_-]{30,}$`)
	telegramChatRe  = regexp.MustCompile(`^(-?\d{3,}|@[A-Za-z][A-Za-z0-9_]{3,})$`)
)

// Validate checks a channel before it is saved.
func (c Channel) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("give the channel a name")
	}
	switch c.Kind {
	case Discord:
		return checkURL(c.URL, "Discord webhook URL", func(u *url.URL) bool {
			return (u.Host == "discord.com" || u.Host == "discordapp.com" || u.Host == "ptb.discord.com" || u.Host == "canary.discord.com") &&
				strings.HasPrefix(u.Path, "/api/webhooks/")
		}, "it should look like https://discord.com/api/webhooks/…")
	case Slack:
		return checkURL(c.URL, "Slack webhook URL", func(u *url.URL) bool {
			return u.Host == "hooks.slack.com" && strings.HasPrefix(u.Path, "/services/")
		}, "it should look like https://hooks.slack.com/services/…")
	case Webhook:
		return checkURL(c.URL, "webhook URL", func(u *url.URL) bool { return true }, "")
	case Telegram:
		if !telegramTokenRe.MatchString(c.Token) {
			return errors.New("that isn't a Telegram bot token (it looks like 123456789:AA…, from @BotFather)")
		}
		if !telegramChatRe.MatchString(c.ChatID) {
			return errors.New("the chat ID should be a number (e.g. 123456789 or -1001234567890) or a @channelname")
		}
		return nil
	}
	return fmt.Errorf("unknown channel type %q", c.Kind)
}

func checkURL(raw, what string, ok func(*url.URL) bool, hint string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("enter the %s", what)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("the %s must start with https://", what)
	}
	if !ok(u) {
		return fmt.Errorf("that doesn't look like a %s: %s", what, hint)
	}
	return nil
}

// Describe is a short, secret-free label for the destination.
func (c Channel) Describe() string {
	switch c.Kind {
	case Telegram:
		return "chat " + c.ChatID
	default:
		u, err := url.Parse(c.URL)
		if err != nil || u.Host == "" {
			return ""
		}
		if c.Kind == Webhook {
			return u.Host + u.Path
		}
		return u.Host
	}
}

func icon(m Message) string {
	switch {
	case m.Kind == Test:
		return "🔔"
	case m.Kind == Deploy && m.Alert.Severity == "":
		return "🚀"
	case m.Kind == Deploy && m.Alert.Severity == alerts.Warning:
		return "↩️"
	case m.Kind == Resolved:
		return "✅"
	case m.Alert.Severity == alerts.Critical:
		return "🔴"
	default:
		return "🟠"
	}
}

func headline(m Message) string {
	switch m.Kind {
	case Test:
		return "TrueDeploy test message"
	case Resolved:
		return "Resolved: " + m.Alert.Title
	}
	return m.Alert.Title
}

func body(m Message) string {
	switch m.Kind {
	case Test:
		return "This channel is set up. Server alerts will arrive here."
	case Resolved:
		return "Back to normal."
	}
	return m.Alert.Detail
}

// TelegramAPI is the Bot API base (overridden in tests).
var TelegramAPI = "https://api.telegram.org"

// request builds the HTTP request for a message.
func request(ctx context.Context, c Channel, m Message) (*http.Request, error) {
	var target string
	var payload any
	text := icon(m) + " " + headline(m)
	switch c.Kind {
	case Discord:
		color := 0xF87171
		switch {
		case m.Kind == Test:
			color = 0x1F6EF2
		case m.Kind == Deploy && m.Alert.Severity == "":
			color = 0x34D399
		case m.Kind == Resolved:
			color = 0x34D399
		case m.Alert.Severity == alerts.Warning:
			color = 0xF5B84A
		}
		target = c.URL
		payload = map[string]any{
			"username": "TrueDeploy",
			"embeds": []map[string]any{{
				"title": truncate(text, 250), "description": truncate(body(m), 3500), "color": color,
				"timestamp": m.At.UTC().Format(time.RFC3339),
			}},
		}
	case Slack:
		target = c.URL
		payload = map[string]any{"text": fmt.Sprintf("*%s*\n%s", slackEscape(text), slackEscape(body(m)))}
	case Telegram:
		target = TelegramAPI + "/bot" + c.Token + "/sendMessage"
		payload = map[string]any{"chat_id": c.ChatID, "text": truncate(text+"\n"+body(m), 4000), "disable_web_page_preview": true}
	case Webhook:
		target = c.URL
		ev := map[string]any{"event": eventName(m), "source": "truedeploy", "at": m.At.UTC().Format(time.RFC3339), "text": text + "\n" + body(m)}
		if m.Kind != Test {
			ev["alert"] = m.Alert
		}
		payload = ev
	default:
		return nil, fmt.Errorf("unknown channel type %q", c.Kind)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TrueDeploy")
	return req, nil
}

// Send posts one message. Errors are written for people and never contain
// the channel's secret URL or token.
func Send(ctx context.Context, client *http.Client, c Channel, m Message) error {
	if m.At.IsZero() {
		m.At = time.Now()
	}
	req, err := request(ctx, c, m)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the URL, which holds the secret
		}
		return fmt.Errorf("couldn't reach %s: %v", hostOf(req.URL), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("%s answered %d%s", hostOf(req.URL), resp.StatusCode, explain(c.Kind, resp.StatusCode, raw))
}

func explain(kind string, status int, raw []byte) string {
	if kind == Telegram {
		var r struct {
			Description string `json:"description"`
		}
		if json.Unmarshal(raw, &r) == nil && r.Description != "" {
			d := r.Description
			if strings.Contains(d, "chat not found") {
				d += " (send your bot a message first, or add it to the group)"
			}
			return ": " + d
		}
	}
	switch status {
	case 401, 403:
		return ": the webhook was refused (was it deleted or regenerated?)"
	case 404:
		return ": the webhook doesn't exist anymore"
	case 429:
		return ": too many messages, try again in a minute"
	}
	if s := strings.TrimSpace(string(raw)); s != "" && len(s) < 200 && !strings.HasPrefix(s, "<") {
		return ": " + s
	}
	return ""
}

func hostOf(u *url.URL) string {
	if u == nil {
		return "the channel"
	}
	return u.Host
}

func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// eventName is the webhook "event" field: alert.opened, alert.resolved,
// alert.test, or deploy.succeeded / deploy.failed / deploy.rolledback.
func eventName(m Message) string {
	if m.Kind != Deploy {
		return "alert." + m.Kind
	}
	switch m.Alert.Severity {
	case alerts.Critical:
		return "deploy.failed"
	case alerts.Warning:
		return "deploy.rolledback"
	}
	return "deploy.succeeded"
}
