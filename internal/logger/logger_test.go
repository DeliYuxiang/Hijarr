package logger

import (
	"os"
	"testing"
)

func TestInit(t *testing.T) {
	// Register a test module so Init() does not flag it as unknown.
	For("engine")

	os.Setenv("LOG_LEVEL", "debug,engine=info")
	defer os.Unsetenv("LOG_LEVEL")

	Init()

	if current.String() != "DEBUG" {
		t.Errorf("expected global level to be DEBUG, got %s", current.String())
	}

	if l, ok := moduleOverrides["engine"]; !ok || l.String() != "INFO" {
		t.Errorf("expected engine level to be INFO, got %s (ok=%v)", l.String(), ok)
	}
}

func TestInitUnknownModule(t *testing.T) {
	// "nonexistent" is not registered → Init() should print a warning to stderr
	// but must not panic or return an error.
	os.Setenv("LOG_LEVEL", "info,nonexistent=debug")
	defer os.Unsetenv("LOG_LEVEL")
	Init() // must not panic
}

func TestInitGlobalOnly(t *testing.T) {
	os.Setenv("LOG_LEVEL", "warn")
	defer os.Unsetenv("LOG_LEVEL")

	Init()

	if current.String() != "WARN" {
		t.Errorf("expected global level to be WARN, got %s", current.String())
	}
}
