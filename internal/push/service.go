// Package push sends Web Push messages for in-app notifications. It works over
// plain HTTP inside the Electron client (which treats the app origin as a
// secure context); browsers require HTTPS for the same APIs.
package push

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type Service struct {
	db         *store.DB
	logger     *slog.Logger
	subject    string
	mu         sync.Mutex
	publicKey  string
	privateKey string
	path       string
}

type vapidFile struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

func NewService(db *store.DB, dataDir string, logger *slog.Logger) (*Service, error) {
	service := &Service{
		db: db, logger: logger, subject: "mailto:admin@superbot.local",
		path: filepath.Join(dataDir, "push-vapid.json"),
	}
	if err := service.loadOrCreateKeys(); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) loadOrCreateKeys() error {
	if data, err := os.ReadFile(s.path); err == nil {
		var stored vapidFile
		if json.Unmarshal(data, &stored) == nil && stored.PublicKey != "" && stored.PrivateKey != "" {
			s.publicKey = stored.PublicKey
			s.privateKey = stored.PrivateKey
			return nil
		}
	}
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	data, _ := json.Marshal(vapidFile{PublicKey: publicKey, PrivateKey: privateKey})
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return err
	}
	s.publicKey = publicKey
	s.privateKey = privateKey
	return nil
}

func (s *Service) PublicKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publicKey
}

func (s *Service) Subscribe(ctx context.Context, accountID, userID uuid.UUID, endpoint, p256dh, auth, userAgent string) (store.PushSubscription, error) {
	endpoint = strings.TrimSpace(endpoint)
	p256dh = strings.TrimSpace(p256dh)
	auth = strings.TrimSpace(auth)
	if endpoint == "" || p256dh == "" || auth == "" {
		return store.PushSubscription{}, fmt.Errorf("endpoint and keys are required")
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	return s.db.CreatePushSubscription(ctx, store.CreatePushSubscriptionParams{
		AccountID: accountID, UserID: userID, Endpoint: endpoint,
		P256dh: p256dh, Auth: auth, UserAgent: userAgent,
	})
}

func (s *Service) Unsubscribe(ctx context.Context, userID uuid.UUID, endpoint string) error {
	n, err := s.db.DeletePushSubscription(ctx, store.DeletePushSubscriptionParams{
		Endpoint: strings.TrimSpace(endpoint), UserID: userID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("subscription not found")
	}
	return nil
}

type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

func (s *Service) send(ctx context.Context, subscription store.PushSubscription, message Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	s.mu.Lock()
	publicKey, privateKey := s.publicKey, s.privateKey
	s.mu.Unlock()
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256dh, Auth: subscription.Auth},
	}, &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		TTL:             3600,
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusGone {
		return fmt.Errorf("push service status %d", response.StatusCode)
	}
	// 404/410 mean the subscription is gone; remove it so we stop retrying.
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		_, _ = s.db.DeletePushSubscription(context.WithoutCancel(ctx), store.DeletePushSubscriptionParams{
			Endpoint: subscription.Endpoint, UserID: subscription.UserID,
		})
	}
	return nil
}

func (s *Service) SendTest(ctx context.Context, accountID uuid.UUID) (sent, failed int, lastErr error) {
	subscriptions, err := s.db.ListPushSubscriptionsByAccount(ctx, accountID)
	if err != nil {
		return 0, 0, err
	}
	for _, subscription := range subscriptions {
		if err := s.send(ctx, subscription, Message{
			Title: "superbot", Body: "Web Push 测试通知", URL: "/", Tag: "superbot-test",
		}); err != nil {
			failed++
			lastErr = err
			s.logger.Warn("push test failed", "error", err)
			continue
		}
		sent++
	}
	return sent, failed, lastErr
}

// NotifyUnpushed delivers notifications created since the last pass and marks
// them. Rows without subscriptions are marked too, so the scan stays bounded.
func (s *Service) NotifyUnpushed(ctx context.Context) {
	rows, err := s.db.ListUnpushedNotifications(ctx, 20)
	if err != nil {
		s.logger.Warn("list unpushed notifications failed", "error", err)
		return
	}
	for _, notification := range rows {
		subscriptions, err := s.db.ListPushSubscriptionsByAccount(ctx, notification.OwnerID)
		if err == nil {
			body := notification.Body
			if len([]rune(body)) > 180 {
				body = string([]rune(body)[:180]) + "…"
			}
			for _, subscription := range subscriptions {
				if err := s.send(ctx, subscription, Message{
					Title: notification.Title, Body: body, URL: "/", Tag: notification.Kind,
				}); err != nil {
					s.logger.Warn("push send failed", "notification_id", notification.ID, "error", err)
				}
			}
		}
		if err := s.db.MarkNotificationPushed(ctx, notification.ID); err != nil {
			s.logger.Warn("mark notification pushed failed", "error", err)
		}
	}
}
