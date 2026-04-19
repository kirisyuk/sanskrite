package criticalchainview

// Known chokepoints with coordinates (Hormuz, Bab-el-Mandeb, Malacca, etc.)
var knownChokepoints = []Chokepoint{
	{ID: "hormuz", Path: "/loc/strait_hormuz", Name: "Strait of Hormuz", Latitude: 26.5, Longitude: 56.3, ShareOfSupply: 0.20, RiskMultiplier: 3.0},
	{ID: "bab_el_mandeb", Path: "/loc/bab_el_mandeb", Name: "Bab-el-Mandeb", Latitude: 12.6, Longitude: 43.3, ShareOfSupply: 0.09, RiskMultiplier: 2.5},
	{ID: "turkish_straits", Path: "/loc/turkish_straits", Name: "Turkish Straits (Bosporus)", Latitude: 41.0, Longitude: 29.0, ShareOfSupply: 0.03, RiskMultiplier: 2.0},
	{ID: "malacca", Path: "/loc/strait_malacca", Name: "Strait of Malacca", Latitude: 2.0, Longitude: 101.5, ShareOfSupply: 0.25, RiskMultiplier: 2.5},
	{ID: "suez", Path: "/loc/suez_canal", Name: "Suez Canal", Latitude: 30.0, Longitude: 32.3, ShareOfSupply: 0.12, RiskMultiplier: 2.5},
	{ID: "panama", Path: "/loc/panama_canal", Name: "Panama Canal", Latitude: 9.1, Longitude: -79.7, ShareOfSupply: 0.05, RiskMultiplier: 2.0},
	{ID: "eu_pipelines", Path: "/loc/eu_pipelines", Name: "EU Gas Pipelines", Latitude: 50.0, Longitude: 10.0, ShareOfSupply: 0.15, RiskMultiplier: 2.0},
}
