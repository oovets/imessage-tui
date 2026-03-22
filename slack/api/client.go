package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://slack.com/api"

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

type AuthInfo struct {
	UserID   string
	UserName string
	TeamName string
}

type UserInfo struct {
	ID          string
	Username    string
	DisplayName string
	RealName    string
	IsBot       bool
	IsAppUser   bool
}

type Channel struct {
	ID          string
	Name        string
	DisplayName string
	UserID      string
	IsPrivate   bool
	IsMember    bool
	IsIM        bool
	IsMPIM      bool
	UnreadCount int
	HasUnread   bool
	LastReadTS  string
	LatestTS    string
}

type File struct {
	ID              string
	Name            string
	MimeType        string
	URLPrivate      string
	Thumb360        string
	IsExternal      bool
	Permalink       string
	PermalinkPublic string
}

func (f File) BestImageURL() string {
	if strings.TrimSpace(f.URLPrivate) != "" {
		return f.URLPrivate
	}
	if strings.TrimSpace(f.Thumb360) != "" {
		return f.Thumb360
	}
	return ""
}

func (f File) IsImage() bool {
	m := strings.ToLower(strings.TrimSpace(f.MimeType))
	return strings.HasPrefix(m, "image/")
}

type Message struct {
	TS         string
	ThreadTS   string
	UserID     string
	Username   string
	Text       string
	BotID      string
	Subtype    string
	Time       time.Time
	Files      []File
	ReplyCount int
}

type slackEnvelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func NewClient(token, baseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultBaseURL
	}
	return &Client{
		token:   strings.TrimSpace(token),
		baseURL: base,
		http:    &http.Client{Timeout: 25 * time.Second},
	}
}

func (c *Client) AuthTest() (*AuthInfo, error) {
	var out struct {
		slackEnvelope
		UserID string `json:"user_id"`
		User   string `json:"user"`
		Team   string `json:"team"`
	}
	if err := c.postForm("auth.test", nil, &out); err != nil {
		return nil, err
	}
	return &AuthInfo{UserID: out.UserID, UserName: out.User, TeamName: out.Team}, nil
}

func (c *Client) ListChannels(limit int) ([]Channel, error) {
	if limit <= 0 {
		limit = 200
	}
	var channels []Channel
	cursor := ""
	for {
		form := url.Values{}
		form.Set("limit", strconv.Itoa(limit))
		form.Set("types", "public_channel,private_channel,mpim,im")
		form.Set("exclude_archived", "true")
		if cursor != "" {
			form.Set("cursor", cursor)
		}
		var out struct {
			slackEnvelope
			Channels []struct {
				ID                 string `json:"id"`
				Name               string `json:"name"`
				User               string `json:"user"`
				IsPrivate          bool   `json:"is_private"`
				IsMember           bool   `json:"is_member"`
				IsIM               bool   `json:"is_im"`
				IsMPIM             bool   `json:"is_mpim"`
				UnreadCountDisplay int    `json:"unread_count_display"`
				HasUnreads         bool   `json:"has_unreads"`
				LastRead           string `json:"last_read"`
				Latest             string `json:"latest"`
			} `json:"channels"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.postForm("conversations.list", form, &out); err != nil {
			return nil, err
		}
		for _, ch := range out.Channels {
			channels = append(channels, Channel{
				ID:          ch.ID,
				Name:        ch.Name,
				DisplayName: ch.Name,
				UserID:      ch.User,
				IsPrivate:   ch.IsPrivate,
				IsMember:    ch.IsMember,
				IsIM:        ch.IsIM,
				IsMPIM:      ch.IsMPIM,
				UnreadCount: ch.UnreadCountDisplay,
				HasUnread:   ch.HasUnreads || ch.UnreadCountDisplay > 0,
				LastReadTS:  ch.LastRead,
				LatestTS:    ch.Latest,
			})
		}
		cursor = strings.TrimSpace(out.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
	}
	return channels, nil
}

func (c *Client) ConversationMembers(channelID string) ([]string, error) {
	form := url.Values{}
	form.Set("channel", channelID)
	form.Set("limit", "200")
	var out struct {
		slackEnvelope
		Members []string `json:"members"`
	}
	if err := c.postForm("conversations.members", form, &out); err != nil {
		return nil, err
	}
	return out.Members, nil
}

func (c *Client) UserMap() (map[string]string, error) {
	users := make(map[string]string)
	cursor := ""
	for {
		form := url.Values{}
		form.Set("limit", "200")
		if cursor != "" {
			form.Set("cursor", cursor)
		}
		var out struct {
			slackEnvelope
			Members []struct {
				ID        string `json:"id"`
				Deleted   bool   `json:"deleted"`
				Name      string `json:"name"`
				IsBot     bool   `json:"is_bot"`
				IsAppUser bool   `json:"is_app_user"`
				Profile   struct {
					DisplayName string `json:"display_name"`
					RealName    string `json:"real_name"`
				} `json:"profile"`
			} `json:"members"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.postForm("users.list", form, &out); err != nil {
			return nil, err
		}
		for _, m := range out.Members {
			if strings.TrimSpace(m.ID) == "" || m.Deleted {
				continue
			}
			name := strings.TrimSpace(m.Profile.DisplayName)
			if name == "" {
				name = strings.TrimSpace(m.Profile.RealName)
			}
			if name == "" {
				name = strings.TrimSpace(m.Name)
			}
			if name == "" {
				name = m.ID
			}
			users[m.ID] = name
		}
		cursor = strings.TrimSpace(out.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
	}
	return users, nil
}

func (c *Client) UserDirectory() ([]UserInfo, error) {
	var users []UserInfo
	cursor := ""
	for {
		form := url.Values{}
		form.Set("limit", "200")
		if cursor != "" {
			form.Set("cursor", cursor)
		}
		var out struct {
			slackEnvelope
			Members []struct {
				ID        string `json:"id"`
				Deleted   bool   `json:"deleted"`
				Name      string `json:"name"`
				IsBot     bool   `json:"is_bot"`
				IsAppUser bool   `json:"is_app_user"`
				Profile   struct {
					DisplayName string `json:"display_name"`
					RealName    string `json:"real_name"`
				} `json:"profile"`
			} `json:"members"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.postForm("users.list", form, &out); err != nil {
			return nil, err
		}
		for _, m := range out.Members {
			if strings.TrimSpace(m.ID) == "" || m.Deleted {
				continue
			}
			users = append(users, UserInfo{
				ID:          m.ID,
				Username:    strings.TrimSpace(m.Name),
				DisplayName: strings.TrimSpace(m.Profile.DisplayName),
				RealName:    strings.TrimSpace(m.Profile.RealName),
				IsBot:       m.IsBot,
				IsAppUser:   m.IsAppUser,
			})
		}
		cursor = strings.TrimSpace(out.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
	}
	return users, nil
}

func (c *Client) UserInfo(userID string) (*UserInfo, error) {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		return nil, fmt.Errorf("missing userID")
	}
	form := url.Values{}
	form.Set("user", uid)
	var out struct {
		slackEnvelope
		User struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsBot     bool   `json:"is_bot"`
			IsAppUser bool   `json:"is_app_user"`
			Profile   struct {
				DisplayName string `json:"display_name"`
				RealName    string `json:"real_name"`
			} `json:"profile"`
		} `json:"user"`
	}
	if err := c.postForm("users.info", form, &out); err != nil {
		return nil, err
	}
	return &UserInfo{
		ID:          strings.TrimSpace(out.User.ID),
		Username:    strings.TrimSpace(out.User.Name),
		DisplayName: strings.TrimSpace(out.User.Profile.DisplayName),
		RealName:    strings.TrimSpace(out.User.Profile.RealName),
		IsBot:       out.User.IsBot,
		IsAppUser:   out.User.IsAppUser,
	}, nil
}

func (c *Client) ChannelHistory(channelID string, limit int, userMap map[string]string) ([]Message, error) {
	if limit <= 0 {
		limit = 80
	}
	form := url.Values{}
	form.Set("channel", channelID)
	form.Set("limit", strconv.Itoa(limit))
	var out struct {
		slackEnvelope
		Messages []rawMessage `json:"messages"`
	}
	if err := c.postForm("conversations.history", form, &out); err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(out.Messages))
	for i := len(out.Messages) - 1; i >= 0; i-- {
		m, ok := out.Messages[i].toMessage(userMap)
		if !ok {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (c *Client) ThreadReplies(channelID, threadTS string, limit int, userMap map[string]string) ([]Message, error) {
	if strings.TrimSpace(threadTS) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 80
	}
	form := url.Values{}
	form.Set("channel", channelID)
	form.Set("ts", threadTS)
	form.Set("limit", strconv.Itoa(limit))
	var out struct {
		slackEnvelope
		Messages []rawMessage `json:"messages"`
	}
	if err := c.postForm("conversations.replies", form, &out); err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(out.Messages))
	for _, rm := range out.Messages {
		m, ok := rm.toMessage(userMap)
		if !ok {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (c *Client) PostMessage(channelID, text, threadTS string) error {
	form := url.Values{}
	form.Set("channel", channelID)
	form.Set("text", text)
	if strings.TrimSpace(threadTS) != "" {
		form.Set("thread_ts", threadTS)
	}
	var out slackEnvelope
	return c.postForm("chat.postMessage", form, &out)
}

func (c *Client) FetchPrivateURL(rawURL string) ([]byte, string, error) {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return nil, "", fmt.Errorf("empty file url")
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("fetch media status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *Client) postForm(method string, form url.Values, out any) error {
	if strings.TrimSpace(c.token) == "" {
		return fmt.Errorf("missing Slack token")
	}
	if form == nil {
		form = url.Values{}
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack api %s: status %d", method, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	raw, _ := json.Marshal(out)
	var probe slackEnvelope
	if err := json.Unmarshal(raw, &probe); err == nil && !probe.OK {
		if probe.Error == "" {
			probe.Error = "unknown_slack_error"
		}
		return fmt.Errorf("slack api %s: %s", method, probe.Error)
	}
	return nil
}

type rawMessage struct {
	Type        string `json:"type"`
	User        string `json:"user"`
	Username    string `json:"username"`
	Text        string `json:"text"`
	TS          string `json:"ts"`
	BotID       string `json:"bot_id"`
	Subtype     string `json:"subtype"`
	ThreadTS    string `json:"thread_ts"`
	ReplyCount  int    `json:"reply_count"`
	UserProfile struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Name        string `json:"name"`
	} `json:"user_profile"`
	BotProfile struct {
		Name string `json:"name"`
	} `json:"bot_profile"`
	Files []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		MimeType        string `json:"mimetype"`
		URLPrivate      string `json:"url_private"`
		Thumb360        string `json:"thumb_360"`
		IsExternal      bool   `json:"is_external"`
		Permalink       string `json:"permalink"`
		PermalinkPublic string `json:"permalink_public"`
	} `json:"files"`
}

func (rm rawMessage) toMessage(userMap map[string]string) (Message, bool) {
	if strings.TrimSpace(rm.TS) == "" {
		return Message{}, false
	}
	username := ""
	if userMap != nil {
		username = userMap[rm.User]
	}
	if strings.TrimSpace(username) == "" {
		username = strings.TrimSpace(rm.UserProfile.DisplayName)
	}
	if strings.TrimSpace(username) == "" {
		username = strings.TrimSpace(rm.UserProfile.RealName)
	}
	if strings.TrimSpace(username) == "" {
		username = strings.TrimSpace(rm.UserProfile.Name)
	}
	if strings.TrimSpace(username) == "" {
		username = strings.TrimSpace(rm.Username)
	}
	if strings.TrimSpace(username) == "" {
		username = strings.TrimSpace(rm.BotProfile.Name)
	}
	files := make([]File, 0, len(rm.Files))
	for _, f := range rm.Files {
		files = append(files, File{
			ID:              f.ID,
			Name:            f.Name,
			MimeType:        f.MimeType,
			URLPrivate:      f.URLPrivate,
			Thumb360:        f.Thumb360,
			IsExternal:      f.IsExternal,
			Permalink:       f.Permalink,
			PermalinkPublic: f.PermalinkPublic,
		})
	}
	return Message{
		TS:         rm.TS,
		ThreadTS:   rm.ThreadTS,
		UserID:     rm.User,
		Username:   username,
		Text:       rm.Text,
		BotID:      rm.BotID,
		Subtype:    rm.Subtype,
		Time:       parseSlackTS(rm.TS),
		Files:      files,
		ReplyCount: rm.ReplyCount,
	}, true
}

func parseSlackTS(ts string) time.Time {
	parts := strings.SplitN(strings.TrimSpace(ts), ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return time.Now()
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Now()
	}
	nsec := int64(0)
	if len(parts) == 2 && parts[1] != "" {
		frac := parts[1]
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}
		nsec, _ = strconv.ParseInt(frac, 10, 64)
	}
	return time.Unix(sec, nsec)
}
