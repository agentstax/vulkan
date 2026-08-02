package cron

import (
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

func TestConfigWithDefaults(t *testing.T) {
	cfg := (&Config{}).WithDefaults()
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout: expected 30s, got %v", cfg.Timeout)
	}
	if cfg.Concurrency != common.ConcurrencyAllow {
		t.Errorf("Concurrency: expected allow, got %q", cfg.Concurrency)
	}

	set := (&Config{Timeout: time.Minute, Concurrency: common.ConcurrencyDefer}).WithDefaults()
	if set.Timeout != time.Minute || set.Concurrency != common.ConcurrencyDefer {
		t.Errorf("set fields overwritten: %+v", set)
	}
}

func TestAlterConfigValidate(t *testing.T) {
	minute := time.Minute
	negative := -time.Minute
	cases := []struct {
		name    string
		cfg     AlterConfig
		wantErr bool
	}{
		{name: "single field", cfg: AlterConfig{Timeout: &minute}},
		{name: "no fields set", cfg: AlterConfig{}, wantErr: true},
		{name: "negative timeout", cfg: AlterConfig{Timeout: &negative}, wantErr: true},
		{name: "bad concurrency", cfg: AlterConfig{Concurrency: "forbid"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if c.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "defaults valid", cfg: *(&Config{}).WithDefaults()},
		{name: "negative timeout", cfg: *(&Config{Timeout: -time.Minute}).WithDefaults(), wantErr: true},
		{name: "bad concurrency", cfg: *(&Config{Concurrency: "forbid"}).WithDefaults(), wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if c.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}
