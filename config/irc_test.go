package config

import (
	"reflect"
	"testing"
)

func TestIRCServerAddress(t *testing.T) {
	tests := []struct {
		server string
		tls    bool
		want   string
	}{
		{"irc.libera.chat", true, "irc.libera.chat:6697"},
		{"irc.libera.chat", false, "irc.libera.chat:6667"},
		{"bouncer.example:7000", true, "bouncer.example:7000"},
		{" ", true, ""},
	}
	for _, tt := range tests {
		if got := IRCServerAddress(tt.server, tt.tls); got != tt.want {
			t.Errorf("IRCServerAddress(%q, %v) = %q, want %q", tt.server, tt.tls, got, tt.want)
		}
	}
}

func TestIRCNetworkFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want IRCNetwork
		ok   bool
	}{
		{
			name: "nothing set",
			env:  map[string]string{},
		},
		{
			name: "a server needs a nick",
			env:  map[string]string{"BB_IRC_SERVER": "irc.libera.chat"},
		},
		{
			name: "tls by default, named after the host",
			env: map[string]string{
				"BB_IRC_SERVER":   "irc.libera.chat",
				"BB_IRC_NICK":     "stefan",
				"BB_IRC_CHANNELS": "#go, #emacs,,",
			},
			want: IRCNetwork{
				ID: "ircliberachat", Name: "irc.libera.chat", Server: "irc.libera.chat:6697",
				TLS: true, Nick: "stefan", Channels: []string{"#go", "#emacs"},
			},
			ok: true,
		},
		{
			name: "a bouncer without tls, with a name and a password",
			env: map[string]string{
				"BB_IRC_SERVER":   "localhost:6667",
				"BB_IRC_NICK":     "stefan",
				"BB_IRC_TLS":      "false",
				"BB_IRC_NETWORK":  "Home Bouncer",
				"BB_IRC_PASSWORD": "stefan/libera:secret",
			},
			want: IRCNetwork{
				ID: "home-bouncer", Name: "Home Bouncer", Server: "localhost:6667",
				Nick: "stefan", Password: "stefan/libera:secret",
			},
			ok: true,
		},
	}
	vars := []string{"BB_IRC_SERVER", "BB_IRC_NICK", "BB_IRC_TLS", "BB_IRC_NETWORK", "BB_IRC_LOGIN", "BB_IRC_PASSWORD", "BB_IRC_CHANNELS"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range vars {
				t.Setenv(name, tt.env[name])
			}
			got, ok := ircNetworkFromEnv()
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ircNetworkFromEnv() = %+v, %v\nwant %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
