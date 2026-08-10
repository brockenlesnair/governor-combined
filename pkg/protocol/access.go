package protocol

import "fmt"

// New creates a Protocol with the default device registry and tier definitions.
func New() *Protocol {
	p := &Protocol{
		devices: make(map[string]Device),
		tiers:   DefaultTiers(),
	}
	for _, d := range DefaultDevices() {
		p.devices[d.Name] = d
	}
	return p
}

// CanAccess checks if an agent with the given tier can use a device.
func (p *Protocol) CanAccess(tier Tier, deviceName string) bool {
	deviceDevices, ok := p.tiers[tier]
	if !ok {
		return false
	}
	for _, name := range deviceDevices {
		if name == deviceName {
			return true
		}
	}
	return false
}

// GetDevice returns the device definition by name.
func (p *Protocol) GetDevice(name string) (Device, error) {
	d, ok := p.devices[name]
	if !ok {
		return Device{}, fmt.Errorf("unknown device: %s", name)
	}
	return d, nil
}

// DevicesForTier returns all devices accessible to the given tier.
func (p *Protocol) DevicesForTier(tier Tier) []Device {
	deviceNames, ok := p.tiers[tier]
	if !ok {
		return nil
	}
	var result []Device
	for _, name := range deviceNames {
		if d, ok := p.devices[name]; ok {
			result = append(result, d)
		}
	}
	return result
}

// DevicesByCategory groups accessible devices by category.
func (p *Protocol) DevicesByCategory(tier Tier) map[string][]Device {
	devices := p.DevicesForTier(tier)
	categorized := make(map[string][]Device)
	for _, d := range devices {
		categorized[d.Category] = append(categorized[d.Category], d)
	}
	return categorized
}

// Guidance returns the appropriate guidance text for the given tier.
func (p *Protocol) Guidance(tier Tier) string {
	switch tier {
	case TierSteer:
		return SteerGuidance()
	case TierSplinter:
		return SplinterGuidance()
	default:
		return ""
	}
}
