package activity

import "testing"

func TestClassify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		exe, title string
		fullscreen bool
		want       Category
	}{
		{`C:\Program Files\Microsoft VS Code\Code.exe`, "main.go - buddy", false, Code},
		{"chrome.exe", "Go documentation", false, Browser},
		{"msedge.exe", "Funny cats - YouTube", false, Media},
		{"Discord.exe", "general", false, Chat},
		{"somegame.exe", "Some Game", true, Game},
		{"somegame.exe", "Some Game", false, Other},
		{"explorer.exe", "", true, Other},
		{"firefox.exe", "Netflix", true, Media},
		{"", "", false, Other},
	}
	for _, tt := range tests {
		title := func() string { return tt.title }
		if got := Classify(tt.exe, title, tt.fullscreen); got != tt.want {
			t.Errorf("Classify(%q, %q, %v) = %v, want %v", tt.exe, tt.title, tt.fullscreen, got, tt.want)
		}
	}
}

// The README promises that only a browser's title is ever read.
func TestClassifyReadsTitleOnlyForBrowsers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		exe  string
		want bool
	}{
		{"chrome.exe", true},
		{`C:\Program Files\Mozilla Firefox\firefox.exe`, true},
		{"code.exe", false},
		{"discord.exe", false},
		{"somegame.exe", false},
		{"", false},
	}
	for _, tt := range tests {
		read := false
		Classify(tt.exe, func() string { read = true; return "" }, false)
		if read != tt.want {
			t.Errorf("Classify(%q) read the title = %v, want %v", tt.exe, read, tt.want)
		}
	}
}
