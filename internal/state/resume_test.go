package state

import (
	"testing"

	"github.com/jirkab/rookery/internal/apiproto"
)

func TestPaneResumeRejectsUnsafeOrMismatchedMetadata(t *testing.T) {
	l := &Loop{app: newApp("resume-test")}
	l.app.panes["p1"] = &Pane{ID: "p1", Agent: "codex", AgentSession: "known-session", Cwd: "/tmp"}
	l.app.panes["p2"] = &Pane{ID: "p2", Agent: "codex", Cwd: "/tmp"}

	tests := []struct {
		name string
		p    apiproto.PaneResumeParams
	}{
		{"unsupported agent", apiproto.PaneResumeParams{Agent: "shell", SessionRef: "known-session"}},
		{"missing recorded metadata", apiproto.PaneResumeParams{SourcePaneID: "p2"}},
		{"agent mismatch", apiproto.PaneResumeParams{SourcePaneID: "p1", Agent: "claude", SessionRef: "known-session"}},
		{"session mismatch", apiproto.PaneResumeParams{SourcePaneID: "p1", Agent: "codex", SessionRef: "different-session"}},
		{"unknown source", apiproto.PaneResumeParams{SourcePaneID: "missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := l.paneResume("test", tt.p)
			if resp.Error == nil || resp.Error.Code != apiproto.ErrInvalidParams && resp.Error.Code != apiproto.ErrPaneNotFound {
				t.Fatalf("paneResume(%+v) = %+v, want a validation error", tt.p, resp)
			}
		})
	}
}
