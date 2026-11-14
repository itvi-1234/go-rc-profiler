package extensioncheck

import (
	"strings"
	"testing"
)

// These tests are the contract between the validator and the extensions. A
// profile that uses a published extension must validate, whatever shape that
// extension gives its fields, and a value the extension does not allow must be
// rejected. The validator may know only what the core profile schema defines:
// the rest comes from each extension's definition. When an extension is
// published, or its shape changes, add or update its case here.
//
// testdata/published holds the definitions as published from the extensions
// repository at 3640fd7.

const (
	commonV2 = "https://runtimeconditions.io/extensions/rc/common-integrations:v1alpha2"
	envV2    = "https://runtimeconditions.io/extensions/rc/env-configuration:v1alpha2"
	natsV2   = "https://runtimeconditions.io/extensions/nats/service:0.2.0"
	s3V2     = "https://runtimeconditions.io/extensions/aws/s3:0.2.0"
	k8sV1    = "https://runtimeconditions.io/extensions/kubernetes/api:0.1.0"
	gaV1     = "https://runtimeconditions.io/extensions/google/analytics:0.1.0"
	gitV1    = "https://runtimeconditions.io/extensions/rc/source-control:0.1.0"
)

type publishedCase struct {
	name       string
	extensions []string
	condition  string
	// breakWith replaces old with new in the condition to give it a value the extension does not allow.
	old, new string
}

var publishedCases = []publishedCase{
	{
		name: "common-integrations http operations are objects", extensions: []string{commonV2},
		condition: "kind: api\ninterface:\n  type: http\n  operations:\n  - {method: GET, path: /orders}\n",
		old:       "GET", new: "FETCH",
	},
	{
		name: "common-integrations smtp operations are strings", extensions: []string{commonV2},
		condition: "kind: api\ninterface:\n  type: smtp\n  operations: [send]\n",
		old:       "send", new: "receive",
	},
	{
		name: "common-integrations relational engine is a string", extensions: []string{commonV2},
		condition: "kind: datastore\ninterface:\n  type: relational\n  engine: postgres\n",
		old:       "postgres", new: "oracle2",
	},
	{
		name: "env-configuration properties depend on the interface", extensions: []string{commonV2, envV2},
		condition: "kind: api\ninterface:\n  type: smtp\n  operations: [send]\nconfiguration:\n  env:\n  - {property: tlsMode, name: SMTP_TLS}\n",
		old:       "tlsMode", new: "database",
	},
	{
		name: "env-configuration alternatives", extensions: []string{commonV2, envV2},
		condition: "kind: cache\ninterface:\n  type: key_value\n  engine: redis\nconfiguration:\n  alternatives:\n  - env: [{property: url, name: A}]\n  - env: [{property: hostname, name: B}]\n",
		old:       "hostname", new: "tlsMode",
	},
	{
		name: "nats operations are objects with their own keys", extensions: []string{natsV2},
		condition: "kind: nats.service\ninterface:\n  type: service\n  operations:\n  - {resource: connection, action: connect}\n  - {resource: subject, action: publish, subject: orders.created}\nconfiguration:\n  env:\n  - {property: url, name: NATS_URL}\n",
		old:       "property: url", new: "property: bogus",
	},
	{
		name: "aws s3 operations are objects with a name and a role", extensions: []string{s3V2},
		condition: "kind: aws.s3\ninterface:\n  type: bucket\n  bucketName: orders\n  operations:\n  - {name: AbortMultipartUpload}\n  - {name: CopyObject, role: source}\n",
		old:       "role: source", new: "role: elsewhere",
	},
	{
		name: "kubernetes operations carry a verb and a scope", extensions: []string{k8sV1},
		condition: "kind: kubernetes.api\ninterface:\n  type: api\n  operations:\n  - {verb: get, apiGroup: '', apiVersion: v1, resource: pods, scope: namespaced}\n",
		old:       "verb: get", new: "verb: grab",
	},
	{
		name: "google analytics events are strings", extensions: []string{gaV1},
		condition: "kind: google.analytics\ninterface:\n  type: web\n  events: [page_view]\nconfiguration:\n  env:\n  - {property: measurementId, name: GA_ID}\n",
		old:       "page_view", new: "scroll",
	},
	{
		name: "source-control access is a list of strings and provider a string", extensions: []string{gitV1},
		condition: "kind: rc.source_control\ninterface:\n  type: git\n  provider: github\n  access: [clone, fetch]\n",
		old:       "clone", new: "rewrite",
	},
}

func publishedProfile(extensions []string, condition string) []byte {
	var out strings.Builder
	out.WriteString("apiVersion: runtimeconditions.io/v1alpha1\nkind: RuntimeConditionsProfile\nmetadata: {name: contract}\nworkload: {uri: 'https://example.test/contract'}\nextensions:\n")
	for _, extension := range extensions {
		out.WriteString("- " + extension + "\n")
	}
	out.WriteString("conditions:\n- ")
	out.WriteString(strings.ReplaceAll(strings.TrimRight(condition, "\n"), "\n", "\n  "))
	out.WriteString("\n")
	return []byte(out.String())
}

func TestEveryPublishedExtensionValidatesItsOwnShape(t *testing.T) {
	options := ProfileOptions{CatalogRoots: []string{"testdata/published"}}
	for _, test := range publishedCases {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateProfileYAML(publishedProfile(test.extensions, test.condition), options); err != nil {
				t.Fatalf("a profile the extension allows was rejected: %v", err)
			}
			if !strings.Contains(test.condition, test.old) {
				t.Fatalf("the case does not contain %q", test.old)
			}
			broken := strings.Replace(test.condition, test.old, test.new, 1)
			if err := ValidateProfileYAML(publishedProfile(test.extensions, broken), options); err == nil {
				t.Fatalf("a value the extension does not allow was accepted:\n%s", broken)
			}
		})
	}
}

func TestOneProfileMayUseSeveralExtensionsWithDifferentShapes(t *testing.T) {
	profile := []byte(`apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsProfile
metadata: {name: contract}
workload: {uri: 'https://example.test/contract'}
extensions:
- ` + commonV2 + `
- ` + envV2 + `
- ` + natsV2 + `
- ` + s3V2 + `
- ` + gitV1 + `
conditions:
- kind: api
  interface: {type: smtp, operations: [send]}
- kind: api
  interface: {type: http, operations: [{method: GET, path: /x}]}
- kind: nats.service
  interface: {type: service, operations: [{resource: connection, action: connect}]}
- kind: aws.s3
  interface: {type: bucket, operations: [{name: AbortMultipartUpload}]}
- kind: rc.source_control
  interface: {type: git, provider: github, access: [clone]}
`)
	if err := ValidateProfileYAML(profile, ProfileOptions{CatalogRoots: []string{"testdata/published"}}); err != nil {
		t.Fatalf("a profile that mixes extensions was rejected: %v", err)
	}
}
