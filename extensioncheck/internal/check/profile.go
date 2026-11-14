package check

import (
	"errors"
	"fmt"
	"slices"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/catalog"
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/diag"
)

// ProfileChecker validates a generated profile against the vocabulary that its
// declared extensions resolve to.
type ProfileChecker struct {
	checker *Checker
	report  *diag.Report
}

// NewProfileChecker returns a profile checker that resolves vocabulary through
// checker and reports to report.
func NewProfileChecker(checker *Checker, report *diag.Report) *ProfileChecker {
	return &ProfileChecker{checker: checker, report: report}
}

// Validate checks a profile's metadata, its declared extension closure, and each
// of its conditions. rawConditions holds the conditions as decoded YAML. They are
// never decoded into fixed types, because everything in a condition beyond its
// kind and interface type is defined by the extensions the profile declares.
func (p *ProfileChecker) Validate(profile catalog.ProfileDocument, rawConditions []any) {
	if profile.APIVersion != "runtimeconditions.io/v1alpha1" {
		p.report.Addf("apiVersion must be runtimeconditions.io/v1alpha1")
	}
	if profile.Kind != "RuntimeConditionsProfile" {
		p.report.Addf("kind must be RuntimeConditionsProfile")
	}
	if len(profile.Extensions) == 0 && len(rawConditions) > 0 {
		p.report.Addf("extensions must declare the extension dependency closure used by conditions")
	}

	// A profile entry may be a short <url>:<version> reference or an id without a
	// version. Look each one up as the definition it names.
	extensions := make([]catalog.ExtensionReference, len(profile.Extensions))
	for i, id := range profile.Extensions {
		extensions[i] = id
		if resolved, err := p.checker.catalog.Lookup(id); err == nil {
			extensions[i] = resolved
		} else if errors.Is(err, catalog.ErrAmbiguous) {
			p.report.Addf("%v", err)
		}
	}

	declared := make(map[catalog.ExtensionReference]bool)
	for _, id := range extensions {
		if declared[id] {
			p.report.Addf("duplicate extension id %s", id)
			continue
		}
		declared[id] = true
		if p.checker.catalog.Nodes[id] == nil {
			p.report.Addf("missing extension definition for %s", id)
			continue
		}
		p.checker.ValidateNode(id)
	}

	// A profile must name every extension it depends on, not just the ones it
	// uses directly, so that its vocabulary is reproducible from the list alone.
	closure := p.extensionClosure(extensions)
	for id := range closure {
		if !declared[id] {
			p.report.Addf("extensions missing dependency %s", id)
		}
	}

	resolved := p.vocabulary(closure)
	p.checker.checkResolvedConflicts("profile", resolved)
	for index, condition := range rawConditions {
		p.validateCondition(index, resolved, condition)
	}
}

func (p *ProfileChecker) extensionClosure(ids []catalog.ExtensionReference) map[catalog.ExtensionReference]bool {
	seen := make(map[catalog.ExtensionReference]bool)
	var visit func(catalog.ExtensionReference)
	visit = func(id catalog.ExtensionReference) {
		if seen[id] {
			return
		}
		seen[id] = true
		node := p.checker.catalog.Nodes[id]
		if node == nil {
			return
		}
		for _, dependency := range node.Definition.Spec.Dependencies {
			if resolved, err := p.checker.catalog.Lookup(dependency); err == nil {
				dependency = resolved
			}
			visit(dependency)
		}
	}
	for _, id := range ids {
		visit(id)
	}
	return seen
}

func (p *ProfileChecker) vocabulary(ids map[catalog.ExtensionReference]bool) catalog.Vocabulary {
	var nodes []*catalog.Node
	for id := range ids {
		if node := p.checker.catalog.Nodes[id]; node != nil {
			nodes = append(nodes, node)
		}
	}
	slices.SortFunc(nodes, func(left *catalog.Node, right *catalog.Node) int {
		return left.ID.Compare(right.ID)
	})
	return catalog.NewVocabulary(nodes)
}

// coreConditionFields and coreInterfaceFields are the fields the core profile
// schema defines. Every other field of a condition is defined by an extension.
var (
	coreConditionFields = map[string]bool{"name": true, "optional": true, "kind": true, "extension": true, "interface": true}
	coreInterfaceFields = map[string]bool{"type": true}
)

// validateCondition checks one condition, held as decoded YAML, against the
// vocabulary and schemas of the extensions the profile declares. It knows only
// what the core schema defines. The shape of everything else comes from the
// extension definitions.
func (p *ProfileChecker) validateCondition(index int, resolved catalog.Vocabulary, raw any) {
	prefix := fmt.Sprintf("conditions[%d]", index)
	condition, ok := raw.(map[string]any)
	if !ok {
		p.report.Addf("%s must be a mapping", prefix)
		return
	}
	kind, _ := condition["kind"].(string)
	interfaceFields, _ := condition["interface"].(map[string]any)
	interfaceType, _ := interfaceFields["type"].(string)
	p.report.ExpectExactlyOne(resolved.KindCount(kind), "%s.kind %s", prefix, kind)
	p.report.ExpectExactlyOne(resolved.InterfaceTypeCount(kind, interfaceType), "%s.interface.type %s/%s", prefix, kind, interfaceType)

	// A field that some extension defines has to be defined for this kind and
	// interface type. A field no extension defines is left to the extension schemas.
	for _, name := range sortedKeys(interfaceFields) {
		if !coreInterfaceFields[name] && resolved.InterfaceFieldDeclared(name) {
			p.report.ExpectExactlyOne(resolved.InterfaceFieldCount(kind, interfaceType, name), "%s.interface.%s for %s/%s", prefix, name, kind, interfaceType)
		}
	}
	for _, name := range sortedKeys(condition) {
		if !coreConditionFields[name] && resolved.ConditionFieldDeclared(name) {
			p.report.ExpectExactlyOne(resolved.ConditionFieldCount(kind, interfaceType, name), "%s.%s for %s/%s", prefix, name, kind, interfaceType)
		}
	}

	// Where extensions enumerate the values of a field, every value the
	// condition holds for that field has to be one of them.
	for _, field := range resolved.FieldValueFields(kind, interfaceType) {
		for _, found := range valuesAt(condition, field) {
			value, isString := found.value.(string)
			if !isString {
				continue
			}
			p.report.ExpectExactlyOne(resolved.FieldValueCount(field, kind, interfaceType, value), "%s.%s %s for %s/%s", prefix, found.path, value, kind, interfaceType)
		}
	}
	p.validateConditionSchemas(prefix, resolved, kind, interfaceType, raw)
}

// validateConditionSchemas applies every extension schema whose scope matches the
// condition. A schema with no declared scope applies to all conditions.
func (p *ProfileChecker) validateConditionSchemas(prefix string, resolved catalog.Vocabulary, kind string, interfaceType string, raw any) {
	for _, node := range resolved.Nodes() {
		for _, item := range node.Definition.Spec.Schemas {
			if item.AppliesToKind != "" && item.AppliesToKind != kind {
				continue
			}
			if item.AppliesToInterfaceType != "" && item.AppliesToInterfaceType != interfaceType {
				continue
			}
			schema, err := compileConditionSchema(item.Schema)
			if err != nil {
				p.report.Addf("%s schema %s could not be compiled: %v", prefix, item.ID, err)
				continue
			}
			instance, err := jsonSchemaValue(raw)
			if err != nil {
				p.report.Addf("%s could not be converted for schema validation: %v", prefix, err)
				continue
			}
			if err := schema.Validate(instance); err != nil {
				p.report.Addf("%s does not satisfy extension schema %s: %v", prefix, item.ID, err)
			}
		}
	}
}
