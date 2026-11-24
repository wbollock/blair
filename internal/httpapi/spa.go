package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

var inlineScript = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.*?)</script>`)

// spa serves the embedded SvelteKit build with an index.html fallback.
// The CSP allows exactly the inline bootstrap scripts by hash (SPEC §9.7).
type spa struct {
	files fs.FS
	index []byte
	csp   string
}

func newSPA(files fs.FS) http.Handler {
	s := &spa{files: files}
	if files != nil {
		s.index, _ = fs.ReadFile(files, "index.html")
	}
	if s.index == nil {
		s.index = []byte(`<!doctype html><meta charset="utf-8"><title>Blair</title><p>The web app is not built. Run <code>mise run build</code>.</p>`)
	}
	hashes := []string{}
	for _, m := range inlineScript.FindAllSubmatch(s.index, -1) {
		if len(bytes.TrimSpace(m[1])) == 0 {
			continue
		}
		sum := sha256.Sum256(m[1])
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	s.csp = "default-src 'self'; script-src 'self' " + strings.Join(hashes, " ") +
		"; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'" +
		"; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
	return s
}

func statFS(files fs.FS, p string) (fs.FileInfo, error) {
	if files == nil {
		return nil, fs.ErrNotExist
	}
	return fs.Stat(files, p)
}

func (s *spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		return
	}
	w.Header().Set("Content-Security-Policy", s.csp)
	p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if p != "" && p != "index.html" {
		if st, err := statFS(s.files, p); err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, s.files, p)
			return
		}
		// Unknown asset-like paths are 404s, not the app shell.
		if strings.Contains(path.Base(p), ".") {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(s.index)
}
