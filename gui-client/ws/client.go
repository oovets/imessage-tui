package ws

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/oovets/bluebubbles-gui/models"
)

type Client struct {
	baseURL  string
	password string
	conn     *websocket.Conn
	Events   chan models.WSEvent
	done     chan struct{}
	mu       sync.Mutex
}

func NewClient(baseURL, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		Events:   make(chan models.WSEvent, 50),
		done:     make(chan struct{}),
	}
}

// Connect dials the WebSocket endpoint
func (c *Client) Connect() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Start read loop in goroutine
	go c.readLoop()

	return nil
}

func (c *Client) dial() (*websocket.Conn, error) {
	// Convert https to wss, http to ws
	wsURL := c.baseURL
	wsURL = strings.ReplaceAll(wsURL, "https://", "wss://")
	wsURL = strings.ReplaceAll(wsURL, "http://", "ws://")

	// Append Socket.IO endpoint with EIO=4 for raw WebSocket transport
	u, err := url.Parse(fmt.Sprintf("%s/socket.io/?EIO=4&transport=websocket&guid=%s", wsURL, url.QueryEscape(c.password)))
	if err != nil {
		return nil, err
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		NetDialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	log.Printf("[WS] Connecting to %s", u.String())
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial failed: %v", err)
	}

	log.Printf("[WS] Connected successfully")
	return conn, nil
}

func (c *Client) sendPong() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.WriteMessage(websocket.TextMessage, []byte("3"))
	}
}

// readLoop handles incoming WebSocket messages with auto-reconnect
func (c *Client) readLoop() {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			return
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[WS] Read error: %v, attempting reconnect...", err)

			// Check if we should stop
			select {
			case <-c.done:
				return
			default:
			}

			// Try to reconnect with backoff
			for attempt := 1; attempt <= 10; attempt++ {
				select {
				case <-c.done:
					return
				default:
				}

				wait := time.Duration(attempt) * 2 * time.Second
				if wait > 30*time.Second {
					wait = 30 * time.Second
				}
				log.Printf("[WS] Reconnect attempt %d in %v...", attempt, wait)
				time.Sleep(wait)

				newConn, err := c.dial()
				if err != nil {
					log.Printf("[WS] Reconnect attempt %d failed: %v", attempt, err)
					continue
				}

				c.mu.Lock()
				c.conn = newConn
				c.mu.Unlock()
				log.Printf("[WS] Reconnected successfully")
				break
			}
			continue
		}

		msg := string(raw)

		switch {
		case strings.HasPrefix(msg, "0"):
			// Socket.IO open frame - contains pingInterval/pingTimeout
			// We must respond with "40" to connect to the default namespace
			log.Printf("[WS] Received handshake frame, sending namespace connect")
			c.mu.Lock()
			c.conn.WriteMessage(websocket.TextMessage, []byte("40"))
			c.mu.Unlock()
			continue

		case strings.HasPrefix(msg, "40"):
			// Socket.IO connect confirmation for namespace
			log.Printf("[WS] Socket.IO namespace connected")
			continue

		case msg == "2":
			// Socket.IO ping - respond with pong
			log.Printf("[WS] Ping received, sending pong")
			c.sendPong()
			continue

		case msg == "3":
			// Socket.IO pong response, ignore
			continue

		case strings.HasPrefix(msg, "42"):
			// Socket.IO event frame: 42[eventName, eventData]
			payload := msg[2:]

			var arr []json.RawMessage
			if err := json.Unmarshal([]byte(payload), &arr); err != nil {
				log.Printf("[WS] Failed to parse event: %v", err)
				continue
			}

			if len(arr) < 1 {
				continue
			}

			var eventType string
			if err := json.Unmarshal(arr[0], &eventType); err != nil {
				continue
			}

			var eventData json.RawMessage
			if len(arr) > 1 {
				eventData = arr[1]
			}

			log.Printf("[WS] Event received: %s", eventType)

			select {
			case c.Events <- models.WSEvent{Type: eventType, Data: eventData}:
			case <-c.done:
				return
			default:
				// Channel full, drop event
				log.Printf("[WS] Events channel full, dropping event: %s", eventType)
			}

		default:
			log.Printf("[WS] Unknown frame: %.50s", msg)
			continue
		}
	}
}

// Close closes the WebSocket connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	close(c.done)
	return c.conn.Close()
}
