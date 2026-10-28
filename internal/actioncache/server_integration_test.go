package actioncache

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func startTestServer(t *testing.T) (*Store, string, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	srv := &Server{Store: store, BaseURL: base}
	go func() { _ = http.Serve(ln, srv.Handler()) }()
	return store, base, func() { _ = ln.Close() }
}

func twirpPost(t *testing.T, base, method, auth string, body any) map[string]any {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+twirpBase+"/"+method, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("%s status %d: %s", method, resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v body=%s", method, err, raw)
	}
	return out
}

func uploadURLFrom(create map[string]any) string {
	if u, ok := create["signed_upload_url"].(string); ok && u != "" {
		return u
	}
	u, _ := create["signedUploadUrl"].(string)
	return u
}

func downloadURLFrom(get map[string]any) string {
	if u, ok := get["signed_download_url"].(string); ok && u != "" {
		return u
	}
	u, _ := get["signedDownloadUrl"].(string)
	return u
}

func assertAzuriteUploadURL(t *testing.T, uploadURL string) {
	t.Helper()
	u, err := http.NewRequest(http.MethodPut, uploadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := u.URL.Path
	if !strings.HasPrefix(path, azuriteBlobPrefix) {
		t.Fatalf("upload URL path %q want prefix %q", path, azuriteBlobPrefix)
	}
	rest := strings.TrimPrefix(path, azuriteBlobPrefix)
	if rest == "" || strings.Contains(rest, "/") {
		t.Fatalf("expected /devstoreaccount1/blob/{id}, got %q", path)
	}
}

func TestRoundTripSinglePut(t *testing.T) {
	_, base, stop := startTestServer(t)
	defer stop()

	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	payload := []byte("hello-cache-archive")

	create := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{
		"key": "k1", "version": "v1",
	})
	if create["ok"] != true {
		t.Fatalf("create: %#v", create)
	}
	uploadURL := uploadURLFrom(create)
	assertAzuriteUploadURL(t, uploadURL)
	put, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	put.Header.Set("x-ms-blob-type", "BlockBlob")
	resp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("put status %d", resp.StatusCode)
	}

	fin := twirpPost(t, base, "FinalizeCacheEntryUpload", auth, map[string]any{
		"key": "k1", "version": "v1", "size_bytes": fmt.Sprintf("%d", len(payload)),
	})
	if fin["ok"] != true {
		t.Fatalf("finalize: %#v", fin)
	}
	if _, ok := fin["entry_id"].(string); !ok {
		t.Fatalf("entry_id missing: %#v", fin)
	}

	get := twirpPost(t, base, "GetCacheEntryDownloadURL", auth, map[string]any{
		"key": "k1", "version": "v1", "restore_keys": []string{},
	})
	if get["ok"] != true {
		t.Fatalf("get: %#v", get)
	}
	dl := downloadURLFrom(get)
	got, err := http.Get(dl)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if !bytes.Equal(body, payload) {
		t.Fatalf("body %q want %q", body, payload)
	}
}

func TestWireProtoFieldNames(t *testing.T) {
	_, base, stop := startTestServer(t)
	defer stop()
	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	save := func(key, body string) {
		c := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": key, "version": "ver"})
		u := uploadURLFrom(c)
		req, _ := http.NewRequest(http.MethodPut, u, strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		twirpPost(t, base, "FinalizeCacheEntryUpload", auth, map[string]any{
			"key": key, "version": "ver", "size_bytes": fmt.Sprintf("%d", len(body)),
		})
	}
	save("npm-linux-abc", "A")
	get := twirpPost(t, base, "GetCacheEntryDownloadURL", auth, map[string]any{
		"key": "missing", "version": "ver", "restore_keys": []string{"npm-linux-"},
	})
	if get["ok"] != true || get["matched_key"] != "npm-linux-abc" {
		t.Fatalf("restore_keys wire: %#v", get)
	}
}

func TestStalePendingReclaimed(t *testing.T) {
	store, base, stop := startTestServer(t)
	defer stop()
	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	first := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "stale", "version": "v"})
	if first["ok"] != true {
		t.Fatalf("first: %#v", first)
	}
	second := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "stale", "version": "v"})
	if second["ok"] != false {
		t.Fatalf("fresh pending must block: %#v", second)
	}
	id := entryID("acme/app", "stale", "v")
	metaPath := filepath.Join(store.entryDir(id), metaName)
	meta, err := loadMeta(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	meta.CreatedAt = time.Now().UTC().Add(-pendingTTL - time.Minute)
	if err := writeMeta(metaPath, *meta); err != nil {
		t.Fatal(err)
	}
	third := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "stale", "version": "v"})
	if third["ok"] != true {
		t.Fatalf("stale pending should reclaim: %#v", third)
	}
}

func TestDuplicateCreateNotOK(t *testing.T) {
	_, base, stop := startTestServer(t)
	defer stop()
	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	first := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "k", "version": "v"})
	if first["ok"] != true {
		t.Fatalf("first: %#v", first)
	}
	second := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "k", "version": "v"})
	if second["ok"] != false {
		t.Fatalf("second should be ok=false: %#v", second)
	}
}

func TestGetMissBeforeFinalize(t *testing.T) {
	_, base, stop := startTestServer(t)
	defer stop()
	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	create := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "k", "version": "v"})
	uploadURL := uploadURLFrom(create)
	req, _ := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader([]byte("x")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	get := twirpPost(t, base, "GetCacheEntryDownloadURL", auth, map[string]any{
		"key": "k", "version": "v", "restoreKeys": []string{},
	})
	if get["ok"] != false {
		t.Fatalf("expected miss before finalize: %#v", get)
	}
}

func TestRestoreKeyPrefixSameVersion(t *testing.T) {
	_, base, stop := startTestServer(t)
	defer stop()
	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	save := func(key, version, body string) {
		c := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": key, "version": version})
		u := uploadURLFrom(c)
		req, _ := http.NewRequest(http.MethodPut, u, strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		twirpPost(t, base, "FinalizeCacheEntryUpload", auth, map[string]any{
			"key": key, "version": version, "size_bytes": fmt.Sprintf("%d", len(body)),
		})
	}
	save("npm-abc", "verA", "A")
	save("npm-xyz", "verB", "B")

	get := twirpPost(t, base, "GetCacheEntryDownloadURL", auth, map[string]any{
		"key": "missing", "version": "verA", "restore_keys": []string{"npm-"},
	})
	if get["ok"] != true || get["matched_key"] != "npm-abc" {
		t.Fatalf("restore: %#v", get)
	}
	getB := twirpPost(t, base, "GetCacheEntryDownloadURL", auth, map[string]any{
		"key": "missing", "version": "verB", "restore_keys": []string{"npm-"},
	})
	if getB["matched_key"] != "npm-xyz" {
		t.Fatalf("version filter: %#v", getB)
	}
}

func TestBlockUpload(t *testing.T) {
	_, base, stop := startTestServer(t)
	defer stop()
	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	create := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "blk", "version": "v"})
	u := uploadURLFrom(create)
	assertAzuriteUploadURL(t, u)

	id1 := base64.StdEncoding.EncodeToString([]byte("block-0001"))
	id2 := base64.StdEncoding.EncodeToString([]byte("block-0002"))
	for _, part := range []struct {
		id   string
		data string
	}{{id1, "hel"}, {id2, "lo"}} {
		req, _ := http.NewRequest(http.MethodPut, u+"?comp=block&blockid="+part.id, strings.NewReader(part.data))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("block status %d", resp.StatusCode)
		}
	}
	list := `<?xml version="1.0" encoding="utf-8"?><BlockList><Latest>` + id1 + `</Latest><Latest>` + id2 + `</Latest></BlockList>`
	req, _ := http.NewRequest(http.MethodPut, u+"?comp=blocklist", strings.NewReader(list))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("blocklist status %d", resp.StatusCode)
	}
	twirpPost(t, base, "FinalizeCacheEntryUpload", auth, map[string]any{
		"key": "blk", "version": "v", "size_bytes": "5",
	})
	get := twirpPost(t, base, "GetCacheEntryDownloadURL", auth, map[string]any{
		"key": "blk", "version": "v", "restore_keys": []string{},
	})
	dl := downloadURLFrom(get)
	got, err := http.Get(dl)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if string(body) != "hello" {
		t.Fatalf("got %q", body)
	}
}

func TestParallelReserve(t *testing.T) {
	_, base, stop := startTestServer(t)
	defer stop()
	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	var wg sync.WaitGroup
	results := make([]bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "race", "version": "v"})
			ok, _ := out["ok"].(bool)
			results[i] = ok
		}(i)
	}
	wg.Wait()
	okCount := 0
	for _, ok := range results {
		if ok {
			okCount++
		}
	}
	if okCount != 1 {
		t.Fatalf("expected exactly one ok, got %d (%v)", okCount, results)
	}
}

func TestEvictLRU(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	authRepo := "acme/app"
	putFinal := func(key string, usedOffset time.Duration) {
		id, ok, err := store.Reserve(authRepo, key, "v")
		if err != nil || !ok {
			t.Fatalf("reserve %s: ok=%v err=%v", key, ok, err)
		}
		if err := store.PutSingle(id, []byte(key)); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := store.Finalize(authRepo, key, "v", int64(len(key))); err != nil || !ok {
			t.Fatalf("finalize %s: %v %v", key, ok, err)
		}
		metaPath := filepath.Join(store.entryDir(id), metaName)
		meta, err := loadMeta(metaPath)
		if err != nil {
			t.Fatal(err)
		}
		meta.UsedAt = time.Now().UTC().Add(usedOffset)
		if err := writeMeta(metaPath, *meta); err != nil {
			t.Fatal(err)
		}
	}
	putFinal("old", -time.Hour)
	putFinal("new", 0)

	freeCalls := 0
	err = store.EvictLRU(50, func() (float64, error) {
		freeCalls++
		if freeCalls == 1 {
			return 1, nil // need eviction
		}
		return 100, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.entryDir(entryID(authRepo, "old", "v"))); !os.IsNotExist(err) {
		t.Fatalf("old entry should be gone: %v", err)
	}
	if _, err := os.Stat(store.entryDir(entryID(authRepo, "new", "v"))); err != nil {
		t.Fatalf("new entry should remain: %v", err)
	}
}

func TestCacheServeBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "utsusemi-test")
	modRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/utsusemi")
	cmd.Dir = modRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dir := t.TempDir()
	serve := exec.Command(bin, "cache-serve", "--addr", "127.0.0.1:0", "--dir", dir)
	stderr, err := serve.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = serve.Process.Kill()
		_, _ = serve.Process.Wait()
	}()

	type lineOrErr struct {
		line string
		err  error
	}
	ch := make(chan lineOrErr, 1)
	go func() {
		buf := make([]byte, 0, 256)
		tmp := make([]byte, 1)
		for {
			n, readErr := stderr.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[0])
				if tmp[0] == '\n' {
					ch <- lineOrErr{line: string(buf)}
					return
				}
			}
			if readErr != nil {
				ch <- lineOrErr{err: readErr}
				return
			}
		}
	}()
	var listenLine string
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read stderr: %v", got.err)
		}
		listenLine = got.line
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for cache-serve listen line")
	}
	// "cache-serve listening on 127.0.0.1:PORT dir=..."
	const prefix = "cache-serve listening on "
	idx := strings.Index(listenLine, prefix)
	if idx < 0 {
		t.Fatalf("unexpected stderr: %q", listenLine)
	}
	rest := strings.TrimSpace(listenLine[idx+len(prefix):])
	addr, _, _ := strings.Cut(rest, " ")
	base := "http://" + addr

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache-serve did not become ready at %s", base)
		}
		time.Sleep(50 * time.Millisecond)
	}

	auth := fakeBearer(t, map[string]any{"repository": "acme/app"})
	create := twirpPost(t, base, "CreateCacheEntry", auth, map[string]any{"key": "svc", "version": "v"})
	u := uploadURLFrom(create)
	assertAzuriteUploadURL(t, u)
	req, _ := http.NewRequest(http.MethodPut, u, strings.NewReader("z"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	twirpPost(t, base, "FinalizeCacheEntryUpload", auth, map[string]any{
		"key": "svc", "version": "v", "size_bytes": "1",
	})
	get := twirpPost(t, base, "GetCacheEntryDownloadURL", auth, map[string]any{
		"key": "svc", "version": "v", "restore_keys": []string{},
	})
	if get["ok"] != true {
		t.Fatalf("get via cache-serve: %#v", get)
	}
}

func TestHookRewritesWithNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	_, base, stop := startTestServer(t)
	defer stop()

	hook, err := HookSource()
	if err != nil {
		t.Fatal(err)
	}
	// Point hook at the ephemeral test server port.
	port := strings.TrimPrefix(base, "http://127.0.0.1:")
	hook = bytes.ReplaceAll(hook, []byte("127.0.0.1:19527"), []byte("127.0.0.1:"+port))
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.js")
	if err := os.WriteFile(hookPath, hook, 0o644); err != nil {
		t.Fatal(err)
	}
	// Mimic @actions/http-client: CacheService calls go through https.request
	// against ACTIONS_RESULTS_URL; the hook must re-dispatch onto http.
	script := `
const https = require('https');
const auth = process.env.AUTH;
const body = JSON.stringify({key:'n', version:'v'});
const req = https.request({
  protocol: 'https:',
  hostname: 'results.actions.githubusercontent.com',
  path: '/twirp/github.actions.results.api.v1.CacheService/CreateCacheEntry',
  method: 'POST',
  headers: {'Content-Type':'application/json','Authorization':auth,'Content-Length':Buffer.byteLength(body)}
}, res => {
  let d='';
  res.on('data', c => d+=c);
  res.on('end', () => { process.stdout.write(d); process.exit(res.statusCode>=300?1:0); });
});
req.on('error', e => { console.error(e); process.exit(1); });
req.write(body);
req.end();
`
	scriptPath := filepath.Join(dir, "client.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--require", hookPath, scriptPath)
	cmd.Env = append(os.Environ(), "AUTH="+fakeBearer(t, map[string]any{"repository": "acme/app"}))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte(`"ok":true`)) && !bytes.Contains(out, []byte(`"ok": true`)) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func fakeBearer(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	mid := base64.RawURLEncoding.EncodeToString(payload)
	return "Bearer " + header + "." + mid + ".sig"
}
