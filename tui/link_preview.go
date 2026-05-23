package tui

import (
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

func supportedMediaLinksFromText(text string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	fields := strings.Fields(text)
	seen := make(map[string]struct{})
	links := make([]string, 0, limit)
	for _, field := range fields {
		raw := strings.Trim(field, "<>()[]{}\"'")
		raw = strings.TrimRight(raw, ".,;:!?")
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			continue
		}
		if !isSupportedMediaHost(u.Host) {
			continue
		}
		normalized := u.String()
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		links = append(links, normalized)
		if len(links) >= limit {
			break
		}
	}
	return links
}

func isSupportedMediaHost(host string) bool {
	host = normalizedPreviewHost(host)
	return host == "youtube.com" ||
		host == "m.youtube.com" ||
		host == "youtu.be" ||
		host == "spotify.com" ||
		host == "open.spotify.com" ||
		host == "instagram.com" ||
		host == "m.instagram.com" ||
		newsSiteNameForHost(host) != ""
}

func mediaSiteName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "Link"
	}
	host := normalizedPreviewHost(u.Host)
	switch host {
	case "youtube.com", "m.youtube.com", "youtu.be":
		return "YouTube"
	case "spotify.com", "open.spotify.com":
		return "Spotify"
	case "instagram.com", "m.instagram.com":
		return "Instagram"
	default:
		if site := newsSiteNameForHost(host); site != "" {
			return site
		}
		return "Link"
	}
}

func normalizedPreviewHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	return host
}

func newsSiteNameForHost(host string) string {
	switch normalizedPreviewHost(host) {
	case "aftonbladet.se":
		return "Aftonbladet"
	case "expressen.se":
		return "Expressen"
	case "dn.se":
		return "DN"
	case "svd.se":
		return "SvD"
	case "svt.se":
		return "SVT"
	case "omni.se":
		return "Omni"
	case "gp.se":
		return "GP"
	case "sydsvenskan.se":
		return "Sydsvenskan"
	case "di.se":
		return "Dagens industri"
	default:
		return ""
	}
}

func linkPreviewLabel(preview models.LinkPreview) string {
	title := strings.TrimSpace(preview.Title)
	author := strings.TrimSpace(preview.AuthorName)
	site := strings.TrimSpace(preview.SiteName)
	if site == "" {
		site = mediaSiteName(preview.URL)
	}
	if title == "" {
		if preview.Unavailable {
			title = "Preview unavailable"
		} else {
			title = "Loading preview..."
		}
	} else if strings.EqualFold(site, "Spotify") && author != "" && !strings.Contains(strings.ToLower(title), strings.ToLower(author)) {
		title = author + " - " + title
	}
	if title == "" {
		return ""
	}
	return linkPreviewBadge(site) + " " + truncatePreview(title, 80)
}

func linkPreviewBadge(site string) string {
	label := "[" + site + "]"
	style := lipgloss.NewStyle()
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "spotify":
		style = style.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("46"))
	case "youtube":
		style = style.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("196"))
	case "instagram":
		style = style.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("201"))
	default:
		style = style.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("240"))
	}
	return style.Render(label)
}

func stripSupportedMediaLinks(text string, links []string) string {
	if strings.TrimSpace(text) == "" || len(links) == 0 {
		return strings.TrimSpace(text)
	}
	linkSet := make(map[string]struct{}, len(links))
	for _, link := range links {
		linkSet[link] = struct{}{}
	}

	var kept []string
	for _, field := range strings.Fields(text) {
		raw := strings.Trim(field, "<>()[]{}\"'")
		raw = strings.TrimRight(raw, ".,;:!?")
		u, err := url.Parse(raw)
		if err == nil && u.Scheme != "" && u.Host != "" {
			if _, ok := linkSet[u.String()]; ok {
				continue
			}
		}
		kept = append(kept, field)
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func linkPreviewsForMessage(msg models.Message, limit int) []models.LinkPreview {
	links := supportedMediaLinksFromText(msg.Text, limit)
	if len(links) == 0 {
		return nil
	}

	byURL := make(map[string]models.LinkPreview, len(msg.LinkPreviews))
	for _, preview := range msg.LinkPreviews {
		if strings.TrimSpace(preview.URL) == "" {
			continue
		}
		byURL[preview.URL] = preview
	}

	out := make([]models.LinkPreview, 0, len(links))
	for _, link := range links {
		if preview, ok := byURL[link]; ok {
			out = append(out, preview)
			continue
		}
		out = append(out, models.LinkPreview{URL: link, SiteName: mediaSiteName(link)})
	}
	return out
}

func messageHasPreviewAttempt(msg models.Message, rawURL string) bool {
	for _, preview := range msg.LinkPreviews {
		if preview.URL != rawURL {
			continue
		}
		if preview.Unavailable {
			return true
		}
		if strings.EqualFold(mediaSiteName(rawURL), "Spotify") {
			return strings.TrimSpace(preview.Title) != "" && strings.TrimSpace(preview.AuthorName) != ""
		}
		return previewHasResolvedTitle(preview)
	}
	return false
}

func previewHasResolvedTitle(preview models.LinkPreview) bool {
	title := strings.ToLower(strings.TrimSpace(preview.Title))
	if title == "" {
		return false
	}
	if title == "search" && newsSiteNameForURL(preview.URL) != "" {
		return false
	}
	return true
}

func newsSiteNameForURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return newsSiteNameForHost(u.Host)
}

func upsertLinkPreview(previews []models.LinkPreview, next models.LinkPreview) []models.LinkPreview {
	for i, preview := range previews {
		if preview.URL == next.URL {
			previews[i] = next
			return previews
		}
	}
	return append(previews, next)
}
