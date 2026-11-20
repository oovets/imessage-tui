package tui

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/oovets/imessage-tui/config"
	"github.com/oovets/imessage-tui/models"
)

// Filters and ignores, after WeeChat's /filter and /ignore.
//
// A filter hides the lines of a buffer that carry matching tags and match a
// regex; filtering can be turned off everywhere (alt+=) or in one buffer
// (alt+-) to see them again. An ignore hides a nick's lines for good, whatever
// the toggles say. Neither deletes anything: the messages stay in the cache
// and the pane, and come back the moment the filter does not apply.
//
// Buffers are named the way WeeChat names them — irc.libera.#go — by the
// backend that owns the chat (provider.BufferNamer). A chat whose backend has
// no such name is never filtered: a filter written for IRC must not reach
// into iMessage because someone typed * as its buffer.

// messageFilters is the compiled state every pane reads.
type messageFilters struct {
	state   config.FiltersState
	filters []compiledFilter // parallel to state.Filters
	ignores []compiledIgnore // parallel to state.Ignores

	// off turns filtering off everywhere; offIn in one buffer. Neither is
	// saved: a restart starts filtered, as WeeChat does.
	off   bool
	offIn map[string]bool

	bufferOf func(chatGUID string) string
}

func newMessageFilters(state config.FiltersState, bufferOf func(string) string) *messageFilters {
	f := &messageFilters{offIn: make(map[string]bool), bufferOf: bufferOf}
	for _, filter := range state.Filters {
		compiled, err := compileFilter(filter)
		if err != nil {
			// A filter that no longer compiles is kept out rather than
			// failing the start; the log says which.
			log.Printf("filter %q skipped: %v", filter.Name, err)
			continue
		}
		f.state.Filters = append(f.state.Filters, filter)
		f.filters = append(f.filters, compiled)
	}
	for _, ignore := range state.Ignores {
		compiled, err := compileIgnore(ignore)
		if err != nil {
			log.Printf("ignore %q skipped: %v", ignore.Mask, err)
			continue
		}
		f.state.Ignores = append(f.state.Ignores, ignore)
		f.ignores = append(f.ignores, compiled)
	}
	return f
}

// hidden reports whether msg is filtered or ignored out of view.
func (f *messageFilters) hidden(msg models.Message) bool {
	if f == nil || f.bufferOf == nil || msg.ChatGUID == "" {
		return false
	}
	if len(f.filters) == 0 && len(f.ignores) == 0 {
		return false
	}
	buffer := f.bufferOf(msg.ChatGUID)
	if buffer == "" {
		return false
	}
	nick := ""
	if msg.Handle != nil {
		nick = msg.Handle.DisplayName
	}
	if !msg.IsFromMe && nick != "" {
		for i := range f.ignores {
			if f.ignores[i].matches(buffer, nick) {
				return true
			}
		}
	}
	if f.off || f.offIn[buffer] {
		return false
	}
	tags := lineTags(msg, nick)
	text := displayText(msg)
	for i := range f.filters {
		if !f.state.Filters[i].Disabled && f.filters[i].matches(buffer, tags, nick, text) {
			return true
		}
	}
	return false
}

// lineTags are the WeeChat tags a message carries, as far as a message here
// can tell: who wrote it, and whether it was the user.
func lineTags(msg models.Message, nick string) []string {
	var tags []string
	if nick != "" {
		tags = append(tags, "nick_"+nick)
	}
	if msg.IsFromMe {
		tags = append(tags, "self_msg")
	}
	return tags
}

// compiledFilter is a Filter ready to match.
type compiledFilter struct {
	buffers maskList
	// tags is an OR of AND groups; nil matches every line.
	tags [][]tagTerm
	// prefix and message are matched against the nick and the text; nil
	// matches anything.
	prefix, message *regexp.Regexp
	negate          bool
}

type tagTerm struct {
	negate  bool
	pattern *regexp.Regexp
}

func compileFilter(filter config.Filter) (compiledFilter, error) {
	var c compiledFilter
	if strings.TrimSpace(filter.Name) == "" {
		return c, errors.New("a filter needs a name")
	}
	if len(filter.Buffers) == 0 {
		return c, errors.New("a filter needs a buffer, or * for all")
	}
	c.buffers = compileMaskList(filter.Buffers)

	if tags := strings.TrimSpace(filter.Tags); tags != "*" && tags != "" {
		for _, group := range strings.Split(tags, ",") {
			var terms []tagTerm
			for _, term := range strings.Split(group, "+") {
				term = strings.TrimSpace(term)
				negate := strings.HasPrefix(term, "!")
				term = strings.TrimPrefix(term, "!")
				if term == "" {
					return c, fmt.Errorf("empty tag in %q", filter.Tags)
				}
				terms = append(terms, tagTerm{negate: negate, pattern: globRegexp(term)})
			}
			c.tags = append(c.tags, terms)
		}
	}

	regex := filter.Regex
	if regex == "" || regex == "*" {
		return c, nil
	}
	switch {
	case strings.HasPrefix(regex, `\!`):
		regex = regex[1:]
	case strings.HasPrefix(regex, "!"):
		c.negate = true
		regex = regex[1:]
	}
	flags := "(?i)"
	if rest, ok := strings.CutPrefix(regex, "(?-i)"); ok {
		flags, regex = "", rest
	}
	prefixPart, messagePart, split := strings.Cut(regex, `\t`)
	if !split {
		prefixPart, messagePart = "", regex
	}
	var err error
	if prefixPart != "" {
		if c.prefix, err = regexp.Compile(flags + prefixPart); err != nil {
			return c, fmt.Errorf("bad regex: %w", err)
		}
	}
	if messagePart != "" {
		if c.message, err = regexp.Compile(flags + messagePart); err != nil {
			return c, fmt.Errorf("bad regex: %w", err)
		}
	}
	return c, nil
}

func (c *compiledFilter) matches(buffer string, tags []string, nick, text string) bool {
	if !c.buffers.matches(buffer) {
		return false
	}
	if c.tags != nil && !matchTags(c.tags, tags) {
		return false
	}
	ok := (c.prefix == nil || c.prefix.MatchString(nick)) &&
		(c.message == nil || c.message.MatchString(text))
	if c.negate {
		return !ok
	}
	return ok
}

// matchTags reports whether a line's tags satisfy any one AND group.
func matchTags(groups [][]tagTerm, tags []string) bool {
	for _, group := range groups {
		all := true
		for _, term := range group {
			present := false
			for _, tag := range tags {
				if term.pattern.MatchString(tag) {
					present = true
					break
				}
			}
			if present == term.negate {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// maskList is a WeeChat buffer list: names or masks with *, and exclusions
// with !. A buffer matches when some mask takes it and no exclusion does.
type maskList []maskTerm

type maskTerm struct {
	negate  bool
	pattern *regexp.Regexp
}

func compileMaskList(masks []string) maskList {
	list := make(maskList, 0, len(masks))
	for _, mask := range masks {
		mask = strings.TrimSpace(mask)
		if mask == "" {
			continue
		}
		negate := strings.HasPrefix(mask, "!")
		list = append(list, maskTerm{negate: negate, pattern: globRegexp(strings.TrimPrefix(mask, "!"))})
	}
	return list
}

func (l maskList) matches(buffer string) bool {
	matched := false
	for _, term := range l {
		if term.pattern.MatchString(buffer) {
			if term.negate {
				return false
			}
			matched = true
		}
	}
	return matched
}

// globRegexp turns a mask where * is any run of characters into a
// case-insensitive regexp over the whole string.
func globRegexp(glob string) *regexp.Regexp {
	quoted := strings.ReplaceAll(regexp.QuoteMeta(glob), `\*`, ".*")
	return regexp.MustCompile("(?i)^" + quoted + "$")
}

// compiledIgnore is an Ignore ready to match.
type compiledIgnore struct {
	nick            *regexp.Regexp
	server, channel string
}

func compileIgnore(ignore config.Ignore) (compiledIgnore, error) {
	c := compiledIgnore{
		server:  strings.ToLower(strings.TrimSpace(ignore.Server)),
		channel: strings.ToLower(strings.TrimSpace(ignore.Channel)),
	}
	mask := strings.TrimSpace(ignore.Mask)
	if mask == "" {
		return c, errors.New("an ignore needs a nick")
	}
	if strings.ContainsAny(mask, "!@") {
		// WeeChat matches nick!user@host. Messages here carry the nick
		// alone, so a host mask would never match: say so, rather than
		// store an ignore that silently does nothing.
		return c, errors.New("only nicks can be ignored here, not user@host masks")
	}
	if pattern, ok := strings.CutPrefix(mask, "re:"); ok {
		flags := "(?i)"
		if rest, ok := strings.CutPrefix(pattern, "(?-i)"); ok {
			flags, pattern = "", rest
		}
		compiled, err := regexp.Compile(flags + pattern)
		if err != nil {
			return c, fmt.Errorf("bad regex: %w", err)
		}
		c.nick = compiled
		return c, nil
	}
	c.nick = globRegexp(mask)
	return c, nil
}

// matches reports whether nick in buffer is ignored. Only IRC buffers have a
// server and a channel to narrow by; an ignore reaches nothing else.
func (c *compiledIgnore) matches(buffer, nick string) bool {
	plugin, rest, ok := strings.Cut(buffer, ".")
	if !ok || plugin != "irc" {
		return false
	}
	server, channel, _ := strings.Cut(rest, ".")
	if c.server != "" && strings.ToLower(server) != c.server {
		return false
	}
	if c.channel != "" && strings.ToLower(channel) != c.channel {
		return false
	}
	return c.nick.MatchString(nick)
}

// --- commands ---

const (
	filterUsage = "usage: /filter list | add|addreplace <name> <buffer>[,<buffer>...] <tags> <regex> | del <name|mask>... | enable|disable|toggle [<name|mask>|@] | rename <name> <new>"
	ignoreUsage = "usage: /ignore list | add|addreplace [re:]<nick> [<server> [<channel>]] | del <number>|-all"
)

// filterResult is what a /filter or /ignore command did: a line for the
// status bar, or a listing to show, and whether the panes must be redrawn.
type filterResult struct {
	toast   string
	listing bool
	changed bool
}

// runFilterCommand carries out /filter. buffer is the focused pane's buffer
// name, for @; empty when its chat has none.
func (f *messageFilters) runFilterCommand(args, buffer string) (filterResult, error) {
	action, rest := nextField(args)
	switch action {
	case "", "list":
		return filterResult{listing: true}, nil

	case "enable", "disable", "toggle":
		target := strings.TrimSpace(rest)
		switch target {
		case "":
			f.off = toggled(f.off, action)
			if f.off {
				return filterResult{toast: "Filtering off", changed: true}, nil
			}
			return filterResult{toast: "Filtering on", changed: true}, nil
		case "@":
			if buffer == "" {
				return filterResult{}, errors.New("this chat has no buffer name; filters reach IRC chats only")
			}
			f.offIn[buffer] = toggled(f.offIn[buffer], action)
			if !f.offIn[buffer] {
				delete(f.offIn, buffer)
				return filterResult{toast: "Filtering on in " + buffer, changed: true}, nil
			}
			return filterResult{toast: "Filtering off in " + buffer, changed: true}, nil
		}
		names := matchingNames(f.state.Filters, strings.Fields(target))
		if len(names) == 0 {
			return filterResult{}, fmt.Errorf("no filter %s", target)
		}
		for i := range f.state.Filters {
			if names[f.state.Filters[i].Name] {
				f.state.Filters[i].Disabled = toggled(f.state.Filters[i].Disabled, action)
			}
		}
		return filterResult{toast: fmt.Sprintf("%d filter(s) %sd", len(names), action), changed: true}, nil

	case "add", "addreplace":
		name, rest := nextField(rest)
		buffers, rest := nextField(rest)
		tags, rest := nextField(rest)
		regex := strings.TrimSpace(rest)
		if name == "" || buffers == "" || tags == "" || regex == "" {
			return filterResult{}, errors.New(filterUsage)
		}
		filter := config.Filter{Name: name, Buffers: strings.Split(buffers, ","), Tags: tags, Regex: regex}
		compiled, err := compileFilter(filter)
		if err != nil {
			return filterResult{}, fmt.Errorf("filter %s: %w", name, err)
		}
		for i := range f.state.Filters {
			if f.state.Filters[i].Name != name {
				continue
			}
			if action == "add" {
				return filterResult{}, fmt.Errorf("filter %s exists; use /filter addreplace", name)
			}
			f.state.Filters[i] = filter
			f.filters[i] = compiled
			return filterResult{toast: "Filter " + name + " replaced", changed: true}, nil
		}
		f.state.Filters = append(f.state.Filters, filter)
		f.filters = append(f.filters, compiled)
		return filterResult{toast: "Filter " + name + " added", changed: true}, nil

	case "del":
		names := matchingNames(f.state.Filters, strings.Fields(rest))
		if len(names) == 0 {
			return filterResult{}, fmt.Errorf("no filter %s", strings.TrimSpace(rest))
		}
		keptState := f.state.Filters[:0]
		keptCompiled := f.filters[:0]
		for i := range f.state.Filters {
			if !names[f.state.Filters[i].Name] {
				keptState = append(keptState, f.state.Filters[i])
				keptCompiled = append(keptCompiled, f.filters[i])
			}
		}
		f.state.Filters, f.filters = keptState, keptCompiled
		return filterResult{toast: fmt.Sprintf("%d filter(s) deleted", len(names)), changed: true}, nil

	case "rename":
		name, rest := nextField(rest)
		newName, _ := nextField(rest)
		if name == "" || newName == "" {
			return filterResult{}, errors.New(filterUsage)
		}
		for i := range f.state.Filters {
			if f.state.Filters[i].Name == newName {
				return filterResult{}, fmt.Errorf("filter %s exists", newName)
			}
		}
		for i := range f.state.Filters {
			if f.state.Filters[i].Name == name {
				f.state.Filters[i].Name = newName
				return filterResult{toast: "Filter " + name + " renamed to " + newName, changed: true}, nil
			}
		}
		return filterResult{}, fmt.Errorf("no filter %s", name)
	}
	return filterResult{}, errors.New(filterUsage)
}

// runIgnoreCommand carries out /ignore.
func (f *messageFilters) runIgnoreCommand(args string) (filterResult, error) {
	action, rest := nextField(args)
	switch action {
	case "", "list":
		return filterResult{listing: true}, nil

	case "add", "addreplace":
		fields := strings.Fields(rest)
		if len(fields) < 1 || len(fields) > 3 {
			return filterResult{}, errors.New(ignoreUsage)
		}
		ignore := config.Ignore{Mask: fields[0]}
		if len(fields) > 1 {
			ignore.Server = fields[1]
		}
		if len(fields) > 2 {
			ignore.Channel = fields[2]
		}
		compiled, err := compileIgnore(ignore)
		if err != nil {
			return filterResult{}, fmt.Errorf("ignore %s: %w", ignore.Mask, err)
		}
		for i, existing := range f.state.Ignores {
			if !sameIgnore(existing, ignore) {
				continue
			}
			if action == "add" {
				return filterResult{}, fmt.Errorf("%s is already ignored there", ignore.Mask)
			}
			f.state.Ignores[i] = ignore
			f.ignores[i] = compiled
			return filterResult{toast: "Ignoring " + describeIgnore(ignore), changed: true}, nil
		}
		f.state.Ignores = append(f.state.Ignores, ignore)
		f.ignores = append(f.ignores, compiled)
		return filterResult{toast: "Ignoring " + describeIgnore(ignore), changed: true}, nil

	case "del":
		arg := strings.TrimSpace(rest)
		if arg == "-all" {
			n := len(f.state.Ignores)
			f.state.Ignores, f.ignores = nil, nil
			return filterResult{toast: fmt.Sprintf("%d ignore(s) deleted", n), changed: true}, nil
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > len(f.state.Ignores) {
			return filterResult{}, fmt.Errorf("no ignore %s; /ignore list numbers them", arg)
		}
		removed := f.state.Ignores[n-1]
		f.state.Ignores = append(f.state.Ignores[:n-1], f.state.Ignores[n:]...)
		f.ignores = append(f.ignores[:n-1], f.ignores[n:]...)
		return filterResult{toast: "No longer ignoring " + describeIgnore(removed), changed: true}, nil
	}
	return filterResult{}, errors.New(ignoreUsage)
}

// listing is what /filter list and /ignore list show.
func (f *messageFilters) listing() []string {
	var lines []string
	state := "on"
	if f.off {
		state = "off (alt+= turns it on)"
	}
	lines = append(lines, "Filtering is "+state)
	for buffer := range f.offIn {
		lines = append(lines, "  off in "+buffer)
	}
	lines = append(lines, "", "Filters")
	if len(f.state.Filters) == 0 {
		lines = append(lines, "  none — /filter add <name> <buffer> <tags> <regex>")
	}
	for _, filter := range f.state.Filters {
		mark := " "
		if filter.Disabled {
			mark = "-"
		}
		lines = append(lines, fmt.Sprintf(" %s %s: %s / %s / %s", mark, filter.Name, strings.Join(filter.Buffers, ","), filter.Tags, filter.Regex))
	}
	lines = append(lines, "", "Ignores")
	if len(f.state.Ignores) == 0 {
		lines = append(lines, "  none — /ignore add <nick> [<server> [<channel>]]")
	}
	for i, ignore := range f.state.Ignores {
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, describeIgnore(ignore)))
	}
	return lines
}

func describeIgnore(ignore config.Ignore) string {
	where := "everywhere"
	switch {
	case ignore.Server != "" && ignore.Channel != "":
		where = "in " + ignore.Channel + " on " + ignore.Server
	case ignore.Server != "":
		where = "on " + ignore.Server
	}
	return ignore.Mask + " " + where
}

func sameIgnore(a, b config.Ignore) bool {
	return strings.EqualFold(a.Mask, b.Mask) &&
		strings.EqualFold(a.Server, b.Server) &&
		strings.EqualFold(a.Channel, b.Channel)
}

// matchingNames are the names of the filters any of masks matches.
func matchingNames(filters []config.Filter, masks []string) map[string]bool {
	names := make(map[string]bool)
	for _, mask := range masks {
		pattern := globRegexp(mask)
		for _, filter := range filters {
			if pattern.MatchString(filter.Name) {
				names[filter.Name] = true
			}
		}
	}
	return names
}

// toggled is a boolean after enable (false), disable (true) or toggle — in
// the sense of "off", which is what both switches store.
func toggled(off bool, action string) bool {
	switch action {
	case "enable":
		return false
	case "disable":
		return true
	default:
		return !off
	}
}

// nextField splits off the first space-separated word, keeping the rest as
// typed so that a regex keeps its spaces.
func nextField(s string) (string, string) {
	s = strings.TrimLeft(s, " ")
	word, rest, _ := strings.Cut(s, " ")
	return word, rest
}
