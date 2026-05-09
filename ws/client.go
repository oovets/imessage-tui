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
	"github.com/oovets/imessage-tui/models"
)

type Client struct {
	baseURL    string
	password   string
	conn       *websocket.Conn
	Events     chan models.WSEvent
	Reconnect  chan struct{}
	Disconnect chan struct{}
	Overflow   chan struct{}
	done       chan struct{}
	mu         sync.Mutex
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
			InsecureSkipVerify: true,
		},
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial failed: %v", err)
	}

	return conn, nil
}

func (c *Client) sendPong() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.WriteMessage(websocket.TextMessage, []byte("3"))
	}
}

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

			select {
			case <-c.done:
				return
			default:
			}

			select {
			case c.Disconnect <- struct{}{}:
			default:
			}

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
				time.Sleep(wait)

				newConn, err := c.dial()
				if err != nil {
					log.Printf("[WS] Reconnect attempt %d failed: %v", attempt, err)
					continue
				}

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
	close(c.done)
	return c.conn.Close()
}
