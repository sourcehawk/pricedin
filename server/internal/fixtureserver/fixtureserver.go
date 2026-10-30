// Package fixtureserver imitates SEC EDGAR and the Massive API with the
// files in testdata, so that Compose, the integration tests and the
// end-to-end tests never call the real services. Each upstream has its own
// path prefix, which is the host name that the real service has:
//
//	/www.sec.gov/...      files of testdata/sec/www.sec.gov
//	/data.sec.gov/...     files of testdata/sec/data.sec.gov
//	/api.massive.com/...  the Massive endpoints that the price adapter calls,
//	                      answered from testdata/prices/massive
//
// So a base URL is the server's address plus the prefix, such as
// http://fixtures:8090/www.sec.gov for PRICEDIN_SEC_BASE_URL.
//
// Like the real services, the SEC paths answer 403 without a User-Agent
// that holds a contact email address, and the Massive paths answer 401
// without the configured bearer token. A missing file answers as the real
// service does: www.sec.gov with 403 and an XML AccessDenied body,
// data.sec.gov with 404.
package fixtureserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Prefixes of the upstreams.
const (
	PrefixSECWWW  = "/www.sec.gov"
	PrefixSECData = "/data.sec.gov"
	PrefixMassive = "/api.massive.com"
)

// Options configure the handler.
type Options struct {
	// Root is the testdata folder, which holds sec/ and prices/massive/.
	Root string
	// MassiveAPIKey is the bearer token that the Massive paths require. It
	// must not be empty.
	MassiveAPIKey string
}

// New returns the handler. It reads the Massive aggregates files once, and
// returns an error when Root lacks the fixture folders or a file does not
// parse.
func New(opts Options) (http.Handler, error) {
	if opts.MassiveAPIKey == "" {
		return nil, errors.New("fixtureserver: a Massive API key is required")
	}
	secRoot, err := os.OpenRoot(filepath.Join(opts.Root, "sec"))
	if err != nil {
		return nil, fmt.Errorf("fixtureserver: %w", err)
	}
	m, err := newMassive(filepath.Join(opts.Root, "prices", "massive"))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(PrefixSECWWW+"/", requireUserAgent(secFiles(secRoot, "www.sec.gov", true)))
	mux.Handle(PrefixSECData+"/", requireUserAgent(secFiles(secRoot, "data.sec.gov", false)))
	mux.Handle(PrefixMassive+"/", http.StripPrefix(PrefixMassive, requireBearer(opts.MassiveAPIKey, m.handler())))
	return mux, nil
}

// requireUserAgent answers 403 with an HTML page, as SEC does, unless the
// request declares a User-Agent with a contact email address.
func requireUserAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "@") {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, "<html><body><h1>Your Request Originates from an Undeclared Automated Tool</h1></body></html>")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secFiles serves the files below host in root. A missing file gives 403
// with an XML AccessDenied body when accessDenied is true, as www.sec.gov
// answers, and 404 otherwise, as data.sec.gov answers.
func secFiles(root *os.Root, host string, accessDenied bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/"+host+"/")
		name := path.Join(host, path.Clean("/"+rel))
		// A folder, a name that leaves the root and a missing file all
		// count as no such file.
		body, err := root.ReadFile(name)
		if err != nil {
			if accessDenied {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
				return
			}
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType(name))
		_, _ = w.Write(body) //nolint:gosec // a fixture file with the type of its extension
	})
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".json":
		return "application/json"
	case ".zip":
		return "application/zip"
	default:
		return "text/plain; charset=utf-8"
	}
}

// randomRequestID returns a new request ID in the form of Massive's.
func randomRequestID() string {
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return hex.EncodeToString(id)
}
