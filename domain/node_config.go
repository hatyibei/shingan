package domain

import "strconv"

// Typed accessors for the most frequently consumed node settings. Each prefers
// the typed Node field and falls back to the legacy Config map (with the same
// coercion the rules historically applied) so JSON inputs carrying the old
// string keys keep working. This is the read side of the ADR-003 Onion
// strengthening: rules call these instead of reaching into Config directly.

// GetMaxIterations returns the loop/control iteration bound. ok is true only
// when a value is present AND coercible to an int — matching the historical
// rule behavior where a missing or unparseable max_iterations is "not set".
func (n *Node) GetMaxIterations() (int, bool) {
	if n == nil {
		return 0, false
	}
	if n.MaxIterations != nil {
		return *n.MaxIterations, true
	}
	return configInt(n.Config, "max_iterations")
}

// GetMaxConcurrency returns the max parallel concurrency. ok is true only when
// present and coercible to an int.
func (n *Node) GetMaxConcurrency() (int, bool) {
	if n == nil {
		return 0, false
	}
	if n.MaxConcurrency != nil {
		return *n.MaxConcurrency, true
	}
	return configInt(n.Config, "max_concurrency")
}

// GetTemperature returns the LLM sampling temperature. ok is true only when
// present and coercible to a float.
func (n *Node) GetTemperature() (float64, bool) {
	if n == nil {
		return 0, false
	}
	if n.Temperature != nil {
		return *n.Temperature, true
	}
	return configFloat(n.Config, "temperature")
}

// GetModelName returns the LLM model identifier, or "" when unset.
func (n *Node) GetModelName() string {
	if n == nil {
		return ""
	}
	if n.ModelName != "" {
		return n.ModelName
	}
	return configString(n.Config, "model")
}

// GetToolCategory returns the coarse tool category, or "" when unset.
func (n *Node) GetToolCategory() string {
	if n == nil {
		return ""
	}
	if n.ToolCategory != "" {
		return n.ToolCategory
	}
	return configString(n.Config, "category")
}

// configInt coerces common numeric (and string) representations to int,
// mirroring the rules' historical toInt/intConfig behavior.
func configInt(cfg map[string]any, key string) (int, bool) {
	if cfg == nil {
		return 0, false
	}
	v, ok := cfg[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i, true
		}
	}
	return 0, false
}

// configFloat coerces common numeric (and string) representations to float64.
func configFloat(cfg map[string]any, key string) (float64, bool) {
	if cfg == nil {
		return 0, false
	}
	v, ok := cfg[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func configString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	if s, ok := cfg[key].(string); ok {
		return s
	}
	return ""
}
