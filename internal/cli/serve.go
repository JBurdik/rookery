package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/apiserver"
	"github.com/jirkab/rookery/internal/attachserver"
	"github.com/jirkab/rookery/internal/config"
	"github.com/jirkab/rookery/internal/notify"
	"github.com/jirkab/rookery/internal/session"
	"github.com/jirkab/rookery/internal/state"
)

const detachEnvVar = "ROOK_DETACHED"

// RunServe implements `rook serve [--session name] [--foreground|-f]`.
func RunServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	sessionName := fs.String("session", session.DefaultName, "session name")
	foreground := fs.Bool("foreground", false, "run in the foreground (do not daemonize)")
	fs.BoolVar(foreground, "f", false, "shorthand for --foreground")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*foreground && os.Getenv(detachEnvVar) == "" {
		if err := daemonize(*sessionName); err != nil {
			return err
		}
		fmt.Printf("started session %q\n", *sessionName)
		return nil
	}
	return serveForeground(*sessionName)
}

// EnsureDaemon starts a daemon for sessionName if one isn't already
// running, waiting until its API socket answers. Used by `rook attach` (and
// bare `rook`) for tmux-`new-session -A`-style ergonomics: attach to
// whatever's there, or stand it up first.
func EnsureDaemon(sessionName string) error {
	if session.IsLive(session.APISocketPath(sessionName)) {
		return nil
	}
	return daemonize(sessionName)
}

// daemonize re-execs the current binary with a Setsid child (Go has no
// fork()), redirects its stdio to the session log, and waits briefly to
// confirm the daemon actually bound its socket.
func daemonize(sessionName string) error {
	if err := session.EnsureDir(sessionName); err != nil {
		return err
	}

	logFile, err := os.OpenFile(session.LogPath(sessionName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "serve", "--session", sessionName, "--foreground")
	cmd.Env = append(os.Environ(), detachEnvVar+"=1")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}

	apiPath := session.APISocketPath(sessionName)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session.IsLive(apiPath) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("session %q did not come up; check %s", sessionName, session.LogPath(sessionName))
}

func serveForeground(sessionName string) error {
	if err := session.EnsureDir(sessionName); err != nil {
		return err
	}

	loop := state.NewLoop(sessionName, Version)
	// The daemon reads the same config the client does, so notification
	// sounds work whether or not anyone is attached.
	if cfg, _, err := config.Load(); err == nil {
		loop.SetSound(notify.New(cfg.UI.Sound))
	} else {
		fmt.Fprintf(os.Stderr, "warning: config: %v\n", err)
		loop.SetSound(notify.New(notify.DefaultConfig()))
	}

	apiLn, err := apiserver.Serve(sessionName, loop)
	if err != nil {
		return err
	}
	attachLn, err := attachserver.Serve(sessionName, loop)
	if err != nil {
		apiLn.Close()
		return err
	}
	if err := session.WritePIDFile(sessionName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write pidfile: %v\n", err)
	}

	// Agent status rules: the embedded defaults, overlaid with anything in
	// ~/.rook/agents. A broken user file costs that file, not the daemon.
	registry, errs := agentstatus.Load(config.AgentsDir())
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "warning: agent manifest: %v\n", err)
	}
	loop.SetAgentRegistry(registry)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		loop.SubmitAPI(apiproto.Request{ID: "shutdown-signal", Method: "server.shutdown"})
	}()

	loop.Run() // blocks until server.shutdown is processed

	apiLn.Close()
	attachLn.Close()
	session.Cleanup(sessionName)
	return nil
}
