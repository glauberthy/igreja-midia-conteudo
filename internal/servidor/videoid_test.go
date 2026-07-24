package servidor

import "testing"

func TestVideoID(t *testing.T) {
	casos := []struct {
		url  string
		quer string
	}{
		{"https://www.youtube.com/watch?v=mg83gcM4ctw", "mg83gcM4ctw"},
		{"https://youtube.com/watch?v=abc123&t=90s", "abc123"},
		{"https://m.youtube.com/watch?v=xyz", "xyz"},
		{"https://youtu.be/mg83gcM4ctw", "mg83gcM4ctw"},
		{"https://youtu.be/mg83gcM4ctw?t=42", "mg83gcM4ctw"},
		{"https://www.youtube.com/embed/mg83gcM4ctw", "mg83gcM4ctw"},
		{"https://www.youtube.com/shorts/mg83gcM4ctw", "mg83gcM4ctw"},
		{"https://www.youtube.com/live/mg83gcM4ctw", "mg83gcM4ctw"},
		{"https://vimeo.com/12345", ""},
		{"não é url :://", ""},
		{"", ""},
	}
	for _, c := range casos {
		if got := videoID(c.url); got != c.quer {
			t.Errorf("videoID(%q) = %q, quero %q", c.url, got, c.quer)
		}
	}
}
