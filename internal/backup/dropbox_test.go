package backup

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDropbox is enough of the Dropbox HTTP API v2 for the provider: the
// token endpoint, upload, download, list_folder (+continue), delete_v2.
type fakeDropbox struct {
	mu       sync.Mutex
	files    map[string][]byte // by full lowercase path
	names    map[string]string // the name as uploaded, by the same key
	refresh  int               // token refreshes seen
	bearer   []string          // every Authorization header on an API call
	pageSize int
	codes    map[string]string // authorization code -> refresh token it mints
	tokenReq []map[string]string
}

func (f *fakeDropbox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fail := func(status int, tag, summary string) {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error_summary":%q,"error":{".tag":%q}}`, summary, tag)
	}
	if r.URL.Path == "/oauth2/token" {
		r.ParseForm()
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		f.tokenReq = append(f.tokenReq, form)
		switch form["grant_type"] {
		case "refresh_token":
			if form["refresh_token"] != "rt-good" || form["client_id"] != "app-key" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			f.refresh++
			fmt.Fprintf(w, `{"access_token":"at-%d","token_type":"bearer","expires_in":14400}`, f.refresh)
		case "authorization_code":
			rt, ok := f.codes[form["code"]]
			if !ok || form["code_verifier"] == "" {
				http.Error(w, `{"error":"invalid_grant","error_description":"code not found"}`, http.StatusBadRequest)
				return
			}
			fmt.Fprintf(w, `{"access_token":"at-x","refresh_token":%q,"token_type":"bearer"}`, rt)
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		}
		return
	}
	auth := r.Header.Get("Authorization")
	f.bearer = append(f.bearer, auth)
	if !strings.HasPrefix(auth, "Bearer at-") {
		fail(http.StatusUnauthorized, "invalid_access_token", "invalid_access_token/")
		return
	}
	arg := map[string]any{}
	if h := r.Header.Get("Dropbox-API-Arg"); h != "" {
		json.Unmarshal([]byte(h), &arg)
	} else {
		json.NewDecoder(r.Body).Decode(&arg)
	}
	path, _ := arg["path"].(string)
	key := strings.ToLower(path)
	switch r.URL.Path {
	case "/2/files/upload":
		if arg["mode"] != "overwrite" {
			fail(http.StatusBadRequest, "bad_mode", "mode")
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.files[key] = b
		f.names[key] = path[strings.LastIndex(path, "/")+1:]
		fmt.Fprintf(w, `{"name":%q,"path_display":%q,"size":%d}`, path[strings.LastIndex(path, "/")+1:], path, len(b))
	case "/2/files/download":
		b, ok := f.files[key]
		if !ok {
			fail(http.StatusConflict, "path", "path/not_found/")
			return
		}
		w.Write(b)
	case "/2/files/delete_v2":
		if _, ok := f.files[key]; !ok {
			fail(http.StatusConflict, "path_lookup", "path_lookup/not_found/")
			return
		}
		delete(f.files, key)
		fmt.Fprint(w, `{"metadata":{".tag":"file"}}`)
	case "/2/files/list_folder", "/2/files/list_folder/continue":
		var folder string
		start := 0
		if r.URL.Path == "/2/files/list_folder" {
			folder = key + "/"
			// The folder exists only once something was put in it.
			found := false
			for k := range f.files {
				if strings.HasPrefix(k, folder) {
					found = true
				}
			}
			if !found {
				fail(http.StatusConflict, "path", "path/not_found/")
				return
			}
		} else {
			cursor, _ := arg["cursor"].(string)
			fmt.Sscanf(cursor, "%d|", &start)
			folder = cursor[strings.Index(cursor, "|")+1:]
		}
		var keys []string
		for k := range f.files {
			if strings.HasPrefix(k, folder) && !strings.Contains(strings.TrimPrefix(k, folder), "/") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		end := len(keys)
		if f.pageSize > 0 && start+f.pageSize < end {
			end = start + f.pageSize
		}
		var entries []string
		for _, k := range keys[start:end] {
			// Dropbox matches paths case-insensitively but reports the name as written.
			entries = append(entries, fmt.Sprintf(`{".tag":"file","name":%q,"path_lower":%q,"size":%d,"server_modified":"2026-09-16T14:05:00Z"}`, f.names[k], k, len(f.files[k])))
		}
		entries = append(entries, `{".tag":"folder","name":"sub","path_lower":"`+folder+`sub"}`)
		fmt.Fprintf(w, `{"entries":[%s],"cursor":"%d|%s","has_more":%v}`, strings.Join(entries, ","), end, folder, end < len(keys))
	default:
		http.NotFound(w, r)
	}
}

func newFakeDropbox(t *testing.T) (*fakeDropbox, *Dropbox) {
	t.Helper()
	f := &fakeDropbox{files: map[string][]byte{}, names: map[string]string{}, codes: map[string]string{"code-1": "rt-good"}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	d := NewDropbox(DropboxConfig{Folder: "/Apps/pecunia", AppKey: "app-key", RefreshToken: "rt-good"})
	d.api, d.content, d.oauth = srv.URL, srv.URL, srv.URL
	return f, d
}

func TestDropboxProvider(t *testing.T) {
	providerSuite(t, func(t *testing.T) Provider {
		_, d := newFakeDropbox(t)
		return d
	})

	t.Run("files go under the folder", func(t *testing.T) {
		f, d := newFakeDropbox(t)
		if err := d.Put("pecunia-20260916T140500Z.tar.gz", writeTemp(t, "x")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.files["/apps/pecunia/pecunia-20260916t140500z.tar.gz"]; !ok {
			t.Fatalf("files %v", f.files)
		}
	})

	t.Run("one refresh serves many calls", func(t *testing.T) {
		f, d := newFakeDropbox(t)
		d.Put("a.tar.gz", writeTemp(t, "a"))
		d.Put("b.tar.gz", writeTemp(t, "b"))
		d.List()
		if f.refresh != 1 {
			t.Fatalf("refreshed %d times, want 1", f.refresh)
		}
		if len(f.bearer) != 3 || f.bearer[2] != "Bearer at-1" {
			t.Fatalf("bearers %v", f.bearer)
		}
	})

	t.Run("a bad refresh token says so", func(t *testing.T) {
		_, d := newFakeDropbox(t)
		d.RefreshToken = "rt-bad"
		err := d.Put("a.tar.gz", writeTemp(t, "a"))
		if err == nil || !strings.Contains(err.Error(), "refresh") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("list follows has_more and keeps the case", func(t *testing.T) {
		f, d := newFakeDropbox(t)
		f.pageSize = 2
		for _, n := range []string{"a", "b", "c", "d", "Mixed"} {
			d.Put(n+".tar.gz", writeTemp(t, n))
		}
		objs, err := d.List()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, o := range objs {
			names = append(names, o.Name)
		}
		if strings.Join(names, ",") != "Mixed.tar.gz,a.tar.gz,b.tar.gz,c.tar.gz,d.tar.gz" {
			t.Fatalf("names %v", names)
		}
		if !objs[0].At.Equal(time.Date(2026, 9, 16, 14, 5, 0, 0, time.UTC)) || objs[0].Size != 5 {
			t.Fatalf("first %+v", objs[0])
		}
	})

	t.Run("an error names the dropbox summary", func(t *testing.T) {
		_, d := newFakeDropbox(t)
		err := d.Delete("nope.tar.gz")
		if err == nil || !strings.Contains(err.Error(), "path_lookup/not_found") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestDropboxAuthorize(t *testing.T) {
	f, d := newFakeDropbox(t)
	d.RefreshToken = ""
	auth, err := d.BeginAuth()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://www.dropbox.com/oauth2/authorize?", "client_id=app-key", "response_type=code",
		"token_access_type=offline", "code_challenge_method=S256", "code_challenge=",
	} {
		if !strings.Contains(auth.URL, want) {
			t.Errorf("url lacks %q: %s", want, auth.URL)
		}
	}
	if strings.Contains(auth.URL, "redirect_uri") {
		t.Errorf("a redirect for a CLI: %s", auth.URL)
	}

	t.Run("the code becomes a refresh token", func(t *testing.T) {
		rt, err := d.FinishAuth(auth, "code-1")
		if err != nil {
			t.Fatal(err)
		}
		if rt != "rt-good" {
			t.Fatalf("token %q", rt)
		}
		req := f.tokenReq[len(f.tokenReq)-1]
		if req["client_id"] != "app-key" || req["code_verifier"] == "" || req["client_secret"] != "" {
			t.Fatalf("token request %v", req)
		}
	})

	t.Run("the app secret goes along when there is one", func(t *testing.T) {
		d.AppSecret = "sec"
		d.FinishAuth(auth, "code-1")
		if req := f.tokenReq[len(f.tokenReq)-1]; req["client_secret"] != "sec" {
			t.Fatalf("token request %v", req)
		}
	})

	t.Run("a wrong code is refused", func(t *testing.T) {
		_, err := d.FinishAuth(auth, "code-9")
		if err == nil || !strings.Contains(err.Error(), "code not found") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("two begins differ", func(t *testing.T) {
		again, _ := d.BeginAuth()
		if again.URL == auth.URL {
			t.Fatal("same challenge twice")
		}
	})
}
