package instance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type pusherMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type pusherEventData struct {
	NodeID int    `json:"node_id"`
	Event  string `json:"event"`
}

type pusherConnected struct {
	SocketID        string `json:"socket_id"`
	ActivityTimeout int    `json:"activity_timeout"`
}

func (i *Instance) reverbListener(ctx context.Context, cfg *ReverbConfig) {
	scheme := "ws"
	if cfg.UseTLS {
		scheme = "wss"
	}
	url := fmt.Sprintf("%s://%s/app/%s?protocol=7&client=go&version=1.0", scheme, cfg.Host, cfg.AppKey)

	const (
		initialBackoff = 2 * time.Second
		maxBackoff     = 60 * time.Second
		pingInterval   = 25 * time.Second
	)

	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("[Reverb] Connecting to %s channel=%s", url, cfg.Channel)

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, http.Header{})
		if err != nil {
			log.Printf("[Reverb] Dial error: %v — retrying in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		log.Printf("[Reverb] Websocket Connected")
		backoff = initialBackoff

		if err := i.reverbSession(ctx, conn, cfg, pingInterval); err != nil {
			log.Printf("[Reverb] Session ended: %v — reconnecting in %s", err, backoff)
		}

		conn.Close()

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = minDuration(backoff*2, maxBackoff)
	}
}

func (i *Instance) reverbSession(ctx context.Context, conn *websocket.Conn, cfg *ReverbConfig, pingInterval time.Duration) error {
	socketID, err := i.awaitConnected(conn)
	if err != nil {
		return fmt.Errorf("await connected: %w", err)
	}
	log.Printf("[Reverb] socket_id=%s", socketID)

	isPrivate := len(cfg.Channel) > 8 && cfg.Channel[:8] == "private-" ||
		len(cfg.Channel) > 9 && cfg.Channel[:9] == "presence-"

	subData := map[string]string{"channel": cfg.Channel}
	if isPrivate && cfg.AppSecret != "" {
		subData["auth"] = signChannel(cfg.AppKey, cfg.AppSecret, socketID, cfg.Channel)
	}

	sub, _ := json.Marshal(pusherMessage{
		Event: "pusher:subscribe",
		Data:  mustMarshal(subData),
	})
	if err := conn.WriteMessage(websocket.TextMessage, sub); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	readErr := make(chan error, 1)
	msgs := make(chan pusherMessage, 16)

	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			var msg pusherMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("[Reverb] Malformed message: %v", err)
				continue
			}
			msgs <- msg
		}
	}()

	for {
		select {
		case <-ctx.Done():
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil

		case err := <-readErr:
			return err

		case <-ping.C:
			p, _ := json.Marshal(pusherMessage{Event: "pusher:ping", Data: mustMarshal(map[string]any{})})
			if err := conn.WriteMessage(websocket.TextMessage, p); err != nil {
				return fmt.Errorf("ping: %w", err)
			}

		case msg := <-msgs:
			i.handleReverbMessage(msg, cfg.Channel)
		}
	}
}

func (i *Instance) awaitConnected(conn *websocket.Conn) (string, error) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return "", err
		}
		var msg pusherMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Event == "pusher:connection_established" {
			var dataStr string
			if err := json.Unmarshal(msg.Data, &dataStr); err != nil {
				return "", fmt.Errorf("parse connection data: %w", err)
			}
			var connected pusherConnected
			if err := json.Unmarshal([]byte(dataStr), &connected); err != nil {
				return "", fmt.Errorf("parse socket_id: %w", err)
			}
			return connected.SocketID, nil
		}
	}
}

func (i *Instance) handleReverbMessage(msg pusherMessage, channel string) {
	switch msg.Event {
	case "pusher:pong",
		"pusher_internal:subscription_succeeded",
		"pusher:connection_established":
		return
	}

	if msg.Channel != channel {
		return
	}

	var dataStr string
	var payload pusherEventData

	if err := json.Unmarshal(msg.Data, &dataStr); err == nil {
		if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
			log.Printf("[Reverb] Failed to decode inner data: %v", err)
			return
		}
	} else {
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("[Reverb] Failed to decode data: %v", err)
			return
		}
	}

	switch payload.Event {
	case "node_updated":
		ctrl, ok := i.controllerMap[payload.NodeID]
		if !ok {
			return
		}
		ctrl.TriggerNodeSync()

	case "subscriptions_updated":
		for _, ctrl := range i.controllerMap {
			ctrl.TriggerSubscriptionSync()
		}

	default:
		log.Printf("[Reverb] Unknown event %q — ignoring", payload.Event)
	}
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func signChannel(appKey, appSecret, socketID, channel string) string {
	stringToSign := socketID + ":" + channel
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(stringToSign))
	sig := hex.EncodeToString(mac.Sum(nil))
	return appKey + ":" + sig
}
