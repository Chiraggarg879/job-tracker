package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Notifier struct {
	token  string
	chatID string
}

func NewNotifier(tc TelegramConfig) *Notifier {
	return &Notifier{token: tc.BotToken, chatID: tc.ChatID}
}

func (n *Notifier) Enabled() bool { return n.token != "" && n.chatID != "" }

func formatJob(j Job) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b> — %s\n", html.EscapeString(j.Company), html.EscapeString(j.Title))
	loc := j.Location
	if strings.TrimSpace(loc) == "" {
		loc = "location not listed"
	}
	fmt.Fprintf(&b, "📍 %s", html.EscapeString(truncate(loc, 120)))
	if j.Posted != "" {
		fmt.Fprintf(&b, "  🕒 %s", html.EscapeString(j.Posted))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "🎓 %s\n", yearsLabel(j))
	fmt.Fprintf(&b, "🔗 <a href=\"%s\">Apply here</a>\n%s", html.EscapeString(j.URL), html.EscapeString(j.URL))
	return b.String()
}

// Telegram rejects messages over 4096 characters; stay safely under it.
const maxMessageLen = 3800

// Notify prints the new jobs and sends them to Telegram as a digest: a
// header plus as few messages as fit the length limit. When there are no
// new jobs it still sends a short note, so a silent bot means a failed run.
func (n *Notifier) Notify(ctx context.Context, jobs []Job) {
	for _, j := range jobs {
		log.Printf("NEW  %-12s | %s | %s\n      %s", j.Company, j.Title, truncate(j.Location, 80), j.URL)
	}
	if !n.Enabled() {
		return
	}

	stamp := time.Now().Format("Mon 2 Jan, 3:04 PM")
	if len(jobs) == 0 {
		n.sendLogged(ctx, fmt.Sprintf("🔍 <b>%s check</b>\nNo new matching jobs since the last check.", stamp))
		return
	}

	header := fmt.Sprintf("🆕 <b>%d new job(s)</b> — %s check\n\n", len(jobs), stamp)
	var msg strings.Builder
	msg.WriteString(header)
	for i, j := range jobs {
		entry := fmt.Sprintf("%d. %s\n\n", i+1, formatJob(j))
		if msg.Len()+len(entry) > maxMessageLen && msg.Len() > 0 {
			n.sendLogged(ctx, msg.String())
			msg.Reset()
		}
		msg.WriteString(entry)
	}
	if msg.Len() > 0 {
		n.sendLogged(ctx, msg.String())
	}
}

func (n *Notifier) sendLogged(ctx context.Context, htmlText string) {
	if err := n.send(ctx, htmlText); err != nil {
		log.Printf("telegram: %v", err)
	}
	// Telegram allows about one message per second per chat.
	time.Sleep(1100 * time.Millisecond)
}

func (n *Notifier) SendText(ctx context.Context, text string) error {
	if !n.Enabled() {
		return nil
	}
	return n.send(ctx, html.EscapeString(text))
}

func (n *Notifier) send(ctx context.Context, htmlText string) error {
	form := url.Values{}
	form.Set("chat_id", n.chatID)
	form.Set("text", htmlText)
	form.Set("parse_mode", "HTML")
	form.Set("disable_web_page_preview", "true")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+n.token+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var body struct {
			Description string `json:"description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body.Description)
	}
	return nil
}

func yearsLabel(j Job) string {
	if j.YearsKnown {
		return fmt.Sprintf("%d+ yrs exp", j.Years)
	}
	return "exp not stated"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
