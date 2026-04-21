package store

import (
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		// HTTPS with .git
		{"https with .git", "https://github.com/owner/repo.git", "github.com/owner/repo"},
		// HTTPS without .git
		{"https without .git", "https://github.com/owner/repo", "github.com/owner/repo"},
		// HTTP with .git
		{"http with .git", "http://github.com/owner/repo.git", "github.com/owner/repo"},
		// SSH with .git
		{"ssh with .git", "git@github.com:owner/repo.git", "github.com/owner/repo"},
		// SSH without .git
		{"ssh without .git", "git@github.com:owner/repo", "github.com/owner/repo"},
		// Credentials embedded
		{"https with credentials", "https://user:token@github.com/owner/repo.git", "github.com/owner/repo"},
		// GitLab
		{"gitlab https", "https://gitlab.com/org/project.git", "gitlab.com/org/project"},
		// Bitbucket SSH
		{"bitbucket ssh", "git@bitbucket.org:team/repo.git", "bitbucket.org/team/repo"},
		// Empty string
		{"empty string", "", ""},
		// Whitespace only
		{"whitespace only", "   ", ""},
		// Convergence: HTTPS and SSH of same repo produce same result
		{"convergence https", "https://github.com/vertigo7x/postgram.git", "github.com/vertigo7x/postgram"},
		{"convergence ssh", "git@github.com:vertigo7x/postgram.git", "github.com/vertigo7x/postgram"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeRemoteURL(tc.input)
			if got != tc.expected {
				t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestNormalizeRemoteURLConvergence(t *testing.T) {
	// Verify HTTPS and SSH of the same repo produce identical results
	pairs := []struct {
		https string
		ssh   string
	}{
		{"https://github.com/owner/repo.git", "git@github.com:owner/repo.git"},
		{"https://gitlab.com/org/project.git", "git@gitlab.com:org/project.git"},
		{"https://bitbucket.org/team/repo.git", "git@bitbucket.org:team/repo.git"},
	}

	for _, p := range pairs {
		httpsResult := NormalizeRemoteURL(p.https)
		sshResult := NormalizeRemoteURL(p.ssh)
		if httpsResult != sshResult {
			t.Errorf("Convergence failed:\n  HTTPS %q → %q\n  SSH   %q → %q",
				p.https, httpsResult, p.ssh, sshResult)
		}
		if httpsResult == "" {
			t.Errorf("Both HTTPS and SSH produced empty result for %q / %q", p.https, p.ssh)
		}
	}
}

func TestNormalizeProject(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		// Already normalized identifier — must be preserved
		{"already normalized host/owner/repo", "github.com/owner/repo", "github.com/owner/repo"},
		{"already normalized with subdomain", "gitlab.com/org/project", "gitlab.com/org/project"},
		// Unix absolute path → basename
		{"unix absolute path", "/home/alice/projects/mi-app", "mi-app"},
		{"unix deep path", "/a/b/c/repo", "repo"},
		// Windows absolute path → basename
		{"windows absolute path", `C:\Users\bob\projects\mi-app`, "mi-app"},
		{"windows path with forward slash", `C:/Users/bob/projects/mi-app`, "mi-app"},
		// Simple name — no separators
		{"simple name", "mi-app", "mi-app"},
		{"simple name with numbers", "postgram-v2", "postgram-v2"},
		// Empty / whitespace → "unknown"
		{"empty string", "", "unknown"},
		{"whitespace only", "   ", "unknown"},
		// Raw URL passed by agent → normalize
		{"raw https url", "https://github.com/owner/repo.git", "github.com/owner/repo"},
		{"raw ssh url", "git@github.com:owner/repo.git", "github.com/owner/repo"},
		// Normalized identifier with exactly 2 slashes (host/owner/repo)
		{"three segments", "github.com/vertigo7x/postgram", "github.com/vertigo7x/postgram"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeProject(tc.input)
			if got != tc.expected {
				t.Errorf("normalizeProject(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
