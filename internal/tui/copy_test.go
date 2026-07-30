package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestCopyResultShowsAndExpiresToast(t *testing.T) {
	m := testModel()
	m.statusMsg = "pane p1 exited (code 0)"

	m.Update(copyResultMsg{text: "first\nsecond"})
	if got := m.renderStatus(); !strings.Contains(got, "copied 2 lines to the clipboard") {
		t.Errorf("status = %q, want copy toast", got)
	}
	if m.toastID == 0 {
		t.Fatal("copy result did not start a toast timer")
	}

	m.Update(toastExpiredMsg{id: m.toastID})
	if got := m.renderStatus(); !strings.Contains(got, "pane p1 exited") {
		t.Errorf("status after expiry = %q, want underlying status restored", got)
	}
}

func TestCopyFailureShowsUsefulToast(t *testing.T) {
	m := testModel()
	m.Update(copyResultMsg{err: errors.New("broken pipe")})

	if got := m.renderStatus(); !strings.Contains(got, "copy failed: could not send clipboard request (broken pipe)") {
		t.Errorf("status = %q, want copy failure toast", got)
	}
}

func TestOlderToastTimerCannotClearNewToast(t *testing.T) {
	m := testModel()
	m.showToast("first")
	first := m.toastID
	m.showToast("second")

	m.Update(toastExpiredMsg{id: first})
	if m.toast != "second" {
		t.Errorf("toast = %q after stale timer, want second", m.toast)
	}
}

func TestUpdateNoticePersistsOverTransientStatus(t *testing.T) {
	m := testModel()
	m.width = 80
	m.Update(updateCheckMsg{tag: "v0.2.3", available: true})
	m.statusMsg = "prefix — ctrl+b ? for help"
	m.showToast("copied 2 lines to the clipboard")

	if got := m.renderStatus(); !strings.Contains(got, "UPDATE AVAILABLE") || !strings.Contains(got, "rook v0.2.3") {
		t.Errorf("status = %q, want persistent update notice", got)
	}
}
