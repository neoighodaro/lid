package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/neoighodaro/lid/internal/failure"
	"gopkg.in/yaml.v3"
)

// AddNetworkDomains adds domains to the network: list in the config at path,
// leaving the comments and key order the user wrote in place. Domains already
// listed are left alone, and the key is created when the file has none.
func AddNetworkDomains(path string, domains []string) error {
	return editNetwork(path, domains, nil)
}

// RemoveNetworkDomains drops domains from the network: list in the config at
// path. Domains that are not listed are ignored, and the key stays behind even
// when the list empties, so the comments around it survive.
func RemoveNetworkDomains(path string, domains []string) error {
	return editNetwork(path, nil, domains)
}

func editNetwork(path string, add, remove []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return failure.New("cannot read the config to edit it").At(path).WithFix("%v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return failure.New("cannot read the config to edit it").At(path).WithFix("%v", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return failure.New("cannot parse the config to edit it").At(path).
			WithFix("fix the YAML by hand, then try again: %v", err)
	}

	mapping, err := documentMapping(&doc)
	if err != nil {
		return failure.At(err, path)
	}
	seq, err := networkSequence(mapping)
	if err != nil {
		return failure.At(err, path)
	}

	if len(remove) > 0 {
		drop := make(map[string]bool, len(remove))
		for _, domain := range remove {
			drop[domain] = true
		}
		kept := make([]*yaml.Node, 0, len(seq.Content))
		for _, node := range seq.Content {
			if !drop[node.Value] {
				kept = append(kept, node)
			}
		}
		seq.Content = kept
	}

	for _, domain := range add {
		if containsScalar(seq, domain) {
			continue
		}
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: domain})
	}

	seq.Style = 0

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return failure.New("cannot rewrite the config").At(path).WithFix("%v", err)
	}
	if err := enc.Close(); err != nil {
		return failure.New("cannot rewrite the config").At(path).WithFix("%v", err)
	}

	return replaceFile(path, buf.Bytes(), info.Mode().Perm())
}

// replaceFile swaps in the new body through a temporary file in the same
// directory, so a config the user hand-wrote is never left half-written when
// the disk fills or the process dies mid-save.
func replaceFile(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return failure.New("cannot write next to the config").At(dir).WithFix("%v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return failure.New("cannot rewrite the config").At(path).WithFix("%v", err)
	}
	if err := tmp.Close(); err != nil {
		return failure.New("cannot rewrite the config").At(path).WithFix("%v", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return failure.New("cannot rewrite the config").At(path).WithFix("%v", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return failure.New("cannot rewrite the config").At(path).WithFix("%v", err)
	}
	return nil
}

func documentMapping(doc *yaml.Node) (*yaml.Node, error) {
	if len(doc.Content) == 0 {
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{mapping}
		return mapping, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, failure.New("the config is not a set of keys").
			WithFix("lid expects a YAML mapping, like 'name: my-project'")
	}
	return root, nil
}

func networkSequence(mapping *yaml.Node) (*yaml.Node, error) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "network" {
			continue
		}
		value := mapping.Content[i+1]
		switch {
		case value.Kind == yaml.SequenceNode:
			return value, nil
		case value.Tag == "!!null":
			value.Kind = yaml.SequenceNode
			value.Tag = "!!seq"
			value.Value = ""
			value.Content = nil
			return value, nil
		default:
			return nil, failure.New("'network' in the config is not a list of hosts").
				WithFix("write it as a list, one host per line, then try again")
		}
	}

	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "network"}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	mapping.Content = append(mapping.Content, key, seq)
	return seq, nil
}

func containsScalar(seq *yaml.Node, value string) bool {
	for _, node := range seq.Content {
		if node.Value == value {
			return true
		}
	}
	return false
}

// ValidateDomain rejects the network entries lid knows a backend cannot use, so
// a typo is caught before it reaches a firewall or the config file.
func ValidateDomain(domain string) error {
	if strings.TrimSpace(domain) == "" {
		return failure.New("a network entry is empty").WithFix("remove it, or name the host it should allow")
	}
	if strings.Contains(domain, "://") {
		return failure.New("network entry %q is a URL", domain).
			WithFix("drop the scheme and use a host pattern, like 'example.com' or '*.example.com'")
	}
	return nil
}
