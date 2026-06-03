package plugin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
	"golang.org/x/mod/semver"
)

// PluginSDKVersion is the current generation of the plugin SDK ABI — the shape
// of the domain.AnalysisRule / plugin.Manifest contract a plugin is compiled
// against. It is bumped only when that contract changes in a way that would
// break a previously-built plugin, which is independent of the user-facing
// release version (version.Version). A rule may advertise the SDK it targets
// via domain.VersionedRule.PluginSDKVersion().
const PluginSDKVersion = "0.9.0"

// MinPluginSDK and MaxPluginSDK bound the plugin SDK generations this binary
// can load. A rule implementing domain.VersionedRule whose declared
// PluginSDKVersion falls outside this inclusive range is rejected:
//   - older than MinPluginSDK → the plugin predates a breaking SDK change;
//     rebuild it against the current SDK.
//   - newer than MaxPluginSDK → the plugin targets an SDK this binary doesn't
//     understand yet; upgrade shingan.
const (
	MinPluginSDK = "0.9.0"
	MaxPluginSDK = "0.9.0"
)

// ErrSDKIncompatible means one or more registered plugin rules declare a plugin
// SDK version outside this binary's supported [MinPluginSDK, MaxPluginSDK]
// range. Callers can detect it with errors.Is and should treat it as a
// configuration error (CLI exit code 3).
var ErrSDKIncompatible = errors.New("plugin: rule built against an incompatible plugin SDK version")

// VerifySDKCompatibility checks every registered rule that implements
// domain.VersionedRule against the binary's supported SDK range. Rules that do
// not implement the interface are skipped (the check is opt-in, like
// Manifest.MinShinganVersion). Returns ErrSDKIncompatible listing each
// offending rule when any are out of range, nil otherwise.
//
// Intended to be called once at startup, after plugins have registered, so a
// generation-mismatched wrapper binary fails fast with an actionable message
// instead of producing subtly wrong findings.
func VerifySDKCompatibility() error {
	mu.RLock()
	defer mu.RUnlock()

	var incompatible []string
	for _, r := range registry {
		vr, ok := r.Rule.(domain.VersionedRule)
		if !ok {
			continue // opt-in: rule makes no SDK-generation claim
		}
		declared := vr.PluginSDKVersion()
		if reason := checkSDKRange(declared); reason != "" {
			incompatible = append(incompatible,
				fmt.Sprintf("%s (declares SDK %q): %s", r.Rule.Name(), declared, reason))
		}
	}
	if len(incompatible) > 0 {
		return fmt.Errorf("%w (this binary supports SDK %s..%s):\n  - %s",
			ErrSDKIncompatible, MinPluginSDK, MaxPluginSDK, strings.Join(incompatible, "\n  - "))
	}
	return nil
}

// checkSDKRange returns an empty string when v is within the supported range,
// or a human-readable reason why it is not.
func checkSDKRange(v string) string {
	if v == "" {
		return "no SDK version declared"
	}
	vv := "v" + strings.TrimPrefix(v, "v")
	if !semver.IsValid(vv) {
		return "not valid semver (want MAJOR.MINOR.PATCH)"
	}
	if semver.Compare(vv, "v"+MinPluginSDK) < 0 {
		return fmt.Sprintf("older than minimum supported SDK %s; rebuild against the current SDK", MinPluginSDK)
	}
	if semver.Compare(vv, "v"+MaxPluginSDK) > 0 {
		return fmt.Sprintf("newer than maximum supported SDK %s; upgrade shingan", MaxPluginSDK)
	}
	return ""
}
