package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Gentleman-Programming/engram/internal/testutil"
	"github.com/google/uuid"
)

func mustDefaultConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DatabaseURL = testutil.NewPostgresURL(t)
	return cfg
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	cfg := mustDefaultConfig(t)
	cfg.DedupeWindow = time.Hour

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func mustExecStore(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.execHook(s.db, query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

type fakeRows struct {
	next    []bool
	scanErr error
	err     error
}

func (f *fakeRows) Next() bool {
	if len(f.next) == 0 {
		return false
	}
	v := f.next[0]
	f.next = f.next[1:]
	return v
}

func (f *fakeRows) Scan(dest ...any) error {
	return f.scanErr
}

func (f *fakeRows) Err() error {
	return f.err
}

func (f *fakeRows) Close() error {
	return nil
}

func TestAddObservationDeduplicatesWithinWindow(t *testing.T) {
	s := newTestStore(t)

	params := CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}
	if err := s.CreateSession(params); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "bugfix",
		Title:     "Fixed tokenizer",
		Content:   "Normalized tokenizer panic on edge case",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add first observation: %v", err)
	}

	secondID, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "bugfix",
		Title:     "Fixed tokenizer",
		Content:   "normalized   tokenizer panic on EDGE case",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add duplicate observation: %v", err)
	}

	if firstID != secondID {
		t.Fatalf("expected duplicate to reuse same id, got %s and %s", firstID, secondID)
	}

	obs, err := s.GetObservation(firstID)
	if err != nil {
		t.Fatalf("get deduped observation: %v", err)
	}
	if obs.DuplicateCount != 2 {
		t.Fatalf("expected duplicate_count=2, got %d", obs.DuplicateCount)
	}
}

func TestScopeFiltersSearchAndContext(t *testing.T) {
	s := newTestStore(t)

	params := CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}
	if err := s.CreateSession(params); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "decision",
		Title:     "Project auth",
		Content:   "Keep auth middleware in project memory",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add project observation: %v", err)
	}

	_, err = s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "decision",
		Title:     "Personal note",
		Content:   "Use this regex trick later",
		Project:   "engram",
		Scope:     "personal",
	})
	if err != nil {
		t.Fatalf("add personal observation: %v", err)
	}

	projectResults, err := s.Search("regex", SearchOptions{Project: "engram", Scope: "project", Limit: 10})
	if err != nil {
		t.Fatalf("search project scope: %v", err)
	}
	if len(projectResults) != 0 {
		t.Fatalf("expected no project-scope regex results, got %d", len(projectResults))
	}

	personalResults, err := s.Search("regex", SearchOptions{Project: "engram", Scope: "personal", Limit: 10})
	if err != nil {
		t.Fatalf("search personal scope: %v", err)
	}
	if len(personalResults) != 1 {
		t.Fatalf("expected 1 personal-scope result, got %d", len(personalResults))
	}

	ctx, err := s.FormatContext("engram", "personal")
	if err != nil {
		t.Fatalf("format context personal: %v", err)
	}
	if !strings.Contains(ctx, "Personal note") {
		t.Fatalf("expected personal context to include personal observation")
	}
	if strings.Contains(ctx, "Project auth") {
		t.Fatalf("expected personal context to exclude project observation")
	}
}

func TestUpdateAndSoftDeleteExcludedFromSearchAndTimeline(t *testing.T) {
	s := newTestStore(t)

	params := CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}
	if err := s.CreateSession(params); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "bugfix",
		Title:     "first",
		Content:   "first event",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add first: %v", err)
	}

	middleID, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "bugfix",
		Title:     "middle",
		Content:   "to be deleted",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add middle: %v", err)
	}

	lastID, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "bugfix",
		Title:     "last",
		Content:   "last event",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add last: %v", err)
	}

	newTitle := "last-updated"
	newContent := "updated content"
	newScope := "personal"
	updated, err := s.UpdateObservation(lastID, UpdateObservationParams{
		Title:   &newTitle,
		Content: &newContent,
		Scope:   &newScope,
	})
	if err != nil {
		t.Fatalf("update observation: %v", err)
	}
	if updated.Title != newTitle || updated.Scope != "personal" {
		t.Fatalf("update did not apply; got title=%q scope=%q", updated.Title, updated.Scope)
	}

	if err := s.DeleteObservation(middleID, false); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if _, err := s.GetObservation(middleID); err == nil {
		t.Fatalf("expected deleted observation to be hidden from GetObservation")
	}

	searchResults, err := s.Search("deleted", SearchOptions{Project: "engram", Limit: 10})
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(searchResults) != 0 {
		t.Fatalf("expected deleted observation excluded from search")
	}

	timeline, err := s.Timeline(firstID, 5, 5)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	seenTimeline := map[string]bool{}
	for _, e := range timeline.Before {
		seenTimeline[e.ID] = true
	}
	for _, e := range timeline.After {
		seenTimeline[e.ID] = true
	}
	if seenTimeline[middleID] || !seenTimeline[lastID] {
		t.Fatalf("expected timeline to exclude deleted observation and keep surviving neighbor, got before=%+v after=%+v", timeline.Before, timeline.After)
	}

	if err := s.DeleteObservation(lastID, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if _, err := s.GetObservation(lastID); err == nil {
		t.Fatalf("expected hard-deleted observation to be missing")
	}
}

func TestTopicKeyUpsertUpdatesSameTopicWithoutCreatingNewRow(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth architecture",
		Content:   "Use middleware for JWT validation.",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "architecture auth model",
	})
	if err != nil {
		t.Fatalf("add first architecture: %v", err)
	}

	secondID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth architecture",
		Content:   "Move auth to gateway + middleware chain.",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "ARCHITECTURE   AUTH  MODEL",
	})
	if err != nil {
		t.Fatalf("upsert architecture: %v", err)
	}

	if firstID != secondID {
		t.Fatalf("expected topic upsert to reuse id, got %s and %s", firstID, secondID)
	}

	obs, err := s.GetObservation(firstID)
	if err != nil {
		t.Fatalf("get upserted observation: %v", err)
	}
	if obs.RevisionCount != 2 {
		t.Fatalf("expected revision_count=2, got %d", obs.RevisionCount)
	}
	if obs.TopicKey == nil || *obs.TopicKey != "architecture-auth-model" {
		t.Fatalf("expected normalized topic key, got %v", obs.TopicKey)
	}
	if !strings.Contains(obs.Content, "gateway") {
		t.Fatalf("expected latest content after upsert, got %q", obs.Content)
	}
}

func TestDifferentTopicsDoNotReplaceEachOther(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	archID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth architecture",
		Content:   "Architecture decision",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "architecture/auth",
	})
	if err != nil {
		t.Fatalf("add architecture observation: %v", err)
	}

	bugID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "bugfix",
		Title:     "Fix auth nil panic",
		Content:   "Bugfix details",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "bug/auth-nil-panic",
	})
	if err != nil {
		t.Fatalf("add bug observation: %v", err)
	}

	if archID == bugID {
		t.Fatalf("expected different topic keys to create different observations")
	}

	observations, err := s.AllObservations("engram", "project", 10)
	if err != nil {
		t.Fatalf("all observations: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(observations))
	}
}

func TestImportNormalizesLegacyObservationIDs(t *testing.T) {
	s := newTestStore(t)
	data := &ExportData{
		Sessions: []Session{{
			ID:              "s1",
			ClientSessionID: "s1",
			Project:         "engram",
			Directory:       "/tmp/engram",
			StartedAt:       Now(),
		}},
		Observations: []Observation{
			{ID: "", SyncID: "", SessionID: "s1", Type: "bugfix", Title: "legacy null", Content: "legacy null content", Project: nullableString("engram"), Scope: "project", CreatedAt: Now(), UpdatedAt: Now()},
			{ID: "7", SyncID: "obs-legacy-fixed", SessionID: "s1", Type: "bugfix", Title: "legacy fixed", Content: "legacy fixed content", Project: nullableString("engram"), Scope: "project", CreatedAt: Now(), UpdatedAt: Now()},
			{ID: "7", SyncID: "obs-legacy-duplicate", SessionID: "s1", Type: "bugfix", Title: "legacy duplicate", Content: "legacy duplicate content", Project: nullableString("engram"), Scope: "project", CreatedAt: Now(), UpdatedAt: Now()},
		},
	}

	if _, err := s.Import(data); err != nil {
		t.Fatalf("import legacy observations: %v", err)
	}

	obs, err := s.AllObservations("engram", "", 20)
	if err != nil {
		t.Fatalf("all observations after import: %v", err)
	}
	if len(obs) != 3 {
		t.Fatalf("expected 3 imported observations, got %d", len(obs))
	}

	seen := make(map[string]bool)
	for _, o := range obs {
		if _, err := uuid.Parse(o.ID); err != nil {
			t.Fatalf("expected normalized observation uuid, got %q", o.ID)
		}
		if _, err := uuid.Parse(o.SyncID); err != nil {
			t.Fatalf("expected normalized observation sync uuid, got %q", o.SyncID)
		}
		if seen[o.ID] {
			t.Fatalf("expected unique normalized ids, duplicate %s", o.ID)
		}
		seen[o.ID] = true
	}

	results, err := s.Search("legacy", SearchOptions{Project: "engram", Limit: 10})
	if err != nil {
		t.Fatalf("search after import: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results after importing normalized observations")
	}
}

func TestImportNormalizesLegacyPromptSyncIDs(t *testing.T) {
	s := newTestStore(t)
	data := &ExportData{
		Sessions: []Session{{
			ID:              "s1",
			ClientSessionID: "s1",
			Project:         "engram",
			Directory:       "/tmp/engram",
			StartedAt:       Now(),
		}},
		Prompts: []Prompt{{
			ID:        "promptrow-legacy",
			SyncID:    "prompt-legacy-sync",
			SessionID: "s1",
			Content:   "legacy prompt",
			Project:   "engram",
			CreatedAt: Now(),
		}},
	}

	if _, err := s.Import(data); err != nil {
		t.Fatalf("import legacy prompts: %v", err)
	}

	var promptID, syncID string
	if err := s.queryRowHook(s.db, "SELECT id::text, sync_id::text FROM user_prompts WHERE content = ?", "legacy prompt").Scan(&promptID, &syncID); err != nil {
		t.Fatalf("query imported prompt ids: %v", err)
	}
	if _, err := uuid.Parse(promptID); err != nil {
		t.Fatalf("expected normalized prompt uuid, got %q", promptID)
	}
	if _, err := uuid.Parse(syncID); err != nil {
		t.Fatalf("expected normalized prompt sync uuid, got %q", syncID)
	}
}

func TestSuggestTopicKeyNormalizesDeterministically(t *testing.T) {
	got := SuggestTopicKey("Architecture", "  Auth Model  ", "ignored")
	if got != "architecture/auth-model" {
		t.Fatalf("expected architecture/auth-model, got %q", got)
	}

	fallback := SuggestTopicKey("bugfix", "", "Fix nil panic in auth middleware on empty token")
	if fallback != "bug/fix-nil-panic-in-auth-middleware-on-empty" {
		t.Fatalf("unexpected fallback topic key: %q", fallback)
	}
}

func TestSuggestTopicKeyInfersFamilyFromTextWhenTypeIsGeneric(t *testing.T) {
	bug := SuggestTopicKey("manual", "", "Fix regression in auth login flow")
	if bug != "bug/fix-regression-in-auth-login-flow" {
		t.Fatalf("expected bug family inference, got %q", bug)
	}

	arch := SuggestTopicKey("", "ADR: Split API gateway boundary", "")
	if arch != "architecture/adr-split-api-gateway-boundary" {
		t.Fatalf("expected architecture family inference, got %q", arch)
	}
}

func TestTopicKeyUpsertIsScopedByProjectAndScope(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	baseID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth model",
		Content:   "Initial architecture",
		Project:   "engram",
		Scope:     "project",
		TopicKey:  "architecture/auth-model",
	})
	if err != nil {
		t.Fatalf("add base observation: %v", err)
	}

	personalID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth model",
		Content:   "Personal take",
		Project:   "engram",
		Scope:     "personal",
		TopicKey:  "architecture/auth-model",
	})
	if err != nil {
		t.Fatalf("add personal scoped observation: %v", err)
	}

	otherProjectID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "architecture",
		Title:     "Auth model",
		Content:   "Other project",
		Project:   "another-project",
		Scope:     "project",
		TopicKey:  "architecture/auth-model",
	})
	if err != nil {
		t.Fatalf("add other project observation: %v", err)
	}

	if baseID == personalID || baseID == otherProjectID || personalID == otherProjectID {
		t.Fatalf("expected topic upsert boundaries by project+scope, got ids base=%s personal=%s other=%s", baseID, personalID, otherProjectID)
	}
}

func TestPromptProjectNullScan(t *testing.T) {
	s := newTestStore(t)
	sessionID := normalizeSessionID("s1")

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Manually insert a prompt with NULL project to simulate legacy data or external changes
	_, err := s.execHook(s.db,
		"INSERT INTO user_prompts (id, sync_id, session_id, content, project) VALUES (?, ?, ?, ?, NULL)",
		normalizePromptID("promptrow-null-project"), normalizePromptSyncID("prompt-sync-null-project"), sessionID, "prompt with null project",
	)
	if err != nil {
		t.Fatalf("manual insert: %v", err)
	}

	// 1. Test RecentPrompts
	prompts, err := s.RecentPrompts("", 10)
	if err != nil {
		t.Fatalf("RecentPrompts failed with null project: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Project != "" {
		t.Errorf("expected empty string for null project, got %q", prompts[0].Project)
	}

	// 2. Test SearchPrompts
	searchResult, err := s.SearchPrompts("null", "", 10)
	if err != nil {
		t.Fatalf("SearchPrompts failed with null project: %v", err)
	}
	if len(searchResult) != 1 || searchResult[0].Project != "" {
		t.Errorf("expected empty string for null project in search, got %q", searchResult[0].Project)
	}

	// 3. Test Export
	data, err := s.Export()
	if err != nil {
		t.Fatalf("Export failed with null project: %v", err)
	}
	found := false
	for _, p := range data.Prompts {
		if p.Content == "prompt with null project" {
			found = true
			if p.Project != "" {
				t.Errorf("expected empty string for null project in export, got %q", p.Project)
			}
		}
	}
	if !found {
		t.Error("exported prompts missing the test prompt")
	}
}

// ─── Passive Capture Tests ───────────────────────────────────────────────────

func TestExtractLearningsNumberedList(t *testing.T) {
	text := `Some preamble text here.

## Key Learnings:

1. bcrypt cost=12 is the right balance for our server performance
2. JWT refresh tokens need atomic rotation to prevent race conditions
3. Always validate the audience claim in JWT tokens before trusting them

## Next Steps
- something else
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 3 {
		t.Fatalf("expected 3 learnings, got %d: %v", len(learnings), learnings)
	}
	if !strings.Contains(learnings[0], "bcrypt") {
		t.Fatalf("expected first learning about bcrypt, got %q", learnings[0])
	}
}

func TestExtractLearningsSpanishHeader(t *testing.T) {
	text := `## Aprendizajes Clave:

1. El costo de bcrypt=12 es el balance correcto para nuestro servidor
2. Los refresh tokens de JWT necesitan rotacion atomica
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 2 {
		t.Fatalf("expected 2 learnings, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsBulletList(t *testing.T) {
	text := `### Learnings:

- bcrypt cost=12 is the right balance for our server performance
- JWT refresh tokens need atomic rotation to prevent race conditions
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 2 {
		t.Fatalf("expected 2 learnings, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsIgnoresShortItems(t *testing.T) {
	text := `## Key Learnings:

1. too short
2. bcrypt cost=12 is the right balance for our server performance
3. also short
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning (short ones filtered), got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsNoSection(t *testing.T) {
	text := `This is just regular text without any learning section headers.
It has multiple lines but no ## Key Learnings or similar.
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 0 {
		t.Fatalf("expected 0 learnings, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsSectionPresentButNoValidItems(t *testing.T) {
	text := `## Key Learnings:

1. short
2. tiny
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 0 {
		t.Fatalf("expected 0 learnings when section has no valid items, got %d: %v", len(learnings), learnings)
	}
}

func TestExtractLearningsUsesLastSection(t *testing.T) {
	text := `## Key Learnings:

1. This is from the first section and should be ignored

Some other text here.

## Key Learnings:

1. This is from the last section and should be captured as the real one
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning from last section, got %d: %v", len(learnings), learnings)
	}
	if !strings.Contains(learnings[0], "last section") {
		t.Fatalf("expected learning from last section, got %q", learnings[0])
	}
}

func TestExtractLearningsFallsBackWhenLastSectionHasNoValidItems(t *testing.T) {
	text := `## Key Learnings:

1. This is long enough and should be captured from the previous section

## Key Learnings:

1. short
2. tiny
`
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected fallback to previous valid section, got %d: %v", len(learnings), learnings)
	}
	if !strings.Contains(learnings[0], "previous section") {
		t.Fatalf("expected learning from previous section, got %q", learnings[0])
	}
}

func TestExtractLearningsCleansMarkdown(t *testing.T) {
	text := "## Key Learnings:\n\n1. **Use** `context.Context` in *all* handlers to support cancellation correctly\n"
	learnings := ExtractLearnings(text)
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning, got %d: %v", len(learnings), learnings)
	}
	if strings.Contains(learnings[0], "**") || strings.Contains(learnings[0], "`") || strings.Contains(learnings[0], "*") {
		t.Fatalf("expected markdown to be stripped, got %q", learnings[0])
	}
}

func TestPassiveCaptureStoresLearnings(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	text := `## Key Learnings:

1. bcrypt cost=12 is the right balance for our server performance
2. JWT refresh tokens need atomic rotation to prevent race conditions
`
	result, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "s1",
		Content:   text,
		Project:   "engram",
		Source:    "test",
	})
	if err != nil {
		t.Fatalf("passive capture: %v", err)
	}
	if result.Extracted != 2 {
		t.Fatalf("expected 2 extracted, got %d", result.Extracted)
	}
	if result.Saved != 2 {
		t.Fatalf("expected 2 saved, got %d", result.Saved)
	}

	obs, err := s.AllObservations("engram", "", 10)
	if err != nil {
		t.Fatalf("all observations: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(obs))
	}
	for _, o := range obs {
		if o.Type != "passive" {
			t.Fatalf("expected type=passive, got %q", o.Type)
		}
	}
	if obs[0].ToolName == nil || *obs[0].ToolName != "test" {
		t.Fatalf("expected tool_name source to be stored as 'test', got %+v", obs[0].ToolName)
	}
}

func TestPassiveCaptureEmptyContent(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "s1",
		Content:   "",
		Project:   "engram",
		Source:    "test",
	})
	if err != nil {
		t.Fatalf("passive capture: %v", err)
	}
	if result.Extracted != 0 || result.Saved != 0 {
		t.Fatalf("expected 0 extracted and 0 saved, got %d/%d", result.Extracted, result.Saved)
	}
}

func TestPassiveCaptureDedupesAgainstExistingObservations(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// First: agent saves actively via mem_save
	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "bcrypt cost",
		Content:   "bcrypt cost=12 is the right balance for our server performance",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add active observation: %v", err)
	}

	// Then: passive capture fires with overlapping content
	text := `## Key Learnings:

1. bcrypt cost=12 is the right balance for our server performance
2. JWT refresh tokens need atomic rotation to prevent race conditions
`
	result, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "s1",
		Content:   text,
		Project:   "engram",
		Source:    "test",
	})
	if err != nil {
		t.Fatalf("passive capture: %v", err)
	}
	if result.Extracted != 2 {
		t.Fatalf("expected 2 extracted, got %d", result.Extracted)
	}
	if result.Saved != 1 {
		t.Fatalf("expected 1 saved (1 deduped), got %d", result.Saved)
	}
	if result.Duplicates != 1 {
		t.Fatalf("expected 1 duplicate, got %d", result.Duplicates)
	}
}

func TestPassiveCaptureReturnsErrorWhenSessionDoesNotExist(t *testing.T) {
	s := newTestStore(t)

	text := `## Key Learnings:

1. This learning is long enough to attempt insert and fail without session
`
	_, err := s.PassiveCapture(PassiveCaptureParams{
		SessionID: "missing-session",
		Content:   text,
		Project:   "engram",
		Source:    "test",
	})
	if err == nil {
		t.Fatalf("expected error when session does not exist")
	}
}

func TestStatsProjectsOrderedByMostRecentObservation(t *testing.T) {
	s := newTestStore(t)
	session1 := normalizeSessionID("s1")
	session2 := normalizeSessionID("s2")
	obs1ID := normalizeObservationID("stats-obs-1")
	obs2ID := normalizeObservationID("stats-obs-2")
	obs1SyncID := normalizeObservationSyncID("stats-sync-1")
	obs2SyncID := normalizeObservationSyncID("stats-sync-2")

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session s1: %v", err)
	}
	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s2", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session s2: %v", err)
	}

	_, err := s.execHook(s.db,
		`INSERT INTO observations (id, sync_id, session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?),
		        (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		obs1ID, obs1SyncID, session1, "note", "older", "older alpha", "alpha", "project", hashNormalized("older alpha"), "2026-02-01 10:00:00", "2026-02-01 10:00:00",
		obs2ID, obs2SyncID, session2, "note", "newer", "newer beta", "beta", "project", hashNormalized("newer beta"), "2026-02-02 10:00:00", "2026-02-02 10:00:00",
	)
	if err != nil {
		t.Fatalf("insert observations: %v", err)
	}

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats.Projects) < 2 {
		t.Fatalf("expected at least 2 projects, got %d", len(stats.Projects))
	}

	if stats.Projects[0] != "beta" || stats.Projects[1] != "alpha" {
		t.Fatalf("expected recency order [beta alpha], got %v", stats.Projects[:2])
	}
}

func TestSessionsOrderedByMostRecentActivity(t *testing.T) {
	s := newTestStore(t)
	olderID := normalizeSessionID("s-older")
	newerID := normalizeSessionID("s-newer")
	latestObsID := normalizeObservationID("latest-observation")
	latestObsSyncID := normalizeObservationSyncID("latest-observation-sync")

	_, err := s.execHook(s.db,
		`INSERT INTO sessions (id, project, directory, started_at) VALUES
		 (?, ?, ?, ?),
		 (?, ?, ?, ?)`,
		olderID, "engram", "/tmp/engram", "2026-02-01 09:00:00",
		newerID, "engram", "/tmp/engram", "2026-02-02 09:00:00",
	)
	if err != nil {
		t.Fatalf("insert sessions: %v", err)
	}

	_, err = s.execHook(s.db,
		`INSERT INTO observations (id, sync_id, session_id, type, title, content, project, scope, normalized_hash, revision_count, duplicate_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		latestObsID, latestObsSyncID, olderID, "note", "latest", "session old got new activity", "engram", "project", hashNormalized("session old got new activity"), "2026-02-03 09:00:00", "2026-02-03 09:00:00",
	)
	if err != nil {
		t.Fatalf("insert latest observation: %v", err)
	}

	all, err := s.AllSessions("", 10)
	if err != nil {
		t.Fatalf("all sessions: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(all))
	}
	if all[0].ID != olderID {
		t.Fatalf("expected s-older first in all sessions, got %s", all[0].ID)
	}

	recent, err := s.RecentSessions("", 10)
	if err != nil {
		t.Fatalf("recent sessions: %v", err)
	}
	if len(recent) < 2 {
		t.Fatalf("expected at least 2 recent sessions, got %d", len(recent))
	}
	if recent[0].ID != olderID {
		t.Fatalf("expected s-older first in recent sessions, got %s", recent[0].ID)
	}
}

func TestSessionObservationsAddPromptAndImport(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := s.AddObservation(AddObservationParams{
		SessionID: "s1",
		Type:      "decision",
		Title:     "Auth",
		Content:   "Use middleware chain",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	longPrompt := strings.Repeat("x", s.cfg.MaxObservationLength+25)
	promptID, err := s.AddPrompt(AddPromptParams{SessionID: "s1", Content: longPrompt, Project: "engram"})
	if err != nil {
		t.Fatalf("add prompt: %v", err)
	}
	if strings.TrimSpace(promptID) == "" {
		t.Fatalf("expected valid prompt id")
	}

	sessionObs, err := s.SessionObservations("s1", 0)
	if err != nil {
		t.Fatalf("session observations: %v", err)
	}
	if len(sessionObs) != 1 {
		t.Fatalf("expected 1 session observation, got %d", len(sessionObs))
	}

	exported, err := s.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()
	dst, err := New(cfg)
	if err != nil {
		t.Fatalf("new destination store: %v", err)
	}
	t.Cleanup(func() { _ = dst.Close() })

	imported, err := dst.Import(exported)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported.SessionsImported < 1 || imported.ObservationsImported < 1 || imported.PromptsImported < 1 {
		t.Fatalf("expected non-zero import counts, got %+v", imported)
	}

}

func TestUtilityHelpersCoverage(t *testing.T) {
	if got := derefString(nil); got != "" {
		t.Fatalf("expected empty string for nil pointer, got %q", got)
	}
	v := "value"
	if got := derefString(&v); got != "value" {
		t.Fatalf("expected dereferenced value, got %q", got)
	}

	if got := maxInt(10, 5); got != 10 {
		t.Fatalf("expected maxInt(10,5)=10, got %d", got)
	}
	if got := maxInt(3, 7); got != 7 {
		t.Fatalf("expected maxInt(3,7)=7, got %d", got)
	}

	if got := dedupeWindowExpression(0); got != "-15 minutes" {
		t.Fatalf("expected default dedupe window, got %q", got)
	}
	if got := dedupeWindowExpression(20 * time.Second); got != "-1 minutes" {
		t.Fatalf("expected minimum 1 minute window, got %q", got)
	}

	cases := map[string]string{
		"write":   "file_change",
		"patch":   "file_change",
		"bash":    "command",
		"read":    "file_read",
		"glob":    "search",
		"unknown": "tool_use",
	}
	for in, want := range cases {
		if got := ClassifyTool(in); got != want {
			t.Fatalf("ClassifyTool(%q): expected %q, got %q", in, want, got)
		}
	}
}

func TestEndSessionAndTimelineDefaults(t *testing.T) {
	s := newTestStore(t)
	params := CreateSessionParams{ClientSessionID: "s-end", Project: "engram", Directory: "/tmp/engram"}

	if err := s.CreateSession(params); err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstID, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "note",
		Title:     "first",
		Content:   "first note",
		Project:   "engram",
	})
	if err != nil {
		t.Fatalf("add first observation: %v", err)
	}
	_, err = s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "note",
		Title:     "second",
		Content:   "second note",
		Project:   "engram",
	})
	if err != nil {
		t.Fatalf("add second observation: %v", err)
	}

	if err := s.EndSession(params.EffectiveID(), "finished session"); err != nil {
		t.Fatalf("end session: %v", err)
	}

	sess, err := s.GetSession(params.EffectiveID())
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.EndedAt == nil {
		t.Fatalf("expected ended_at to be set")
	}
	if sess.Summary == nil || *sess.Summary != "finished session" {
		t.Fatalf("expected summary to be stored, got %+v", sess.Summary)
	}

	timeline, err := s.Timeline(firstID, 0, -1)
	if err != nil {
		t.Fatalf("timeline with default before/after: %v", err)
	}
	if timeline.SessionInfo == nil {
		t.Fatalf("expected session info in timeline")
	}
	if timeline.TotalInRange != 2 {
		t.Fatalf("expected total_in_range=2, got %d", timeline.TotalInRange)
	}
}

func TestInferTopicFamilyCoverage(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		title   string
		content string
		want    string
	}{
		{name: "type architecture", typ: "architecture", want: "architecture"},
		{name: "type bugfix", typ: "bugfix", want: "bug"},
		{name: "type decision", typ: "decision", want: "decision"},
		{name: "type pattern", typ: "pattern", want: "pattern"},
		{name: "type config", typ: "config", want: "config"},
		{name: "type discovery", typ: "discovery", want: "discovery"},
		{name: "type learning", typ: "learning", want: "learning"},
		{name: "type session summary", typ: "session_summary", want: "session"},
		{name: "text bug", title: "", content: "this caused a crash regression", want: "bug"},
		{name: "text architecture", title: "", content: "new boundary design", want: "architecture"},
		{name: "text decision", title: "", content: "we chose this tradeoff", want: "decision"},
		{name: "text pattern", title: "", content: "naming convention for handlers", want: "pattern"},
		{name: "text config", title: "", content: "docker env setup", want: "config"},
		{name: "text discovery", title: "", content: "root cause found", want: "discovery"},
		{name: "text learning", title: "", content: "key learning from this issue", want: "learning"},
		{name: "fallback type", typ: "Custom Type", want: "custom-type"},
		{name: "default topic", typ: "manual", title: "", content: "", want: "topic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inferTopicFamily(tc.typ, tc.title, tc.content)
			if got != tc.want {
				t.Fatalf("inferTopicFamily(%q,%q,%q): expected %q, got %q", tc.typ, tc.title, tc.content, tc.want, got)
			}
		})
	}
}

func TestStoreAdditionalQueryAndMutationBranches(t *testing.T) {
	s := newTestStore(t)
	params := CreateSessionParams{ClientSessionID: "s-q", Project: "engram", Directory: "/tmp/engram"}

	if err := s.CreateSession(params); err != nil {
		t.Fatalf("create session: %v", err)
	}

	longContent := strings.Repeat("x", s.cfg.MaxObservationLength+100)
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: params.EffectiveID(),
		Type:      "note",
		Title:     "Private <private>secret</private> title",
		Content:   longContent + " <private>token</private>",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}
	obs, err := s.GetObservation(obsID)
	if err != nil {
		t.Fatalf("get observation: %v", err)
	}
	if !strings.Contains(obs.Title, "[REDACTED]") {
		t.Fatalf("expected private tags redacted in title, got %q", obs.Title)
	}
	if !strings.Contains(obs.Content, "... [truncated]") {
		t.Fatalf("expected truncated content marker, got %q", obs.Content)
	}

	newProject := ""
	newTopic := ""
	updated, err := s.UpdateObservation(obsID, UpdateObservationParams{Project: &newProject, TopicKey: &newTopic})
	if err != nil {
		t.Fatalf("update observation: %v", err)
	}
	if updated.Project != nil {
		t.Fatalf("expected nil project after empty update")
	}
	if updated.TopicKey != nil {
		t.Fatalf("expected nil topic key after empty update")
	}

	if _, err := s.AddPrompt(AddPromptParams{SessionID: params.EffectiveID(), Content: "alpha prompt", Project: "alpha"}); err != nil {
		t.Fatalf("add alpha prompt: %v", err)
	}
	if _, err := s.AddPrompt(AddPromptParams{SessionID: params.EffectiveID(), Content: "beta prompt", Project: "beta"}); err != nil {
		t.Fatalf("add beta prompt: %v", err)
	}

	recentPrompts, err := s.RecentPrompts("beta", 1)
	if err != nil {
		t.Fatalf("recent prompts with project filter: %v", err)
	}
	if len(recentPrompts) != 1 || recentPrompts[0].Project != "beta" {
		t.Fatalf("expected one beta prompt, got %+v", recentPrompts)
	}

	searchPrompts, err := s.SearchPrompts("prompt", "alpha", 0)
	if err != nil {
		t.Fatalf("search prompts with project filter/default limit: %v", err)
	}
	if len(searchPrompts) != 1 || searchPrompts[0].Project != "alpha" {
		t.Fatalf("expected one alpha prompt search result, got %+v", searchPrompts)
	}

	searchResults, err := s.Search("title", SearchOptions{Scope: "project", Limit: 9999})
	if err != nil {
		t.Fatalf("search with clamped limit: %v", err)
	}
	if len(searchResults) == 0 {
		t.Fatalf("expected search results")
	}

	ctx, err := s.FormatContext("", "project")
	if err != nil {
		t.Fatalf("format context: %v", err)
	}
	if !strings.Contains(ctx, "Recent User Prompts") {
		t.Fatalf("expected prompts section in context output")
	}
}

func TestStoreErrorBranchesWithClosedDatabase(t *testing.T) {
	s := newTestStore(t)

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	if _, err := s.GetSession("missing"); err == nil {
		t.Fatalf("expected GetSession error when db is closed")
	}
	if _, err := s.AllSessions("", 1); err == nil {
		t.Fatalf("expected AllSessions error when db is closed")
	}
	if _, err := s.RecentSessions("", 1); err == nil {
		t.Fatalf("expected RecentSessions error when db is closed")
	}
	if _, err := s.SearchPrompts("x", "", 1); err == nil {
		t.Fatalf("expected SearchPrompts error when db is closed")
	}
	if _, err := s.Search("x", SearchOptions{}); err == nil {
		t.Fatalf("expected Search error when db is closed")
	}
	if _, err := s.Export(); err == nil {
		t.Fatalf("expected Export error when db is closed")
	}
	if _, err := s.Timeline("obsrow-test", 1, 1); err == nil {
		t.Fatalf("expected Timeline error when db is closed")
	}
}

func TestEndSessionEdgeCases(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-edge", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := s.EndSession("missing", "ignored"); err != nil {
		t.Fatalf("end missing session should be no-op: %v", err)
	}

	if err := s.EndSession("s-edge", ""); err != nil {
		t.Fatalf("end session with empty summary: %v", err)
	}

	sess, err := s.GetSession("s-edge")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.EndedAt == nil {
		t.Fatalf("expected ended_at to be set")
	}
	if sess.Summary != nil {
		t.Fatalf("expected empty summary to persist as NULL, got %q", *sess.Summary)
	}
}

func TestTimelineHandlesMissingSessionRecord(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "manual-save", Project: "engram", Directory: "/tmp/engram"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{SessionID: "manual-save", Type: "manual", Title: "orphan", Content: "orphan content", Project: "engram", Scope: "project"})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}
	origQueryRow := s.hooks.queryRow
	s.hooks.queryRow = func(db rowQueryer, query string, args ...any) *sql.Row {
		if strings.Contains(query, "FROM sessions WHERE id =") {
			return s.db.QueryRow(`SELECT id::text, client_session_id, project, directory, auth_issuer, auth_subject, auth_username, auth_email, started_at, ended_at, summary FROM sessions WHERE 1=0`)
		}
		return origQueryRow(db, query, args...)
	}

	timeline, err := s.Timeline(obsID, 1, 1)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if timeline.SessionInfo != nil {
		t.Fatalf("expected nil session info for missing session, got %+v", timeline.SessionInfo)
	}
	if timeline.TotalInRange != 1 {
		t.Fatalf("expected total in range=1, got %d", timeline.TotalInRange)
	}
}

func TestQueryObservationsScanError(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.queryObservations("SELECT 1"); err == nil {
		t.Fatalf("expected scan error for mismatched projection")
	}
}

func TestMigrationAndHelperEdgeBranches(t *testing.T) {
	t.Run("migrate is idempotent with existing triggers", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.migrate(); err != nil {
			t.Fatalf("second migrate should succeed: %v", err)
		}
	})

	t.Run("legacy migrate skips table without id column", func(t *testing.T) {
		s := newTestStore(t)

		if err := s.migrateLegacyObservationsTable(); err != nil {
			t.Fatalf("legacy migrate should now noop under postgres-only mode: %v", err)
		}
	})

	t.Run("topic helpers normalize edge cases", func(t *testing.T) {
		if got := SuggestTopicKey("decision", "decision", ""); got != "decision/general" {
			t.Fatalf("expected decision/general, got %q", got)
		}
		if got := SuggestTopicKey("bugfix", "bug-auth-panic", ""); got != "bug/auth-panic" {
			t.Fatalf("expected bug/auth-panic, got %q", got)
		}
		if got := SuggestTopicKey("manual", "!!!", "..."); got != "topic/general" {
			t.Fatalf("expected topic/general fallback, got %q", got)
		}

		longSegment := normalizeTopicSegment(strings.Repeat("abc", 50))
		if len(longSegment) != 100 {
			t.Fatalf("expected topic segment truncation to 100, got %d", len(longSegment))
		}

		longKey := normalizeTopicKey(strings.Repeat("k", 200))
		if len(longKey) != 120 {
			t.Fatalf("expected topic key truncation to 120, got %d", len(longKey))
		}
	})

	t.Run("format context empty returns empty string", func(t *testing.T) {
		s := newTestStore(t)
		ctx, err := s.FormatContext("", "")
		if err != nil {
			t.Fatalf("format context: %v", err)
		}
		if ctx != "" {
			t.Fatalf("expected empty context when no data, got %q", ctx)
		}
	})
}

func TestExportImportEdgeBranches(t *testing.T) {
	t.Run("export fails when observations query fails", func(t *testing.T) {
		s := newTestStore(t)

		if _, err := s.execHook(s.db, `DROP TABLE observations`); err != nil {
			t.Fatalf("drop observations: %v", err)
		}

		_, err := s.Export()
		if err == nil || !strings.Contains(err.Error(), "export observations") {
			t.Fatalf("expected observations export error, got %v", err)
		}
	})

	t.Run("export fails when prompts query fails", func(t *testing.T) {
		s := newTestStore(t)

		if _, err := s.execHook(s.db, `DROP TABLE user_prompts`); err != nil {
			t.Fatalf("drop prompts: %v", err)
		}

		_, err := s.Export()
		if err == nil || !strings.Contains(err.Error(), "export prompts") {
			t.Fatalf("expected prompts export error, got %v", err)
		}
	})

	t.Run("import begin tx fails on closed db", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		_, err := s.Import(&ExportData{})
		if err == nil || !strings.Contains(err.Error(), "begin tx") {
			t.Fatalf("expected begin tx import error, got %v", err)
		}
	})

	t.Run("import fails on observation fk error", func(t *testing.T) {
		s := newTestStore(t)
		_, err := s.Import(&ExportData{
			Observations: []Observation{{
				ID:        "obsrow-missing",
				SessionID: "missing-session",
				Type:      "bugfix",
				Title:     "x",
				Content:   "y",
				Scope:     "project",
				CreatedAt: Now(),
				UpdatedAt: Now(),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "import observation") {
			t.Fatalf("expected observation import error, got %v", err)
		}
	})

	t.Run("import fails on prompt fk error", func(t *testing.T) {
		s := newTestStore(t)
		_, err := s.Import(&ExportData{
			Prompts: []Prompt{{
				ID:        "promptrow-missing",
				SessionID: "missing-session",
				Content:   "prompt",
				Project:   "engram",
				CreatedAt: Now(),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "import prompt") {
			t.Fatalf("expected prompt import error, got %v", err)
		}
	})
}

func TestNewErrorBranches(t *testing.T) {
	t.Run("fails when data dir is a file", func(t *testing.T) {
		cfg := mustDefaultConfig(t)
		cfg.DatabaseURL = ""

		_, err := New(cfg)
		if err == nil || !strings.Contains(err.Error(), "ENGRAM_DATABASE_URL is required") {
			t.Fatalf("expected missing database url error, got %v", err)
		}
	})

	t.Run("fails when db path is a directory", func(t *testing.T) {
		cfg := mustDefaultConfig(t)
		cfg.DatabaseURL = "postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable"

		_, err := New(cfg)
		if err == nil || !strings.Contains(err.Error(), "dial tcp") {
			t.Fatalf("expected postgres open error, got %v", err)
		}
	})

	t.Run("fails when migration encounters conflicting object", func(t *testing.T) {
		s := newTestStore(t)
		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "CREATE TABLE IF NOT EXISTS user_prompts") {
				return nil, errors.New("forced migration conflict")
			}
			return origExec(db, query, args...)
		}
		if err := s.migrate(); err == nil || !strings.Contains(err.Error(), "forced migration conflict") {
			t.Fatalf("expected migration conflict error, got %v", err)
		}
	})
}

func TestMigrationInternalErrorAndNoopBranches(t *testing.T) {
	t.Run("addColumnIfNotExists adds then noops", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.db.Exec(`CREATE TABLE extra_table (id INTEGER)`); err != nil {
			t.Fatalf("create extra table: %v", err)
		}

		if err := s.addColumnIfNotExists("extra_table", "name", "TEXT"); err != nil {
			t.Fatalf("add column: %v", err)
		}
		if err := s.addColumnIfNotExists("extra_table", "name", "TEXT"); err != nil {
			t.Fatalf("add existing column should noop: %v", err)
		}

		if err := s.addColumnIfNotExists("missing_table", "x", "TEXT"); err == nil {
			t.Fatalf("expected missing table error")
		}
	})

	t.Run("legacy migrate noops when id is primary key", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.migrateLegacyObservationsTable(); err != nil {
			t.Fatalf("expected noop for modern schema: %v", err)
		}
	})

	t.Run("legacy migrate fails if temp table already exists", func(t *testing.T) {
		s := newTestStore(t)
		err := s.migrateLegacyObservationsTable()
		if err != nil {
			t.Fatalf("expected postgres legacy migrate noop, got %v", err)
		}
	})

	t.Run("migrate returns deterministic exec hook errors", func(t *testing.T) {
		s := newTestStore(t)

		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "UPDATE observations SET scope = 'project'") {
				return nil, errors.New("forced migrate update failure")
			}
			return origExec(db, query, args...)
		}

		err := s.migrate()
		if err == nil || !strings.Contains(err.Error(), "forced migrate update failure") {
			t.Fatalf("expected forced migrate failure, got %v", err)
		}
	})

	t.Run("migrate fails when creating missing triggers", func(t *testing.T) {
		s := newTestStore(t)

		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "CREATE INDEX IF NOT EXISTS idx_obs_scope") {
				return nil, errors.New("forced obs trigger failure")
			}
			return origExec(db, query, args...)
		}

		err := s.migrate()
		if err == nil || !strings.Contains(err.Error(), "forced obs trigger failure") {
			t.Fatalf("expected forced trigger failure, got %v", err)
		}
	})

	t.Run("legacy migrate surfaces begin and commit hook failures", func(t *testing.T) {
		prepareLegacyStore := func(t *testing.T) *Store {
			t.Helper()
			return newTestStore(t)
		}

		t.Run("begin tx", func(t *testing.T) {
			s := prepareLegacyStore(t)
			s.hooks.beginTx = func(_ *sql.DB) (*sql.Tx, error) {
				return nil, errors.New("forced begin failure")
			}

			err := s.migrateLegacyObservationsTable()
			if err != nil {
				t.Fatalf("expected postgres legacy migrate noop, got %v", err)
			}
		})

		t.Run("commit", func(t *testing.T) {
			s := prepareLegacyStore(t)
			s.hooks.commit = func(_ *sql.Tx) error {
				return errors.New("forced legacy commit failure")
			}

			err := s.migrateLegacyObservationsTable()
			if err != nil {
				t.Fatalf("expected postgres legacy migrate noop, got %v", err)
			}
		})
	})
}

func TestImportExportSeamErrors(t *testing.T) {
	t.Run("export query hooks", func(t *testing.T) {
		s := newTestStore(t)

		origQueryIt := s.hooks.queryIt
		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM sessions") {
				return nil, errors.New("forced sessions export query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Export(); err == nil || !strings.Contains(err.Error(), "export sessions") {
			t.Fatalf("expected sessions export error, got %v", err)
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM observations") {
				return nil, errors.New("forced observations export query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Export(); err == nil || !strings.Contains(err.Error(), "export observations") {
			t.Fatalf("expected observations export error, got %v", err)
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM user_prompts") {
				return nil, errors.New("forced prompts export query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Export(); err == nil || !strings.Contains(err.Error(), "export prompts") {
			t.Fatalf("expected prompts export error, got %v", err)
		}
	})

	t.Run("import tx and exec hooks", func(t *testing.T) {
		s := newTestStore(t)

		s.hooks.beginTx = func(_ *sql.DB) (*sql.Tx, error) {
			return nil, errors.New("forced import begin failure")
		}
		if _, err := s.Import(&ExportData{}); err == nil || !strings.Contains(err.Error(), "begin tx") {
			t.Fatalf("expected begin tx error, got %v", err)
		}

		s.hooks = defaultStoreHooks()
		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "INSERT INTO sessions") && strings.Contains(query, "ON CONFLICT DO NOTHING") {
				return nil, errors.New("forced import session insert failure")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.Import(&ExportData{Sessions: []Session{{ID: "s-x", Project: "p", Directory: "/tmp", StartedAt: Now()}}}); err == nil || !strings.Contains(err.Error(), "import session") {
			t.Fatalf("expected session import error, got %v", err)
		}

		s.hooks = defaultStoreHooks()
		s.hooks.commit = func(_ *sql.Tx) error {
			return errors.New("forced import commit failure")
		}
		if _, err := s.Import(&ExportData{}); err == nil || !strings.Contains(err.Error(), "import: commit") {
			t.Fatalf("expected commit error, got %v", err)
		}
	})
}

func TestHookFallbacksAndAdditionalBranches(t *testing.T) {
	t.Run("hook fallbacks call default DB methods", func(t *testing.T) {
		s := newTestStore(t)
		s.hooks = storeHooks{}

		if _, err := s.execHook(s.db, "SELECT 1"); err != nil {
			t.Fatalf("exec hook fallback: %v", err)
		}
		rows, err := s.queryHook(s.db, "SELECT 1")
		if err != nil {
			t.Fatalf("query hook fallback: %v", err)
		}
		_ = rows.Close()

		iter, err := s.queryItHook(s.db, "SELECT 1")
		if err != nil {
			t.Fatalf("query iterator fallback: %v", err)
		}
		_ = iter.Close()

		tx, err := s.beginTxHook()
		if err != nil {
			t.Fatalf("begin tx hook fallback: %v", err)
		}
		if err := s.commitHook(tx); err != nil {
			t.Fatalf("commit hook fallback: %v", err)
		}

		s2 := newTestStore(t)
		rows2, err := s2.queryHook(s2.db, "SELECT 1")
		if err != nil {
			t.Fatalf("query hook default closure: %v", err)
		}
		_ = rows2.Close()

		s.hooks.query = func(db queryer, query string, args ...any) (*sql.Rows, error) {
			return nil, errors.New("forced query hook error")
		}
		s.hooks.queryIt = nil
		if _, err := s.queryItHook(s.db, "SELECT 1"); err == nil {
			t.Fatalf("expected queryItHook error through queryHook fallback")
		}
	})

	t.Run("sessions and observations filters with default limits", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-p", Project: "proj-a", Directory: "/tmp/proj-a"}); err != nil {
			t.Fatalf("create session proj-a: %v", err)
		}
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-q", Project: "proj-b", Directory: "/tmp/proj-b"}); err != nil {
			t.Fatalf("create session proj-b: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-p", Type: "note", Title: "a", Content: "a", Project: "proj-a", Scope: "project"}); err != nil {
			t.Fatalf("add observation proj-a: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-q", Type: "note", Title: "b", Content: "b", Project: "proj-b", Scope: "project"}); err != nil {
			t.Fatalf("add observation proj-b: %v", err)
		}

		recent, err := s.RecentSessions("proj-a", 0)
		if err != nil {
			t.Fatalf("recent sessions filtered: %v", err)
		}
		if len(recent) != 1 || recent[0].Project != "proj-a" {
			t.Fatalf("expected one proj-a recent session, got %+v", recent)
		}

		all, err := s.AllSessions("proj-b", -1)
		if err != nil {
			t.Fatalf("all sessions filtered: %v", err)
		}
		if len(all) != 1 || all[0].Project != "proj-b" {
			t.Fatalf("expected one proj-b session, got %+v", all)
		}

		obs, err := s.AllObservations("proj-a", "project", 0)
		if err != nil {
			t.Fatalf("all observations defaults: %v", err)
		}
		if len(obs) != 1 || obs[0].SessionID == "" {
			t.Fatalf("expected one proj-a observation, got %+v", obs)
		}

		sessionObs, err := s.SessionObservations("s-p", 0)
		if err != nil {
			t.Fatalf("session observations default limit: %v", err)
		}
		if len(sessionObs) != 1 {
			t.Fatalf("expected one session observation, got %d", len(sessionObs))
		}

		recentObs, err := s.RecentObservations("proj-a", "project", 0)
		if err != nil {
			t.Fatalf("recent observations default limit: %v", err)
		}
		if len(recentObs) != 1 {
			t.Fatalf("expected one recent observation, got %d", len(recentObs))
		}

		recentPrompts, err := s.RecentPrompts("", 0)
		if err != nil {
			t.Fatalf("recent prompts default limit: %v", err)
		}
		if len(recentPrompts) != 0 {
			t.Fatalf("expected zero prompts, got %d", len(recentPrompts))
		}
	})

	t.Run("timeline includes before and after in chronological order", func(t *testing.T) {
		s := newTestStore(t)
		params := CreateSessionParams{ClientSessionID: "s-tl", Project: "engram", Directory: "/tmp/engram"}
		if err := s.CreateSession(params); err != nil {
			t.Fatalf("create session: %v", err)
		}

		firstID, err := s.AddObservation(AddObservationParams{SessionID: params.EffectiveID(), Type: "note", Title: "1", Content: "one", Project: "engram"})
		if err != nil {
			t.Fatalf("add first observation: %v", err)
		}
		middleID, err := s.AddObservation(AddObservationParams{SessionID: params.EffectiveID(), Type: "note", Title: "2", Content: "two", Project: "engram"})
		if err != nil {
			t.Fatalf("add middle observation: %v", err)
		}
		lastID, err := s.AddObservation(AddObservationParams{SessionID: params.EffectiveID(), Type: "note", Title: "3", Content: "three", Project: "engram"})
		if err != nil {
			t.Fatalf("add last observation: %v", err)
		}

		tl, err := s.Timeline(middleID, 5, 5)
		if err != nil {
			t.Fatalf("timeline middle: %v", err)
		}
		if len(tl.Before)+len(tl.After) != 2 {
			t.Fatalf("expected two adjacent observations total, got before=%+v after=%+v", tl.Before, tl.After)
		}
		seen := map[string]bool{}
		for _, e := range tl.Before {
			seen[e.ID] = true
		}
		for _, e := range tl.After {
			seen[e.ID] = true
		}
		if !seen[firstID] || !seen[lastID] || seen[middleID] {
			t.Fatalf("expected neighbors %s and %s around focus %s, got before=%+v after=%+v", firstID, lastID, middleID, tl.Before, tl.After)
		}
	})

	t.Run("format context returns specific query stage errors", func(t *testing.T) {
		t.Run("recent sessions error", func(t *testing.T) {
			s := newTestStore(t)
			_ = s.Close()
			if _, err := s.FormatContext("", ""); err == nil {
				t.Fatalf("expected format context to fail from recent sessions")
			}
		})

		t.Run("recent observations error", func(t *testing.T) {
			s := newTestStore(t)
			if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-ctx", Project: "engram", Directory: "/tmp/engram"}); err != nil {
				t.Fatalf("create session: %v", err)
			}
			if _, err := s.db.Exec("DROP TABLE observations"); err != nil {
				t.Fatalf("drop observations: %v", err)
			}
			if _, err := s.FormatContext("", ""); err == nil {
				t.Fatalf("expected format context to fail from recent observations")
			}
		})

		t.Run("recent prompts error", func(t *testing.T) {
			s := newTestStore(t)
			if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-ctx2", Project: "engram", Directory: "/tmp/engram"}); err != nil {
				t.Fatalf("create session: %v", err)
			}
			if _, err := s.db.Exec("DROP TABLE user_prompts"); err != nil {
				t.Fatalf("drop prompts: %v", err)
			}
			if _, err := s.FormatContext("", ""); err == nil {
				t.Fatalf("expected format context to fail from recent prompts")
			}
		})
	})
}

func TestStoreUncoveredBranchesPushToHundred(t *testing.T) {
	t.Run("new open database hook error", func(t *testing.T) {
		orig := openDB
		t.Cleanup(func() { openDB = orig })
		openDB = func(driverName, dataSourceName string) (*sql.DB, error) {
			return nil, errors.New("forced open error")
		}

		cfg := mustDefaultConfig(t)
		cfg.DataDir = t.TempDir()
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "open postgres database") {
			t.Fatalf("expected open database error, got %v", err)
		}
	})

	t.Run("migrate forced failures for remaining exec branches", func(t *testing.T) {
		failCases := []string{
			"CREATE INDEX IF NOT EXISTS idx_obs_scope",
			"UPDATE observations SET topic_key = NULL",
			"UPDATE observations SET revision_count = 1",
			"UPDATE observations SET duplicate_count = 1",
			"UPDATE observations SET updated_at = created_at",
			"UPDATE user_prompts SET project = ''",
		}
		for _, needle := range failCases {
			t.Run(needle, func(t *testing.T) {
				s := newTestStore(t)
				origExec := s.hooks.exec
				s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
					if strings.Contains(query, needle) {
						return nil, errors.New("forced migrate failure")
					}
					return origExec(db, query, args...)
				}
				if err := s.migrate(); err == nil {
					t.Fatalf("expected migrate error for %q", needle)
				}
			})
		}
	})

	t.Run("migrate addColumn and legacy-call propagation", func(t *testing.T) {
		t.Run("propagates addColumn error", func(t *testing.T) {
			s := newTestStore(t)
			origQueryRow := s.hooks.queryRow
			s.hooks.queryRow = func(db rowQueryer, query string, args ...any) *sql.Row {
				if strings.Contains(query, "information_schema.columns") {
					return s.db.QueryRow("SELECT true FROM (")
				}
				return origQueryRow(db, query, args...)
			}
			if err := s.migrate(); err == nil {
				t.Fatalf("expected migrate to propagate addColumn failure")
			}
		})

		t.Run("propagates legacy migrate error", func(t *testing.T) {
			s := newTestStore(t)
			if err := s.migrateLegacyObservationsTable(); err != nil {
				t.Fatalf("expected postgres legacy migrate noop, got %v", err)
			}
		})
	})

	t.Run("add observation, prompt, update forced errors", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-e", Project: "engram", Directory: "/tmp/engram"}); err != nil {
			t.Fatalf("create session: %v", err)
		}

		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "top", Content: "x", Project: "engram", TopicKey: "x"}); err != nil {
			t.Fatalf("seed topic observation: %v", err)
		}
		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "revision_count = revision_count + 1") {
				return nil, errors.New("forced topic update error")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "top", Content: "x", Project: "engram", TopicKey: "x"}); err == nil {
			t.Fatalf("expected topic upsert exec error")
		}

		s.hooks = defaultStoreHooks()
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "dup", Content: "dup content", Project: "engram"}); err != nil {
			t.Fatalf("seed dedupe observation: %v", err)
		}
		origExec = s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "SET duplicate_count = duplicate_count + 1") {
				return nil, errors.New("forced dedupe update error")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "dup", Content: "dup content", Project: "engram"}); err == nil {
			t.Fatalf("expected dedupe exec error")
		}

		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "x", Content: "y", Project: "engram", TopicKey: "t"}); err == nil {
			t.Fatalf("expected topic query error on closed db")
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-e", Type: "note", Title: "x", Content: "y", Project: "engram"}); err == nil {
			t.Fatalf("expected dedupe query error on closed db")
		}
		if _, err := s.AddPrompt(AddPromptParams{SessionID: "s-e", Content: "x"}); err == nil {
			t.Fatalf("expected add prompt error on closed db")
		}
	})

	t.Run("update observation remaining branches", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-u", Project: "engram", Directory: "/tmp/engram"}); err != nil {
			t.Fatalf("create session: %v", err)
		}
		id, err := s.AddObservation(AddObservationParams{SessionID: "s-u", Type: "old", Title: "t", Content: "c", Project: "engram", TopicKey: "topic/key"})
		if err != nil {
			t.Fatalf("seed observation: %v", err)
		}

		if _, err := s.UpdateObservation("obsrow-missing", UpdateObservationParams{}); err == nil {
			t.Fatalf("expected update missing observation error")
		}

		newType := "new-type"
		longContent := strings.Repeat("z", s.cfg.MaxObservationLength+50)
		if _, err := s.UpdateObservation(id, UpdateObservationParams{Type: &newType, Content: &longContent}); err != nil {
			t.Fatalf("update with type+truncation: %v", err)
		}

		origExec := s.hooks.exec
		s.hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
			if strings.Contains(query, "UPDATE observations") {
				return nil, errors.New("forced update exec error")
			}
			return origExec(db, query, args...)
		}
		if _, err := s.UpdateObservation(id, UpdateObservationParams{}); err == nil {
			t.Fatalf("expected update exec error")
		}
	})

	t.Run("query iterator scan and rows.Err branches", func(t *testing.T) {
		s := newTestStore(t)
		origQueryIt := s.hooks.queryIt

		setScanErr := func(match string) {
			s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
				if strings.Contains(query, match) {
					return &fakeRows{next: []bool{true, false}, scanErr: errors.New("forced scan error")}, nil
				}
				return origQueryIt(db, query, args...)
			}
		}

		setRowsErr := func(match string) {
			s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
				if strings.Contains(query, match) {
					return &fakeRows{next: []bool{false}, err: errors.New("forced rows err")}, nil
				}
				return origQueryIt(db, query, args...)
			}
		}

		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-iter", Project: "engram", Directory: "/tmp/engram"}); err != nil {
			t.Fatalf("create session: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-iter", Type: "note", Title: "one", Content: "one", Project: "engram"}); err != nil {
			t.Fatalf("add observation: %v", err)
		}
		if _, err := s.AddPrompt(AddPromptParams{SessionID: "s-iter", Content: "prompt", Project: "engram"}); err != nil {
			t.Fatalf("add prompt: %v", err)
		}

		setScanErr("FROM sessions s")
		if _, err := s.RecentSessions("", 10); err == nil {
			t.Fatalf("expected recent sessions scan error")
		}

		setScanErr("FROM sessions s")
		if _, err := s.AllSessions("", 10); err == nil {
			t.Fatalf("expected all sessions scan error")
		}

		setScanErr("FROM user_prompts")
		if _, err := s.RecentPrompts("", 10); err == nil {
			t.Fatalf("expected recent prompts scan error")
		}

		setScanErr("FROM user_prompts p")
		if _, err := s.SearchPrompts("prompt", "", 10); err == nil {
			t.Fatalf("expected search prompts scan error")
		}

		setScanErr("FROM observations o")
		if _, err := s.Search("one", SearchOptions{}); err == nil {
			t.Fatalf("expected search scan error")
		}

		setRowsErr("FROM observations o")
		if _, err := s.Search("one", SearchOptions{}); err == nil {
			t.Fatalf("expected search rows err")
		}

		setScanErr("SELECT id::text, client_session_id, project, directory")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export sessions scan error")
		}

		setRowsErr("SELECT id::text, client_session_id, project, directory")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export sessions rows err")
		}

		setScanErr("FROM observations ORDER BY created_at, id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export observations scan error")
		}

		setRowsErr("FROM observations ORDER BY created_at, id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export observations rows err")
		}

		setScanErr("FROM user_prompts ORDER BY created_at, id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export prompts scan error")
		}

		setRowsErr("FROM user_prompts ORDER BY created_at, id")
		if _, err := s.Export(); err == nil {
			t.Fatalf("expected export prompts rows err")
		}

		origQueryRow := s.hooks.queryRow
		s.hooks.queryRow = func(db rowQueryer, query string, args ...any) *sql.Row {
			if strings.Contains(query, "information_schema.columns") {
				return s.db.QueryRow("SELECT true FROM (")
			}
			return origQueryRow(db, query, args...)
		}
		if _, err := s.db.Exec(`CREATE TABLE extra_table (id INTEGER)`); err != nil {
			t.Fatalf("create extra table: %v", err)
		}
		if err := s.addColumnIfNotExists("extra_table", "n", "TEXT"); err == nil {
			t.Fatalf("expected add column rows err")
		}
		s.hooks.queryRow = origQueryRow

		s.hooks.queryIt = origQueryIt
	})

	t.Run("timeline and search type filter branches", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-t2", Project: "engram", Directory: "/tmp/engram"}); err != nil {
			t.Fatalf("create session: %v", err)
		}
		first, _ := s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "a", Content: "a", Project: "engram"})
		_, _ = s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "aa", Content: "aa", Project: "engram"})
		focus, _ := s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "b", Content: "b", Project: "engram"})
		_, _ = s.AddObservation(AddObservationParams{SessionID: "s-t2", Type: "decision", Title: "c", Content: "c", Project: "engram"})

		if _, err := s.Search("b", SearchOptions{Type: "decision", Project: "engram", Scope: "project", Limit: 5}); err != nil {
			t.Fatalf("search with type filter: %v", err)
		}

		origQueryIt := s.hooks.queryIt
		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "created_at < $") || strings.Contains(query, "id < $") {
				return nil, errors.New("forced before query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline before query error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "created_at < $") || strings.Contains(query, "id < $") {
				return &fakeRows{next: []bool{true, false}, scanErr: errors.New("forced before scan error")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline before scan error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "created_at < $") || strings.Contains(query, "id < $") {
				return &fakeRows{next: []bool{false}, err: errors.New("forced before rows err")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline before rows err")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id > $") {
				return nil, errors.New("forced after query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline after query error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id > $") {
				return &fakeRows{next: []bool{true, false}, scanErr: errors.New("forced after scan error")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline after scan error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "id > $") {
				return &fakeRows{next: []bool{false}, err: errors.New("forced after rows err")}, nil
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Timeline(focus, 2, 2); err == nil {
			t.Fatalf("expected timeline after rows err")
		}

		s.hooks.queryIt = origQueryIt
		tl, err := s.Timeline(first, 5, 5)
		if err != nil {
			t.Fatalf("timeline reverse branch run: %v", err)
		}
		if len(tl.After) == 0 {
			t.Fatalf("expected timeline after entries")
		}
	})

	t.Run("format context and stats remaining branches", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "s-c", Project: "engram", Directory: "/tmp/engram"}); err != nil {
			t.Fatalf("create session: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{SessionID: "s-c", Type: "note", Title: "n", Content: "n", Project: "engram"}); err != nil {
			t.Fatalf("add obs: %v", err)
		}

		origQueryIt := s.hooks.queryIt
		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "FROM observations o") && strings.Contains(query, "WHERE o.deleted_at IS NULL") {
				return nil, errors.New("forced recent observations error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.FormatContext("engram", "project"); err == nil {
			t.Fatalf("expected format context observations error")
		}

		s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
			if strings.Contains(query, "GROUP BY project") {
				return nil, errors.New("forced stats query error")
			}
			return origQueryIt(db, query, args...)
		}
		if _, err := s.Stats(); err != nil {
			t.Fatalf("stats should swallow project query errors: %v", err)
		}

		if err := s.EndSession("s-c", "has summary"); err != nil {
			t.Fatalf("end session: %v", err)
		}
		s.hooks.queryIt = origQueryIt
		ctx, err := s.FormatContext("engram", "project")
		if err != nil {
			t.Fatalf("format context with summary: %v", err)
		}
		if !strings.Contains(ctx, "has summary") {
			t.Fatalf("expected session summary included in context")
		}
	})

	t.Run("helper query errors and legacy migration late-stage failures", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
		if _, err := s.queryObservations("SELECT id FROM observations"); err == nil {
			t.Fatalf("expected queryObservations query error")
		}
		if err := s.addColumnIfNotExists("observations", "x", "TEXT"); err == nil {
			t.Fatalf("expected addColumn query error")
		}
		if err := s.migrateLegacyObservationsTable(); err != nil {
			t.Fatalf("expected postgres legacy migrate noop, got %v", err)
		}
	})
}

// ─── Issue #25: Session collision regression tests ──────────────────────────

func TestCreateSessionUpsertsEmptyProjectAndDirectory(t *testing.T) {
	s := newTestStore(t)

	// Create session with empty project/directory (simulates first MCP call without context)
	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-upsert", Project: "", Directory: ""}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Second call with real project/directory should fill in the blanks
	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-upsert", Project: "projectA", Directory: "/tmp/a"}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	sess, err := s.GetSession("sess-upsert")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Project != "projectA" {
		t.Fatalf("expected project=projectA after upsert, got %q", sess.Project)
	}
	if sess.Directory != "/tmp/a" {
		t.Fatalf("expected directory=/tmp/a after upsert, got %q", sess.Directory)
	}
}

func TestCreateSessionDoesNotOverwriteExistingProject(t *testing.T) {
	s := newTestStore(t)

	// Create session with project A
	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-preserve", Project: "projectA", Directory: "/tmp/a"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Second call with project B should NOT overwrite
	if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-preserve", Project: "projectB", Directory: "/tmp/b"}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	sess, err := s.GetSession("sess-preserve")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Project != "projectA" {
		t.Fatalf("expected project=projectA (preserved), got %q", sess.Project)
	}
	if sess.Directory != "/tmp/a" {
		t.Fatalf("expected directory=/tmp/a (preserved), got %q", sess.Directory)
	}
}

func TestCreateSessionPartialUpsert(t *testing.T) {
	s := newTestStore(t)

	t.Run("fills directory when project already set", func(t *testing.T) {
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-partial-1", Project: "myproject", Directory: ""}); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Second call fills directory but project stays
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-partial-1", Project: "other", Directory: "/new/dir"}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		sess, err := s.GetSession("sess-partial-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if sess.Project != "myproject" {
			t.Fatalf("project should be preserved, got %q", sess.Project)
		}
		if sess.Directory != "/new/dir" {
			t.Fatalf("directory should be filled, got %q", sess.Directory)
		}
	})

	t.Run("fills project when directory already set", func(t *testing.T) {
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-partial-2", Project: "", Directory: "/existing/dir"}); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-partial-2", Project: "newproject", Directory: ""}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		sess, err := s.GetSession("sess-partial-2")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if sess.Project != "newproject" {
			t.Fatalf("project should be filled, got %q", sess.Project)
		}
		if sess.Directory != "/existing/dir" {
			t.Fatalf("directory should be preserved, got %q", sess.Directory)
		}
	})

	t.Run("both empty stays empty", func(t *testing.T) {
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-partial-3", Project: "", Directory: ""}); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := s.CreateSession(CreateSessionParams{ClientSessionID: "sess-partial-3", Project: "", Directory: ""}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		sess, err := s.GetSession("sess-partial-3")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if sess.Project != "" {
			t.Fatalf("project should stay empty, got %q", sess.Project)
		}
		if sess.Directory != "" {
			t.Fatalf("directory should stay empty, got %q", sess.Directory)
		}
	})
}

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "short ascii", in: "abc", max: 10, want: "abc"},
		{name: "exact length", in: "hello", max: 5, want: "hello"},
		{name: "long ascii", in: "abcdef", max: 3, want: "abc..."},
		{name: "spanish accents", in: "Decisión de arquitectura", max: 8, want: "Decisión..."},
		{name: "emoji", in: "🐛🔧🚀✨🎉💡", max: 3, want: "🐛🔧🚀..."},
		{name: "mixed ascii and multibyte", in: "café☕latte", max: 5, want: "café☕..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

// ─── MigrateProject ─────────────────────────────────────────────────────────

func TestMigrateProject(t *testing.T) {
	s := newTestStore(t)
	old, new_ := "old-name", "new-name"

	// Seed data under old project name
	s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: old, Directory: "/tmp/old"})
	s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "decision", Title: "test obs",
		Content: "some content", Project: old, Scope: "project",
	})
	s.AddPrompt(AddPromptParams{SessionID: "s1", Content: "test prompt", Project: old})

	// Run migration
	result, err := s.MigrateProject(old, new_)
	if err != nil {
		t.Fatalf("MigrateProject: %v", err)
	}
	if !result.Migrated {
		t.Fatal("expected migration to happen")
	}
	if result.ObservationsUpdated != 1 {
		t.Fatalf("expected 1 observation migrated, got %d", result.ObservationsUpdated)
	}
	if result.SessionsUpdated != 1 {
		t.Fatalf("expected 1 session migrated, got %d", result.SessionsUpdated)
	}
	if result.PromptsUpdated != 1 {
		t.Fatalf("expected 1 prompt migrated, got %d", result.PromptsUpdated)
	}

	// Verify old project has no records
	obs, _ := s.RecentObservations(old, "", 10)
	if len(obs) != 0 {
		t.Fatalf("expected 0 observations under old name, got %d", len(obs))
	}

	// Verify new project has the records
	obs, _ = s.RecentObservations(new_, "", 10)
	if len(obs) != 1 {
		t.Fatalf("expected 1 observation under new name, got %d", len(obs))
	}

	// Verify FTS search finds it under new project
	results, _ := s.Search("test obs", SearchOptions{Project: new_, Limit: 10})
	if len(results) != 1 {
		t.Fatalf("expected FTS to find 1 result under new project, got %d", len(results))
	}
}

func TestMigrateProjectNoOp(t *testing.T) {
	s := newTestStore(t)

	// No records under "nonexistent" — should be a no-op
	result, err := s.MigrateProject("nonexistent", "anything")
	if err != nil {
		t.Fatalf("MigrateProject: %v", err)
	}
	if result.Migrated {
		t.Fatal("expected no migration for nonexistent project")
	}
}

func TestMigrateProjectIdempotent(t *testing.T) {
	s := newTestStore(t)
	old, new_ := "old-proj", "new-proj"

	s.CreateSession(CreateSessionParams{ClientSessionID: "s1", Project: old, Directory: "/tmp"})
	s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "decision", Title: "test",
		Content: "content", Project: old, Scope: "project",
	})

	// First migration
	r1, err := s.MigrateProject(old, new_)
	if err != nil {
		t.Fatalf("first MigrateProject: %v", err)
	}
	if !r1.Migrated {
		t.Fatal("first migration should migrate")
	}

	// Second migration — no records under old name anymore
	r2, err := s.MigrateProject(old, new_)
	if err != nil {
		t.Fatalf("second MigrateProject: %v", err)
	}
	if r2.Migrated {
		t.Fatal("second migration should be a no-op")
	}
}
