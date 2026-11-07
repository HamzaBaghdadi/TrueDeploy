package services

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"truedeploy/internal/alerts"
	"truedeploy/internal/channels"
	"truedeploy/internal/store"
)

// ChannelView is a channel without its secret (webhook URL or bot token).
type ChannelView struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	CriticalOnly bool   `json:"criticalOnly"`
	Recoveries   bool   `json:"recoveries"`
	Deploys      bool   `json:"deploys"`
	ChatID       string `json:"chatId,omitempty"`
	// Target describes where messages go, e.g. "discord.com" or "chat 1234".
	Target    string    `json:"target"`
	LastSent  time.Time `json:"lastSent,omitempty"`
	LastError string    `json:"lastError,omitempty"`
}

// ChannelInput saves a channel. An empty URL or Token keeps the saved one,
// so editing a channel never requires pasting the secret again.
type ChannelInput struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	CriticalOnly bool   `json:"criticalOnly"`
	Recoveries   bool   `json:"recoveries"`
	Deploys      bool   `json:"deploys"`
	URL          string `json:"url"`
	Token        string `json:"token"`
	ChatID       string `json:"chatId"`
}

type channelState struct {
	lastSent time.Time
	lastErr  string
}

// channelHub sends alert messages and remembers each channel's last result.
type channelHub struct {
	client *http.Client
	mu     sync.Mutex
	state  map[string]channelState
}

func newChannelHub() *channelHub {
	return &channelHub{client: &http.Client{Timeout: 15 * time.Second}, state: map[string]channelState{}}
}

func (h *channelHub) record(id string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.state[id]
	if err != nil {
		st.lastErr = err.Error()
	} else {
		st.lastSent, st.lastErr = time.Now(), ""
	}
	h.state[id] = st
}

func (h *channelHub) view(c channels.Channel) ChannelView {
	h.mu.Lock()
	st := h.state[c.ID]
	h.mu.Unlock()
	return ChannelView{
		ID: c.ID, Kind: c.Kind, Name: c.Name, Enabled: c.Enabled, CriticalOnly: c.CriticalOnly, Recoveries: c.Recoveries,
		ChatID: c.ChatID, Deploys: c.Deploys, Target: c.Describe(), LastSent: st.lastSent, LastError: st.lastErr,
	}
}

// dispatch sends messages to every channel that wants them: channels in
// parallel, each channel's messages in order.
func (h *channelHub) dispatch(list []channels.Channel, msgs []channels.Message) {
	var wg sync.WaitGroup
	for _, c := range list {
		var want []channels.Message
		for _, m := range msgs {
			if c.Wants(m) {
				want = append(want, m)
			}
		}
		if len(want) == 0 {
			continue
		}
		wg.Add(1)
		go func(c channels.Channel, want []channels.Message) {
			defer wg.Done()
			for _, m := range want {
				err := channels.Send(context.Background(), h.client, c, m)
				h.record(c.ID, err)
				if err != nil {
					return // the rest would fail the same way
				}
			}
		}(c, want)
	}
	wg.Wait()
}

// messages turns a check's changes into channel messages.
func messages(opened, resolved []alerts.Alert, at time.Time) []channels.Message {
	var out []channels.Message
	for _, a := range opened {
		out = append(out, channels.Message{Kind: channels.Opened, Alert: a, At: at})
	}
	for _, a := range resolved {
		out = append(out, channels.Message{Kind: channels.Resolved, Alert: a, At: at})
	}
	return out
}

func (m *MonitorService) ListChannels() ([]ChannelView, error) {
	list, err := m.store.ListChannels()
	if err != nil {
		return nil, err
	}
	out := make([]ChannelView, 0, len(list))
	for _, c := range list {
		out = append(out, m.hub.view(c))
	}
	return out, nil
}

// channelFrom merges the form with the saved channel (for kept secrets).
func (m *MonitorService) channelFrom(in ChannelInput) (channels.Channel, error) {
	c := channels.Channel{
		ID: in.ID, Kind: in.Kind, Name: strings.TrimSpace(in.Name), Enabled: in.Enabled,
		CriticalOnly: in.CriticalOnly, Recoveries: in.Recoveries, Deploys: in.Deploys,
		URL: strings.TrimSpace(in.URL), Token: strings.TrimSpace(in.Token), ChatID: strings.TrimSpace(in.ChatID),
	}
	if c.ID != "" {
		prev, err := m.store.GetChannel(c.ID)
		if err != nil {
			return c, err
		}
		if prev.Kind == c.Kind {
			if c.URL == "" {
				c.URL = prev.URL
			}
			if c.Token == "" {
				c.Token = prev.Token
			}
		}
	}
	if c.Kind == channels.Telegram {
		c.URL = ""
	} else {
		c.Token, c.ChatID = "", ""
	}
	return c, c.Validate()
}

func (m *MonitorService) SaveChannel(in ChannelInput) (*ChannelView, error) {
	c, err := m.channelFrom(in)
	if err != nil {
		return nil, err
	}
	if c.ID == "" {
		c.ID = store.NewID()
	}
	if err := m.store.PutChannel(c); err != nil {
		return nil, err
	}
	v := m.hub.view(c)
	return &v, nil
}

func (m *MonitorService) DeleteChannel(id string) error { return m.store.DeleteChannel(id) }

// TestChannel sends a test message with the form's settings (saved or not).
func (m *MonitorService) TestChannel(in ChannelInput) (string, error) {
	c, err := m.channelFrom(in)
	if err != nil {
		return "", err
	}
	err = channels.Send(context.Background(), m.hub.client, c, channels.Message{Kind: channels.Test})
	if c.ID != "" {
		m.hub.record(c.ID, err)
	}
	if err != nil {
		return "", err
	}
	return "Test message sent to " + c.Name + ". Check that it arrived.", nil
}

// notifyChannels sends a check's changes in the background.
func (m *MonitorService) notifyChannels(opened, resolved []alerts.Alert, at time.Time) {
	msgs := messages(opened, resolved, at)
	if len(msgs) == 0 {
		return
	}
	list, err := m.store.ListChannels()
	if err != nil || len(list) == 0 {
		return
	}
	go m.hub.dispatch(list, msgs)
}

// NotifyDeploy tells channels that want it about a finished deploy.
func (m *MonitorService) NotifyDeploy(p store.Project, d store.Deployment) {
	list, err := m.store.ListChannels()
	if err != nil || len(list) == 0 {
		return
	}
	m.hub.dispatch(list, []channels.Message{deployMessage(p, d)})
}

func deployMessage(p store.Project, d store.Deployment) channels.Message {
	a := alerts.Alert{ServerID: p.ServerID, ProjectID: p.ID}
	what := p.Name
	if d.Commit != "" {
		what += " (" + d.Commit + ")"
	}
	switch {
	case d.RolledBackTo != "":
		a.Severity = alerts.Warning
		a.Title = p.Name + " was rolled back to " + d.RolledBackTo
		a.Detail = "The new version " + d.Commit + " failed its health check: " + d.Unhealthy + ". The previous healthy version is running again."
	case d.Status == "failed":
		a.Severity = alerts.Critical
		a.Title = "Deploy of " + what + " failed"
		a.Detail = d.Error
	default:
		a.Title = "Deployed " + what
		a.Detail = d.Message
		if a.Detail == "" {
			a.Detail = "Deployed successfully."
		}
	}
	return channels.Message{Kind: channels.Deploy, Alert: a, At: d.FinishedAt}
}
