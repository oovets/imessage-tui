# BlueBubbles GUI - Implementation Plan

Syfte: ge GUI:t en ren men tydligt mer professionell look, förbättra UX i vardagsfloden, och införa first-run onboarding där användaren kan ange BlueBubbles server + lösenord en gång.

Statuslegend:
- [ ] Inte startad
- [-] Pågår
- [x] Klar

## Fas 0 - Baseline och ramar

- [ ] Bekräfta nulage och constraints
	- Kartlägg nuvarande startflode i [cmd/gui/main.go](cmd/gui/main.go)
	- Kartlägg config-laddning i [config/config.go](config/config.go)
	- Kartlägg themes och UI-konstanter i [gui/theme.go](gui/theme.go) och [gui/app.go](gui/app.go)

- [ ] Definiera designprinciper
	- En spacing-skala (t.ex. 4/8/12/16/24)
	- En neutral baspalett + en accentfarg
	- Typografihierarki (headline/body/meta)

- [ ] Definition of Done
	- GUI ser konsekvent ut med tydlig visuell hierarki
	- First-run wizard visas automatiskt nar credentials saknas
	- Credentials sparas sakert (keyring) med tydlig fallback
	- Appen kan paketeras for enkel forsta uppstart

## Fas 1 - Credential-lager (saker lagring)

- [x] Skapa nytt credentials-lager
	- Ny fil: credentials manager i config-lagret (lagring/hamtning/rensning)
	- Primar lagring: OS keyring (Linux Secret Service)
	- Fallback: configfil endast om keyring inte ar tillgangligt

- [x] Integrera med befintlig config
	- URL kan ligga i yaml, hemlighet lases primart fran keyring
	- Behall stod for BB_SERVER_URL och BB_PASSWORD via env vars

- [x] Acceptance
	- Tom config + ingen env => ingen hard fail i startup
	- Sparade credentials kan lasas efter omstart
	- Fel i keyring hanteras med begriplig varning

## Fas 2 - First-run onboarding wizard

- [x] Bygg onboarding-UI
	- Steg 1: Server URL
	- Steg 2: Password/API-token (med Visa/Dolj)
	- Steg 3: Testa anslutning
	- Steg 4: Spara och starta app

- [x] Startupflode
	- I [cmd/gui/main.go](cmd/gui/main.go):
		- Forsok ladda config + credentials
		- Om saknas: starta wizard-fonster
		- Vid lyckat test: spara och fortsatt till ordinarie GUI

- [x] UX-detaljer
	- Validering av URL-format
	- Tydliga felmeddelanden (network/auth/cert)
	- "Forsok igen" utan att tappa inmatning

- [x] Acceptance
	- Ny anvandare kan komma igang utan att satta env vars manuellt
	- Aterkommande start bypassar wizard

## Fas 3 - Visual polish (clean + pro look)

- [x] Theme-uppgradering
	- Samla design tokens (spacing, radius, border, typografi)
	- Justera kontrast for aktiv chat, hover och unread
	- Konsekventa storlekar for labels, metadata, badges

- [x] Layout-finputs
	- Chatlist: tydligare radstruktur och metadata spacing
	- Meddelandepanel: battre avstand mellan bubbles/kluster
	- Input: premium-kansla med ren inramning och tydlig fokusstate

- [x] Smarta states
	- Empty state nar ingen chat ar vald
	- Loading state vid initial laddning
	- Tydlig disabled state for actions under anslutning

- [x] Acceptance
	- Upplevd kvalitet uppgraderad utan visuellt "brus"
	- Ingen regress i dark/light mode

## Fas 4 - UX-forbattringar i arbetsflodet

- [x] Settings-dialog
	- Flytta/strukturera spridda toggles till en samlad settings-vy
	- Gruppindelning: Appearance, Behavior, Preview, Connection

- [x] Input och meddelandeflod
	- Forbattra reply-chip UX
	- Visuell feedback vid skickat meddelande
	- Stabil scroll och fokus efter inkommande event

- [x] Chatlist-beteende
	- Forbattra markering av aktiv/unread
	- Behall snabb navigering med tangentbord

- [x] Acceptance
	- Vanliga uppgifter gar snabbare och med farre missklick

## Fas 5 - Paketering och forsta-upplevelse

- [x] Linux paketeringsspår
	- Befintlig install-script-flode fortsatt stod
	- Utvardera AppImage eller Flatpak for enklare distribution
	- Se till att onboarding triggas korrekt i paketerad build

- [x] Desktop integration
	- Launcher + ikon + tydligt appnamn
	- Loggning och felhantering for support

- [-] Acceptance
	- "Installera -> starta -> fyll i wizard -> klar" utan manuell config-edit

## Fas 6 - QA, regression och dokumentation

- [x] Testmatris
	- Forsta start (utan config)
	- Uppstart med env vars
	- Uppstart med sparad keyring
	- Natt-/dagtema, olika fontstorlekar

- [-] Regression
	- Ingen brytning i split panes, chat load, message send, ws reconnect

- [x] README-uppdatering
	- Ny sektion: First-run setup
	- Ny sektion: Credential storage behavior
	- Uppdaterad launch/paketering

## Genomforandeordning (vi kor denna)

1. Fas 1 (credentials)
2. Fas 2 (wizard)
3. Fas 3 (visual polish)
4. Fas 4 (UX)
5. Fas 5 (paketering)
6. Fas 6 (QA + docs)

## Arbetsmodell

- Vi tar en fas i taget.
- Varje fas avslutas med:
	- Kodandring
	- Snabb verifiering
	- Kort changelog i denna fil (under "Progress log")

## Progress log

- 2026-03-31: PLAN skapad.
- 2026-03-31: Fas 1 implementerad (keyring + fallback + config integration).
- 2026-03-31: Fas 2 implementerad (GUI first-run wizard med validering och connection test).
- 2026-03-31: Fas 3 delvis implementerad (theme polish + chatlist preview + empty state).
- 2026-03-31: Fas 4 delvis implementerad (samlad settings-dialog med grupperade sektioner).
- 2026-03-31: Fas 3 utokad med loading state vid chatbyte.
- 2026-03-31: Fas 5 delvis implementerad (AppImage packaging script + metadata).
- 2026-03-31: Fas 6 delvis implementerad (README uppdaterad for setup, credentials och AppImage).
- 2026-03-31: UI polish justerad igen (inga scrollbars, inga separatorer, floating input box med reply-chip och send-status).
- 2026-03-31: Desktop integration fardigställd i install-script (egen ikon + desktop metadata).
- 2026-03-31: QA-checklist lagd i QA.md; manuell smoke-test mot riktig server kvarstar.
- 2026-03-31: Sista visual polish-pass klar (chatlist cards/active state + forfinad message-layout).
- 2026-03-31: Message bubbles borttagna, input satt till enradig och gap mot sista meddelande finjusterat.
- 2026-03-31: Split-separator visual borttagen helt och pane-top shadow borttagen.
- 2026-03-31: Release cleanup-pass klar (separator dead-code borttagen, standard `go test ./...` gron, QA release gate uppdaterad).
- 2026-03-31: Slack-GUI borttaget helt (kod + QA-referenser) for ren release-scope.
