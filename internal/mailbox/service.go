package mailbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

type Config struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Folder             string `json:"folder"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

type Message struct {
	UID         uint32       `json:"uid"`
	Subject     string       `json:"subject"`
	From        string       `json:"from"`
	Date        time.Time    `json:"date"`
	MessageID   string       `json:"message_id,omitempty"`
	InReplyTo   []string     `json:"in_reply_to,omitempty"`
	ThreadID    string       `json:"thread_id,omitempty"`
	Body        string       `json:"body,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
}

type Thread struct {
	ID       string    `json:"id"`
	Subject  string    `json:"subject"`
	Messages []Message `json:"messages"`
}

type Service struct{}

func NewService() *Service { return &Service{} }

func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return Config{}, errors.New("IMAP host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 993
	}
	if strings.TrimSpace(cfg.Folder) == "" {
		cfg.Folder = "INBOX"
	}
	return cfg, nil
}

func (s *Service) Recent(ctx context.Context, cfg Config, password string, limit int) ([]Message, error) {
	address := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	client, err := imapclient.DialTLS(address, &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: cfg.Host, InsecureSkipVerify: cfg.InsecureSkipVerify},
	})
	if err != nil {
		return nil, fmt.Errorf("connect IMAP: %w", err)
	}
	defer client.Close()
	if err := client.Login(cfg.Username, password).Wait(); err != nil {
		return nil, fmt.Errorf("IMAP login: %w", err)
	}
	selected, err := client.Select(cfg.Folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("select folder: %w", err)
	}
	if selected.NumMessages == 0 {
		return []Message{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	end := selected.NumMessages
	start := uint32(1)
	if end > uint32(limit) {
		start = end - uint32(limit) + 1
	}
	var set imap.SeqSet
	set.AddRange(start, end)
	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetch := client.Fetch(set, &imap.FetchOptions{
		Envelope: true, UID: true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	})
	out := make([]Message, 0, limit)
	for {
		message := fetch.Next()
		if message == nil {
			break
		}
		item := Message{}
		var envelope *imap.Envelope
		var rawBody []byte
		for data := message.Next(); data != nil; data = message.Next() {
			switch value := data.(type) {
			case imapclient.FetchItemDataEnvelope:
				envelope = value.Envelope
			case imapclient.FetchItemDataUID:
				item.UID = uint32(value.UID)
			case imapclient.FetchItemDataBodySection:
				if value.Literal != nil {
					rawBody, _ = io.ReadAll(io.LimitReader(value.Literal, 1<<20))
				}
			}
		}
		if len(rawBody) > 0 {
			item.Body, item.Attachments = parseMessage(rawBody)
		}
		if envelope != nil {
			item.Subject = envelope.Subject
			item.Date = envelope.Date
			item.MessageID = envelope.MessageID
			item.InReplyTo = envelope.InReplyTo
			if len(envelope.From) > 0 {
				item.From = envelope.From[0].Addr()
			}
		}
		out = append(out, item)
	}
	if err := fetch.Close(); err != nil {
		return nil, fmt.Errorf("fetch inbox: %w", err)
	}
	assignThreads(out)
	return out, nil
}

// assignThreads links replies to the earliest known ancestor. Messages
// without usable Message-ID headers fall back to a normalized subject.
func assignThreads(messages []Message) {
	byID := make(map[string]int, len(messages))
	for index, message := range messages {
		if message.MessageID != "" {
			byID[message.MessageID] = index
		}
	}
	resolve := func(message Message) string {
		seen := map[string]bool{}
		current := message
		for {
			if len(current.InReplyTo) == 0 {
				break
			}
			parentID := current.InReplyTo[0]
			if seen[parentID] {
				break
			}
			seen[parentID] = true
			parentIndex, ok := byID[parentID]
			if !ok {
				return parentID
			}
			current = messages[parentIndex]
		}
		if current.MessageID != "" {
			return current.MessageID
		}
		return normalizedSubject(message.Subject)
	}
	for index := range messages {
		messages[index].ThreadID = resolve(messages[index])
	}
}

func parseMessage(raw []byte) (string, []Attachment) {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return "", nil
	}
	defer reader.Close()
	var body strings.Builder
	var attachments []Attachment
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch header := part.Header.(type) {
		case *mail.InlineHeader:
			contentType, _, _ := header.ContentType()
			if strings.HasPrefix(contentType, "text/plain") && body.Len() < 20_000 {
				data, _ := io.ReadAll(io.LimitReader(part.Body, 20_000))
				body.Write(data)
			}
		case *mail.AttachmentHeader:
			filename, _ := header.Filename()
			contentType, _, _ := header.ContentType()
			data, _ := io.ReadAll(io.LimitReader(part.Body, 1<<20))
			attachments = append(attachments, Attachment{
				Filename: filename, MimeType: contentType, Size: len(data),
			})
		}
	}
	return strings.TrimSpace(body.String()), attachments
}

func normalizedSubject(subject string) string {
	value := strings.TrimSpace(subject)
	for {
		lower := strings.ToLower(value)
		switch {
		case strings.HasPrefix(lower, "re:"):
			value = strings.TrimSpace(value[3:])
		case strings.HasPrefix(lower, "fwd:"):
			value = strings.TrimSpace(value[4:])
		case strings.HasPrefix(lower, "fw:"):
			value = strings.TrimSpace(value[3:])
		default:
			return "subject:" + strings.ToLower(value)
		}
	}
}

func GroupThreads(messages []Message) []Thread {
	order := make([]string, 0)
	threads := make(map[string]*Thread)
	for _, message := range messages {
		id := message.ThreadID
		if id == "" {
			id = normalizedSubject(message.Subject)
		}
		thread, ok := threads[id]
		if !ok {
			thread = &Thread{ID: id, Subject: message.Subject}
			threads[id] = thread
			order = append(order, id)
		}
		thread.Messages = append(thread.Messages, message)
	}
	out := make([]Thread, 0, len(order))
	for _, id := range order {
		out = append(out, *threads[id])
	}
	return out
}

type SMTPConfig struct {
	Host               string `json:"host"`
	Port               int    `json:"smtp_port"`
	Username           string `json:"username"`
	From               string `json:"from"`
	InsecureSkipVerify bool   `json:"smtp_insecure_skip_verify"`
}

type Outgoing struct {
	To      []string
	CC      []string
	Subject string
	Body    string
}

func ParseSMTPConfig(raw json.RawMessage) (SMTPConfig, error) {
	var cfg SMTPConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return SMTPConfig{}, err
	}
	if strings.TrimSpace(cfg.Host) == "" || strings.TrimSpace(cfg.From) == "" {
		return SMTPConfig{}, errors.New("SMTP host and from are required")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return cfg, nil
}

type plainAuth struct {
	username string
	password string
}

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (a plainAuth) Next([]byte, bool) ([]byte, error) { return nil, nil }

func (s *Service) Send(ctx context.Context, cfg SMTPConfig, password string, message Outgoing) error {
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	client, err := smtp.NewClient(connection, cfg.Host)
	if err != nil {
		connection.Close()
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, InsecureSkipVerify: cfg.InsecureSkipVerify}); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}
	username := cfg.Username
	if username == "" {
		username = cfg.From
	}
	if ok, _ := client.Extension("AUTH"); ok {
		if cfg.InsecureSkipVerify {
			if err := client.Auth(plainAuth{username: username, password: password}); err != nil {
				return fmt.Errorf("SMTP AUTH: %w", err)
			}
		} else {
			if err := client.Auth(smtp.PlainAuth("", username, password, cfg.Host)); err != nil {
				return fmt.Errorf("SMTP AUTH: %w", err)
			}
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return err
	}
	recipients := append(append([]string{}, message.To...), message.CC...)
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	headers := []string{
		"From: " + cfg.From,
		"To: " + strings.Join(message.To, ", "),
		"Subject: " + mime.QEncoding.Encode("utf-8", message.Subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	if len(message.CC) > 0 {
		headers = append(headers, "Cc: "+strings.Join(message.CC, ", "))
	}
	payload := strings.Join(headers, "\r\n") + "\r\n\r\n" + message.Body
	if _, err := writer.Write([]byte(payload)); err != nil {
		writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
