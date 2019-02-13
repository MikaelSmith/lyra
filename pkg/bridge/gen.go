package bridge

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform/helper/schema"
)

var prefix = `// Code generated by Lyra DO NOT EDIT.

// This code is generated on a per-provider basis using "tf-gen"
// Long term our hope is to remove this generation step and adopt dynamic approach

package generated

import (
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
	"github.com/lyraproj/lyra/pkg/bridge"
	"github.com/lyraproj/puppet-evaluator/eval"
	"github.com/lyraproj/servicesdk/service"
)
`

var providerTemplate = `
// {{.TitleType}}Handler ...
type {{.TitleType}}Handler struct {
	provider *schema.Provider
}

// Create ...
func (h *{{.TitleType}}Handler) Create(desired *{{.TitleType}}) (*{{.TitleType}}, string, error) {
	rc := &terraform.ResourceConfig{
		Config: bridge.TerraformMarshal(desired),
	}
	id, err := bridge.Create(h.provider, "{{.TFType}}", rc)
	if err != nil {
		return nil, "", err
	}
	actual, err := h.Read(id)
	if err != nil {
		return nil, "", err
	}
	return actual, id, nil
}

// Read ...
func (h *{{.TitleType}}Handler) Read(externalID string) (*{{.TitleType}}, error) {
	id, actual, err := bridge.Read(h.provider, "{{.TFType}}", externalID)
	if err != nil {
		return nil, err
	}
	x := &{{.TitleType}}{ {{.TitleType}}_id: &id }
	bridge.TerraformUnmarshal(actual, x)
	return x, nil
}

// Delete ...
func (h *{{.TitleType}}Handler) Delete(externalID string) error {
	return bridge.Delete(h.provider, "{{.TFType}}", externalID)
}
`

var typeTemplate = `
type {{.goType}} struct {
{{range .fields}}
    {{.name}} {{.typ}}
{{end}}
}
`

var saltValue int

func deriveType(goType, name string) string {
	saltValue++
	return fmt.Sprintf("%s_%s_%d", goType, name, saltValue)
}

func getGoType(handler io.Writer, goType, name string, s *schema.Schema) string {
	//   TypeBool - bool
	//   TypeInt - int
	//   TypeFloat - float64
	//   TypeString - string
	//   TypeList - []interface{}
	//   TypeMap - map[string]interface{}
	//   TypeSet - *schema.Set
	var t string
	switch s.Type {
	case schema.TypeBool:
		t = "bool"
	case schema.TypeInt:
		t = "int"
	case schema.TypeFloat:
		t = "float64"
	case schema.TypeString:
		t = "string"
	case schema.TypeList:
		switch s.Elem.(type) {
		case *schema.Resource:
			t = deriveType(goType, name)
			generateResourceType(handler, t, s.Elem.(*schema.Resource), false)
			t = "[]" + t
		case *schema.Schema:
			t = "[]" + getGoType(handler, goType, name, s.Elem.(*schema.Schema))
		default:
			panic(fmt.Sprintf("Unsupported TypeList: %v", s.Elem))
		}
	case schema.TypeMap:
		t = "map[string]string"
	case schema.TypeSet:
		switch s.Elem.(type) {
		case *schema.Resource:
			t = deriveType(goType, name)
			generateResourceType(handler, t, s.Elem.(*schema.Resource), false)
			t = "[]" + t
		case *schema.Schema:
			t = "[]" + getGoType(handler, goType, name, s.Elem.(*schema.Schema))
		default:
			panic(fmt.Sprintf("Unsupported TypeSet: %v", s.Elem))
		}
	default:
		panic(fmt.Sprintf("Unknown schema type: %v", s.Type))
	}
	return t
}

func getGoTypeWithPtr(handler io.Writer, goType, name string, s *schema.Schema) string {
	t := getGoType(handler, goType, name, s)
	if !s.Required {
		t = "*" + t
	}
	return t
}

func generateResourceType(handler io.Writer, goType string, r *schema.Resource, insertID bool) {
	// Sort fields to give predictable code generation
	names := make([]string, 0)
	for name := range r.Schema {
		names = append(names, name)
	}
	sort.Strings(names)
	// Determine field names and types
	var fields []map[string]string
	if insertID {
		fields = []map[string]string{map[string]string{
			"name": goType + "_id",
			"typ":  "*string `lyra:\"ignore\"`",
		}}
	} else {
		fields = []map[string]string{}
	}
	for _, name := range names {
		fields = append(fields, map[string]string{
			"name": strings.Title(name),
			"typ":  getGoTypeWithPtr(handler, goType, name, r.Schema[name]),
		})
	}
	// Render template
	tmpl := template.Must(template.New("").Parse(typeTemplate))
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, map[string]interface{}{
		"goType": goType,
		"fields": fields,
	})
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(handler, buf.String())

}

type providerType struct {
	TitleType string
	TFType    string
}

func generateProvider(handler io.Writer, rType string) {
	tmpl := template.Must(template.New("provider").Parse(providerTemplate))
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, providerType{strings.Title(rType), rType})
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(handler, buf.String())
}

func mkdirs(filename string) {
	dirName := filepath.Dir(filename)
	if _, serr := os.Stat(dirName); serr != nil {
		merr := os.MkdirAll(dirName, os.ModePerm)
		if merr != nil {
			panic(merr)
		}
	}
}

// Generate the Lyra boilerplate needed to bridge to a Terraform provider
func Generate(p *schema.Provider, ns, filename string) {

	handler := bytes.NewBufferString("")
	fmt.Fprintf(handler, prefix)
	fmt.Fprintf(handler, "\nfunc Initialize(sb *service.ServerBuilder, p *schema.Provider) {\n")
	fmt.Fprintf(handler, "    var evs []eval.Type\n")

	rTypes := make([]string, 0)
	for rType := range p.ResourcesMap {
		rTypes = append(rTypes, rType)
	}
	sort.Strings(rTypes)

	for _, rType := range rTypes {
		rTitleType := strings.Title(rType)
		fmt.Fprintf(handler, "    evs = sb.RegisterTypes(\"%s\", %s{})\n", ns, rTitleType)
		fmt.Fprintf(handler, "    sb.RegisterHandler(\"%s::%sHandler\", &%sHandler{provider: p}, evs[0])\n", ns, rTitleType, rTitleType)
	}
	fmt.Fprintf(handler, "}\n\n")

	for _, rType := range rTypes {
		r := p.ResourcesMap[rType]
		generateResourceType(handler, strings.Title(rType), r, true)
		generateProvider(handler, rType)
	}

	mkdirs(filename)
	err := ioutil.WriteFile(filename, handler.Bytes(), 0644)
	if err != nil {
		panic(err)
	}

}
