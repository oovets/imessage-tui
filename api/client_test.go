package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestSendMessageWithTempGUIDUsesProvidedTempGUID(t *testing.T) {
	var payload map[string]interface{}
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/message/text" {
			t.Fatalf("path = %s, want /api/v1/message/text", r.URL.Path)
		}
		if got, want := r.URL.Query().Get("guid"), "secret"; got != want {
			t.Fatalf("guid query = %s, want %s", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    r,
		}, nil
	})}

	if err := client.SendMessageWithTempGUID("chat-a", "hello", "", "pending-guid"); err != nil {
		t.Fatalf("SendMessageWithTempGUID returned error: %v", err)
	}

	if got, want := payload["tempGuid"], "pending-guid"; got != want {
		t.Fatalf("tempGuid = %v, want %s", got, want)
	}
}

func TestGetLinkPreviewUsesSpotifyOEmbedEndpoint(t *testing.T) {
	var requestedURLs []string
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, r.URL.String())
		if strings.HasPrefix(r.URL.String(), "https://open.spotify.com/oembed?") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"provider_name": "Spotify",
					"title": "Example Song"
				}`)),
				Request: r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`<html><head>
				<meta name="music:musician_description" content="Example Artist">
			</head></html>`)),
			Request: r,
		}, nil
	})}

	preview, err := client.GetLinkPreview("https://open.spotify.com/track/example")
	if err != nil {
		t.Fatalf("GetLinkPreview returned error: %v", err)
	}
	if len(requestedURLs) != 2 {
		t.Fatalf("request count = %d, want 2 (%v)", len(requestedURLs), requestedURLs)
	}
	if !strings.HasPrefix(requestedURLs[0], "https://open.spotify.com/oembed?") {
		t.Fatalf("first requested URL = %q, want Spotify oEmbed endpoint", requestedURLs[0])
	}
	if got, want := preview.Title, "Example Song"; got != want {
		t.Fatalf("preview title = %q, want %q", got, want)
	}
	if got, want := preview.AuthorName, "Example Artist"; got != want {
		t.Fatalf("preview author = %q, want %q", got, want)
	}
	if got, want := preview.SiteName, "Spotify"; got != want {
		t.Fatalf("preview site = %q, want %q", got, want)
	}
}

func TestGetLinkPreviewUsesInstagramHTMLMetadataFallback(t *testing.T) {
	var requestedURLs []string
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, r.URL.String())
		if strings.HasPrefix(r.URL.String(), "https://noembed.com/embed?") {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`<html><head>
				<meta property="og:title" content="Creator on Instagram: &quot;A short video&quot;">
				<meta property="og:site_name" content="Instagram">
			</head></html>`)),
			Request: r,
		}, nil
	})}

	preview, err := client.GetLinkPreview("https://www.instagram.com/reel/C1abcDEFghi/")
	if err != nil {
		t.Fatalf("GetLinkPreview returned error: %v", err)
	}
	if len(requestedURLs) != 2 {
		t.Fatalf("request count = %d, want 2 (%v)", len(requestedURLs), requestedURLs)
	}
	if got, want := preview.Title, `Creator on Instagram: "A short video"`; got != want {
		t.Fatalf("preview title = %q, want %q", got, want)
	}
	if got, want := preview.SiteName, "Instagram"; got != want {
		t.Fatalf("preview site = %q, want %q", got, want)
	}
}

func TestGetLinkPreviewUsesNewsHTMLMetadataFallback(t *testing.T) {
	var requestedURLs []string
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, r.URL.String())
		if strings.HasPrefix(r.URL.String(), "https://noembed.com/embed?") {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`<html><head>
				<meta property="og:title" content="Nyhetstitel från Aftonbladet">
				<meta property="og:site_name" content="Aftonbladet">
			</head></html>`)),
			Request: r,
		}, nil
	})}

	preview, err := client.GetLinkPreview("https://www.aftonbladet.se/nyheter/a/example")
	if err != nil {
		t.Fatalf("GetLinkPreview returned error: %v", err)
	}
	if len(requestedURLs) != 1 {
		t.Fatalf("request count = %d, want 1 (%v)", len(requestedURLs), requestedURLs)
	}
	if got, want := preview.Title, "Nyhetstitel från Aftonbladet"; got != want {
		t.Fatalf("preview title = %q, want %q", got, want)
	}
	if got, want := preview.SiteName, "Aftonbladet"; got != want {
		t.Fatalf("preview site = %q, want %q", got, want)
	}
}

func TestGetLinkPreviewHandlesMetaAttributesBeforeProperty(t *testing.T) {
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.String(), "https://noembed.com/embed?") {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`<html><head>
				<meta data-rh="true" property="og:title" content="Uppgifter: Han skulle vräkas – tog fram pistolen och öppnade eld">
				<meta data-rh="true" property="og:site_name" content="Aftonbladet">
				<title>search</title>
			</head></html>`)),
			Request: r,
		}, nil
	})}

	preview, err := client.GetLinkPreview("https://www.aftonbladet.se/nyheter/a/example")
	if err != nil {
		t.Fatalf("GetLinkPreview returned error: %v", err)
	}
	if got, want := preview.Title, "Uppgifter: Han skulle vräkas – tog fram pistolen och öppnade eld"; got != want {
		t.Fatalf("preview title = %q, want %q", got, want)
	}
}

func TestGetLinkPreviewPrefersNewsHTMLOverGenericOEmbedSearchTitle(t *testing.T) {
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.String(), "https://noembed.com/embed?") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"provider_name": "Aftonbladet",
					"title": "search"
				}`)),
				Request: r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`<html><head>
				<meta data-rh="true" property="og:title" content="Uppgifter: Han skulle vräkas – tog fram pistolen och öppnade eld">
				<meta data-rh="true" property="og:site_name" content="Aftonbladet">
				<title>search</title>
			</head></html>`)),
			Request: r,
		}, nil
	})}

	preview, err := client.GetLinkPreview("https://www.aftonbladet.se/nyheter/a/example")
	if err != nil {
		t.Fatalf("GetLinkPreview returned error: %v", err)
	}
	if got, want := preview.Title, "Uppgifter: Han skulle vräkas – tog fram pistolen och öppnade eld"; got != want {
		t.Fatalf("preview title = %q, want %q", got, want)
	}
}

func TestDeleteChatUsesPrivateAPIEndpoint(t *testing.T) {
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.Method, http.MethodDelete; got != want {
			t.Fatalf("method = %s, want %s", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/chat/chat-a/delete"; got != want {
			t.Fatalf("path = %s, want %s", got, want)
		}
		if got, want := r.URL.Query().Get("guid"), "secret"; got != want {
			t.Fatalf("guid query = %s, want %s", got, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    r,
		}, nil
	})}

	if err := client.DeleteChat("chat-a"); err != nil {
		t.Fatalf("DeleteChat returned error: %v", err)
	}
}

func TestRenameChatUsesPrivateAPIEndpoint(t *testing.T) {
	var payload map[string]string
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.Method, http.MethodPut; got != want {
			t.Fatalf("method = %s, want %s", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/chat/chat-a"; got != want {
			t.Fatalf("path = %s, want %s", got, want)
		}
		if got, want := r.URL.Query().Get("guid"), "secret"; got != want {
			t.Fatalf("guid query = %s, want %s", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    r,
		}, nil
	})}

	if err := client.RenameChat("chat-a", "Family"); err != nil {
		t.Fatalf("RenameChat returned error: %v", err)
	}
	if got, want := payload["displayName"], "Family"; got != want {
		t.Fatalf("displayName = %q, want %q", got, want)
	}
}

func TestDeleteChatReturnsAPIError(t *testing.T) {
	client := NewClient("http://bluebubbles.test", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"message":"private api disabled"}`)),
			Request:    r,
		}, nil
	})}

	if err := client.DeleteChat("chat-a"); err == nil || !strings.Contains(err.Error(), "private api disabled") {
		t.Fatalf("DeleteChat error = %v, want API body", err)
	}
}
