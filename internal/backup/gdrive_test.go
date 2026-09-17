package backup

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeFile struct {
	id, name, parent, mime string
	data                   []byte
}

// fakeDrive is enough of the Drive API v3 for the provider: the token
// endpoint, files.list with a q filter, files.create for a folder, a
// resumable upload in one PUT, files.get?alt=media, files.delete.
type fakeDrive struct {
	mu       sync.Mutex
	files    map[string]*fakeFile // by id
	next     int
	refresh  int
	created  int // folders made
	pageSize int
	tokenReq []map[string]string
	sessions map[string]map[string]any // resumable session id -> metadata
}

var (
	reName   = regexp.MustCompile(`name\s*=\s*'([^']*)'`)
	reParent = regexp.MustCompile(`'([^']*)'\s+in\s+parents`)
)

func (f *fakeDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"error":{"code":%d,"message":%q,"errors":[]}}`, code, msg)
	}
	if r.URL.Path == "/token" {
		r.ParseForm()
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		f.tokenReq = append(f.tokenReq, form)
		switch {
		case form["grant_type"] == "refresh_token" && form["refresh_token"] == "rt-good" && form["client_id"] == "cid" && form["client_secret"] == "csec":
			f.refresh++
			fmt.Fprintf(w, `{"access_token":"at-%d","expires_in":3599,"token_type":"Bearer"}`, f.refresh)
		case form["grant_type"] == "authorization_code" && form["code"] == "code-1" && form["code_verifier"] != "" && form["client_secret"] == "csec" && strings.HasPrefix(form["redirect_uri"], "http://127.0.0.1:"):
			fmt.Fprint(w, `{"access_token":"at-x","refresh_token":"rt-good","token_type":"Bearer"}`)
		default:
			http.Error(w, `{"error":"invalid_grant","error_description":"Bad Request"}`, http.StatusBadRequest)
		}
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer at-") {
		fail(401, "Invalid Credentials")
		return
	}
	newID := func() string { f.next++; return "id" + strconv.Itoa(f.next) }
	fileJSON := func(df *fakeFile) string {
		return fmt.Sprintf(`{"id":%q,"name":%q,"mimeType":%q,"size":%q,"modifiedTime":"2026-09-16T14:05:00.000Z"}`, df.id, df.name, df.mime, strconv.Itoa(len(df.data)))
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/drive/v3/files":
		q := r.URL.Query().Get("q")
		if !strings.Contains(q, "trashed = false") && !strings.Contains(q, "trashed=false") {
			fail(400, "want trashed = false in q")
			return
		}
		var name, parent string
		if m := reName.FindStringSubmatch(q); m != nil {
			name = m[1]
		}
		if m := reParent.FindStringSubmatch(q); m != nil {
			parent = m[1]
		}
		onlyFolders := strings.Contains(q, "mimeType = 'application/vnd.google-apps.folder'")
		noFolders := strings.Contains(q, "mimeType != 'application/vnd.google-apps.folder'")
		var ids []string
		for id, df := range f.files {
			if name != "" && df.name != name {
				continue
			}
			if parent != "" && df.parent != parent {
				continue
			}
			isFolder := df.mime == "application/vnd.google-apps.folder"
			if (onlyFolders && !isFolder) || (noFolders && isFolder) {
				continue
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		start, _ := strconv.Atoi(r.URL.Query().Get("pageToken"))
		end := len(ids)
		if f.pageSize > 0 && start+f.pageSize < end {
			end = start + f.pageSize
		}
		var items []string
		for _, id := range ids[start:end] {
			items = append(items, fileJSON(f.files[id]))
		}
		next := ""
		if end < len(ids) {
			next = fmt.Sprintf(`,"nextPageToken":"%d"`, end)
		}
		fmt.Fprintf(w, `{"files":[%s]%s}`, strings.Join(items, ","), next)
	case r.Method == "POST" && r.URL.Path == "/drive/v3/files":
		var meta map[string]any
		json.NewDecoder(r.Body).Decode(&meta)
		if meta["mimeType"] != "application/vnd.google-apps.folder" {
			fail(400, "only folders are created here")
			return
		}
		df := &fakeFile{id: newID(), name: meta["name"].(string), mime: "application/vnd.google-apps.folder", parent: "root"}
		f.files[df.id] = df
		f.created++
		fmt.Fprint(w, fileJSON(df))
	case r.Method == "POST" && r.URL.Path == "/upload/drive/v3/files":
		if r.URL.Query().Get("uploadType") != "resumable" {
			fail(400, "want uploadType=resumable")
			return
		}
		var meta map[string]any
		json.NewDecoder(r.Body).Decode(&meta)
		sid := newID()
		f.sessions[sid] = meta
		w.Header().Set("Location", "http://"+r.Host+"/upload/session/"+sid)
		w.WriteHeader(http.StatusOK)
	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/upload/session/"):
		meta, ok := f.sessions[strings.TrimPrefix(r.URL.Path, "/upload/session/")]
		if !ok {
			fail(404, "no such session")
			return
		}
		data, _ := io.ReadAll(r.Body)
		if got := r.Header.Get("Content-Length"); got != strconv.Itoa(len(data)) {
			fail(400, "content-length "+got)
			return
		}
		parents, _ := meta["parents"].([]any)
		parent := ""
		if len(parents) > 0 {
			parent, _ = parents[0].(string)
		}
		df := &fakeFile{id: newID(), name: meta["name"].(string), parent: parent, mime: "application/gzip", data: data}
		f.files[df.id] = df
		fmt.Fprint(w, fileJSON(df))
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/drive/v3/files/"):
		df, ok := f.files[strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")]
		if !ok || r.URL.Query().Get("alt") != "media" {
			fail(404, "File not found: "+strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")+".")
			return
		}
		w.Write(df.data)
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/drive/v3/files/"):
		id := strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")
		if _, ok := f.files[id]; !ok {
			fail(404, "File not found: "+id+".")
			return
		}
		delete(f.files, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		fail(404, "no route "+r.Method+" "+r.URL.Path)
	}
}

func newFakeDrive(t *testing.T) (*fakeDrive, *GDrive) {
	t.Helper()
	f := &fakeDrive{files: map[string]*fakeFile{}, sessions: map[string]map[string]any{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	g := NewGDrive(GDriveConfig{Folder: "pecunia", ClientID: "cid", ClientSecret: "csec", RefreshToken: "rt-good"})
	g.api, g.oauth = srv.URL, srv.URL
	return f, g
}

func (f *fakeDrive) byName(name string) []*fakeFile {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*fakeFile
	for _, df := range f.files {
		if df.name == name {
			out = append(out, df)
		}
	}
	return out
}

func TestGDriveProvider(t *testing.T) {
	providerSuite(t, func(t *testing.T) Provider {
		_, g := newFakeDrive(t)
		return g
	})

	t.Run("the folder is made once and found after", func(t *testing.T) {
		f, g := newFakeDrive(t)
		g.Put("a.tar.gz", writeTemp(t, "a"))
		g.Put("b.tar.gz", writeTemp(t, "b"))
		again := NewGDrive(g.GDriveConfig)
		again.api, again.oauth = g.api, g.oauth
		again.Put("c.tar.gz", writeTemp(t, "c"))
		if f.created != 1 {
			t.Fatalf("folders made %d, want 1", f.created)
		}
		folder := f.byName("pecunia")
		if len(folder) != 1 || folder[0].mime != "application/vnd.google-apps.folder" {
			t.Fatalf("folder %+v", folder)
		}
		if c := f.byName("c.tar.gz"); len(c) != 1 || c[0].parent != folder[0].id {
			t.Fatalf("c.tar.gz %+v", c)
		}
	})

	t.Run("the same name again replaces, never duplicates", func(t *testing.T) {
		f, g := newFakeDrive(t)
		g.Put("a.tar.gz", writeTemp(t, "one"))
		g.Put("a.tar.gz", writeTemp(t, "two"))
		a := f.byName("a.tar.gz")
		if len(a) != 1 || string(a[0].data) != "two" {
			t.Fatalf("a.tar.gz %+v", a)
		}
	})

	t.Run("one refresh serves many calls", func(t *testing.T) {
		f, g := newFakeDrive(t)
		g.Put("a.tar.gz", writeTemp(t, "a"))
		g.List()
		if f.refresh != 1 {
			t.Fatalf("refreshed %d times", f.refresh)
		}
	})

	t.Run("a bad refresh token says so", func(t *testing.T) {
		_, g := newFakeDrive(t)
		g.RefreshToken = "rt-bad"
		err := g.Put("a.tar.gz", writeTemp(t, "a"))
		if err == nil || !strings.Contains(err.Error(), "refresh") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("list follows nextPageToken", func(t *testing.T) {
		f, g := newFakeDrive(t)
		f.pageSize = 2
		for _, n := range []string{"a", "b", "c", "d", "e"} {
			g.Put(n+".tar.gz", writeTemp(t, n))
		}
		objs, err := g.List()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, o := range objs {
			names = append(names, o.Name)
		}
		if strings.Join(names, ",") != "a.tar.gz,b.tar.gz,c.tar.gz,d.tar.gz,e.tar.gz" {
			t.Fatalf("names %v", names)
		}
		if !objs[0].At.Equal(time.Date(2026, 9, 16, 14, 5, 0, 0, time.UTC)) || objs[0].Size != 1 {
			t.Fatalf("first %+v", objs[0])
		}
	})

	t.Run("an error names drive's message", func(t *testing.T) {
		_, g := newFakeDrive(t)
		g.Put("a.tar.gz", writeTemp(t, "a"))
		g.api = g.api + "/broken"
		err := g.Delete("a.tar.gz")
		if err == nil || !strings.Contains(err.Error(), "no route") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestGDriveAuthorize(t *testing.T) {
	f, g := newFakeDrive(t)
	g.RefreshToken = ""
	auth, err := g.BeginAuth("http://127.0.0.1:4321/")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://accounts.google.com/o/oauth2/v2/auth?", "client_id=cid", "response_type=code",
		"scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fdrive.file", "access_type=offline", "prompt=consent",
		"code_challenge_method=S256", "code_challenge=", "redirect_uri=http%3A%2F%2F127.0.0.1%3A4321%2F",
	} {
		if !strings.Contains(auth.URL, want) {
			t.Errorf("url lacks %q: %s", want, auth.URL)
		}
	}

	t.Run("the code becomes a refresh token", func(t *testing.T) {
		rt, err := g.FinishAuth(auth, "code-1")
		if err != nil || rt != "rt-good" {
			t.Fatalf("%q, %v", rt, err)
		}
		req := f.tokenReq[len(f.tokenReq)-1]
		if req["redirect_uri"] != "http://127.0.0.1:4321/" {
			t.Fatalf("token request %v", req)
		}
	})

	t.Run("a wrong code is refused", func(t *testing.T) {
		_, err := g.FinishAuth(auth, "code-9")
		if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("no client id is refused", func(t *testing.T) {
		g.ClientID = ""
		if _, err := g.BeginAuth("http://127.0.0.1:1/"); err == nil || !strings.Contains(err.Error(), "client_id") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestAuthListener(t *testing.T) {
	t.Run("hands over the code the browser brings", func(t *testing.T) {
		l, err := NewAuthListener()
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		if !strings.HasPrefix(l.RedirectURI, "http://127.0.0.1:") {
			t.Fatalf("redirect %q", l.RedirectURI)
		}
		resp, err := http.Get(l.RedirectURI + "?code=abc&scope=x")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(body), "close") {
			t.Fatalf("page %q", body)
		}
		select {
		case code := <-l.Code:
			if code != "abc" {
				t.Fatalf("code %q", code)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no code")
		}
	})

	t.Run("a denial is an error, not a hang", func(t *testing.T) {
		l, err := NewAuthListener()
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		http.Get(l.RedirectURI + "?error=access_denied")
		select {
		case err := <-l.Err:
			if !strings.Contains(err.Error(), "access_denied") {
				t.Fatalf("err %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no error")
		}
	})
}

// CodeFromRedirect is exercised through the command: a redirect URL pasted
// by hand yields its code, and a bare code passes through.
func TestCodeFromRedirect(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:4321/?code=4%2Fabc&scope=x": "4/abc",
		"  4/abc  ": "4/abc",
		"":          "",
	}
	for in, want := range cases {
		if got := CodeFromRedirect(in); got != want {
			t.Errorf("CodeFromRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}
