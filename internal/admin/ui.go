package admin

import (
	_ "embed"
	"net/http"
)

//go:embed admin.html
var adminHTML string

func AdminPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(adminHTML))
}
