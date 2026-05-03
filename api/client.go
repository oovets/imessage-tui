package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oovets/imessage-tui/models"
	"github.com/tidwall/gjson"
)

type Client struct {
	baseURL         string
	password        string
	httpClient      *http.Client
	contactCache    map[string]string
	previewProxyURL string
	oembedEndpoint  string
}

func NewClient(baseURL, password string) *Client {
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

func (c *Client) addAuth(u *url.URL) {
	q := u.Query()
	if !strings.Contains(u.Path, "chat/query") {
		q.Set("password", c.password)
	} else {
		q.Set("guid", c.password)
	}
	u.RawQuery = q.Encode()
}

func (c *Client) GetChats(limit int) ([]models.Chat, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/chat/query", c.baseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("guid", c.password)
	u.RawQuery = q.Encode()

	payload := map[string]interface{}{}
	body, _ := json.Marshal(payload)

	resp, err := c.httpClient.Post(u.String(), "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	result := gjson.GetBytes(respBody, "data.data")
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(respBody, "data.chats")
	}
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(respBody, "data")
	}

	var chats []models.Chat
	if err := json.Unmarshal([]byte(result.Raw), &chats); err != nil {
		return nil, fmt.Errorf("failed to parse chats: %v", err)
	}

	type chatWithActivity struct {
		chat         models.Chat
		lastMsgTime  int64
		messageCount int
	}

	chatActivities := make([]chatWithActivity, len(chats))

	contactMap, _ := c.GetContacts()

	for i := range chats {
		for j := range chats[i].Participants {
			if chats[i].Participants[j].DisplayName == "" {
				if name, exists := contactMap[chats[i].Participants[j].Address]; exists {
					chats[i].Participants[j].DisplayName = name
				}
			}
		}
	}

	type activityResult struct {
		index        int
		lastMsgTime  int64
		messageCount int
		messageText  string
	}
	resultsChan := make(chan activityResult, len(chats))

	maxConcurrent := 5
	semaphore := make(chan struct{}, maxConcurrent)

	for i, chat := range chats {
		go func(idx int, chatGUID string) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			msgs, err := c.getMessages(chatGUID, 1, false)
			result := activityResult{index: idx}
			if err == nil && len(msgs) > 0 {
				result.lastMsgTime = msgs[0].DateCreated
				result.messageCount = 1
				result.messageText = msgs[0].Text
			}
			resultsChan <- result
		}(i, chat.GUID)
	}

	for i := 0; i < len(chats); i++ {
		result := <-resultsChan
		chatActivities[result.index].chat = chats[result.index]
		chatActivities[result.index].lastMsgTime = result.lastMsgTime
		chatActivities[result.index].messageCount = result.messageCount
		chatActivities[result.index].chat.LastMessageText = result.messageText
	}

	slices.SortFunc(chatActivities, func(a, b chatWithActivity) int {
		if a.lastMsgTime != b.lastMsgTime {
			return int(b.lastMsgTime - a.lastMsgTime)
		}
		if a.messageCount != b.messageCount {
			return b.messageCount - a.messageCount
		}
		return 0
	})

	resultChats := make([]models.Chat, 0, len(chatActivities))
	for _, ca := range chatActivities {
		resultChats = append(resultChats, ca.chat)
	}

	if len(resultChats) > limit {
		resultChats = resultChats[:limit]
	}

	return resultChats, nil
}

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
		q.Set("with", "attachments")
		q.Set("withAttachments", "true")
		q.Set("includeAttachments", "true")
	}
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s (status %d)", string(body), resp.StatusCode)
	}

	result := gjson.GetBytes(body, "data.data")
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(body, "data")
	}
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(body, "messages")
	}

	var messages []models.Message
	if err := json.Unmarshal([]byte(result.Raw), &messages); err != nil {
		return nil, fmt.Errorf("failed to parse messages: %v", err)
	}

	contactMap, _ := c.GetContacts()

	for i := range messages {
		messages[i].ChatGUID = chatGUID
		if messages[i].Handle != nil && messages[i].Handle.DisplayName == "" {
			if name, exists := contactMap[messages[i].Handle.Address]; exists {
				messages[i].Handle.DisplayName = name
			}
		}
	}
	slices.Reverse(messages)

	return messages, nil
}

func (c *Client) SendMessage(chatGUID, text, replyToGUID string) error {
	return c.SendMessageWithTempGUID(chatGUID, text, replyToGUID, uuid.New().String())
}

func (c *Client) SendMessageWithTempGUID(chatGUID, text, replyToGUID, tempGUID string) error {
	tempGUID = strings.TrimSpace(tempGUID)
	if tempGUID == "" {
		tempGUID = uuid.New().String()
	}
	if strings.TrimSpace(replyToGUID) != "" {
		err := c.sendMessage(chatGUID, text, replyToGUID, "private-api", tempGUID)
		if err == nil {
			return nil
		}
	}
	return c.sendMessage(chatGUID, text, "", "apple-script", tempGUID)
}

func (c *Client) sendMessage(chatGUID, text, replyToGUID, method, tempGUID string) error {
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
		"tempGuid": tempGUID,
	}
	if strings.TrimSpace(replyToGUID) != "" {
		payload["selectedMessageGuid"] = replyToGUID
		payload["partIndex"] = 0
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

func (c *Client) MarkChatRead(chatGUID string) error {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/chat/%s/read", c.baseURL, url.PathEscape(chatGUID)))
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("guid", c.password)
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Post(u.String(), "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("mark chat read API error: %s (status %d)", string(respBody), resp.StatusCode)
	}

	return nil
}

func (c *Client) GetContacts() (map[string]string, error) {
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

	resp, err := c.httpClient.Post(u.String(), "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	result := gjson.GetBytes(body, "data.data")
	if !result.Exists() || result.Raw == "null" {
		result = gjson.GetBytes(body, "data")
	}

	type ContactResponse struct {
		DisplayName  string `json:"displayName"`
		PhoneNumbers []struct {
			Address string `json:"address"`
		} `json:"phoneNumbers"`
	}

	contactMap := make(map[string]string)
	var contacts []ContactResponse
	if err := json.Unmarshal([]byte(result.Raw), &contacts); err != nil {
		return contactMap, nil
	}

	for _, contact := range contacts {
		if contact.DisplayName != "" && len(contact.PhoneNumbers) > 0 {
			for _, phone := range contact.PhoneNumbers {
				if phone.Address != "" {
					contactMap[phone.Address] = contact.DisplayName
				}
			}
		}
	}

	c.contactCache = contactMap

	return contactMap, nil
}

func (c *Client) DownloadAttachment(guid string) ([]byte, string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/attachment/%s/download", c.baseURL, url.PathEscape(guid)))
	if err != nil {
		return nil, "", err
	}
	c.addAuth(u)

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("attachment download failed: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
	if err != nil {
		return nil, "", err
	}

	mimeType := resp.Header.Get("Content-Type")
	return data, mimeType, nil
}

func (c *Client) Ping() error {
	_, err := c.GetChats(1)
	return err
}
