package catalog

// ProfileDocument holds the profile fields the core specification defines. A
// condition is deliberately not modelled: its shape beyond kind and interface.type
// belongs to the extensions the profile declares, so conditions are checked as
// decoded YAML against those extensions' definitions.
type ProfileDocument struct {
	APIVersion string               `yaml:"apiVersion"`
	Kind       string               `yaml:"kind"`
	Extensions []ExtensionReference `yaml:"extensions"`
}
