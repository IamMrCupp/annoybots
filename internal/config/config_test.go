package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestLoadDefaultsAndTwitch(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "bot.yaml", `
bot: arywen
networks:
  - name: testnet
    server: irc.example.org:6697
    tls: true
    nick: Arywen
    channels: ["#lobby"]
  - name: twitch
    kind: twitch
    nick: Arywen
    password_env: TWITCH_TOKEN
    channels: ["#mychannel"]
personality:
  name: Arywen
`)
	c, err := Load(cfg, "", "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Health.Addr != ":8080" {
		t.Fatalf("expected default health addr, got %q", c.Health.Addr)
	}
	tw := c.Networks[1]
	if !tw.TLS || tw.Server != "irc.chat.twitch.tv:6697" {
		t.Fatalf("twitch defaults not applied: %+v", tw)
	}
	if tw.Rate.Burst != 18 {
		t.Fatalf("expected twitch burst default 18, got %d", tw.Rate.Burst)
	}
	if c.Networks[0].User != "Arywen" {
		t.Fatalf("expected user to default to nick, got %q", c.Networks[0].User)
	}
}

func TestLoadQuotePackFromFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rm.txt", "# rick and morty\nWubba lubba dub dub\n\nGet schwifty\n")
	cfg := writeFile(t, dir, "bot.yaml", `
bot: kurkutu
networks:
  - name: n
    server: s:6667
    nick: K
personality:
  name: Kurkutu
  quotes:
    enabled: true
    packs:
      - name: rickmorty
        file: rm.txt
`)
	c, err := Load(cfg, "", "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	lines := c.Personality.Quotes.Packs[0].Lines
	if len(lines) != 2 || lines[0] != "Wubba lubba dub dub" || lines[1] != "Get schwifty" {
		t.Fatalf("quote pack not parsed correctly: %#v", lines)
	}
}

func TestValidateRejectsBadKind(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "bot.yaml", `
networks:
  - name: n
    server: s:6667
    nick: K
    kind: discord
personality:
  name: K
`)
	if _, err := Load(cfg, "", ""); err == nil {
		t.Fatal("expected error for invalid network kind")
	}
}

func TestValidateRejectsTwitchWithoutToken(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "bot.yaml", `
networks:
  - name: twitch
    kind: twitch
    nick: K
personality:
  name: K
`)
	if _, err := Load(cfg, "", ""); err == nil {
		t.Fatal("expected error for twitch network without password_env")
	}
}
