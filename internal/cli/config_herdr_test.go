package cli

import (
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

func TestConfigRowsShowHerdrPanes(t *testing.T) {
	has := func(cfg *config.Config) bool {
		for _, row := range configRows(cfg) {
			if row == [2]string{"herdr.panes", "on"} {
				return true
			}
		}
		return false
	}
	if has(&config.Config{}) {
		t.Error("herdr.panes shown when off")
	}
	if !has(&config.Config{Herdr: config.Herdr{Panes: true}}) {
		t.Error("herdr.panes missing when on")
	}
}
