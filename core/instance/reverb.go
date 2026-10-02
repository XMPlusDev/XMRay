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
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type reverbOutbound struct {
	event  string
	data   any
	result chan error
}

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

const reverbChannel = "private-xmplus"

type reverbSession struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool
}

const reverbWriteTimeout = 10 * time.Second

func (s *reverbSession) send(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("reverb session closed")
	}
	s.conn.SetWriteDeadline(time.Now().Add(reverbWriteTimeout))
	return s.conn.WriteMessage(websocket.TextMessage, b)
}

func (s *reverbSession) sendRaw(messageType int, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("reverb session closed")
	}
	s.conn.SetWriteDeadline(time.Now().Add(reverbWriteTimeout))
	return s.conn.WriteMessage(messageType, b)
}

func (s *reverbSession) push(event string, data any) error {
	payload, err := json.Marshal(pusherMessage{
		Event:   "client-" + event,
		Channel: reverbChannel,
		Data:    mustMarshal(data),
	})
	if err != nil {
		return err
	}
	return s.send(payload)
}

func (s *reverbSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
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

		log.Printf("[Reverb] Connecting to websocket channel=[%s] via [%s]", reverbChannel, cfg.Host)

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, http.Header{})
		if err != nil {
			log.Printf("[Reverb] Connection error: %v — retrying in %s", err, backoff)
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
		return fmt.Errorf("Reconnection Error: %w", err)
	}

	subData := map[string]string{"channel": reverbChannel}
	if cfg.AppSecret != "" {
		subData["auth"] = signChannel(cfg.AppKey, cfg.AppSecret, socketID, reverbChannel)
	}

	sub, _ := json.Marshal(pusherMessage{
		Event: "pusher:subscribe",
		Data:  mustMarshal(subData),
	})

	sess := &reverbSession{conn: conn}
	if err := sess.send(sub); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	i.reverbMu.Lock()
	i.currentPusher = sess.push
	i.reverbMu.Unlock()
	defer func() {
		i.reverbMu.Lock()
		i.currentPusher = nil
		i.reverbMu.Unlock()
		sess.close()
	}()

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
			sess.sendRaw(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil

		case err := <-readErr:
			return err

		case <-ping.C:
			p, _ := json.Marshal(pusherMessage{
				Event: "pusher:ping",
				Data:  mustMarshal(map[string]any{}),
			})
			if err := sess.send(p); err != nil {
				return fmt.Errorf("ping: %w", err)
			}

		case msg := <-msgs:
			i.handleReverbMessage(msg, reverbChannel)
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
	case "pusher:pong", "pusher_internal:subscription_succeeded", "pusher:connection_established":
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

	case "server_updated":
		select {
		case i.serverPollTrigger <- struct{}{}:
			log.Printf("[Reverb] server_updated received — queued server poll trigger")
		default:
			log.Printf("[Reverb] server_updated received — poll trigger already queued")
		}
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
