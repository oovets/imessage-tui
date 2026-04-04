package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oovets/bluebubbles-gui/models"
	"github.com/tidwall/gjson"
)

type Client struct {
	baseURL         string
	password        string
	httpClient      *http.Client
	contactCache    map[string]string // Cached contact map to avoid repeated fetches
	previewProxyURL string
	oembedEndpoint  string
}

func NewClient(baseURL, password string) *Client {
	// Skip TLS verification for self-signed certs (common for BlueBubbles)
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		password:       password,
		httpClient:     httpClient,
		contactCache:   make(map[string]string),
		oembedEndpoint: "https://noembed.com/embed",
	}
}

// addAuth appends the password/guid query parameter
func (c *Client) addAuth(u *url.URL) {
	q := u.Query()
	// Try both password and guid parameter names
	if !strings.Contains(u.Path, "chat/query") {
		q.Set("password", c.password)
	} else {
		q.Set("guid", c.password)
	}
	u.RawQuery = q.Encode()
}

func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Has("guid") {
		q.Set("guid", "***")
	}
	if q.Has("password") {
		q.Set("password", "***")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// GetChats fetches chats sorted by most recent activity
func (c *Client) GetChats(limit int) ([]models.Chat, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/chat/query", c.baseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("guid", c.password)
	u.RawQuery = q.Encode()

	log.Printf("GetChats (POST): %s", redactedURL(u.String()))

	// Request body - fetch more to account for filtering
	payload := map[string]interface{}{}
	body, _ := json.Marshal(payload)

	// Use POST instead of GET
	resp, err := c.httpClient.Post(u.String(), "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("GetChats error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Printf("GetChats response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Try to extract chats from different possible response structures
	// Try data.data first
	result := gjson.GetBytes(respBody, "data.data")
	if !result.Exists() || result.Raw == "null" {
		// Try data.chats
		result = gjson.GetBytes(respBody, "data.chats")
	}
	if !result.Exists() || result.Raw == "null" {
		// Try just data
		result = gjson.GetBytes(respBody, "data")
	}

	var chats []models.Chat
	if err := json.Unmarshal([]byte(result.Raw), &chats); err != nil {
		log.Printf("Failed to parse chats: %v, raw: %s", err, result.Raw)
		return nil, fmt.Errorf("failed to parse chats: %v", err)
	}

	// Debug: log first chat structure
	if len(chats) > 0 {
		log.Printf("First chat debug: DisplayName=%q ChatIdentifier=%q participants=%d",
			chats[0].DisplayName, chats[0].ChatIdentifier, len(chats[0].Participants))
	}

	// Since LastMessage is always null, we need to fetch the latest message per chat
	// to sort by actual activity. This is expensive but necessary for proper sorting.

	type chatWithActivity struct {
		chat         models.Chat
		lastMsgTime  int64
		messageCount int
	}

	chatActivities := make([]chatWithActivity, len(chats))

	// Fetch contacts once to enrich chat participant names
	contactMap, _ := c.GetContacts()

	// Fill in contact display names for participants
	for i := range chats {
		for j := range chats[i].Participants {
			if chats[i].Participants[j].DisplayName == "" {
				// Try to find display name from contact map
				if name, exists := contactMap[chats[i].Participants[j].Address]; exists {
					chats[i].Participants[j].DisplayName = name
				}
			}
		}
	}

	log.Printf("Fetching activity info for %d chats (parallel)...", len(chats))

	// Use goroutines to fetch messages in parallel
	type activityResult struct {
		index        int
		lastMsgTime  int64
		messageCount int
		messageText  string
	}
	resultsChan := make(chan activityResult, len(chats))

	// Limit concurrent requests to avoid overwhelming the server
	maxConcurrent := 5
	semaphore := make(chan struct{}, maxConcurrent)

	for i, chat := range chats {
		go func(idx int, chatGUID string) {
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			msgs, err := c.getMessages(chatGUID, 1, false)
			result := activityResult{index: idx}
			if err != nil {
			} else if len(msgs) == 0 {
			} else {
				result.lastMsgTime = msgs[0].DateCreated
				result.messageCount = 1
				result.messageText = msgs[0].Text
			}
			resultsChan <- result
		}(i, chat.GUID)
	}

	// Collect results and enrich chats with message preview
	for i := 0; i < len(chats); i++ {
		result := <-resultsChan
		chatActivities[result.index].chat = chats[result.index]
		chatActivities[result.index].lastMsgTime = result.lastMsgTime
		chatActivities[result.index].messageCount = result.messageCount
		chatActivities[result.index].chat.LastMessageText = result.messageText

		if result.messageText != "" {
		} else {
		}
	}

	// Sort by last message time (descending - newest first)
	slices.SortFunc(chatActivities, func(a, b chatWithActivity) int {
		if a.lastMsgTime != b.lastMsgTime {
			return int(b.lastMsgTime - a.lastMsgTime) // descending
		}
		// Tie-breaker: chats with messages before empty ones
		if a.messageCount != b.messageCount {
			return b.messageCount - a.messageCount
		}
		return 0
	})

	// Extract sorted chats
	result_chats := make([]models.Chat, 0, len(chatActivities))
	for _, ca := range chatActivities {
		result_chats = append(result_chats, ca.chat)
	}

	// Trim to requested limit
	if len(result_chats) > limit {
		result_chats = result_chats[:limit]
	}

	log.Printf("Successfully loaded %d chats (sorted by activity)", len(result_chats))
	return result_chats, nil
}

// GetMessages fetches messages for a chat, newest first (will be reversed by caller)
func (c *Client) GetMessages(chatGUID string, limit int) ([]models.Message, error) {
	return c.getMessages(chatGUID, limit, true)
}

func (c *Client) getMessages(chatGUID string, limit int, includeAttachments bool) ([]models.Message, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/chat/%s/message", c.baseURL, url.QueryEscape(chatGUID)))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("guid", c.password)
	q.Set("limit", fmt.Sprintf("%d", limit))
	if includeAttachments {
		// Some BlueBubbles server versions only include attachment relations when
		// explicitly requested; unknown params are ignored by other versions.
		q.Set("with", "attachments")
		q.Set("withAttachments", "true")
		q.Set("includeAttachments", "true")
	}
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		log.Printf("GetMessages error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("GetMessages error response: %s", string(body))
		return nil, fmt.Errorf("API error: %s (status %d)", string(body), resp.StatusCode)
	}

	// Try different response structures
	result := gjson.GetBytes(body, "data.data")
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(body, "data")
	}
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(body, "messages")
	}

	var messages []models.Message
	if err := json.Unmarshal([]byte(result.Raw), &messages); err != nil {
		log.Printf("Failed to parse messages: %v, raw value was: %s", err, result.Raw)
		return nil, fmt.Errorf("failed to parse messages: %v", err)
	}

	// Fetch contacts to enrich message sender names
	contactMap, _ := c.GetContacts()

	// Inject chat GUID, fill in handle display names, and reverse (BlueBubbles returns newest first)
	for i := range messages {
		messages[i].ChatGUID = chatGUID
		// Fill in display name for message handle from contact map
		if messages[i].Handle != nil && messages[i].Handle.DisplayName == "" {
			if name, exists := contactMap[messages[i].Handle.Address]; exists {
				messages[i].Handle.DisplayName = name
			}
		}
	}
	slices.Reverse(messages)

	log.Printf("Successfully loaded %d messages for chat", len(messages))
	return messages, nil
}

// SendMessage posts a new iMessage. If replyToGUID is set it first tries the
// Private API (required for threaded replies); if that fails it falls back to
// apple-script and sends the message without threading context.
func (c *Client) SendMessage(chatGUID, text, replyToGUID string) error {
	if strings.TrimSpace(replyToGUID) != "" {
		err := c.sendMessage(chatGUID, text, replyToGUID, "private-api")
		if err == nil {
			return nil
		}
		log.Printf("SendMessage private-api failed (%v), falling back to apple-script", err)
	}
	return c.sendMessage(chatGUID, text, "", "apple-script")
}

func (c *Client) sendMessage(chatGUID, text, replyToGUID, method string) error {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/message/text", c.baseURL))
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("guid", c.password)
	u.RawQuery = q.Encode()

	payload := map[string]interface{}{
		"chatGuid": chatGUID,
		"message":  text,
		"method":   method,
		"tempGuid": uuid.New().String(),
	}
	if strings.TrimSpace(replyToGUID) != "" {
		payload["selectedMessageGuid"] = replyToGUID
		payload["partIndex"] = 0
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	log.Printf("SendMessage POST method=%s: %s", method, redactedURL(u.String()))

	resp, err := c.httpClient.Post(u.String(), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	log.Printf("SendMessage response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("API error: %s (status %d)", string(respBody), resp.StatusCode)
	}

	return nil
}

func (c *Client) SendReaction(chatGUID, selectedMessageGUID, reaction string, partIndex int) error {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/message/react", c.baseURL))
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("guid", c.password)
	u.RawQuery = q.Encode()

	payload := map[string]interface{}{
		"chatGuid":            chatGUID,
		"selectedMessageGuid": selectedMessageGUID,
		"reaction":            reaction,
		"partIndex":           partIndex,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(u.String(), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("reaction API error: %s (status %d)", string(respBody), resp.StatusCode)
	}

	return nil
}

// GetContacts fetches all contacts from BlueBubbles (uses cache to avoid repeated fetches)
func (c *Client) GetContacts() (map[string]string, error) {
	// Return cached contacts if already fetched
	if len(c.contactCache) > 0 {
		return c.contactCache, nil
	}

	u, err := url.Parse(fmt.Sprintf("%s/api/v1/contact/query", c.baseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("guid", c.password)
	u.RawQuery = q.Encode()

	log.Printf("GetContacts (POST): %s", redactedURL(u.String()))

	resp, err := c.httpClient.Post(u.String(), "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		log.Printf("GetContacts error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Printf("GetContacts response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Printf("GetContacts error (status %d)", resp.StatusCode)
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	// Extract contacts from response
	result := gjson.GetBytes(body, "data.data")
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(body, "data")
	}

	// BlueBubbles contacts have a different structure than Handle
	type ContactResponse struct {
		DisplayName  string `json:"displayName"`
		PhoneNumbers []struct {
			Address string `json:"address"`
		} `json:"phoneNumbers"`
	}

	// Parse contacts and map address -> name
	contactMap := make(map[string]string)
	var contacts []ContactResponse
	if err := json.Unmarshal([]byte(result.Raw), &contacts); err != nil {
		log.Printf("Failed to parse contacts: %v", err)
		return contactMap, nil // Return empty map, don't fail
	}

	for _, contact := range contacts {
		if contact.DisplayName != "" && len(contact.PhoneNumbers) > 0 {
			// Use the first phone number as the primary address
			for _, phone := range contact.PhoneNumbers {
				if phone.Address != "" {
					contactMap[phone.Address] = contact.DisplayName
					log.Printf("Contact: %s -> %s", phone.Address, contact.DisplayName)
				}
			}
		}
	}

	// Cache the results for future use
	c.contactCache = contactMap

	log.Printf("Successfully loaded %d contacts (cached)", len(contactMap))
	return contactMap, nil
}

// DownloadAttachment fetches the raw bytes of an attachment by GUID.
// Returns the data and the MIME type reported by the server.
func (c *Client) DownloadAttachment(guid string) ([]byte, string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/attachment/%s/download", c.baseURL, url.PathEscape(guid)))
	if err != nil {
		return nil, "", err
	}
	c.addAuth(u)

	log.Printf("DownloadAttachment: %s", redactedURL(u.String()))

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("attachment download failed: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024)) // 20 MB cap
	if err != nil {
		return nil, "", err
	}

	mimeType := resp.Header.Get("Content-Type")
	return data, mimeType, nil
}

// Ping checks server connectivity by trying to fetch chats
func (c *Client) Ping() error {
	log.Println("Pinging server via chat query...")
	// Just try to call GetChats - if it succeeds, server is up
	_, err := c.GetChats(1)
	if err != nil {
		log.Printf("Ping failed: %v", err)
		return err
	}
	log.Println("✓ Ping successful")
	return nil
}
