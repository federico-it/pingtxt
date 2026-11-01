package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Resend (resend.com) sends the emails over its HTTP API instead of SMTP:
// https://resend.com/docs/api-reference/emails/send-email

const (
	resendAPI = "https://api.resend.com"
	// resendMaxTo is the most recipients of one email.
	resendMaxTo = 50
	// resendGap spaces the requests of a batch (subscribers), under the
	// default limit of 2 requests per second.
	resendGap = 550 * time.Millisecond
	// resendTries is how many times a request is sent when rate limited.
	resendTries = 4
)

type resendEmail struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
	HTML    string            `json:"html"`
	Headers map[string]string `json:"headers,omitempty"`
}

// resendError is an error answer of the API.
type resendError struct {
	Status  int
	Message string
}

func (e *resendError) Error() string {
	return fmt.Sprintf("resend: %d %s", e.Status, e.Message)
}

// refused tells a problem with the message itself (a bad recipient...)
// from one with the account or the key, which stops the delivery.
func (e *resendError) refused() bool {
	return e.Status == http.StatusBadRequest || e.Status == http.StatusUnprocessableEntity
}

// deliverResend sends the messages one by one through the Resend API.
func (n *Notifier) deliverResend(ctx context.Context, ec *emailConfig, msgs []outMail, failed func(i int, err error)) error {
	if ec.APIKey == "" {
		return errors.New("resend api key is required")
	}
	for i, m := range msgs {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(n.resendGap()):
			}
		}
		err := n.resendOne(ctx, ec, m)
		var re *resendError
		if err != nil {
			if failed == nil || !errors.As(err, &re) || !re.refused() {
				return err
			}
			failed(i, err)
		}
	}
	return nil
}

func (n *Notifier) resendGap() time.Duration {
	if n.resendWait != nil {
		return n.resendWait(0)
	}
	return resendGap
}

func (n *Notifier) resendOne(ctx context.Context, ec *emailConfig, m outMail) error {
	body := resendEmail{
		From:    ec.From.String(),
		To:      m.to,
		Subject: m.msg.hd.subject,
		Text:    m.msg.text,
		HTML:    m.msg.html,
		Headers: map[string]string{"Auto-Submitted": "auto-generated"},
	}
	if u := m.msg.hd.unsubscribe; u != "" {
		body.Headers["List-Unsubscribe"] = "<" + u + ">"
		body.Headers["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	// The same key on every try: Resend sends the email once even when an
	// answer is lost.
	kb := make([]byte, 16)
	_, _ = rand.Read(kb)
	key := hex.EncodeToString(kb)
	base := resendAPI
	if n.resendURL != "" {
		base = n.resendURL
	}
	for try := 1; ; try++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/emails", bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+ec.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("User-Agent", "pingtxt")
		resp, err := n.client.Do(req)
		if err != nil {
			return fmt.Errorf("resend: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode/100 == 2 {
			return nil
		}
		if resp.StatusCode == http.StatusTooManyRequests && try < resendTries {
			wait := retryAfter(resp.Header.Get("Retry-After"))
			if n.resendWait != nil {
				wait = n.resendWait(try)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		return &resendError{Status: resp.StatusCode, Message: resendMessage(raw)}
	}
}

// retryAfter reads the Retry-After seconds of a rate limited answer,
// between 1 and 10 seconds.
func retryAfter(v string) time.Duration {
	s, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || s < 1 {
		s = 1
	}
	return time.Duration(min(s, 10)) * time.Second
}

// resendMessage is the message of an error answer, or its start.
func resendMessage(raw []byte) string {
	var e struct {
		Message string `json:"message"`
		Name    string `json:"name"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Message != "" {
		return e.Message
	}
	return clip(strings.TrimSpace(string(raw)), 200)
}
