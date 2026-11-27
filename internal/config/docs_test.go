package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEveryConfigKeyIsDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	var headings []string
	for _, line := range strings.Split(string(doc), "\n") {
		if strings.HasPrefix(line, "#") {
			headings = append(headings, line)
		}
	}
	keys := append(yamlKeys(reflect.TypeOf(Config{}), ""), sectionsKey)
	for _, key := range keys {
		if !headingNames(headings, key) {
			t.Errorf("docs/configuration.md has no heading for `%s`", key)
		}
	}
}

func headingNames(headings []string, key string) bool {
	for _, heading := range headings {
		if strings.Contains(heading, "`"+key+"`") {
			return true
		}
	}
	return false
}

func yamlKeys(t reflect.Type, prefix string) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		key := prefix + name
		inner := field.Type
		if inner.Kind() == reflect.Slice {
			inner = inner.Elem()
		}
		if inner.Kind() == reflect.Struct {
			keys = append(keys, yamlKeys(inner, key+".")...)
			continue
		}
		keys = append(keys, key)
	}
	return keys
}
