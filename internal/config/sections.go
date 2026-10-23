package config

import (
	"strings"

	"github.com/neoighodaro/lid/internal/failure"
	"gopkg.in/yaml.v3"
)

const sectionsKey = "backends"

// effectiveDocument returns the top mapping of data as backend sees it: the
// shared config with backends.<backend> merged on top. A file without a
// backends key comes back exactly as parsed.
func effectiveDocument(data []byte, backend string) (*yaml.Node, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, false, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || valueOf(root, sectionsKey) == nil {
		return root, false, nil
	}

	base, sections, err := splitSections(root)
	if err != nil {
		return nil, true, err
	}
	if section, ok := sections[backend]; ok {
		if err := mergeSection(base, section, sectionsKey+"."+backend); err != nil {
			return nil, true, err
		}
	}
	return base, true, nil
}

// splitSections separates root into the shared config and its per-backend
// sections, both alias-free copies. sections is nil when root has no
// backends key; null sections are left out.
func splitSections(root *yaml.Node) (*yaml.Node, map[string]*yaml.Node, error) {
	raw := valueOf(root, sectionsKey)
	resolvedRoot := resolve(root)
	base := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: resolvedRoot.Line, Column: resolvedRoot.Column}
	for i := 0; i+1 < len(resolvedRoot.Content); i += 2 {
		if resolvedRoot.Content[i].Value != sectionsKey {
			base.Content = append(base.Content, resolvedRoot.Content[i], resolvedRoot.Content[i+1])
		}
	}
	if raw == nil {
		return base, nil, nil
	}

	sections := map[string]*yaml.Node{}
	rawSections := follow(raw)
	if isNull(rawSections) {
		return base, sections, nil
	}
	if rawSections.Kind != yaml.MappingNode {
		return nil, nil, failure.New("line %d: %s must be a map of backend names", raw.Line, sectionsKey).
			WithFix("write it as 'backends:' followed by 'sbx:' or 'nono:' sections")
	}

	for i := 0; i+1 < len(rawSections.Content); i += 2 {
		key, value := rawSections.Content[i], rawSections.Content[i+1]
		name := key.Value
		path := sectionsKey + "." + name
		if !contains(Backends, name) {
			return nil, nil, failure.New("line %d: lid has no backend named %q", key.Line, name).
				WithFix("use one of %s", strings.Join(Backends, ", "))
		}
		if line := mergeKeyLine(value); line != 0 {
			return nil, nil, failure.New("line %d: %s uses a << merge key", line, path).
				WithFix("write the keys out in the section; lists there already add to the shared ones")
		}
		section := resolve(value)
		if isNull(section) {
			continue
		}
		if section.Kind != yaml.MappingNode {
			return nil, nil, failure.New("line %d: %s is %s, but a section must be a map", value.Line, path, kindName(section)).
				WithFix("put the keys this backend adds under it")
		}
		for _, identity := range []string{"name", "backend"} {
			if k := keyOf(section, identity); k != nil {
				return nil, nil, failure.New("line %d: %s.%s cannot be set in a section", k.Line, path, identity).
					WithFix("set it once at the top of the file")
			}
		}
		sections[name] = section
	}
	return base, sections, nil
}

// mergeSection merges section into base: lists add, maps merge, single
// values replace, and a null section value changes nothing. Joined lists
// drop repeats, except scripts, where running a command twice is deliberate.
func mergeSection(base, section *yaml.Node, path string) error {
	for i := 0; i+1 < len(section.Content); i += 2 {
		key, sv := section.Content[i], section.Content[i+1]
		at := path + "." + key.Value
		if isNull(sv) {
			continue
		}
		bi := indexOf(base, key.Value)
		if bi < 0 {
			base.Content = append(base.Content, key, sv)
			continue
		}
		bv := base.Content[bi+1]
		if isNull(bv) {
			base.Content[bi+1] = sv
			continue
		}
		if sv.Kind != bv.Kind {
			return failure.New("line %d: %s is %s, but the shared config has %s", sv.Line, at, kindName(sv), kindName(bv)).
				WithFix("make them the same kind, or move the key out of the shared config")
		}
		switch sv.Kind {
		case yaml.MappingNode:
			if err := mergeSection(bv, sv, at); err != nil {
				return err
			}
		case yaml.SequenceNode:
			bv.Content = append(bv.Content, sv.Content...)
			if !strings.Contains(at, ".scripts.") {
				bv.Content = withoutRepeats(bv.Content)
			}
		default:
			base.Content[bi+1] = sv
		}
	}
	return nil
}

func withoutRepeats(items []*yaml.Node) []*yaml.Node {
	seen := map[string]bool{}
	out := items[:0:0]
	for _, item := range items {
		if item.Kind == yaml.ScalarNode {
			if seen[item.Value] {
				continue
			}
			seen[item.Value] = true
		}
		out = append(out, item)
	}
	return out
}

// resolve deep-copies n with every alias replaced by a copy of its anchor
// and every << merge key expanded, so the result can be merged and mutated
// without touching the document it came from.
func resolve(n *yaml.Node) *yaml.Node {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
		}
		return resolve(n.Content[0])
	case yaml.AliasNode:
		return resolve(n.Alias)
	}

	c := *n
	c.Anchor = ""
	c.Content = nil
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			c.Content = append(c.Content, resolve(item))
		}
	case yaml.MappingNode:
		var merged []*yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if key.ShortTag() == "!!merge" {
				merged = append(merged, value)
				continue
			}
			c.Content = append(c.Content, resolve(key), resolve(value))
		}
		for _, m := range merged {
			for _, from := range mergeSources(resolve(m)) {
				for i := 0; i+1 < len(from.Content); i += 2 {
					if indexOf(&c, from.Content[i].Value) < 0 {
						c.Content = append(c.Content, from.Content[i], from.Content[i+1])
					}
				}
			}
		}
	}
	return &c
}

func mergeSources(n *yaml.Node) []*yaml.Node {
	switch n.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{n}
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, item := range n.Content {
			if item.Kind == yaml.MappingNode {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}

func mergeKeyLine(n *yaml.Node) int {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].ShortTag() == "!!merge" {
				return n.Content[i].Line
			}
			if line := mergeKeyLine(n.Content[i+1]); line != 0 {
				return line
			}
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if line := mergeKeyLine(item); line != 0 {
				return line
			}
		}
	}
	return 0
}

func follow(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a map"
	}
	return "a single value"
}

func indexOf(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func keyOf(m *yaml.Node, key string) *yaml.Node {
	if i := indexOf(m, key); i >= 0 {
		return m.Content[i]
	}
	return nil
}

func valueOf(m *yaml.Node, key string) *yaml.Node {
	if i := indexOf(m, key); i >= 0 {
		return m.Content[i+1]
	}
	return nil
}
