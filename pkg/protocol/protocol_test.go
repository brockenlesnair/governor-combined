package protocol

import (
	"testing"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if len(p.devices) == 0 {
		t.Fatal("no devices registered")
	}
	if len(p.tiers) == 0 {
		t.Fatal("no tiers defined")
	}
}

func TestCanAccess(t *testing.T) {
	p := New()

	tests := []struct {
		name       string
		tier       Tier
		device     string
		wantAccess bool
	}{
		// Splinter agents can access search and validation
		{"splinter can search", TierSplinter, "search_code", true},
		{"splinter can validate", TierSplinter, "validate_code", true},
		{"splinter can get impact", TierSplinter, "get_impact", true},
		{"splinter can create ADR", TierSplinter, "adr_create", true},

		// Splinter agents CANNOT access oversight tools
		{"splinter cannot rigour", TierSplinter, "rigour_check", false},
		{"splinter cannot audit", TierSplinter, "audit_project", false},
		{"splinter cannot sarif", TierSplinter, "sarif_export", false},

		// Steer agents can access everything
		{"steer can rigour", TierSteer, "rigour_check", true},
		{"steer can audit", TierSteer, "audit_project", true},
		{"steer can sarif", TierSteer, "sarif_export", true},
		{"steer can search", TierSteer, "search_code", true},
		{"steer can validate", TierSteer, "validate_code", true},

		// Unknown device
		{"steer cannot unknown", TierSteer, "nonexistent_device", false},
		{"splinter cannot unknown", TierSplinter, "nonexistent_device", false},

		// Unknown tier
		{"unknown tier denied", Tier("unknown"), "search_code", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.CanAccess(tt.tier, tt.device)
			if got != tt.wantAccess {
				t.Errorf("CanAccess(%q, %q) = %v, want %v", tt.tier, tt.device, got, tt.wantAccess)
			}
		})
	}
}

func TestGetDevice(t *testing.T) {
	p := New()

	d, err := p.GetDevice("search_code")
	if err != nil {
		t.Fatalf("GetDevice(search_code) error: %v", err)
	}
	if d.Name != "search_code" {
		t.Errorf("device name = %q, want %q", d.Name, "search_code")
	}
	if d.Category != "search" {
		t.Errorf("device category = %q, want %q", d.Category, "search")
	}

	_, err = p.GetDevice("nonexistent")
	if err == nil {
		t.Error("GetDevice(nonexistent) should return error")
	}
}

func TestDevicesForTier(t *testing.T) {
	p := New()

	splinter := p.DevicesForTier(TierSplinter)
	steer := p.DevicesForTier(TierSteer)

	if len(splinter) == 0 {
		t.Fatal("splinter has no devices")
	}
	if len(steer) == 0 {
		t.Fatal("steer has no devices")
	}
	if len(steer) <= len(splinter) {
		t.Errorf("steer (%d devices) should have more than splinter (%d)", len(steer), len(splinter))
	}

	// Every splinter device should also be accessible to steer
	splinterNames := make(map[string]bool)
	for _, d := range splinter {
		splinterNames[d.Name] = true
	}
	for _, d := range steer {
		if splinterNames[d.Name] {
			// splinter device should also be in steer
			if !p.CanAccess(TierSteer, d.Name) {
				t.Errorf("steer cannot access splinter device %q", d.Name)
			}
		}
	}
}

func TestDevicesByCategory(t *testing.T) {
	p := New()

	categories := p.DevicesByCategory(TierSplinter)
	if len(categories) == 0 {
		t.Fatal("no categories found")
	}

	for cat, devices := range categories {
		if cat == "" {
			t.Error("empty category name")
		}
		if len(devices) == 0 {
			t.Errorf("category %q has no devices", cat)
		}
	}
}

func TestGuidance(t *testing.T) {
	p := New()

	steerGuidance := p.Guidance(TierSteer)
	splinterGuidance := p.Guidance(TierSplinter)
	unknownGuidance := p.Guidance(Tier("unknown"))

	if steerGuidance == "" {
		t.Error("steer guidance is empty")
	}
	if splinterGuidance == "" {
		t.Error("splinter guidance is empty")
	}
	if unknownGuidance != "" {
		t.Error("unknown tier should return empty guidance")
	}
	if steerGuidance == splinterGuidance {
		t.Error("steer and splinter guidance should differ")
	}
}

func TestDefaultDevices_UniqueNames(t *testing.T) {
	devices := DefaultDevices()
	seen := make(map[string]bool)
	for _, d := range devices {
		if seen[d.Name] {
			t.Errorf("duplicate device name: %q", d.Name)
		}
		seen[d.Name] = true
	}
}

func TestDefaultTiers_CoverAllDevices(t *testing.T) {
	p := New()

	// Every device should be accessible to at least one tier
	for name := range p.devices {
		if !p.CanAccess(TierSteer, name) && !p.CanAccess(TierSplinter, name) {
			t.Errorf("device %q not accessible to any tier", name)
		}
	}
}
