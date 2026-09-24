package buildinfo

import "fmt"

// Format returns the stable, human-readable diagnostic line for a component.
func Format(component, version string) string {
	return fmt.Sprintf("atlasrisk %s version %s", component, version)
}
