package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/oovets/imessage-tui/models"
)

func dedupeMessages(messages []models.Message) []models.Message {
	if len(messages) < 2 {
		return messages
	}

	out := make([]models.Message, 0, len(messages))
	keyToIndex := make(map[string]int, len(messages)*2)

	for _, msg := range messages {
		keys := messageDedupeKeys(msg)
		duplicateIndex := -1
		for _, key := range keys {
			if idx, ok := keyToIndex[key]; ok {
				duplicateIndex = idx
				break
			}
		}

		if duplicateIndex >= 0 {
			out[duplicateIndex] = mergeDuplicateMessage(out[duplicateIndex], msg)
			for _, key := range keys {
				keyToIndex[key] = duplicateIndex
			}
			for _, key := range messageDedupeKeys(out[duplicateIndex]) {
				keyToIndex[key] = duplicateIndex
			}
			continue
		}

		idx := len(out)
		out = append(out, msg)
		for _, key := range keys {
			keyToIndex[key] = idx
		}
	}

	return out
}

func foldReactionMessages(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return messages
	}
	out := make([]models.Message, 0, len(messages))
	indexByGUID := make(map[string]int, len(messages))
	pending := make(map[string][]string)

	for _, msg := range messages {
		if emoji := reactionEmoji(msg); emoji != "" {
			targetGUID := normalizeAssociatedMessageGUID(msg.AssociatedMessageGUID)
			if idx, ok := indexByGUID[targetGUID]; ok {
				addReaction(&out[idx], emoji)
			} else if targetGUID != "" {
				pending[targetGUID] = append(pending[targetGUID], emoji)
			}
			continue
		}

		idx := len(out)
		out = append(out, msg)
		if guid := strings.TrimSpace(msg.GUID); guid != "" {
			indexByGUID[guid] = idx
			for _, emoji := range pending[guid] {
				addReaction(&out[idx], emoji)
			}
			delete(pending, guid)
		}
	}

	return out
}

func addReactionToMessages(messages []models.Message, associatedGUID, emoji string) bool {
	associatedGUID = normalizeAssociatedMessageGUID(associatedGUID)
	if associatedGUID == "" || emoji == "" {
		return false
	}
	for i := range messages {
		if messages[i].GUID != associatedGUID {
			continue
		}
		addReaction(&messages[i], emoji)
		return true
	}
	return false
}

func addReaction(msg *models.Message, emoji string) {
	if msg.ReactionCounts == nil {
		msg.ReactionCounts = make(map[string]int)
	}
	msg.ReactionCounts[emoji]++
}

func reactionEmoji(msg models.Message) string {
	if normalizeAssociatedMessageGUID(msg.AssociatedMessageGUID) == "" {
		return ""
	}
	raw := strings.ToLower(strings.TrimSpace(msg.AssociatedMessageType + " " + msg.Text))
	switch {
	case strings.Contains(raw, "2000"):
		return "❤️"
	case strings.Contains(raw, "3000"):
		return "👍"
	case strings.Contains(raw, "4000"):
		return "👎"
	case strings.Contains(raw, "5000"):
		return "😂"
	case strings.Contains(raw, "6000"):
		return "‼️"
	case strings.Contains(raw, "7000"):
		return "❓"
	case strings.Contains(raw, "❤️"), strings.Contains(raw, "❤"), strings.Contains(raw, "love"), strings.Contains(raw, "loved"), strings.Contains(raw, "heart"), strings.Contains(raw, "hjarta"):
		return "❤️"
	case strings.Contains(raw, "👍"), strings.Contains(raw, "like"), strings.Contains(raw, "liked"), strings.Contains(raw, "thumbs up"), strings.Contains(raw, "thumbsup"):
		return "👍"
	case strings.Contains(raw, "👎"), strings.Contains(raw, "dislike"), strings.Contains(raw, "disliked"), strings.Contains(raw, "thumbs down"), strings.Contains(raw, "thumbsdown"):
		return "👎"
	case strings.Contains(raw, "😂"), strings.Contains(raw, "🤣"), strings.Contains(raw, "laugh"), strings.Contains(raw, "laughed"), strings.Contains(raw, "haha"):
		return "😂"
	case strings.Contains(raw, "‼"), strings.Contains(raw, "emphasize"), strings.Contains(raw, "emphasized"), strings.Contains(raw, "exclaim"), strings.Contains(raw, "!!"):
		return "‼️"
	case strings.Contains(raw, "❓"), strings.Contains(raw, "question"), strings.Contains(raw, "?"):
		return "❓"
	default:
		return ""
	}
}

func normalizeAssociatedMessageGUID(guid string) string {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return ""
	}
	if idx := strings.LastIndex(guid, "/"); idx >= 0 && idx < len(guid)-1 {
		guid = guid[idx+1:]
	}
	return strings.TrimSpace(guid)
}

func messageDedupeKeys(msg models.Message) []string {
	keys := make([]string, 0, 2)
	if guid := strings.TrimSpace(msg.GUID); guid != "" {
		keys = append(keys, "guid:"+guid)
	}
	if fingerprint := messageFingerprint(msg); fingerprint != "" {
		keys = append(keys, "fingerprint:"+fingerprint)
	}
	return keys
}

func messageFingerprint(msg models.Message) string {
	text := strings.TrimSpace(msg.Text)
	associatedGUID := strings.TrimSpace(msg.AssociatedMessageGUID)
	associatedType := strings.TrimSpace(msg.AssociatedMessageType)
	attachmentKeys := attachmentFingerprintKeys(msg.Attachments)

	if msg.DateCreated == 0 && text == "" && associatedGUID == "" && associatedType == "" && len(attachmentKeys) == 0 {
		return ""
	}

	handleAddress := ""
	if msg.Handle != nil {
		handleAddress = strings.TrimSpace(msg.Handle.Address)
	}

	parts := []string{
		strings.TrimSpace(msg.ChatGUID),
		strconv.FormatBool(msg.IsFromMe),
		strconv.FormatInt(msg.DateCreated, 10),
		handleAddress,
		text,
		associatedGUID,
		associatedType,
		strings.Join(attachmentKeys, "\x1e"),
	}
	return strings.Join(parts, "\x1f")
}

func attachmentFingerprintKeys(attachments []models.Attachment) []string {
	if len(attachments) == 0 {
		return nil
	}

	keys := make([]string, 0, len(attachments))
	for _, att := range attachments {
		key := attachmentFingerprintKey(att)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func attachmentFingerprintKey(att models.Attachment) string {
	parts := []string{
		strings.TrimSpace(att.GUID),
		strings.TrimSpace(att.MimeType),
		strings.TrimSpace(att.FileName),
		strings.TrimSpace(att.URL),
		strings.TrimSpace(att.Path),
		strings.TrimSpace(att.PathOnDisk),
	}
	nonEmpty := false
	for _, part := range parts {
		if part != "" {
			nonEmpty = true
			break
		}
	}
	if !nonEmpty {
		return ""
	}
	return strings.Join(parts, "\x1f")
}

func mergeDuplicateMessage(base, incoming models.Message) models.Message {
	merged := base
	if messageCompletenessScore(incoming) > messageCompletenessScore(base) {
		merged = incoming
	}

	if strings.TrimSpace(merged.GUID) == "" {
		merged.GUID = firstNonEmpty(base.GUID, incoming.GUID)
	}
	if strings.TrimSpace(merged.Text) == "" {
		merged.Text = firstNonEmpty(base.Text, incoming.Text)
	}
	if merged.DateCreated == 0 {
		if base.DateCreated != 0 {
			merged.DateCreated = base.DateCreated
		} else {
			merged.DateCreated = incoming.DateCreated
		}
	}
	if merged.Handle == nil {
		if base.Handle != nil {
			merged.Handle = base.Handle
		} else {
			merged.Handle = incoming.Handle
		}
	}
	if strings.TrimSpace(merged.ChatGUID) == "" {
		merged.ChatGUID = firstNonEmpty(base.ChatGUID, incoming.ChatGUID)
	}
	if strings.TrimSpace(merged.AssociatedMessageGUID) == "" {
		merged.AssociatedMessageGUID = firstNonEmpty(base.AssociatedMessageGUID, incoming.AssociatedMessageGUID)
	}
	if strings.TrimSpace(merged.AssociatedMessageType) == "" {
		merged.AssociatedMessageType = firstNonEmpty(base.AssociatedMessageType, incoming.AssociatedMessageType)
	}
	if merged.ReactionCounts == nil {
		if base.ReactionCounts != nil {
			merged.ReactionCounts = base.ReactionCounts
		} else {
			merged.ReactionCounts = incoming.ReactionCounts
		}
	}
	merged.Attachments = mergeAttachments(base.Attachments, incoming.Attachments)

	return merged
}

func messageCompletenessScore(msg models.Message) int {
	score := 0
	if strings.TrimSpace(msg.GUID) != "" {
		score += 4
	}
	if strings.TrimSpace(msg.Text) != "" {
		score += 2
	}
	if msg.DateCreated != 0 {
		score++
	}
	if msg.Handle != nil {
		score++
	}
	if strings.TrimSpace(msg.ChatGUID) != "" {
		score++
	}
	score += len(msg.Attachments)
	if strings.TrimSpace(msg.AssociatedMessageGUID) != "" {
		score++
	}
	if msg.ReactionCounts != nil {
		score++
	}
	return score
}

func mergeAttachments(a, b []models.Attachment) []models.Attachment {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	out := make([]models.Attachment, 0, len(a)+len(b))
	seen := make(map[string]struct{}, len(a)+len(b))
	add := func(att models.Attachment) {
		key := attachmentFingerprintKey(att)
		if key != "" {
			if _, exists := seen[key]; exists {
				return
			}
			seen[key] = struct{}{}
		}
		out = append(out, att)
	}
	for _, att := range a {
		add(att)
	}
	for _, att := range b {
		add(att)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
