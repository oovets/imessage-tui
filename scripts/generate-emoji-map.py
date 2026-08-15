#!/usr/bin/env python3
"""Regenerate emojiset/slack_generated.go.

The source is the desktop client's src/slack/emojiMap.ts, which is itself
generated from iamcal/emoji-data -- the dataset Slack uses. Going through that
file rather than the dataset directly keeps the two clients decoding incoming
Slack text identically, which is the whole point of having the table.

    python3 scripts/generate-emoji-map.py [path/to/emojiMap.ts]
    gofmt -w emojiset/slack_generated.go

Run it when Slack adds names, which is roughly once a Unicode release.
"""

import json
import os
import re
import sys

DEFAULT_SOURCE = os.path.expanduser("~/Code/imessage/src/slack/emojiMap.ts")
OUTPUT = os.path.join(os.path.dirname(__file__), "..", "emojiset", "slack_generated.go")

PAIR = re.compile(r'"((?:[^"\\]|\\.)*)"\s*:\s*"((?:[^"\\]|\\.)*)"')


def block(source: str, name: str) -> str:
    """Return the object literal exported as `name`, braces included."""
    start = source.index("{", source.index(f"export const {name}"))
    depth = 0
    for i in range(start, len(source)):
        if source[i] == "{":
            depth += 1
        elif source[i] == "}":
            depth -= 1
            if depth == 0:
                return source[start:i + 1]
    raise ValueError(f"unterminated object literal for {name}")


def entries(source: str, name: str) -> dict:
    return {
        json.loads(f'"{k}"'): json.loads(f'"{v}"')
        for k, v in PAIR.findall(block(source, name))
    }


def go_string(value: str) -> str:
    # Emoji and shortcode names carry neither quotes nor backslashes; anything
    # that did would need escaping, so refuse rather than emit broken Go.
    if '"' in value or "\\" in value:
        raise ValueError(f"value needs escaping: {value!r}")
    return f'"{value}"'


def main() -> None:
    path = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_SOURCE
    with open(path, encoding="utf-8") as handle:
        source = handle.read()

    shortcodes = entries(source, "SLACK_EMOJI")
    tones = entries(source, "SKIN_TONES")

    with open(OUTPUT, "w", encoding="utf-8") as out:
        out.write(
            "// Code generated from the desktop client's src/slack/emojiMap.ts, which is\n"
            "// itself generated from iamcal/emoji-data — the dataset Slack uses. Do not\n"
            "// edit by hand; see scripts/generate-emoji-map.py.\n"
            "\n"
            "package emojiset\n"
            "\n"
            "// slackShortcodes is the full Slack shortcode set. It exists to decode\n"
            "// arbitrary incoming text, which needs every name Slack might send, not the\n"
            "// handful a person is likely to type.\n"
            "var slackShortcodes = map[string]string{\n"
        )
        for name in sorted(shortcodes):
            out.write(f"\t{go_string(name)}: {go_string(shortcodes[name])},\n")
        out.write("}\n\n")

        out.write(
            "// skinTones maps Slack's tone shortcodes to the Unicode modifiers they stand\n"
            "// for. Slack sends the base emoji and the tone as separate tokens.\n"
            "var skinTones = map[string]string{\n"
        )
        for name in sorted(tones):
            out.write(f"\t{go_string(name)}: {go_string(tones[name])},\n")
        out.write("}\n")

    print(f"wrote {len(shortcodes)} shortcodes and {len(tones)} skin tones to {OUTPUT}")
    print("now run: gofmt -w emojiset/slack_generated.go")


if __name__ == "__main__":
    main()
