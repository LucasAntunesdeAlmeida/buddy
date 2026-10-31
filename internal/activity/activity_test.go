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
		if got := Classify(tt.exe, tt.title, tt.fullscreen); got != tt.want {
			t.Errorf("Classify(%q, %q, %v) = %v, want %v", tt.exe, tt.title, tt.fullscreen, got, tt.want)
		}
	}
}
