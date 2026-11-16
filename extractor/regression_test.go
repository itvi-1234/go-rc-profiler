// Package extractor_test holds the refactoring regression suite. It is an
// external test package on purpose: it may only reach the exported profiler
// surface, so internal reorganization cannot quietly satisfy it.
//
// Most fixtures under testdata/regression are self-contained copies of artifacts
// the profiler is expected to keep compiling today: the first-party Go
// declaration packages and their extension definitions and the rc-demos request
// logger workload. Their golden profiles are the reviewed outputs those inputs
// produce now. One fixture, full-vocabulary, is written for this suite alone to
// reach behavior the reviewed artifacts never trigger; it says so in its own source.
package extractor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runtimeconditions/go-rc-profiler/extractor"
	"gopkg.in/yaml.v3"
)

// TestDeclarationWorkloadProfileMatchesGolden locks the profile the rc-demos Go
// request logger produces from the first-party declaration packages.
func TestDeclarationWorkloadProfileMatchesGolden(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "request-logger-http"), extractor.Options{
		Name:              "request-logger-http",
		WorkloadURI:       "github.com/runtimeconditions/rc-demos/apps/request-logger-http",
		WorkloadVersion:   "dev",
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "request-logger-http.golden.yaml")
}

// TestDeclarationWorkloadProfileIsStableWithoutGoPackages proves the syntax-only
// path still reaches the same profile when semantic loading is disabled.
func TestDeclarationWorkloadProfileIsStableWithoutGoPackages(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "request-logger-http"), extractor.Options{
		Name:              "request-logger-http",
		WorkloadURI:       "github.com/runtimeconditions/rc-demos/apps/request-logger-http",
		WorkloadVersion:   "dev",
		DisableGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "request-logger-http.golden.yaml")
}

// TestFullDeclarationVocabularyProfileMatchesGolden exercises the whole
// first-party declarative vocabulary in one profile: every HTTP method, API
// specs with and without a version, both datastore interfaces, caches, sensitive
// and optional environment inputs, configuration alternatives, and the schema
// shapes the profiler derives from Go types.
func TestFullDeclarationVocabularyProfileMatchesGolden(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "full-vocabulary"), extractor.Options{
		Name:              "full-vocabulary",
		WorkloadURI:       "github.com/runtimeconditions/go-rc-profiler/regression/full-vocabulary",
		WorkloadVersion:   "v1.0.0",
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "full-vocabulary.golden.yaml")
}

// TestSkipValidationEmitsTheSameProfile proves the escape hatch only skips the
// validation layers. It must still emit the profile, and the extension list it
// assembles itself must already be ordered, because extension closure resolution
// is not there to reorder it.
func TestSkipValidationEmitsTheSameProfile(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "full-vocabulary"), extractor.Options{
		Name:              "full-vocabulary",
		WorkloadURI:       "github.com/runtimeconditions/go-rc-profiler/regression/full-vocabulary",
		WorkloadVersion:   "v1.0.0",
		SkipValidation:    true,
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "full-vocabulary.golden.yaml")
}

// TestDeclarationWorkloadFailsClosed locks the declarative-path diagnostics that
// stop a profile from being emitted.
func TestDeclarationWorkloadFailsClosed(t *testing.T) {
	isolateGoEnvironment(t)

	tests := []struct {
		name      string
		source    string
		wantError string
	}{
		{
			name: "condition name is not a compile-time string",
			source: `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

func declaration(name string) {
	common.API(name)
}
`,
			wantError: "API name must be a string literal or string const",
		},
		{
			name: "operation schema type cannot be resolved",
			source: `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

func declaration() {
	common.API("todos-api", common.GET("/todos", common.Response[chan int]()))
}
`,
			wantError: "unsupported schema type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workloadDir := stageDeclarationWorkload(t, test.source)

			_, err := extractor.ExtractDir(workloadDir, extractor.Options{
				Name:            "declaration-failure",
				WorkloadURI:     "github.com/example/declaration-failure",
				WorkloadVersion: "v0.1.0",
			})
			requireErrorContains(t, err, test.wantError)
		})
	}
}

// TestWorkloadWithoutRuntimeConditionsMetadataProfilesEmpty proves that ordinary
// imports without a declaration package produce a structurally valid profile with
// no conditions instead of a failure.
func TestWorkloadWithoutRuntimeConditionsMetadataProfilesEmpty(t *testing.T) {
	isolateGoEnvironment(t)
	workloadDir := t.TempDir()
	writeFile(t, filepath.Join(workloadDir, "go.mod"), "module example.com/plain\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(workloadDir, "main.go"), "package main\n\nimport \"net/http\"\n\nfunc main() { _ = http.StatusOK }\n")

	profile, err := extractor.ExtractDir(workloadDir, extractor.Options{
		Name:              "plain",
		WorkloadURI:       "github.com/example/plain",
		WorkloadVersion:   "v0.1.0",
		ExtensionRoots:    []string{regressionPath(t, "extensions")},
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Conditions) != 0 || len(profile.Extensions) != 0 {
		t.Fatalf("expected an empty profile, got %#v", profile)
	}
}

func regressionPath(t *testing.T, parts ...string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(append([]string{"testdata", "regression"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func requireGoldenProfile(t *testing.T, profile *extractor.RuntimeConditionsProfile, golden string) {
	t.Helper()
	got, err := yaml.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(regressionPath(t, "golden", golden))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("profile differs from %s\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected an error containing %q, got: %v", want, err)
	}
}

// isolateGoEnvironment keeps extraction independent of the developer's Go
// workspace, which otherwise reaches the go/packages load.
func isolateGoEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
}

// stageDeclarationWorkload builds a module that consumes the vendored first-party
// declaration packages.
func stageDeclarationWorkload(t *testing.T, source string) string {
	t.Helper()
	workloadDir := t.TempDir()
	extensionsRoot := regressionPath(t, "extensions")
	writeFile(t, filepath.Join(workloadDir, "go.mod"), "module github.com/example/declaration\n\ngo 1.25.0\n\n"+
		"require github.com/runtimeconditions/extensions/common-integrations/go v0.0.0\n\n"+
		"replace github.com/runtimeconditions/extensions/common-integrations/go => "+filepath.Join(extensionsRoot, "common-integrations", "go")+"\n")
	writeFile(t, filepath.Join(workloadDir, "main.go"), source)
	return workloadDir
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
