package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
)

// handleOpenExternal is used when an embedded WebKit view cannot create a
// popup. Only same-origin JSON requests may ask the local server to open a
// web URL in the host's default browser.
func (s *Server) handleOpenExternal(w http.ResponseWriter, r *http.Request) {
	if s.openExternal == nil {
		http.Error(w, "external browser not configured", http.StatusNotImplemented)
		return
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil || !isLoopbackHost(host) || r.Header.Get("Origin") != "http://"+r.Host {
		http.Error(w, "same-origin request required", http.StatusForbidden)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "JSON request required", http.StatusUnsupportedMediaType)
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	if err := decoder.Decode(&body); err != nil {
		http.Error(w, "invalid URL request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "unexpected request data", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(body.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
		(u.Scheme != "https" && (u.Scheme != "http" || !isLoopbackHost(u.Hostname()))) {
		http.Error(w, "external URL must use HTTPS or loopback HTTP", http.StatusBadRequest)
		return
	}
	logURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	if err := s.openExternal(u.String()); err != nil {
		s.logInteraction("external-browser-failed", "", "", "", fmt.Sprintf("%s: %v", logURL, err), "error")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.logInteraction("external-browser-open", "", "", "", logURL, "info")
	w.WriteHeader(http.StatusNoContent)
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}
