package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is enough of the S3 REST API for the provider: path-style, one
// bucket, PUT/GET/DELETE an object and a ListObjectsV2 with continuation.
type fakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
	auth    []string // every Authorization header seen
	pages   int      // objects per list page; 0 means all
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	if r.Header.Get("x-amz-date") == "" || r.Header.Get("x-amz-content-sha256") == "" {
		http.Error(w, "unsigned", http.StatusForbidden)
		return
	}
	key, ok := strings.CutPrefix(r.URL.Path, "/"+f.bucket+"/")
	if !ok && r.URL.Path != "/"+f.bucket {
		http.Error(w, "NoSuchBucket", http.StatusNotFound)
		return
	}
	switch {
	case r.Method == "PUT":
		b, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(b)
		if got := r.Header.Get("x-amz-content-sha256"); got != hex.EncodeToString(sum[:]) {
			http.Error(w, "XAmzContentSHA256Mismatch", http.StatusBadRequest)
			return
		}
		f.objects[key] = b
	case r.Method == "GET" && key == "":
		q := r.URL.Query()
		if q.Get("list-type") != "2" {
			http.Error(w, "want list-type=2", http.StatusBadRequest)
			return
		}
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, q.Get("prefix")) && k > q.Get("continuation-token") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		truncated := false
		if f.pages > 0 && len(keys) > f.pages {
			keys = keys[:f.pages]
			truncated = true
		}
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Name>`+f.bucket+`</Name>`)
		fmt.Fprintf(w, "<IsTruncated>%v</IsTruncated>", truncated)
		if truncated {
			fmt.Fprintf(w, "<NextContinuationToken>%s</NextContinuationToken>", keys[len(keys)-1])
		}
		for _, k := range keys {
			fmt.Fprintf(w, "<Contents><Key>%s</Key><LastModified>2026-09-16T14:05:00.000Z</LastModified><Size>%d</Size></Contents>", k, len(f.objects[k]))
		}
		fmt.Fprint(w, "</ListBucketResult>")
	case r.Method == "GET":
		b, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
	case r.Method == "DELETE":
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "nope", http.StatusMethodNotAllowed)
	}
}

func newFakeS3(t *testing.T) (*fakeS3, *S3) {
	t.Helper()
	f := &fakeS3{bucket: "ledger", objects: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, &S3{S3Config{Bucket: "ledger", Prefix: "pecunia", Endpoint: srv.URL, AccessKey: "AKIA", SecretKey: "s3cr3t"}}
}

func TestS3Provider(t *testing.T) {
	providerSuite(t, func(t *testing.T) Provider {
		_, p := newFakeS3(t)
		return p
	})

	t.Run("keys live under the prefix", func(t *testing.T) {
		f, p := newFakeS3(t)
		src := writeTemp(t, "x")
		if err := p.Put("a.tar.gz", src); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.objects["pecunia/a.tar.gz"]; !ok {
			t.Fatalf("objects %v", f.objects)
		}
		if !strings.HasPrefix(f.auth[0], "AWS4-HMAC-SHA256 Credential=AKIA/") {
			t.Fatalf("auth %q", f.auth[0])
		}
	})

	t.Run("no prefix puts keys at the root", func(t *testing.T) {
		f, p := newFakeS3(t)
		p.Prefix = ""
		p.Put("a.tar.gz", writeTemp(t, "x"))
		if _, ok := f.objects["a.tar.gz"]; !ok {
			t.Fatalf("objects %v", f.objects)
		}
	})

	t.Run("list follows the continuation token", func(t *testing.T) {
		f, p := newFakeS3(t)
		f.pages = 2
		for _, n := range []string{"a", "b", "c", "d", "e"} {
			p.Put(n+".tar.gz", writeTemp(t, n))
		}
		f.objects["other/x.tar.gz"] = []byte("not ours")
		objs, err := p.List()
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
		if !objs[0].At.Equal(time.Date(2026, 9, 16, 14, 5, 0, 0, time.UTC)) {
			t.Fatalf("at %v", objs[0].At)
		}
	})

	t.Run("an error names the S3 code", func(t *testing.T) {
		_, p := newFakeS3(t)
		p.Bucket = "missing"
		_, err := p.List()
		if err == nil || !strings.Contains(err.Error(), "NoSuchBucket") {
			t.Fatalf("err %v", err)
		}
	})
}

// TestSigV4 pins the signer to the worked examples in the S3 documentation
// ("Examples: Signature Calculations in the Authorization Header"), keys and
// all — they are the published test vectors, not real credentials.
func TestSigV4(t *testing.T) {
	s := &S3{S3Config{AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", Region: "us-east-1"}}
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	empty := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	t.Run("get object", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
		req.Header.Set("Range", "bytes=0-9")
		s.sign(req, empty, at)
		want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;range;x-amz-content-sha256;x-amz-date,Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
		if got := req.Header.Get("Authorization"); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	})

	t.Run("list with a query", func(t *testing.T) {
		u, _ := url.Parse("https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J")
		req := &http.Request{Method: "GET", URL: u, Header: http.Header{}, Host: u.Host}
		s.sign(req, empty, at)
		want := "Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"
		if got := req.Header.Get("Authorization"); !strings.HasSuffix(got, want) {
			t.Fatalf("got  %s\nwant …%s", got, want)
		}
	})
}

func TestS3URL(t *testing.T) {
	cases := []struct {
		cfg  S3Config
		key  string
		want string
	}{
		{S3Config{Bucket: "b", Region: "eu-west-1"}, "pecunia/a.tar.gz", "https://b.s3.eu-west-1.amazonaws.com/pecunia/a.tar.gz"},
		{S3Config{Bucket: "b", Endpoint: "https://minio.local:9000"}, "a.tar.gz", "https://minio.local:9000/b/a.tar.gz"},
		{S3Config{Bucket: "b", Endpoint: "https://minio.local:9000/"}, "", "https://minio.local:9000/b"},
		{S3Config{Bucket: "b", Region: "us-east-1"}, "a b.tar.gz", "https://b.s3.us-east-1.amazonaws.com/a%20b.tar.gz"},
	}
	for _, c := range cases {
		if got := (&S3{c.cfg}).url(c.key); got != c.want {
			t.Errorf("url(%+v, %q) = %q, want %q", c.cfg, c.key, got, c.want)
		}
	}
}
