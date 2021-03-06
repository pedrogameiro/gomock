// gomock generates method stubs for implementing an interface.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/pborman/getopt"
	"golang.org/x/tools/imports"
)

const usageParameters = `<package> <interface>
Generates mocks for a go interface. 

<package>
	Package name or path to the package of interface to mock

<interface>
	Name of the interface to mock

Examples:
    gomock hash Hash
    gomock golang.org/x/tools/godoc/analysis Link 

    gomock --package testutils io Reader
    gomock --directory $GOPATH/src/github.com/pedrogameiro/gomock hash Hash
`

// findInterface returns the import path and identifier of an interface.
// For example, given "http.ResponseWriter", findInterface returns
// "net/http", "ResponseWriter".
// If a fully qualified interface is given, such as "net/http.ResponseWriter",
// it simply parses the input.
func findInterface(iface string, srcDir string) (path string, id string, err error) {
	if len(strings.Fields(iface)) != 1 {
		return "", "", fmt.Errorf("couldn't parse interface: %s", iface)
	}

	srcPath := filepath.Join(srcDir, "__go_impl__.go")

	if slash := strings.LastIndex(iface, "/"); slash > -1 {
		// package path provided
		dot := strings.LastIndex(iface, ".")
		// make sure iface does not end with "/" (e.g. reject net/http/)
		if slash+1 == len(iface) {
			return "", "", fmt.Errorf("interface name cannot end with a '/' character: %s", iface)
		}
		// make sure iface does not end with "." (e.g. reject net/http.)
		if dot+1 == len(iface) {
			return "", "", fmt.Errorf("interface name cannot end with a '.' character: %s", iface)
		}
		// make sure iface has exactly one "." after "/" (e.g. reject net/http/httputil)
		if strings.Count(iface[slash:], ".") != 1 {
			return "", "", fmt.Errorf("invalid interface name: %s", iface)
		}
		return iface[:dot], iface[dot+1:], nil
	}

	src := []byte("package hack\n" + "var i " + iface)
	// If we couldn't determine the import path, goimports will
	// auto fix the import path.
	imp, err := imports.Process(srcPath, src, nil)
	if err != nil {
		return "", "", fmt.Errorf("couldn't parse interface: %s", iface)
	}

	// imp should now contain an appropriate import.
	// Parse out the import and the identifier.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, srcPath, imp, 0)
	if err != nil {
		panic(err)
	}
	if len(f.Imports) == 0 {
		return "", "", fmt.Errorf("unrecognized interface: %s", iface)
	}
	raw := f.Imports[0].Path.Value   // "io"
	path, err = strconv.Unquote(raw) // io
	if err != nil {
		panic(err)
	}
	decl := f.Decls[1].(*ast.GenDecl)      // var i io.Reader
	spec := decl.Specs[0].(*ast.ValueSpec) // i io.Reader
	sel := spec.Type.(*ast.SelectorExpr)   // io.Reader
	id = sel.Sel.Name                      // Reader
	return path, id, nil
}

// Pkg is a parsed build.Package.
type Pkg struct {
	*build.Package
	*token.FileSet
}

// Spec is ast.TypeSpec with the associated comment map.
type Spec struct {
	*ast.TypeSpec
	ast.CommentMap
}

// typeSpec locates the *ast.TypeSpec for type id in the import path.
func typeSpec(path string, id string, srcDir string) (Pkg, Spec, []*ast.ImportSpec, error) {
	pkg, err := build.Import(path, srcDir, 0)
	if err != nil {
		return Pkg{}, Spec{}, nil, fmt.Errorf("couldn't find package %s: %v", path, err)
	}

	fset := token.NewFileSet() // share one fset across the whole package
	var files []string
	files = append(files, pkg.GoFiles...)
	files = append(files, pkg.CgoFiles...)
	for _, file := range files {
		f, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, file), nil, parser.ParseComments)
		if err != nil {
			continue
		}

		cmap := ast.NewCommentMap(fset, f, f.Comments)

		for _, decl := range f.Decls {
			decl, ok := decl.(*ast.GenDecl)
			if !ok || decl.Tok != token.TYPE {
				continue
			}
			for _, spec := range decl.Specs {
				spec := spec.(*ast.TypeSpec)
				if spec.Name.Name != id {
					continue
				}
				p := Pkg{Package: pkg, FileSet: fset}
				s := Spec{TypeSpec: spec, CommentMap: cmap.Filter(decl)}
				return p, s, f.Imports, nil
			}
		}
	}
	return Pkg{}, Spec{}, nil, fmt.Errorf("type %s not found in %s", id, path)
}

// gofmt pretty-prints e.
func (p Pkg) gofmt(e ast.Expr) string {
	var buf bytes.Buffer
	printer.Fprint(&buf, p.FileSet, e)
	return buf.String()
}

// fullType returns the fully qualified type of e.
// Examples, assuming package net/http:
// 	fullType(int) => "int"
// 	fullType(Handler) => "http.Handler"
// 	fullType(io.Reader) => "io.Reader"
// 	fullType(*Request) => "*http.Request"
func (p Pkg) fullType(e ast.Expr) string {
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			// Using typeSpec instead of IsExported here would be
			// more accurate, but it'd be crazy expensive, and if
			// the type isn't exported, there's no point trying
			// to implement it anyway.
			if n.IsExported() {
				n.Name = p.Package.Name + "." + n.Name
			}
		case *ast.SelectorExpr:
			return false
		}
		return true
	})
	return p.gofmt(e)
}

func (p Pkg) params(field *ast.Field) []Param {
	var params []Param
	typ := p.fullType(field.Type)
	for _, name := range field.Names {
		params = append(params, Param{Name: name.Name, Type: typ})
	}
	// Handle anonymous params
	if len(params) == 0 {
		params = []Param{Param{Type: typ}}
	}
	return params
}

// Method represents a method signature.
type Method struct {
	Recv     string
	RecvVar  string
	RecvName string
	Func
}

// Func represents a function signature.
type Func struct {
	Name     string
	Params   []Param
	Res      []Param
	Comments string
}

// Param represents a parameter in a function or method signature.
type Param struct {
	Name     string
	Type     string
	Variadic bool
}

func (p Pkg) funcsig(f *ast.Field, cmap ast.CommentMap) Func {
	fn := Func{Name: f.Names[0].Name}
	typ := f.Type.(*ast.FuncType)
	if typ.Params != nil {
		for i, field := range typ.Params.List {
			for _, param := range p.params(field) {
				// only for method parameters:
				// assign a blank identifier "_" to an anonymous parameter
				if param.Name == "" || param.Name == "_" {
					param.Name = "p" + strconv.Itoa(i)
				}
				if param.Type[0:3] == "..." {
					param.Variadic = true
				}
				fn.Params = append(fn.Params, param)
			}
		}
	}
	if typ.Results != nil {
		for _, field := range typ.Results.List {
			fn.Res = append(fn.Res, p.params(field)...)
		}
	}
	if commentsBefore(f, cmap.Comments()) {
		fn.Comments = flattenCommentMap(cmap)
	}
	return fn
}

// The error interface is built-in.
var errorInterface = []Func{{
	Name: "Error",
	Res:  []Param{{Type: "string"}},
}}

// funcs returns the set of methods required to implement iface.
// It is called funcs rather than methods because the
// function descriptions are functions; there is no receiver.
func funcs(iface string, srcDir string) ([]Func, []*ast.ImportSpec, error) {
	// Special case for the built-in error interface.
	if iface == "error" {
		return errorInterface, nil, nil
	}

	// Locate the interface.
	path, id, err := findInterface(iface, srcDir)
	if err != nil {
		return nil, nil, err
	}

	// Parse the package and find the interface declaration.
	p, spec, astImpt, err := typeSpec(path, id, srcDir)
	if err != nil {
		return nil, nil, fmt.Errorf("interface %s not found: %s", iface, err)
	}
	idecl, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		return nil, nil, fmt.Errorf("not an interface: %s", iface)
	}

	if idecl.Methods == nil {
		return nil, nil, fmt.Errorf("empty interface: %s", iface)
	}

	var fns []Func
	for _, fndecl := range idecl.Methods.List {
		if len(fndecl.Names) == 0 {
			// Embedded interface: recurse
			var embedded []Func
			embedded, astImpt, err = funcs(p.fullType(fndecl.Type), srcDir)
			if err != nil {
				return nil, nil, err
			}
			fns = append(fns, embedded...)
			continue
		}

		fn := p.funcsig(fndecl, spec.CommentMap.Filter(fndecl))
		fns = append(fns, fn)
	}
	return fns, astImpt, nil
}

const stub = "// {{.Name}} Mock\n" +
	"{{if .Comments}}{{.Comments}}{{end}}" +
	"func ({{.Recv}}) {{.Name}}" +
	"({{range .Params}}{{.Name}} {{.Type}}, {{end}})" +
	"({{range .Res}}{{.Name}} {{.Type}}, {{end}})" +
	"{\n" +
	`if {{.RecvVar}}.{{.Name}}Mock == nil { ` +
	`{{.RecvVar}}.T.Log("\n" + string(debug.Stack()) + "\n")` + "\n" +
	`{{.RecvVar}}.T.Fatal("Unimplemented mock {{.Recv}}.{{.Name}} was called") }` + "\n" +
	`{{if .Res}}return{{end}} {{.RecvVar}}.{{.Name}}Mock` +
	`({{range .Params}}{{.Name}}{{if .Variadic }}...{{end}},  {{end}})` +
	"\n}\n\n"

const mockStruct = "// {{.RecvName}} Mock\n" +
	"type {{.RecvName}} struct {\n" +
	"T *testing.T \n"

const methodDeclaration = "{{.Name}}Mock func" +
	"({{range .Params}}{{.Name}} {{.Type}}, {{end}})" +
	"({{range .Res}}{{.Name}} {{.Type}}, {{end}})\n"

var tmpl = template.Must(template.New("test").Parse(stub))
var tmplMockStruct = template.Must(template.New("test").Parse(mockStruct))
var tmplMethodDeclaration = template.Must(template.New("test").Parse(methodDeclaration))

// genStubs prints nicely formatted method stubs
// for fns using receiver expression recv.
// If recv is not a valid receiver expression,
// genStubs will panic.
// genStubs won't generate stubs for
// alrzeady implemented methods of receiver.
func genStubs(packageName, recv string, fns []Func, srcDir string, astImpt []*ast.ImportSpec, ifacePath string) []byte {
	var buf bytes.Buffer

	buf.Write([]byte("package " + packageName + "\n"))

	buf.Write([]byte("import (\n"))
	buf.Write([]byte(`"` + ifacePath + `"` + "\n"))
	if astImpt != nil {
		for _, i := range astImpt {
			if i.Path == nil {
				continue
			}
			if i.Name != nil {
				buf.Write([]byte(i.Name.Name + " "))
			}
			buf.Write([]byte(i.Path.Value + "\n"))
		}
	}
	buf.Write([]byte(")\n"))

	for i, fn := range fns {
		recvVar := strings.Split(recv, " ")[0]
		recvName := strings.Split(recv, " ")[1][1:]
		meth := Method{Recv: recv, Func: fn, RecvName: recvName, RecvVar: recvVar}

		if i == 0 {
			tmplMockStruct.Execute(&buf, meth)
		}

		tmplMethodDeclaration.Execute(&buf, meth)

		if i == len(fns)-1 {
			buf.Write([]byte("}\n"))
		}

	}

	for _, fn := range fns {
		recvVar := strings.Split(recv, " ")[0]
		recvName := strings.Split(recv, " ")[1][1:]
		meth := Method{Recv: recv, Func: fn, RecvName: recvName, RecvVar: recvVar}

		err := tmpl.Execute(&buf, meth)
		if err != nil {
			panic(err)
		}
	}

	pretty, err := imports.Process(srcDir+"/mock.go", buf.Bytes(), nil)
	if err != nil {
		panic(err.Error() + string(buf.Bytes()))
	}
	return pretty
}

// commentsBefore reports whether commentGroups precedes a field.
func commentsBefore(field *ast.Field, cg []*ast.CommentGroup) bool {
	if len(cg) > 0 {
		return cg[0].Pos() < field.Pos()
	}
	return false
}

// flattenCommentMap flattens the comment map to a string.
// This function must be used at the point when m is expected to have a single
// element.
func flattenCommentMap(m ast.CommentMap) string {
	if len(m) != 1 {
		panic("flattenCommentMap expects comment map of length 1")
	}
	var result strings.Builder
	for _, cgs := range m {
		for _, cg := range cgs {
			for _, c := range cg.List {
				result.WriteString(c.Text)
				// add an end-of-line character if this is '//'-style comment
				if c.Text[1] == '/' {
					result.WriteString("\n")
				}
			}
		}
	}

	// for '/*'-style comments, make sure to append EOL character to the comment
	// block
	if s := result.String(); !strings.HasSuffix(s, "\n") {
		result.WriteString("\n")
	}

	return result.String()
}

func main() {
	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	getopt.SetParameters(usageParameters)
	optDir := getopt.StringLong("directory", 'd', pwd, "package source directory, useful for vendored code")
	optPKGName := getopt.StringLong("package", 'p', "mocks", "package name")
	optHelp := getopt.BoolLong("help", 'h', "Help")
	getopt.Parse()

	argsLen := len(getopt.Args())
	if *optHelp || argsLen < 2 || argsLen > 2 {
		getopt.Usage()
		os.Exit(0)
	}

	ifacePath := getopt.Arg(0)
	ifaceName := getopt.Arg(1)
	recv := "m *" + ifaceName

	iface := ifacePath + "." + ifaceName
	fns, astImpt, err := funcs(iface, *optDir)
	if err != nil {
		fatal(err)
	}

	src := genStubs(*optPKGName, recv, fns, *optDir, astImpt, ifacePath)
	fmt.Print(string(src))
}

func fatal(msg interface{}) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
