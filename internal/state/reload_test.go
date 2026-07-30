package state

import (
	"testing"

	"github.com/jirkab/rookery/internal/apiproto"
)

func TestServerReloadCallsConfiguredReloader(t *testing.T) {
	loop := NewLoop("test", "test")
	called := false
	loop.SetReloader(func() error {
		called = true
		return nil
	})

	resp, deferred := loop.handleAPI(apiproto.Request{ID: "reload", Method: "server.reload"}, make(chan apiproto.Response, 1))
	if deferred {
		t.Fatal("server.reload must answer immediately")
	}
	if resp.Error != nil || !called {
		t.Fatalf("reload response = %+v, called = %v", resp, called)
	}
}

func TestServerReloadWithoutReloaderFails(t *testing.T) {
	loop := NewLoop("test", "test")
	resp, _ := loop.handleAPI(apiproto.Request{ID: "reload", Method: "server.reload"}, make(chan apiproto.Response, 1))
	if resp.Error == nil || resp.Error.Code != apiproto.ErrInternal {
		t.Fatalf("reload response = %+v, want internal error", resp)
	}
}
