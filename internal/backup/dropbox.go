package backup

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// Dropbox talks to the HTTP API v2 directly: five endpoints and the OAuth
// token exchange, no SDK. Authorization is OAuth 2 with PKCE, the way a CLI
// without a secret it can keep does it: the owner opens a URL, approves,
// pastes a code back, and pecunia turns that into a refresh token that lasts
// until revoked. Each run mints a four-hour access token from it.
type Dropbox struct {
	DropboxConfig
	api, content, oauth string // base URLs; the tests point them at a fake
	token               string // the access token minted for this process
}

const (
	dropboxAPI     = "https://api.dropboxapi.com"
	dropboxContent = "https://content.dropboxapi.com"
	// dropboxAuthorize is where the owner goes to approve the app.
	dropboxAuthorize = "https://www.dropbox.com/oauth2/authorize"
	// dropboxUploadMax is as much as files/upload takes in one request; a
	// larger archive needs an upload session, which pecunia does not do yet.
	dropboxUploadMax = 150 << 20
)

// DropboxOAuth is the token endpoint's host; the command tests point it at
// a fake so the consent flow can run without Dropbox.
var DropboxOAuth = "https://api.dropboxapi.com"

var dropboxClient = &http.Client{Timeout: 10 * time.Minute}

func NewDropbox(cfg DropboxConfig) *Dropbox {
	if cfg.Folder == "" {
		cfg.Folder = "/Apps/pecunia"
	}
	return &Dropbox{DropboxConfig: cfg, api: dropboxAPI, content: dropboxContent, oauth: DropboxOAuth}
}

func (d *Dropbox) path(name string) string { return path.Join(d.Folder, name) }

// access is the bearer token, minted on first use from the refresh token.
func (d *Dropbox) access() (string, error) {
	if d.token != "" {
		return d.token, nil
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {d.RefreshToken},
		"client_id":     {d.AppKey},
	}
	if d.AppSecret != "" {
		form.Set("client_secret", d.AppSecret)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := d.tokenCall(form, &tok); err != nil {
		return "", fmt.Errorf("dropbox refresh token: %w", err)
	}
	d.token = tok.AccessToken
	return d.token, nil
}

// tokenCall posts to the OAuth token endpoint and decodes what comes back.
func (d *Dropbox) tokenCall(form url.Values, into any) error {
	resp, err := dropboxClient.PostForm(d.oauth+"/oauth2/token", form)
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

// call is one API request: an RPC endpoint takes its argument as the JSON
// body; a content endpoint takes it in Dropbox-API-Arg and the file as the
// body. The response body is returned open for the caller to read.
func (d *Dropbox) call(base, endpoint string, arg any, body io.Reader, size int64) (io.ReadCloser, error) {
	tok, err := d.access()
	if err != nil {
		return nil, err
	}
	argJSON, err := json.Marshal(arg)
	if err != nil {
		return nil, err
	}
	var req *http.Request
	if base == d.content {
		req, err = http.NewRequest("POST", base+endpoint, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Dropbox-API-Arg", string(argJSON))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = size
	} else {
		req, err = http.NewRequest("POST", base+endpoint, strings.NewReader(string(argJSON)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := dropboxClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp.Body, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var e struct {
		Summary string `json:"error_summary"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Summary != "" {
		return nil, fmt.Errorf("dropbox %s: %s", endpoint, strings.TrimSuffix(e.Summary, "/"))
	}
	return nil, fmt.Errorf("dropbox %s: %s: %s", endpoint, resp.Status, strings.TrimSpace(string(raw)))
}

func (d *Dropbox) Put(name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > dropboxUploadMax {
		return fmt.Errorf("%s is %s, over the %d MB a single Dropbox upload takes", name, Size(st.Size()), dropboxUploadMax>>20)
	}
	arg := map[string]any{"path": d.path(name), "mode": "overwrite", "mute": true}
	body, err := d.call(d.content, "/2/files/upload", arg, f, st.Size())
	if err != nil {
		return err
	}
	return body.Close()
}

func (d *Dropbox) Get(name string) (io.ReadCloser, error) {
	body, err := d.call(d.content, "/2/files/download", map[string]any{"path": d.path(name)}, nil, 0)
	if err != nil && strings.Contains(err.Error(), "not_found") {
		return nil, fmt.Errorf("no %s in Dropbox folder %s", name, d.Folder)
	}
	return body, err
}

func (d *Dropbox) Delete(name string) error {
	body, err := d.call(d.api, "/2/files/delete_v2", map[string]any{"path": d.path(name)}, nil, 0)
	if err != nil {
		return err
	}
	return body.Close()
}

// listPage is what list_folder and list_folder/continue answer with.
type listPage struct {
	Entries []struct {
		Tag            string    `json:".tag"`
		Name           string    `json:"name"`
		Size           int64     `json:"size"`
		ServerModified time.Time `json:"server_modified"`
	} `json:"entries"`
	Cursor  string `json:"cursor"`
	HasMore bool   `json:"has_more"`
}

// List is every file directly in the folder. A folder nobody has written to
// yet does not exist in Dropbox's eyes, which is an empty list, not an error.
func (d *Dropbox) List() ([]Object, error) {
	var objs []Object
	endpoint, arg := "/2/files/list_folder", map[string]any{"path": d.Folder}
	for {
		body, err := d.call(d.api, endpoint, arg, nil, 0)
		if err != nil {
			if endpoint == "/2/files/list_folder" && strings.Contains(err.Error(), "path/not_found") {
				return nil, nil
			}
			return nil, err
		}
		var page listPage
		err = json.NewDecoder(body).Decode(&page)
		body.Close()
		if err != nil {
			return nil, fmt.Errorf("dropbox list: %w", err)
		}
		for _, e := range page.Entries {
			if e.Tag != "file" {
				continue
			}
			objs = append(objs, Object{Name: e.Name, Size: e.Size, At: e.ServerModified})
		}
		if !page.HasMore {
			break
		}
		endpoint, arg = "/2/files/list_folder/continue", map[string]any{"cursor": page.Cursor}
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name < objs[j].Name })
	return objs, nil
}

// Auth is one authorization attempt: the URL the owner opens and the PKCE
// verifier that proves the code they bring back was ours to ask for.
type Auth struct {
	URL      string
	verifier string
	redirect string // the redirect URI the code was issued for, where one was used
}

// BeginAuth starts the consent flow. No redirect URI: Dropbox then shows the
// code on its own page for the owner to copy, which is what a CLI wants.
// token_access_type=offline is what makes a refresh token come back.
func (d *Dropbox) BeginAuth() (Auth, error) {
	if d.AppKey == "" {
		return Auth{}, errors.New("dropbox.app_key is empty — make an app at dropbox.com/developers/apps first")
	}
	verifier, challenge, err := pkce()
	if err != nil {
		return Auth{}, err
	}
	q := url.Values{
		"client_id":             {d.AppKey},
		"response_type":         {"code"},
		"token_access_type":     {"offline"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return Auth{URL: dropboxAuthorize + "?" + q.Encode(), verifier: verifier}, nil
}

// pkce is a fresh verifier and its S256 challenge (RFC 7636).
func pkce() (verifier, challenge string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// FinishAuth trades the code the owner pasted for the refresh token.
func (d *Dropbox) FinishAuth(a Auth, code string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {strings.TrimSpace(code)},
		"client_id":     {d.AppKey},
		"code_verifier": {a.verifier},
	}
	if d.AppSecret != "" {
		form.Set("client_secret", d.AppSecret)
	}
	var tok struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := d.tokenCall(form, &tok); err != nil {
		return "", fmt.Errorf("dropbox authorization: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", errors.New("dropbox authorization: no refresh token in the answer — is the app allowed offline access?")
	}
	return tok.RefreshToken, nil
}

// Size is a byte count in words: 10.7 KB, 1.2 MB.
func Size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
