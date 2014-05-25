// 24 may 2014
package main

import (
	"fmt"
	"os"
	"strings"
	"go/token"
	"go/ast"
	"go/parser"
	"sort"
	"io/ioutil"
	"path/filepath"
	"os/exec"
)

func getPackage(path string) (pkg *ast.Package) {
	fileset := token.NewFileSet()		// parser.ParseDir() actually writes to this; not sure why it doesn't return one instead
	filter := func(i os.FileInfo) bool {
		return strings.HasSuffix(i.Name(), "_windows.go")
	}
	pkgs, err := parser.ParseDir(fileset, path, filter, parser.AllErrors)
	if err != nil {
		panic(err)
	}
	if len(pkgs) != 1 {
		panic("more than one package found")
	}
	for k, _ := range pkgs {		// get the sole key
		pkg = pkgs[k]
	}
	return pkg
}

type walker struct {
	desired	func(string) bool
}

var known = map[string]string{}
var unknown = map[string]struct{}{}

func (w *walker) Visit(node ast.Node) ast.Visitor {
	if n, ok := node.(*ast.Ident); ok {
		if w.desired(n.Name) {
			if n.Obj != nil {
				delete(unknown, n.Name)
				kind := n.Obj.Kind.String()
				if known[n.Name] != "" && known[n.Name] != kind {
					panic(n.Name + "(" + kind + ") already known to be a " + known[n.Name])
				}
				known[n.Name] = kind
			} else if _, ok := known[n.Name]; !ok {		// only if not known
				unknown[n.Name] = struct{}{}
			}
		}
	}
	return w
}

func gatherNames(pkg *ast.Package) {
	desired := func(name string) bool {
		if strings.HasPrefix(name, "_") && len(name) > 1 {
			return !strings.ContainsAny(name,
				"abcdefghijklmnopqrstuvwxyz")
		}
		return false
	}
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			ast.Walk(&walker{desired}, d)
		}
	}
}

func preamble(pkg string) string {
	return "// autogenerated by windowsconstgen; do not edit\n" +
		"package " + pkg + "\n"
}

func main() {
	if len(os.Args) < 3 {
		panic("usage: " + os.Args[0] + " path goarch [go-command-options...]")
	}
	pkgpath := os.Args[1]
	targetarch := os.Args[2]
	goopts := os.Args[3:]		// valid if len(os.Args) == 3; in that case this will just be a slice of length zero

	pkg := getPackage(pkgpath)
	gatherNames(pkg)

	// if we still have some known, I didn't clean things up completely
	knowns := ""
	for ident, kind := range known {
		if kind != "var" && kind != "const" {
			continue
		}
		knowns += "\n" + ident + " (" + kind + ")"
	}
	if knowns != "" {
//ignore for now while I still migrate everything
//		panic("error: the following are still known!" + knowns)		// has a newline already
	}

	// keep sorted for git
	consts := make([]string, 0, len(unknown))
	for ident, _ := range unknown {
		consts = append(consts, ident)
	}
	sort.Strings(consts)

	// thanks to james4k in irc.freenode.net/#go-nuts
	tmpdir, err := ioutil.TempDir("", "windowsconstgen")
	if err != nil {
		panic(err)
	}
	genoutname := filepath.Join(tmpdir, "gen.go")
	f, err := os.Create(genoutname)
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(f, "%s" +
		"import \"fmt\"\n" +
		"// #include <windows.h>\n" +
		"// #include <commctrl.h>\n" +
		"import \"C\"\n" +
		"func main() {\n" +
		"	fmt.Println(%q)\n",
		preamble("main"), preamble("ui"))
	for _, ident := range consts {
		fmt.Fprintf(f, "	fmt.Println(\"const %s =\", C.%s)\n", ident, ident[1:])
	}
	fmt.Fprintf(f, "}\n")
	f.Close()

	cmd := exec.Command("go", "run")
	cmd.Args = append(cmd.Args, goopts...)		// valid if len(goopts) == 0; in that case this will just be a no-op
	cmd.Args = append(cmd.Args, genoutname)
	f, err = os.Create(filepath.Join(pkgpath, "zconstants_windows_" + targetarch + ".go"))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	cmd.Env = append(cmd.Env, os.Environ()...)		// otherwise $PATH doesn't get carried over and things mysteriously fail
	cmd.Env = append(cmd.Env,
		"GOOS=windows",
		"GOARCH=" + targetarch,
		"CGO_ENABLED=1")		// needed as it's not set by default in cross-compiles
	err = cmd.Run()
	if err != nil {
		// TODO find a way to get the exit code
		os.Exit(1)
	}

	// TODO remove the temporary directory
}