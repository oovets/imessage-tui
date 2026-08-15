package slack

import (
	"log"
	"strings"
	"sync"

	slackapi "github.com/slack-go/slack"
)

// userCache resolves Slack ids to names.
//
// It exists for rate limits, not for speed. conversations.history returns
// senders as bare ids, so a channel with thirty participants would otherwise
// cost thirty users.info calls per history load — and users.info is capped
// around a hundred calls a minute. One users.list on first use answers the
// whole workspace instead, and only ids missing from it fall through to a
// single lookup.
type userCache struct {
	api *slackapi.Client

	mu     sync.RWMutex
	names  map[string]string
	bots   map[string]bool
	loaded bool
}

func newUserCache(api *slackapi.Client) *userCache {
	return &userCache{
		api:   api,
		names: make(map[string]string),
		bots:  make(map[string]bool),
	}
}

// loadAll pulls the whole member list once. A failure is not fatal: names then
// resolve one at a time through users.info, which is slower but correct.
func (c *userCache) loadAll() {
	c.mu.RLock()
	done := c.loaded
	c.mu.RUnlock()
	if done {
		return
	}

	users, err := c.api.GetUsers()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Marked loaded either way — a workspace too large for users.list would
	// otherwise retry the same failing call on every chat list refresh.
	c.loaded = true
	if err != nil {
		log.Printf("[slack] users.list failed, falling back to per-user lookups: %v", err)
		return
	}
	for i := range users {
		c.names[users[i].ID] = displayName(&users[i])
		c.bots[users[i].ID] = users[i].IsBot
	}
}

// name returns a display name for a user id, falling back to the id itself.
// An id shown raw is ugly but honest; an empty sender name would silently drop
// the "who wrote this" line in a channel.
func (c *userCache) name(userID string) string {
	if userID == "" {
		return ""
	}
	c.mu.RLock()
	name, ok := c.names[userID]
	c.mu.RUnlock()
	if ok {
		return name
	}

	user, err := c.api.GetUserInfo(userID)
	if err != nil {
		log.Printf("[slack] users.info %s failed: %v", userID, err)
		c.remember(userID, userID, false)
		return userID
	}
	resolved := displayName(user)
	c.remember(userID, resolved, user.IsBot)
	return resolved
}

// botName resolves a bot id (B…), which apps and webhooks send instead of a
// user id.
func (c *userCache) botName(botID string) string {
	if botID == "" {
		return ""
	}
	c.mu.RLock()
	name, ok := c.names[botID]
	c.mu.RUnlock()
	if ok {
		return name
	}

	bot, err := c.api.GetBotInfo(slackapi.GetBotInfoParameters{Bot: botID})
	if err != nil {
		log.Printf("[slack] bots.info %s failed: %v", botID, err)
		c.remember(botID, botID, true)
		return botID
	}
	c.remember(botID, bot.Name, true)
	return bot.Name
}

// isBot reports whether an id belongs to a bot. Unknown ids are not bots:
// hiding a real conversation is worse than listing a bot DM.
func (c *userCache) isBot(userID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bots[userID]
}

func (c *userCache) remember(id, name string, isBot bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names[id] = name
	c.bots[id] = isBot
}

// snapshot is the id→name map mrkdwn needs to resolve <@U123> mentions.
func (c *userCache) snapshot() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.names))
	for id, name := range c.names {
		out[id] = name
	}
	return out
}

// displayName picks what Slack itself shows: the chosen display name, then the
// real name, then the handle.
func displayName(user *slackapi.User) string {
	if user == nil {
		return ""
	}
	if name := strings.TrimSpace(user.Profile.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(user.RealName); name != "" {
		return name
	}
	return strings.TrimSpace(user.Name)
}
