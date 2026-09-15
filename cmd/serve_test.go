package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

func TestRequireLoopback(t *testing.T) {
	cases := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:4270", false},
		{"127.5.5.5:4270", false}, // entire 127.0.0.0/8 is loopback
		{"localhost:4270", false},
		{"[::1]:4270", false},
		{"0.0.0.0:4270", true},
		{"192.168.1.5:4270", true},
		{"example.com:4270", true},
		{"not-an-addr", true},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			err := requireLoopback(tc.addr)
			if tc.wantErr && err == nil {
				t.Fatalf("requireLoopback(%q): expected error, got nil", tc.addr)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("requireLoopback(%q): unexpected error: %v", tc.addr, err)
			}
		})
	}
}

func TestRemoteResolverFromConfig(t *testing.T) {
	origLoadConfig := loadConfig
	defer func() { loadConfig = origLoadConfig }()

	loadConfig = func() (*config.Config, error) {
		return &config.Config{Remotes: []config.Remote{
			{Name: "host1", Type: "ssh", Host: "host1.example.com"},
		}}, nil
	}

	r, ok, err := remoteResolverFromConfig("host1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a configured remote")
	}
	if r.Host != "host1.example.com" {
		t.Fatalf("unexpected remote: %+v", r)
	}

	_, ok, err = remoteResolverFromConfig("unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an unconfigured remote")
	}
}

func TestRemoteResolverFromConfig_LoadError(t *testing.T) {
	origLoadConfig := loadConfig
	defer func() { loadConfig = origLoadConfig }()

	wantErr := errors.New("boom")
	loadConfig = func() (*config.Config, error) { return nil, wantErr }

	_, _, err := remoteResolverFromConfig("host1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped %v, got %v", wantErr, err)
	}
}

func TestBuildEmbeddedServerReturnsWorkingHandlerAndRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origLoadAllLive := loadAllLiveFn
	origLoadConfig := loadConfig
	origReapOrphanedLocal := reapOrphanedLocal
	t.Cleanup(func() {
		loadAllLiveFn = origLoadAllLive
		loadConfig = origLoadConfig
		reapOrphanedLocal = origReapOrphanedLocal
	})
	loadAllLiveFn = func(time.Duration) ([]sessions.Session, error) {
		return nil, nil
	}
	loadConfig = func() (*config.Config, error) {
		return &config.Config{}, nil
	}
	reapOrphanedLocal = func() error {
		return nil
	}

	srv, registry, err := BuildEmbeddedServer(14 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("BuildEmbeddedServer: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil *webui.Server")
	}
	if registry == nil {
		t.Fatal("expected non-nil *webterm.Registry")
	}
	defer registry.Shutdown()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sessions status = %d, want 200", resp.StatusCode)
	}
}
