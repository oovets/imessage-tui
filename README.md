[![Go](https://img.shields.io/badge/go-1.25%2B-00ADD8.svg)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/tui-bubble%20tea-ff69b4.svg)](https://github.com/charmbracelet/bubbletea)
[![Docs](https://img.shields.io/badge/docs-mkdocs--material-blue.svg)](https://stevoo.net/imessage-tui/)

keyboard-first terminal client for imessage and slack, in one chat list.

imessage goes through a bluebubbles-compatible server; slack talks to slack
directly. both render the same way, and which backend owns a conversation is
never something you have to think about.

```
== contents ==

  quick start          get it running
  configuration        settings, env vars, theme
  slack                tokens, what works, what does not
  using it             keys, composer commands, chat list
  behaviour            link previews, emoticons, chat management
  development          layout, checks, troubleshooting
```

---

```
== quick start ==

  requirements
    go 1.25 or newer
    a bluebubbles-compatible server, reachable over http and websocket
    slack tokens, if you want slack -- optional, see below

  the bluebubbles server is checked at startup and the app exits if it is
  unreachable, whether or not slack is configured.

  build and run
    go build -o imessage-tui .
    export BB_SERVER_URL=https://your-server:1234
    export BB_PASSWORD=your-api-password
    ./imessage-tui

  the tui owns the terminal, so nothing is ever printed to stdout.
  runtime logs go to ~/.imessage-tui.log -- read that first when something
  looks wrong.
```

```
== features ==

  chat
    imessage and slack in one list, searchable, with unread markers
    optimistic sending: your message appears at once and reconciles later
    tapbacks and slack reactions render as compact emoji on the message
    thread replies, quoted replies, image attachments, link previews

  layout
    up to 8 panes, split horizontally or vertically, each with its own chat
    per-pane focus, resizable dividers, resizable chat list
    layout, ui state and message cache survive a restart

  reading
    sender names get a stable colour per person, so a busy channel can be
    read by who is talking. shown automatically wherever more than one
    person talks, and toggleable per pane
    optional timestamps, line numbers, sender names, pane dividers, previews

  connection
    realtime over websocket (imessage) and socket mode (slack)
    api polling reconciles anything the realtime feed missed
    a backend that is down costs only itself
```

---

```
== configuration ==

  read from env vars and ~/.config/imessage-tui/imessage.yaml; env wins.
  credentials prefer the os keyring and fall back to the config file.
  ui state, layout, message cache and chat aliases live in
  ~/.config/imessage-tui/

  setting                   env var                     default   what it does
  ----------------------------------------------------------------------------
  server_url                BB_SERVER_URL               required  bluebubbles url
  password                  BB_PASSWORD                 required  api password
  theme                     BB_THEME                    auto      auto|light|dark
  message_limit             BB_MESSAGE_LIMIT            50        messages per chat
  chat_limit                BB_CHAT_LIMIT               50        imessage chats listed
  poll_interval_sec         BB_POLL_INTERVAL_SEC        10        refresh; 0 disables
  enable_link_previews      BB_ENABLE_LINK_PREVIEWS     true      preview metadata
  max_previews_per_message  BB_MAX_PREVIEWS_PER_MESSAGE 2         previews per message
  preview_proxy_url         BB_PREVIEW_PROXY_URL        empty     optional json proxy
  oembed_endpoint           BB_OEMBED_ENDPOINT          noembed   oembed endpoint

  env only
  ----------------------------------------------------------------------------
  BB_INSECURE_TLS           unset     skip tls verification (self-signed; insecure)
  BB_SLACK_TOKEN            unset     slack user token, one workspace
  BB_SLACK_APP_TOKEN        unset     slack app-level token, for realtime
  BB_SLACK_WORKSPACE        slack     label for the env workspace

  chat_limit applies to imessage only. it means "the most recent N", which
  needs a list ordered by recency; slack's is not, so every slack
  conversation is fetched rather than an arbitrary slice of them.
```

```yaml
# ~/.config/imessage-tui/imessage.yaml
server_url: "https://your-server:1234"
password: "your-api-password"
theme: auto
message_limit: 50
chat_limit: 50
poll_interval_sec: 10
enable_link_previews: true
max_previews_per_message: 2
```

```
== theme ==

  auto keeps a compromise palette. every colour is chosen to stay readable on
  light and dark alike, and message text carries no colour at all so it
  inherits the terminal's own foreground.

  pinning dark or light additionally tunes the palette for that background:
  on dark the accent blue, timestamps, dividers and sender colours brighten,
  since they no longer have to survive a white background too.

  auto never picks the tuned palette. background detection queries the
  terminal over stdin, multiplexers answer it themselves, and a wrong guess
  paints unreadable text -- a conservative palette is the better failure.

  on a black-background xterm: set theme: dark, and make sure TERM is
  xterm-256color. a TERM without a colour suffix makes lipgloss strip every
  colour, so the palette never gets a chance.
```

---

```
== slack ==

  slack conversations appear in the same chat list as imessage. nothing about
  it goes in imessage.yaml: slack is on when tokens are present and off when
  they are not, and the app starts either way. a workspace that fails to
  connect costs that workspace only.

  two tokens per workspace, because slack needs two
    xoxp-...   user token: the web api acts as you and sees what you see
    xapp-...   app-level token: opens socket mode, and nothing else

  fastest way in, for one workspace
    export BB_SLACK_TOKEN=xoxp-...
    export BB_SLACK_APP_TOKEN=xapp-...
    export BB_SLACK_WORKSPACE=acme     # optional label

  for several workspaces, or to keep tokens out of your shell profile, put
  them in the os keyring. an existing ~/.slack_config.json is imported on
  first run and the log says so -- delete that file afterwards, it holds both
  tokens in plaintext.

  what works
    channels, private channels, dms, group dms
    thread replies, shown inline and marked, and /r #N to answer in a thread
    files, opened with /img #N or a click
    reactions, mentions resolved to names, emoji shortcodes
    realtime over socket mode, several workspaces at once

  what does not
    deleting or renaming a conversation. imessage-only; the app says so
    rather than pretending
    marking read without a write scope on the user token (channels:write,
    groups:write, im:write, mpim:write). without it slack answers
    missing_scope, the app stops asking for that workspace, and everything
    else keeps working
    channels you have not joined, archived channels, closed dms. the list is
    your membership, the same thing slack's own sidebar shows

  ordering
    imessage first, then each workspace: people and group chats before
    channels, alphabetically. slack's conversation list carries no
    last-activity timestamp, and getting one would cost a history call per
    conversation, so recency ordering is not on offer.
    with more than one workspace connected, names are prefixed with the
    workspace so two "#general" stay apart.

  if conversations are missing, the log says what was fetched
    grep "conversations" ~/.imessage-tui.log
```

---

```
== keys ==

  single-letter shortcuts (? q d r g G /) and the plain arrow keys only act
  when you are not typing. with the composer or chat search focused they edit
  text instead. anything using ctrl, alt or shift works everywhere, including
  mid-message.

  moving around
    tab                    switch focus between chat list and panes
    esc                    back to the chat list
    up / down, k / j       navigate chats, or scroll messages
    g / G                  top / bottom of the chat list; in a pane, G and
                           end both jump to the newest message
    pgup / pgdown          scroll the focused pane
    /                      search the chat list
    enter                  open the selected chat, or send the composer
    shift+enter            newline in the composer
    ? / F1                 help
    q / ctrl+c             quit

  panes
    ctrl+f                 split the focused pane left and right
    ctrl+g                 split the focused pane top and bottom
    ctrl+w                 close the focused pane
    shift+left / right     move between panes, also while writing
    ctrl+up / ctrl+down    move to the pane above or below
    ctrl+shift+left/right  resize the focused split
    ctrl+left / ctrl+right resize the chat list

  toggles
    ctrl+s                 chat list
    ctrl+t                 timestamps
    ctrl+n                 line numbers
    ctrl+e                 pane dividers
    ctrl+p                 chat previews in the list
    ctrl+b                 sender names, focused pane only
    alt+m                  sender names, every pane and new ones

  chat actions
    d, then D              delete the selected chat, esc cancels
    r                      rename the selected chat
    ctrl+d / ctrl+r        the same two, from a pane

  mouse
    click to focus a pane or open an image, scroll to move through history,
    drag the chat list edge or a pane divider to resize.
```

```
== composer commands ==

  typed into the message box and sent with enter, instead of the message.
  the #N is the row number shown by ctrl+n.

    /r #N text    reply to row N. on slack this posts into that message's
                  thread, the only way to answer in a thread rather than
                  beside it. on imessage it becomes a quoted reply
    /img #N       open the image on row N
    /h            react to the newest message with a heart
    /lol          react with laughing
    /tu           react with thumbs up
    /te           react with thumbs down
    /!!           react with emphasis
    /?            react with a question mark

  on slack the six reactions map onto :heart: :joy: :+1: :-1: :bangbang:
  :question:
```

```
== chat list and status ==

  there is no persistent status bar. prompts you have to act on -- delete and
  rename confirmations, errors, toasts -- appear on the bottom row while they
  are live, then disappear.

  unread chats are marked in the chat list and with a dot on pane headers.
  a pane you are looking at clears its own marker.
```

---

```
== behaviour ==

  emoticons
    the composer rewrites text emoticons as emoji. an emoticon has to stand
    as its own whitespace-delimited word, so "hej :)" converts and
    "http://x" does not

      <3 </3 :) :-) =) :( ;) :| :* 8) :'( \o/   convert as you type
      :D :P :p :O :o xD XD :/ :\ B) o/          convert on the next space

    the second group waits for a word boundary so ":Down" and "xDrive"
    survive and urls keep their "://". a rewrite is skipped while the cursor
    sits behind the end of the text, so editing earlier in a draft never
    moves you to the end.

  link previews
    supported media urls render a compact preview line

      [YouTube] video title        [Spotify] track or playlist title
      [Instagram] post or reel     [Aftonbladet] article title

    hosts: youtube.com m.youtube.com youtu.be spotify.com open.spotify.com
    instagram.com m.instagram.com aftonbladet.se expressen.se dn.se svd.se
    svt.se omni.se gp.se sydsvenskan.se di.se

    fetches are async: a fallback label first, then the metadata. html
    metadata is preferred, so generic titles like "search" are ignored and
    refetched.

  emoji
    shortcodes are decoded from the same table the composer autocompletes
    from, generated from the dataset slack itself uses -- so :saluting_face:
    arrives as an emoji rather than as text, and anything you can receive is
    something you can type with ":".
    workspace-custom emoji have no unicode character and stay as their name.

  chat management
    delete uses the bluebubbles private api and clears the local cache only
    after the server confirms: d, then D, esc to cancel.
    rename uses the group-rename api when available; if the server rejects
    it, the alias is saved in chat_overrides.json and applied on refresh.
```

---

```
== development ==

  layout
    main.go              startup: config, backends, theme, bubble tea
    provider/            the seam between the ui and the chat backends
    provider/imessage/   adapts api/ and ws/ to it
    provider/slack/      slack web api, socket mode, mrkdwn, guids
    emojiset/            the shared shortcode table, generated
    api/                 bluebubbles http client, contacts, attachments
    ws/                  socket.io/websocket client, reconnect + overflow
    config/              config, credentials, ui/layout/cache state
    models/              chat, message, attachment, link preview
    tui/                 models, split layout, rendering, input, persistence

    conversations are addressed by guid, and which backend owns one is
    derived from its prefix: unprefixed is imessage, sl: is slack. nothing
    outside provider/ parses a guid.

  checks
    go test ./...
    gofmt -l .
    go vet ./...
    go build ./...

  docs
    the root *.md files are the source of truth. scripts/docs.sh stages them
    into docs/ for mkdocs, and docs/*.md is gitignored, so never edit there.
    AGENTS.md has the rules that are easy to get wrong in this codebase.
    SLACK.md has the slack design and its known limits.
```

```
== bluebubbles setup ==

  the server must run on a mac signed into icloud with messages enabled.
  grant it full disk access, accessibility and automation.

  verify connectivity with the same url and password the app uses

    curl -k "https://your-server/api/v1/server/info?password=YOUR_PASSWORD"
```

```
== troubleshooting ==

  no colour at all
    check TERM. a value without a colour suffix, such as rxvt or plain
    xterm, makes lipgloss strip every escape sequence. use xterm-256color.

  tls errors
    verify url, certificate trust and password. certificates are verified by
    default; for a self-signed server set BB_INSECURE_TLS=1, which is
    insecure and env-only on purpose.

  missing contact names
    make sure contacts are available to bluebubbles itself.

  stale chats
    check websocket connectivity. polling reconciles open chats when enabled.

  slack conversations missing
    grep "conversations" ~/.imessage-tui.log for what was actually fetched.
    0 dm usually means the token lacks im:read.

  anything else
    ~/.imessage-tui.log has it. the tui never writes to the terminal.
```
