package protocol

// DefaultTiers returns the tier → device mapping for the two access levels.
func DefaultTiers() map[Tier][]string {
	return map[Tier][]string{
		TierSteer: steerDevices(),
		TierSplinter: splinterDevices(),
	}
}

// steerDevices returns all devices accessible to steer agents.
// Steer agents have full access — they oversee project-wide health.
func steerDevices() []string {
	var names []string
	for _, d := range DefaultDevices() {
		names = append(names, d.Name)
	}
	return names
}

// splinterDevices returns the curated subset accessible to all agents.
// Splinter agents get guidance + limited tool access for self-checking.
func splinterDevices() []string {
	return []string{
		"search_code",
		"get_callers",
		"get_callees",
		"get_impact",
		"validate_code",
		"validate_diff",
		"adr_create",
		"adr_list",
	}
}
