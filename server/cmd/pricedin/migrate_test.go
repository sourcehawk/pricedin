package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

func TestMigrateRequiresTheDatabaseURL(t *testing.T) {
	for _, sub := range []string{"up", "down", "status"} {
		err := execute(t, nil, "migrate", sub)
		if !errors.Is(err, config.ErrMissing) || !strings.Contains(err.Error(), "PRICEDIN_DATABASE_URL") {
			t.Errorf("migrate %s error = %v, want a missing PRICEDIN_DATABASE_URL", sub, err)
		}
	}
}

func TestMigrateRejectsAnUnknownSubcommand(t *testing.T) {
	if err := execute(t, map[string]string{"PRICEDIN_DATABASE_URL": "postgres://db/x"}, "migrate", "sideways"); err == nil {
		t.Error("migrate sideways error = nil, want an error")
	}
}
