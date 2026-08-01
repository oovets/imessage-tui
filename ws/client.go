package ws

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/oovets/imessage-tui/models"
)

// wsReadLimit caps a single incoming WebSocket frame so a malicious or buggy
// server can't force an unbounded allocation.
const wsReadLimit = 16 << 20 // 16 MiB

// Reconnect backoff bounds for the read loop. The wait doubles after each
// failed dial, starting at reconnectBackoffMin and never exceeding
// reconnectBackoffMax, so a long server outage polls politely instead of
// hammering the endpoint or spinning the CPU.
const (
	reconnectBackoffMin = 2 * time.Second
	reconnectBackoffMax = 30 * time.Second
)

// insecureTLS reports whether the user explicitly opted out of TLS certificate
// verification (e.g. for a self-signed BlueBubbles server). Secure by default.
func insecureTLS() bool {
	return os.Getenv("BB_INSECURE_TLS") == "1"
}

type Client struct {
	baseURL    string
	password   string
	conn       *websocket.Conn
	Events     chan models.WSEvent
	Reconnect  chan struct{}
	Disconnect chan struct{}
	Overflow   chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.Mutex

	// Reconnect backoff bounds. Defaults come from the package constants and
	// are overridable from tests to keep reconnect timing short.
	backoffMin time.Duration
	backoffMax time.Duration
}

func NewClient(baseURL, password string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		password:   password,
		Events:     make(chan models.WSEvent, 500),
		Reconnect:  make(chan struct{}, 4),
		Disconnect: make(chan struct{}, 4),
		Overflow:   make(chan struct{}, 4),
		done:       make(chan struct{}),
		backoffMin: reconnectBackoffMin,
		backoffMax: reconnectBackoffMax,
	}
}

func (c *Client) Connect() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	go c.readLoop()

	return nil
}

func (c *Client) dial() (*websocket.Conn, error) {
	wsURL := c.baseURL
	wsURL = strings.ReplaceAll(wsURL, "https://", "wss://")
	wsURL = strings.ReplaceAll(wsURL, "http://", "ws://")

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
			InsecureSkipVerify: insecureTLS(),
		},
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial failed: %v", err)
	}
	conn.SetReadLimit(wsReadLimit)

	return conn, nil
}

func (c *Client) sendPong() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	// A stuck peer must not block the read loop forever on the pong write.
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := c.conn.WriteMessage(websocket.TextMessage, []byte("3")); err != nil {
		log.Printf("[WS] pong write failed: %v", err)
	}
}

func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *Client) readLoop() {
	// capped exponential backoff for reconnect attempts
	backoff := c.backoffMin
	for {
		if c.isClosed() {
			return
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			return
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			if !c.isClosed() {
				select {
				case c.Disconnect <- struct{}{}:
				default:
				}
			}

			// Reconnect forever with capped exponential backoff. We never go
			// back to reading the dead connection, so Disconnect is signaled
			// exactly once per failed connection instead of being spammed on
			// every re-read.
			for !c.isClosed() {
				select {
				case <-time.After(backoff):
				case <-c.done:
					return
				}

				newConn, err := c.dial()
				if err != nil {
					log.Printf("[WS] Reconnect failed (backoff %s): %v", backoff, err)
					backoff *= 2
					if backoff > c.backoffMax {
						backoff = c.backoffMax
					}
					continue
				}

				backoff = c.backoffMin
				c.mu.Lock()
				c.conn = newConn
				c.mu.Unlock()
				select {
				case c.Reconnect <- struct{}{}:
				default:
				}
				break
			}
			continue
		}

		backoff = c.backoffMin

		msg := string(raw)

		switch {
		case strings.HasPrefix(msg, "0"):
			c.mu.Lock()
			c.conn.WriteMessage(websocket.TextMessage, []byte("40"))
			c.mu.Unlock()
			continue

		case strings.HasPrefix(msg, "40"):
			continue

		case msg == "2":
			c.sendPong()
			continue

		case msg == "3":
			continue

		case strings.HasPrefix(msg, "42"):
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

			select {
			case c.Events <- models.WSEvent{Type: eventType, Data: eventData}:
			case <-c.done:
				return
			default:
				log.Printf("[WS] Events channel full, dropping event: %s (will request resync)", eventType)
				select {
				case c.Overflow <- struct{}{}:
				default:
				}
			}

		default:
			continue
		}
	}
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	c.closeOnce.Do(func() { close(c.done) })
	return c.conn.Close()
}
