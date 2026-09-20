package app

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed web/*.html web/*.js web/*.css
var web embed.FS

func staticHandler() http.Handler {
	files, _ := fs.Sub(web, "web")
	static := http.FileServer(http.FS(files))
	entries, _ := fs.ReadDir(files, ".")
	assets := map[string]bool{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".js") || strings.HasSuffix(entry.Name(), ".css") {
			assets[entry.Name()] = true
		}
	}
	fingerprint := sha256.New()
	for _, entry := range entries {
		name := entry.Name()
		if !assets[name] {
			continue
		}
		data, _ := fs.ReadFile(files, name)
		fingerprint.Write([]byte(name))
		fingerprint.Write(data)
	}
	assetPrefix := fmt.Sprintf("/assets/%x/", fingerprint.Sum(nil)[:12])
	indexBytes, _ := fs.ReadFile(files, "index.html")
	index := string(indexBytes)
	for name := range assets {
		index = strings.ReplaceAll(index, `"/`+name+`"`, `"`+assetPrefix+name+`"`)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(index))
			return
		}
		versioned := strings.HasPrefix(r.URL.Path, assetPrefix)
		if versioned {
			r = r.Clone(r.Context())
			r.URL.Path = "/" + strings.TrimPrefix(r.URL.Path, assetPrefix)
		}
		if !assets[strings.TrimPrefix(r.URL.Path, "/")] {
			http.NotFound(w, r)
			return
		}
		if versioned {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		static.ServeHTTP(w, r)
	})
}
