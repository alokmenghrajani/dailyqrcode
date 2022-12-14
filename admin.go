package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"text/template"
	"time"

	"gorm.io/gorm"
)

func (app *App) admin(w http.ResponseWriter, r *http.Request) {
	var queued []Url
	isAdmin := isAdmin(w, r)
	n := truncatedNow()

	if isAdmin {
		err := app.db.Where("active_at > ?", n).Find(&queued).Error
		panicOnErr(err)
	}

	w.Header().Add("Cache-Control", "private, no-store")

	// render page
	const html = `<html>
		<head>
			<link rel="icon" type="image/png" href="/static/favicon.png" sizes="32x32">
		</head>
		<body>
			<h1>Admin</h1>
			{{if .IsAdmin }}
				<p>Current time: {{.Now}}</p>
				<p>Queue size: {{len .Queued}}</p>
				<form action="/admin/add" method="POST">
					<p>url: <input name="url" type="text"></p>
					<p>comment: <input name="comment" type="text"></p>
					<p>private: <input name="private" type="text"></p>
					<p><input type="submit" value="add url"></p>
				</form>
				<h2>Queued</h2>
				<ul>
				{{range .Queued}}
					<li>
						<p><a href="{{.Url}}">{{.Url}}</a></p>
						<p>Active at: {{.ActiveAt.Format "Jan 2, 2006"}}</p>
						<p>Comment: {{.Comment}}</p>
						<p>Private: {{.Private}}</p>
					</li>
				{{else}}
					<b>No more items in queue!</b>
				{{end}}
				<ul>
			{{else}}
				<form action="/admin/login" method="POST">
					<p>password: <input name="password" type="password"></p>
					<p><input type="submit" value="log in"></p>
				</form>
			{{end}}
		</body>
	</html>`
	t, err := template.New("html").Parse(html)
	panicOnErr(err)

	err = t.ExecuteTemplate(w, "html", struct {
		Now     time.Time
		IsAdmin bool
		Queued  []Url
	}{Now: time.Now(), IsAdmin: isAdmin, Queued: queued})
	panicOnErr(err)
}

func (app *App) adminLogin(w http.ResponseWriter, r *http.Request) {
	cookie := &http.Cookie{
		Name:     "admin",
		Value:    r.PostFormValue("password"),
		Expires:  time.Now().Add(365 * 24 * time.Hour),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (app *App) adminAdd(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(w, r) {
		return
	}
	now := truncatedNow()
	var mostRecent Url
	var newTime time.Time
	err := app.db.Order("id desc").First(&mostRecent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		newTime = now
		err = nil
	} else {
		newTime = mostRecent.ActiveAt.Add(24 * time.Hour)
	}
	panicOnErr(err)

	// Don't backfill gaps
	if newTime.Before(now) {
		newTime = now
	}

	u := r.PostFormValue("url")
	newUrl := Url{
		Url:      &u,
		ActiveAt: newTime,
		Comment:  r.PostFormValue("comment"),
		Private:  r.PostFormValue("private"),
	}
	err = app.db.Save(&newUrl).Error
	panicOnErr(err)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func isAdmin(w http.ResponseWriter, r *http.Request) bool {
	adminCookie, err := r.Cookie("admin")
	if err != nil {
		return false
	}
	h := sha256.New()
	h.Write([]byte(adminCookie.Value))
	key, err := base64.StdEncoding.DecodeString(os.Getenv("adminkey"))
	panicOnErr(err)
	return bytes.Equal(h.Sum(nil), key)
}
