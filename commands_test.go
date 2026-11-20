package main

import (
	"bufio"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/oovets/imessage-tui/config"
	"github.com/zalando/go-keyring"
)

// answers is a terminal that reads its answers from lines, the way a pipe
// does.
func answers(lines ...string) *terminal {
	return &terminal{
		in:  bufio.NewReader(strings.NewReader(strings.Join(lines, "\n") + "\n")),
		out: io.Discard,
	}
}

func TestIRCAddOffersTheLastNetworksServerAndNick(t *testing.T) {
	keyring.MockInit()

	steps := []struct {
		name    string
		answers []string
		want    config.IRCNetwork
	}{
		{
			name:    "the first network asks for everything",
			answers: []string{"Libera", "bnc.example.com:6697", "", "stefan", "", "bnc@tui:one", ""},
			want: config.IRCNetwork{
				ID: "libera", Name: "Libera", Server: "bnc.example.com:6697", TLS: true,
				Nick: "stefan", Password: "bnc@tui:one",
			},
		},
		{
			name:    "the next takes server, tls and nick from it",
			answers: []string{"OFTC", "", "", "", "", "bnc@tui:two", ""},
			want: config.IRCNetwork{
				ID: "oftc", Name: "OFTC", Server: "bnc.example.com:6697", TLS: true,
				Nick: "stefan", Password: "bnc@tui:two",
			},
		},
		{
			name:    "an answer still overrides the suggestion",
			answers: []string{"Home", "localhost:6667", "n", "other", "", "", "#a, #b"},
			want: config.IRCNetwork{
				ID: "home", Name: "Home", Server: "localhost:6667",
				Nick: "other", Channels: []string{"#a", "#b"},
			},
		},
		{
			name:    "after a plain-text network, tls stays off unless asked for",
			answers: []string{"Home Two", "", "", "", "", "", ""},
			want: config.IRCNetwork{
				ID: "home-two", Name: "Home Two", Server: "localhost:6667", Nick: "other",
			},
		},
	}
	for i, step := range steps {
		if err := runIRC("add", nil, answers(step.answers...)); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		stored, err := config.LoadStoredIRCNetworks()
		if err != nil {
			t.Fatal(err)
		}
		if len(stored) != i+1 {
			t.Fatalf("%s: %d networks stored, want %d", step.name, len(stored), i+1)
		}
		if got := stored[i]; !reflect.DeepEqual(got, step.want) {
			t.Fatalf("%s:\n got %+v\nwant %+v", step.name, got, step.want)
		}
	}
}
