package config

import "html/template"

var fm = template.FuncMap{
	"add": func(a, b int) int { return a + b },
}
var TPL = template.New("public").Funcs(fm)

func init() {
	TPL = template.Must(TPL.ParseGlob("../public/html/*.gohtml"))
	// TPL = template.Must(TPL.ParseGlob("public/html/*.gohtml"))
}
