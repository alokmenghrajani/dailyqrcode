package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/skip2/go-qrcode"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var redirectPath = regexp.MustCompile(`^/[0-9]+$`)

type App struct {
	db *gorm.DB
}

type Url struct {
	Id       uint `gorm:"primary;auto_increment"`
	ActiveAt time.Time
	Url      *string
	Comment  string
	Private  string
}

func main() {
	var err error

	// Setup app
	app := &App{}

	database := os.Getenv("DATABASE_URL")
	if database == "" {
		app.db, err = gorm.Open(sqlite.Open("daily_qr_code.sqlite"))
	} else {
		app.db, err = gorm.Open(postgres.Open(database))
	}
	panicOnErr(err)

	err = app.db.AutoMigrate(&Url{})
	panicOnErr(err)

	// Static pages
	fs := http.FileServer(http.Dir("./static"))
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Main functionality
	mux.HandleFunc("/img/", nopanic(app.image))
	mux.HandleFunc("/large/", nopanic(app.largeImage))
	mux.HandleFunc("/about", nopanic(app.about))
	mux.HandleFunc("/archive", nopanic(app.archive))
	mux.HandleFunc("/sitemap.xml", nopanic(app.sitemap))
	mux.HandleFunc("/l/", nopanic(app.redirect))
	mux.HandleFunc("/", nopanic(app.index))
	mux.HandleFunc("/robots.txt", nopanic(app.robotsTxt))

	// Admin pages
	mux.HandleFunc("/admin", nopanic(app.admin))
	mux.HandleFunc("/admin/login", nopanic(app.adminLogin))
	mux.HandleFunc("/admin/add", nopanic(app.adminAdd))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	err = http.ListenAndServe(fmt.Sprintf(":%s", port), mux)
	log.Fatal(err)
}

func (app *App) index(w http.ResponseWriter, r *http.Request) {
	if redirectPath.MatchString(r.URL.Path) {
		id := r.URL.Path[1:]
		var url Url
		err := app.db.First(&url, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.Header().Add("Cache-Control", "public, max-age=3600")
			http.NotFound(w, r)
			return
		}
		panicOnErr(err)

		if url.ActiveAt.After(truncatedNow()) {
			w.Header().Add("Cache-Control", "public, max-age=3600")
			http.NotFound(w, r)
			return
		}

		w.Header().Add("Cache-Control", "public, max-age=86400, immutable")
		renderPage(w, fmt.Sprintf("#%d", url.Id), url)
		return
	}

	// Grab current url
	var url Url
	now := truncatedNow()
	err := app.db.Where("active_at=?", now).First(&url).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Find most recent url
		err = app.db.Where("active_at<?", now).Order("id desc").First(&url).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			panic("Sorry, the site is broken real bad.")
		}
	}

	w.Header().Add("Cache-Control", "public, max-age=3600")
	renderPage(w, "Daily QR Code", url)
}

func renderPage(w http.ResponseWriter, title string, url Url) {
	const html = `<html>
	<head>
		<link href="/static/style.css" rel="stylesheet">
		<link rel="icon" type="image/png" href="/static/favicon.png" sizes="32x32">
		<title>Daily QR Code | #{{.Url.Id}}</title>
		<meta property="og:title" content="Daily QR Code #{{.Url.Id}}">
		<meta property="og:description" content="A fresh surprise every day!">
		<meta property="og:type" content="article">
		<meta property="og:url" content="https://da.ilyqrco.de/{{.Url.Id}}">
		<meta property="og:image" content="https://da.ilyqrco.de/large/{{.Url.Id}}">
		<meta name="twitter:card" content="summary_large_image">
		<meta name="viewport" content="width=device-width, initial-scale=1">
	</head>
	<body>
		<h1>{{.Title}}</h1>
		<div>Scan with your phone's camera app &#x25A0; Come back tomorrow!</div>
		<div id="d">
			<img class="tl" src="/static/tl.png">
			<img class="br" src="/static/br.png">
			<img id="i" src="/img/{{ .Url.Id }}">
		</div>
		<div>{{if .Url.Comment}} {{ .Url.Comment }} {{end}}</div>
		<div><a href="/about">About</a> &#x25A0; <a href="/archive">Archive</a></div>
	</body>
</html>`
	t, err := template.New("html").Parse(html)
	panicOnErr(err)

	err = t.ExecuteTemplate(w, "html", struct {
		Url   Url
		Title string
	}{Url: url, Title: title})
	panicOnErr(err)
}

// The QR code points to /l/<id>
func (app *App) redirect(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/l/") {
		panic(fmt.Errorf("unexpected path: %s", r.URL.Path))
	}
	id := r.URL.Path[3:]
	var url Url
	err := app.db.First(&url, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		w.Header().Add("Cache-Control", "public, max-age=3600")
		http.NotFound(w, r)
		return
	}
	panicOnErr(err)

	if url.ActiveAt.After(truncatedNow()) {
		w.Header().Add("Cache-Control", "public, max-age=3600")
		http.NotFound(w, r)
		return
	}

	w.Header().Add("Cache-Control", "public, max-age=86400, immutable")
	http.Redirect(w, r, *url.Url, http.StatusSeeOther)
}

func (app *App) about(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "public, max-age=86400, immutable")
	const html = `<html>
		<head>
			<title>About Daily QR Code</title>
			<link href="/static/style.css" rel="stylesheet">
			<link rel="icon" type="image/png" href="/static/favicon.png" sizes="32x32">
			<meta name="viewport" content="width=device-width, initial-scale=1">
		</head>
		<body>
			<h1>Daily QRCode</h1>
			<ul>
				<li>https://da.ilyqrco.de/ is nonjudgemental — everyone gets the same content.
				<li>https://da.ilyqrco.de/ is a fresh surprise every day — see you tomorrow!
				<li>https://da.ilyqrco.de/ is <a href="https://github.com/alokmenghrajani/dailyqrcode">open source</a>.
			</ul>
		</body>
	</html>`
	t, err := template.New("html").Parse(html)
	panicOnErr(err)

	err = t.ExecuteTemplate(w, "html", nil)
	panicOnErr(err)
}

func (app *App) archive(w http.ResponseWriter, r *http.Request) {
	var allUrls []Url
	now := truncatedNow()
	err := app.db.Where("active_at <= ?", now).Find(&allUrls).Error
	panicOnErr(err)

	w.Header().Add("Cache-Control", "public, max-age=3600")
	const html = `<html>
		<head>
			<title>Daily QR Code Archive</title>
			<link rel="icon" type="image/png" href="/static/favicon.png" sizes="32x32">
			<meta name="viewport" content="width=device-width, initial-scale=1">
		</head>
		<body>
			<h1>Archive</h1>
			<ul>
				{{range .AllUrls}}
					<li>
					  <p><a href="/{{.Id}}">{{.ActiveAt.Format "Jan 2, 2006"}}</a>: {{.Comment}}<br><small><a href="{{.Url}}">{{.Url}}</a></small></p>
					</li>
				{{else}}
					<p>Sorry, archive is empty.</p>
				{{end}}
			</ul>
		</body>
	</html>`
	t, err := template.New("html").Parse(html)
	panicOnErr(err)

	err = t.ExecuteTemplate(w, "html", struct{ AllUrls []Url }{AllUrls: allUrls})
	panicOnErr(err)
}

func (app *App) sitemap(w http.ResponseWriter, r *http.Request) {
	var allUrls []Url
	now := truncatedNow()
	err := app.db.Where("active_at <= ?", now).Find(&allUrls).Error
	panicOnErr(err)

	w.Header().Add("Content-Type", "application/xml")
	w.Header().Add("Cache-Control", "public, max-age=3600")
	const xml = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xsi:schemaLocation="http://www.sitemaps.org/schemas/sitemap/0.9 http://www.sitemaps.org/schemas/sitemap/0.9/sitemap.xsd">
  <url>
    <loc>https://da.ilyqrco.de/</loc>
    <lastmod>{{.Now.Format "2006-01-02T15:04:05+07:00"}}</lastmod>
  </url>
  <url>
    <loc>https://da.ilyqrco.de/about</loc>
    <lastmod>{{.Now.Format "2006-01-02T15:04:05+07:00"}}</lastmod>
  </url>
  <url>
    <loc>https://da.ilyqrco.de/archive</loc>
    <lastmod>{{.Now.Format "2006-01-02T15:04:05+07:00"}}</lastmod>
  </url>
{{range .AllUrls}}
  <url>
    <loc>https://da.ilyqrco.de/{{.Id}}</loc>
    <lastmod>{{.ActiveAt.Format "2006-01-02T15:04:05+07:00"}}</lastmod>
  </url>
{{end}}
</urlset>`
	t, err := template.New("xml").Parse(xml)
	panicOnErr(err)

	err = t.ExecuteTemplate(w, "xml", struct {
		AllUrls []Url
		Now     time.Time
	}{AllUrls: allUrls, Now: now})
	panicOnErr(err)
}

func (app *App) image(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/img/") {
		panic(fmt.Errorf("unexpected path: %s", r.URL.Path))
	}
	id := r.URL.Path[5:]
	app.imageCommon(w, id, 1)
}

func (app *App) largeImage(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/large/") {
		panic(fmt.Errorf("unexpected path: %s", r.URL.Path))
	}
	id := r.URL.Path[7:]
	app.imageCommon(w, id, 25)
}

func (app *App) imageCommon(w http.ResponseWriter, id string, size int) {
	qr, err := qrcode.New(fmt.Sprintf("http://da.ilyqrco.de/l/%s", id), qrcode.Low)
	panicOnErr(err)

	qr.BackgroundColor = color.RGBA{0xff, 0xff, 0xff, 0x00}
	qr.ForegroundColor = color.RGBA{66, 176, 245, 0xff}
	tmColor := color.RGBA{166, 176, 245, 0xff}
	img := qr.Image(-size).(*image.Paletted)
	img.Palette = append(img.Palette, tmColor)
	tmColorIndex := uint8(img.Palette.Index(tmColor))
	for i := 0; i < 3*size; i++ {
		for j := 0; j < 3*size; j++ {
			pos := img.PixOffset(6*size+j, 6*size+i)
			img.Pix[pos] = tmColorIndex

			pos = img.PixOffset(img.Rect.Dx()-9*size+j, 6*size+i)
			img.Pix[pos] = tmColorIndex

			pos = img.PixOffset(6*size+j, img.Rect.Dy()-9*size+i)
			img.Pix[pos] = tmColorIndex
		}
	}

	encoder := png.Encoder{CompressionLevel: png.BestCompression}

	var b bytes.Buffer
	err = encoder.Encode(&b, img)
	panicOnErr(err)

	w.Header().Add("Cache-Control", "public, max-age=86400, immutable")
	w.Write(b.Bytes())
}

func (app *App) robotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("User-agent: *\nAllow: /\n\nSitemap: https://da.ilyqrco.de/sitemap.xml"))
}
