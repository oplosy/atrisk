package buildinfo

import "fmt"

// Format returns the stable, human-readable diagnostic line for a component.
func Format(component, version, runtimeVersion string) string {
	return fmt.Sprintf("atlasrisk %s version %s runtime %s", component, version, runtimeVersion)
}
