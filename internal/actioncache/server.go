package actioncache

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

const (
	twirpBase = "/twirp/github.actions.results.api.v1.CacheService"
	// azuriteBlobPrefix is required so @actions/cache (useAzureSdk) can parse
	// account/container/blob from an IP-style URL (see Azurite path style).
	azuriteBlobPrefix = "/devstoreaccount1/blob/"
	legacyBlobPrefix  = "/blob/"
	// maxUploadBytes caps a single PUT body (GitHub Actions cache archive limit).
	maxUploadBytes = 10 << 30
)

// Server serves CacheService v2 Twirp JSON and Azure Block Blob uploads.
type Server struct {
	Store   *Store
	BaseURL string // e.g. http://127.0.0.1:19527 — no trailing slash
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc(twirpBase+"/CreateCacheEntry", s.handleCreate)
	mux.HandleFunc(twirpBase+"/FinalizeCacheEntryUpload", s.handleFinalize)
	mux.HandleFunc(twirpBase+"/GetCacheEntryDownloadURL", s.handleGet)
	mux.HandleFunc(azuriteBlobPrefix, s.handleBlob)
	mux.HandleFunc(legacyBlobPrefix, s.handleBlob)
	return mux
}

// ListenAndServe binds addr and serves until ln is closed.
func ListenAndServe(ln net.Listener, store *Store) error {
	base := "http://" + ln.Addr().String()
	srv := &Server{Store: store, BaseURL: strings.TrimRight(base, "/")}
	return http.Serve(ln, srv.Handler())
}

func (s *Server) signedBlobURL(id string) string {
	return s.BaseURL + azuriteBlobPrefix + id
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := decodeJSONObject(r.Body)
	if err != nil {
		twirpError(w, "malformed_request", err.Error())
		return
	}
	key := jsonString(raw, "key")
	version := jsonString(raw, "version")
	if key == "" || version == "" {
		twirpError(w, "invalid_argument", "key and version are required")
		return
	}
	repo := RepoFromBearer(r.Header.Get("Authorization"))
	id, ok, err := s.Store.Reserve(repo, key, version)
	if err != nil {
		twirpError(w, "internal", err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"signed_upload_url": s.signedBlobURL(id),
		"signedUploadUrl":   s.signedBlobURL(id),
	})
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := decodeJSONObject(r.Body)
	if err != nil {
		twirpError(w, "malformed_request", err.Error())
		return
	}
	key := jsonString(raw, "key")
	version := jsonString(raw, "version")
	size, err := parseSize(jsonAny(raw, "size_bytes", "sizeBytes"))
	if err != nil {
		twirpError(w, "invalid_argument", err.Error())
		return
	}
	repo := RepoFromBearer(r.Header.Get("Authorization"))
	id, ok, err := s.Store.Finalize(repo, key, version, size)
	if err != nil {
		twirpError(w, "internal", err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "unable to finalize"})
		return
	}
	entryNum := entryIDNumeric(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"entry_id": entryNum,
		"entryId":  entryNum,
	})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := decodeJSONObject(r.Body)
	if err != nil {
		twirpError(w, "malformed_request", err.Error())
		return
	}
	key := jsonString(raw, "key")
	version := jsonString(raw, "version")
	restoreKeys := jsonStringSlice(raw, "restore_keys", "restoreKeys")
	repo := RepoFromBearer(r.Header.Get("Authorization"))
	hit, err := s.Store.Lookup(repo, key, restoreKeys, version)
	if err != nil {
		twirpError(w, "internal", err.Error())
		return
	}
	if hit == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false})
		return
	}
	url := s.signedBlobURL(hit.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"signed_download_url": url,
		"signedDownloadUrl":   url,
		"matched_key":         hit.Key,
		"matchedKey":          hit.Key,
	})
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	id, ok := blobIDFromPath(r.URL.Path)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	comp := q.Get("comp")
	switch r.Method {
	case http.MethodPut:
		limited := http.MaxBytesReader(w, r.Body, maxUploadBytes)
		body, err := io.ReadAll(limited)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		switch comp {
		case "block":
			blockID := q.Get("blockid")
			if blockID == "" {
				http.Error(w, "blockid required", http.StatusBadRequest)
				return
			}
			if err := s.Store.PutBlock(id, blockID, body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("x-ms-request-id", id)
			w.WriteHeader(http.StatusCreated)
		case "blocklist":
			ids, err := parseBlockList(body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := s.Store.CommitBlocks(id, ids); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("x-ms-request-id", id)
			w.WriteHeader(http.StatusCreated)
		default:
			if err := s.Store.PutSingle(id, body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("x-ms-request-id", id)
			w.WriteHeader(http.StatusCreated)
		}
	case http.MethodGet, http.MethodHead:
		path := s.Store.ArchivePath(id)
		http.ServeFile(w, r, path)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func blobIDFromPath(path string) (string, bool) {
	for _, prefix := range []string{azuriteBlobPrefix, legacyBlobPrefix} {
		if strings.HasPrefix(path, prefix) {
			id := strings.Trim(strings.TrimPrefix(path, prefix), "/")
			if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
				return "", false
			}
			return id, true
		}
	}
	return "", false
}

type blockListXML struct {
	XMLName     xml.Name `xml:"BlockList"`
	Latest      []string `xml:"Latest"`
	Uncommitted []string `xml:"Uncommitted"`
	Committed   []string `xml:"Committed"`
}

func parseBlockList(body []byte) ([]string, error) {
	var list blockListXML
	if err := xml.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	out := append([]string{}, list.Latest...)
	out = append(out, list.Uncommitted...)
	out = append(out, list.Committed...)
	if len(out) == 0 {
		return nil, fmt.Errorf("empty block list")
	}
	for i, id := range out {
		if decoded, err := base64.StdEncoding.DecodeString(id); err == nil && len(decoded) > 0 {
			out[i] = id
		}
	}
	return out, nil
}

func parseSize(v any) (int64, error) {
	switch t := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return int64(t), nil
	case string:
		if t == "" {
			return 0, nil
		}
		return strconv.ParseInt(t, 10, 64)
	case json.Number:
		return t.Int64()
	default:
		return 0, fmt.Errorf("unsupported sizeBytes type %T", v)
	}
}

func entryIDNumeric(hexID string) string {
	if len(hexID) >= 16 {
		if n, err := strconv.ParseUint(hexID[:16], 16, 64); err == nil {
			return strconv.FormatUint(n, 10)
		}
	}
	return "1"
}

func decodeJSONObject(r io.Reader) (map[string]any, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return map[string]any{}, nil
	}
	return raw, nil
}

func jsonString(raw map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := raw[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			return t
		case json.Number:
			return t.String()
		}
	}
	return ""
}

func jsonAny(raw map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := raw[k]; ok {
			return v
		}
	}
	return nil
}

func jsonStringSlice(raw map[string]any, keys ...string) []string {
	for _, k := range keys {
		v, ok := raw[k]
		if !ok || v == nil {
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func twirpError(w http.ResponseWriter, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusInternalServerError
	switch code {
	case "malformed_request", "invalid_argument":
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code": code,
		"msg":  msg,
	})
}
