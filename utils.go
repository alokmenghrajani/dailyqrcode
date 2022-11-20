package main

import (
	"net/http"
	"runtime/debug"
	"text/template"
	"time"
)

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}

func truncatedNow() time.Time {
	return time.Now().Truncate(24 * time.Hour)
}

func nopanic(h func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Header().Add("Cache-Control", "public, max-age=3600")

				const html = `<html>
					<head>
						<link href="/static/style.css" rel="stylesheet">
						<link rel="icon" type="image/png" href="/static/favicon.png" sizes="32x32">
					</head>
					<body>
						<h1>Error</h1>
						<p>{{.V}}</p>
					</body>
				</html>`
				t, err := template.New("html").Parse(html)
				panicOnErr(err)

				err = t.ExecuteTemplate(w, "html", struct{ V, Stack interface{} }{V: v, Stack: string(debug.Stack())})
				panicOnErr(err)
			}
		}()
		h(w, r)
	}
}
