package config

import (
	"html/template"
	"path/filepath"
	"runtime"
)

var fm = template.FuncMap{
	"add": func(a, b int) int { return a + b },
}
var TPL = template.New("public").Funcs(fm)

func init() {
	_, filename, _, _ := runtime.Caller(0)

	dir := filepath.Dir(filename)

	baseDir := filepath.Join(dir, "../../")

	filePattern := filepath.Join(baseDir, "public", "html", "*.gohtml")

	TPL = template.Must(TPL.ParseGlob(filePattern))
}
