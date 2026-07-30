package cli

import "testing"

func TestAttachArgs(t *testing.T) {
	tests := []struct {
		args             []string
		wantRemote, want string
		wantErr          bool
	}{
		{want: "default"},
		{args: []string{"review"}, want: "review"},
		{args: []string{"--remote", "devbox"}, wantRemote: "devbox", want: "default"},
		{args: []string{"review", "--remote", "devbox"}, wantRemote: "devbox", want: "review"},
		{args: []string{"--remote"}, wantErr: true},
		{args: []string{"one", "two"}, wantErr: true},
	}
	for _, tt := range tests {
		remote, name, err := attachArgs(tt.args)
		if (err != nil) != tt.wantErr {
			t.Errorf("attachArgs(%q) error = %v, want error=%v", tt.args, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && (remote != tt.wantRemote || name != tt.want) {
			t.Errorf("attachArgs(%q) = (%q, %q), want (%q, %q)", tt.args, remote, name, tt.wantRemote, tt.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b"), "'a'\"'\"'b'"; got != want {
		t.Errorf("shellQuote = %q, want %q", got, want)
	}
}
