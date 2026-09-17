package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GDrive talks to the Drive API v3 directly: files.list with a query,
// files.create for the folder, a resumable upload sent in one PUT (so any
// size goes, not the 5 MB a multipart upload takes), files.get?alt=media,
// files.delete. Scope drive.file: pecunia sees and touches only what it
// made, so nothing else in the Drive is ever at risk.
//
// Authorization is OAuth 2 with PKCE on a loopback redirect, the only shape
// Google still allows a desktop app: pecunia listens on 127.0.0.1, the
// browser lands there with the code. When the browser is on another machine
// the owner pastes the URL it landed on instead.
type GDrive struct {
	GDriveConfig
	api, oauth string // base URLs; the tests point them at a fake
	token      string
	folderID   string
}

const (
	gdriveAPI       = "https://www.googleapis.com"
	gdriveAuthorize = "https://accounts.google.com/o/oauth2/v2/auth"
	gdriveScope     = "https://www.googleapis.com/auth/drive.file"
	folderMime      = "application/vnd.google-apps.folder"
)

// GDriveOAuth is the token endpoint's host; the command tests point it at a
// fake so the consent flow can run without Google.
var GDriveOAuth = "https://oauth2.googleapis.com"

var gdriveClient = &http.Client{Timeout: 10 * time.Minute}

func NewGDrive(cfg GDriveConfig) *GDrive {
	if cfg.Folder == "" {
		cfg.Folder = "pecunia"
	}
	return &GDrive{GDriveConfig: cfg, api: gdriveAPI, oauth: GDriveOAuth}
}

// access is the bearer token, minted on first use from the refresh token.
func (g *GDrive) access() (string, error) {
	if g.token != "" {
		return g.token, nil
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {g.RefreshToken},
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := g.tokenCall(form, &tok); err != nil {
		return "", fmt.Errorf("google drive refresh token: %w", err)
	}
	g.token = tok.AccessToken
	return g.token, nil
}

func (g *GDrive) tokenCall(form url.Values, into any) error {
	resp, err := gdriveClient.PostForm(g.oauth+"/token", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			if e.Description != "" {
				return fmt.Errorf("%s: %s", e.Error, e.Description)
			}
			return errors.New(e.Error)
		}
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, into)
}

// do signs and sends one request; a non-2xx becomes an error carrying
// Drive's own message.
func (g *GDrive) do(req *http.Request) (*http.Response, error) {
	tok, err := g.access()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := gdriveClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		return nil, fmt.Errorf("google drive %s %s: %s", req.Method, req.URL.Path, e.Error.Message)
	}
	return nil, fmt.Errorf("google drive %s %s: %s: %s", req.Method, req.URL.Path, resp.Status, strings.TrimSpace(string(raw)))
}

// getJSON is a GET decoded into out.
func (g *GDrive) getJSON(u string, out any) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	resp, err := g.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// driveFile is what files.list and files.create report of a file.
type driveFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType"`
	Size         string    `json:"size"` // a string in Drive's JSON
	ModifiedTime time.Time `json:"modifiedTime"`
}

// query runs files.list with q and follows every page.
func (g *GDrive) query(q string) ([]driveFile, error) {
	var all []driveFile
	token := ""
	for {
		v := url.Values{
			"q":        {q},
			"fields":   {"nextPageToken,files(id,name,mimeType,size,modifiedTime)"},
			"pageSize": {"1000"},
		}
		if token != "" {
			v.Set("pageToken", token)
		}
		var page struct {
			Files         []driveFile `json:"files"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := g.getJSON(g.api+"/drive/v3/files?"+v.Encode(), &page); err != nil {
			return nil, err
		}
		all = append(all, page.Files...)
		if page.NextPageToken == "" {
			return all, nil
		}
		token = page.NextPageToken
	}
}

// quote is a string literal inside a Drive query.
func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// folder is the id of the backup folder at the top of My Drive, made on
// first use and found by name after; only its own files are looked at, so
// an owner's other folders of the same name elsewhere do not matter.
func (g *GDrive) folder() (string, error) {
	if g.folderID != "" {
		return g.folderID, nil
	}
	found, err := g.query(fmt.Sprintf("name = %s and mimeType = '%s' and 'root' in parents and trashed = false", quote(g.Folder), folderMime))
	if err != nil {
		return "", err
	}
	if len(found) > 0 {
		g.folderID = found[0].ID
		return g.folderID, nil
	}
	body, _ := json.Marshal(map[string]any{"name": g.Folder, "mimeType": folderMime})
	req, err := http.NewRequest("POST", g.api+"/drive/v3/files", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var made driveFile
	if err := json.NewDecoder(resp.Body).Decode(&made); err != nil {
		return "", err
	}
	g.folderID = made.ID
	return g.folderID, nil
}

// lookup is the files in the folder with this name — Drive allows several.
func (g *GDrive) lookup(name string) ([]driveFile, error) {
	id, err := g.folder()
	if err != nil {
		return nil, err
	}
	return g.query(fmt.Sprintf("name = %s and %s in parents and mimeType != '%s' and trashed = false", quote(name), quote(id), folderMime))
}

// Put uploads the file, replacing one of the same name: Drive would happily
// keep both, and two archives with one name is a list nobody can read.
func (g *GDrive) Put(name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	folderID, err := g.folder()
	if err != nil {
		return err
	}
	old, err := g.lookup(name)
	if err != nil {
		return err
	}
	// Start the resumable session: the metadata goes first, the Location
	// that comes back takes the bytes.
	meta, _ := json.Marshal(map[string]any{"name": name, "parents": []string{folderID}})
	req, err := http.NewRequest("POST", g.api+"/upload/drive/v3/files?uploadType=resumable", strings.NewReader(string(meta)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "application/gzip")
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(st.Size(), 10))
	resp, err := g.do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	session := resp.Header.Get("Location")
	if session == "" {
		return errors.New("google drive: the upload session came back without a Location")
	}
	put, err := http.NewRequest("PUT", session, f)
	if err != nil {
		return err
	}
	put.ContentLength = st.Size()
	put.Header.Set("Content-Type", "application/gzip")
	resp, err = g.do(put)
	if err != nil {
		return err
	}
	resp.Body.Close()
	for _, o := range old {
		if err := g.delete(o.ID); err != nil {
			return err
		}
	}
	return nil
}

func (g *GDrive) Get(name string) (io.ReadCloser, error) {
	found, err := g.lookup(name)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no %s in Google Drive folder %s", name, g.Folder)
	}
	req, err := http.NewRequest("GET", g.api+"/drive/v3/files/"+url.PathEscape(found[0].ID)+"?alt=media", nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (g *GDrive) List() ([]Object, error) {
	id, err := g.folder()
	if err != nil {
		return nil, err
	}
	files, err := g.query(fmt.Sprintf("%s in parents and mimeType != '%s' and trashed = false", quote(id), folderMime))
	if err != nil {
		return nil, err
	}
	var objs []Object
	for _, f := range files {
		size, _ := strconv.ParseInt(f.Size, 10, 64)
		objs = append(objs, Object{Name: f.Name, Size: size, At: f.ModifiedTime})
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name < objs[j].Name })
	return objs, nil
}

func (g *GDrive) Delete(name string) error {
	found, err := g.lookup(name)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return fmt.Errorf("no %s in Google Drive folder %s", name, g.Folder)
	}
	for _, f := range found {
		if err := g.delete(f.ID); err != nil {
			return err
		}
	}
	return nil
}

func (g *GDrive) delete(id string) error {
	req, err := http.NewRequest("DELETE", g.api+"/drive/v3/files/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	resp, err := g.do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// BeginAuth starts the consent flow, sending the browser back to
// redirectURI — an AuthListener's, normally. access_type=offline and
// prompt=consent together are what make Google hand out a refresh token
// (without prompt=consent a second authorization of the same client gives
// none).
func (g *GDrive) BeginAuth(redirectURI string) (Auth, error) {
	if g.ClientID == "" {
		return Auth{}, errors.New("gdrive.client_id is empty — make a Desktop app OAuth client in the Google Cloud console first")
	}
	verifier, challenge, err := pkce()
	if err != nil {
		return Auth{}, err
	}
	q := url.Values{
		"client_id":             {g.ClientID},
		"response_type":         {"code"},
		"scope":                 {gdriveScope},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return Auth{URL: gdriveAuthorize + "?" + q.Encode(), verifier: verifier, redirect: redirectURI}, nil
}

// FinishAuth trades the code for the refresh token.
func (g *GDrive) FinishAuth(a Auth, code string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {strings.TrimSpace(code)},
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"redirect_uri":  {a.redirect},
		"code_verifier": {a.verifier},
	}
	var tok struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := g.tokenCall(form, &tok); err != nil {
		return "", fmt.Errorf("google drive authorization: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", errors.New("google drive authorization: no refresh token in the answer — revoke pecunia at myaccount.google.com/permissions and try again")
	}
	return tok.RefreshToken, nil
}

// AuthListener is the loopback the browser comes back to with the code.
type AuthListener struct {
	RedirectURI string
	Code        chan string
	Err         chan error
	srv         *http.Server
}

// NewAuthListener listens on a free port of 127.0.0.1 and serves one page:
// the one Google sends the browser to, which takes the code off the query,
// tells the owner they can close the tab, and hands the code over.
func NewAuthListener() (*AuthListener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	l := &AuthListener{
		RedirectURI: "http://" + ln.Addr().String() + "/",
		Code:        make(chan string, 1),
		Err:         make(chan error, 1),
	}
	l.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if e := r.URL.Query().Get("error"); e != "" {
			fmt.Fprintf(w, "<p>Google said: %s. You can close this tab.</p>", e)
			select {
			case l.Err <- fmt.Errorf("google drive authorization: %s", e):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "<p>pecunia has what it needs. You can close this tab.</p>")
		select {
		case l.Code <- code:
		default:
		}
	})}
	go l.srv.Serve(ln)
	return l, nil
}

func (l *AuthListener) Close() error { return l.srv.Close() }

// CodeFromRedirect takes the code out of a redirect URL pasted by hand, and
// passes a bare code through.
func CodeFromRedirect(s string) string {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		return u.Query().Get("code")
	}
	return s
}
