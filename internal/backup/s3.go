package backup

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
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

// S3 talks to a bucket over the REST API with Signature Version 4, by hand:
// four requests, one signer. The SDK would be a hundred packages for what
// fits in this file, and every S3-compatible service — MinIO, R2, B2 —
// speaks this same dialect when given an endpoint.
type S3 struct {
	S3Config
}

// now is swapped in tests, where the signature has to match a known one.
var now = time.Now

var s3Client = &http.Client{Timeout: 10 * time.Minute}

func (s *S3) key(name string) string {
	if s.Prefix == "" {
		return name
	}
	return path.Join(s.Prefix, name)
}

// url is the object's address: path-style under an endpoint, the way every
// S3-compatible service expects, virtual-hosted on AWS itself.
func (s *S3) url(key string) string {
	var base string
	if s.Endpoint != "" {
		base = strings.TrimSuffix(s.Endpoint, "/") + "/" + s.Bucket
	} else {
		base = fmt.Sprintf("https://%s.s3.%s.amazonaws.com", s.Bucket, s.Region)
	}
	if key == "" {
		return base
	}
	return base + "/" + escapePath(key)
}

func (s *S3) Put(name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", s.url(s.key(name)), f)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := s.do(req, hex.EncodeToString(h.Sum(nil)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (s *S3) Get(name string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", s.url(s.key(name)), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.do(req, emptyHash)
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return nil, fmt.Errorf("no %s in bucket %s", name, s.Bucket)
		}
		return nil, err
	}
	return resp.Body, nil
}

func (s *S3) Delete(name string) error {
	req, err := http.NewRequest("DELETE", s.url(s.key(name)), nil)
	if err != nil {
		return err
	}
	resp, err := s.do(req, emptyHash)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// listResult is the part of a ListObjectsV2 response the provider reads.
type listResult struct {
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
	} `xml:"Contents"`
}

// List walks every page of the prefix and reports the names under it.
func (s *S3) List() ([]Object, error) {
	prefix := ""
	if s.Prefix != "" {
		prefix = strings.TrimSuffix(s.Prefix, "/") + "/"
	}
	var objs []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}}
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		req, err := http.NewRequest("GET", s.url("")+"/?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := s.do(req, emptyHash)
		if err != nil {
			return nil, err
		}
		var page listResult
		err = xml.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", s.Bucket, err)
		}
		for _, c := range page.Contents {
			name, ok := strings.CutPrefix(c.Key, prefix)
			if !ok || strings.Contains(name, "/") {
				continue
			}
			objs = append(objs, Object{Name: name, Size: c.Size, At: c.LastModified})
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			break
		}
		token = page.NextContinuationToken
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name < objs[j].Name })
	return objs, nil
}

// s3Error is the body S3 sends with a failure.
type s3Error struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// do signs and sends, and turns a non-2xx into an error carrying S3's code.
func (s *S3) do(req *http.Request, payloadHash string) (*http.Response, error) {
	s.sign(req, payloadHash, now())
	resp, err := s3Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var e s3Error
	if xml.Unmarshal(body, &e) == nil && e.Code != "" {
		return nil, fmt.Errorf("s3 %s %s: %s: %s", req.Method, req.URL.Path, e.Code, e.Message)
	}
	return nil, fmt.Errorf("s3 %s %s: %s: %s", req.Method, req.URL.Path, resp.Status, strings.TrimSpace(string(body)))
}

const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// sign adds the SigV4 headers, as "Authenticating Requests (AWS Signature
// Version 4)" lays them out: canonical request, string to sign, signing key.
func (s *S3) sign(req *http.Request, payloadHash string, at time.Time) {
	at = at.UTC()
	date := at.Format("20060102")
	region := s.Region
	if region == "" {
		region = "us-east-1"
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	req.Header.Set("Host", host)
	req.Header.Set("x-amz-date", at.Format("20060102T150405Z"))
	req.Header.Set("x-amz-content-sha256", payloadHash)

	// Canonical headers: every header, lowercased, sorted, values trimmed.
	var names []string
	for k := range req.Header {
		names = append(names, strings.ToLower(k))
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		v := req.Header.Get(k)
		if k == "host" {
			v = host
		}
		fmt.Fprintf(&canonHeaders, "%s:%s\n", k, strings.TrimSpace(v))
	}
	signed := strings.Join(names, ";")

	canonReq := strings.Join([]string{
		req.Method,
		canonicalPath(req.URL),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{date, region, "s3", "aws4_request"}, "/")
	toSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		at.Format("20060102T150405Z"),
		scope,
		hashHex([]byte(canonReq)),
	}, "\n")

	k := hmacSum([]byte("AWS4"+s.SecretKey), date)
	k = hmacSum(k, region)
	k = hmacSum(k, "s3")
	k = hmacSum(k, "aws4_request")
	sig := hex.EncodeToString(hmacSum(k, toSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s,SignedHeaders=%s,Signature=%s",
		s.AccessKey, scope, signed, sig))
}

// canonicalPath is the URI path, each segment encoded the way SigV4 wants
// (the same as escapePath), "/" for an empty one.
func canonicalPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	// EscapedPath keeps a few characters SigV4 encodes; re-encode from the
	// decoded form so both sides agree.
	return "/" + escapePath(strings.TrimPrefix(u.Path, "/"))
}

// canonicalQuery is the query, sorted by name, each name and value encoded.
func canonicalQuery(q url.Values) string {
	var names []string
	for k := range q {
		names = append(names, k)
	}
	sort.Strings(names)
	var parts []string
	for _, k := range names {
		for _, v := range q[k] {
			parts = append(parts, escape(k)+"="+escape(v))
		}
	}
	return strings.Join(parts, "&")
}

// escape is SigV4's URI encoding: unreserved characters kept, everything
// else %XX in upper case, a space never a "+".
func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// escapePath encodes a key segment by segment, keeping its slashes.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		segs[i] = escape(seg)
	}
	return strings.Join(segs, "/")
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSum(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}
