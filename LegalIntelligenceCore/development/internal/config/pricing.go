package config

// PricingConfig holds the path to the per-model pricing YAML (parsed by the
// cost tracker, not by this package).
type PricingConfig struct {
	TablePath string // LIC_PRICING_TABLE_PATH
}

func loadPricingConfig() PricingConfig {
	return PricingConfig{
		TablePath: envString("LIC_PRICING_TABLE_PATH", "/etc/lic/pricing.yaml"),
	}
}
