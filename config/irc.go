package config

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// IRCNetwork is one configured IRC network.
//
// Like the Slack workspaces and Telegram accounts, every network lives in one
// OS keyring entry, never the config file: Password is a secret, and keeping
// the rest beside it means one entry, one keyring prompt.
type IRCNetwork struct {
	// ID is the slug in every chat GUID. It is derived from the name once,
	// when the network is added, and stored, so renaming nothing ever moves a
	// chat.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Server is host:port.
	Server string `json:"server"`
	TLS    bool   `json:"tls"`
	Nick   string `json:"nick"`
	// Login is the SASL account. With it, Password is the SASL password;
	// without it, Password is sent as the server password, which is how a
	// bouncer like soju is often logged into ("user/network:password").
	Login    string   `json:"login,omitempty"`
	Password string   `json:"password,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

const ircKeyringUser = "irc.networks"

// ircMu serialises read-modify-write of the network list.
var ircMu sync.Mutex

// LoadIRCNetworks returns the configured networks. A network set in the
// environment (BB_IRC_SERVER and BB_IRC_NICK) is used on its own, the way
// BB_SLACK_TOKEN is: the environment is for trying something out, and mixing
// it with the stored list would make two networks of one.
func LoadIRCNetworks() ([]IRCNetwork, error) {
	if network, ok := ircNetworkFromEnv(); ok {
		return []IRCNetwork{network}, nil
	}
	ircMu.Lock()
	defer ircMu.Unlock()
	return loadIRCNetworks()
}

// LoadStoredIRCNetworks returns the networks in the keyring, ignoring the
// environment — what the irc subcommands edit.
func LoadStoredIRCNetworks() ([]IRCNetwork, error) {
	ircMu.Lock()
	defer ircMu.Unlock()
	return loadIRCNetworks()
}

func loadIRCNetworks() ([]IRCNetwork, error) {
	blob, err := keyring.Get(keyringService, ircKeyringUser)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var networks []IRCNetwork
	if err := json.Unmarshal([]byte(blob), &networks); err != nil {
		return nil, err
	}
	out := networks[:0]
	for _, network := range networks {
		network.ID = strings.TrimSpace(network.ID)
		if network.ID == "" || strings.TrimSpace(network.Server) == "" || strings.TrimSpace(network.Nick) == "" {
			continue
		}
		out = append(out, network)
	}
	return out, nil
}

func saveIRCNetworks(networks []IRCNetwork) error {
	if len(networks) == 0 {
		err := keyring.Delete(keyringService, ircKeyringUser)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	blob, err := json.Marshal(networks)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, ircKeyringUser, string(blob))
}

// AddIRCNetwork stores a new network and returns it with its id. The id is
// the name's slug, numbered when another network already has it.
func AddIRCNetwork(network IRCNetwork) (IRCNetwork, error) {
	ircMu.Lock()
	defer ircMu.Unlock()
	networks, err := loadIRCNetworks()
	if err != nil {
		return IRCNetwork{}, err
	}
	taken := make(map[string]struct{}, len(networks))
	for _, existing := range networks {
		taken[existing.ID] = struct{}{}
	}
	base := IRCNetworkID(network.Name)
	if base == "" {
		base = "irc"
	}
	network.ID = base
	for i := 2; ; i++ {
		if _, clash := taken[network.ID]; !clash {
			break
		}
		network.ID = base + "-" + strconv.Itoa(i)
	}
	return network, saveIRCNetworks(append(networks, network))
}

// RemoveIRCNetwork forgets a network. Removing one that is not there is not an
// error.
func RemoveIRCNetwork(id string) error {
	ircMu.Lock()
	defer ircMu.Unlock()
	networks, err := loadIRCNetworks()
	if err != nil {
		return err
	}
	kept := networks[:0]
	for _, network := range networks {
		if network.ID != id {
			kept = append(kept, network)
		}
	}
	return saveIRCNetworks(kept)
}

// IRCNetworkID is the slug a network's name becomes in GUIDs: lower case
// letters, digits and dashes, which never contain the GUID separator.
func IRCNetworkID(name string) string {
	return SlackWorkspaceID(name)
}

// IRCServerAddress completes a server address: a bare host gets the usual
// port, 6697 with TLS and 6667 without.
func IRCServerAddress(server string, tls bool) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	if tls {
		return net.JoinHostPort(server, "6697")
	}
	return net.JoinHostPort(server, "6667")
}

// ircNetworkFromEnv reads one network from BB_IRC_* variables. TLS is on
// unless BB_IRC_TLS says otherwise: a password should not cross the network in
// the clear by default.
func ircNetworkFromEnv() (IRCNetwork, bool) {
	server := strings.TrimSpace(os.Getenv("BB_IRC_SERVER"))
	nick := strings.TrimSpace(os.Getenv("BB_IRC_NICK"))
	if server == "" || nick == "" {
		return IRCNetwork{}, false
	}
	useTLS := true
	if value := strings.TrimSpace(os.Getenv("BB_IRC_TLS")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			useTLS = parsed
		}
	}
	name := strings.TrimSpace(os.Getenv("BB_IRC_NETWORK"))
	if name == "" {
		name = server
		if host, _, err := net.SplitHostPort(server); err == nil {
			name = host
		}
	}
	id := IRCNetworkID(name)
	if id == "" {
		id = "irc"
	}
	var channels []string
	for _, channel := range strings.Split(os.Getenv("BB_IRC_CHANNELS"), ",") {
		if channel = strings.TrimSpace(channel); channel != "" {
			channels = append(channels, channel)
		}
	}
	return IRCNetwork{
		ID:       id,
		Name:     name,
		Server:   IRCServerAddress(server, useTLS),
		TLS:      useTLS,
		Nick:     nick,
		Login:    strings.TrimSpace(os.Getenv("BB_IRC_LOGIN")),
		Password: os.Getenv("BB_IRC_PASSWORD"),
		Channels: channels,
	}, true
}
