package backend

import "testing"

func TestS3Endpoint(t *testing.T) {
	tests := []struct {
		name string
		cfg  S3Config
		want string
	}{
		{"no port", S3Config{Endpoint: "s3.example.com"}, "s3.example.com"},
		{"strip scheme", S3Config{Endpoint: "https://s3.example.com"}, "s3.example.com"},
		{"strip trailing slash", S3Config{Endpoint: "s3.example.com/"}, "s3.example.com"},
		{"explicit port", S3Config{Endpoint: "s3.example.com", Port: 9000}, "s3.example.com:9000"},
		{"keep existing port", S3Config{Endpoint: "s3.example.com:9000", Port: 9000}, "s3.example.com:9000"},
	}
	for _, tt := range tests {
		if got := s3Endpoint(tt.cfg); got != tt.want {
			t.Errorf("%s: s3Endpoint() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestObjectKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{"no prefix", "", "a/b/c", "a/b/c"},
		{"with prefix", "backup/x", "a/b/c", "backup/x/a/b/c"},
		{"strip leading slash", "backup", "/a/b", "backup/a/b"},
		{"prefix trailing slash", "backup/", "a", "backup/a"},
	}
	for _, tt := range tests {
		a := &s3Accessor{cfg: S3Config{Prefix: tt.prefix}}
		if got := a.objectKey(tt.key); got != tt.want {
			t.Errorf("%s: objectKey() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNewS3ClientAccessStyle(t *testing.T) {
	base := S3Config{Endpoint: "s3.example.com", AccessKey: "ak", SecretKey: "sk"}

	for _, style := range []string{"", "auto", "path", "virtual-hosted", "virtual"} {
		c := base
		c.AccessStyle = style
		if _, err := newS3Client(c); err != nil {
			t.Errorf("newS3Client(style=%q): %v", style, err)
		}
	}

	bad := base
	bad.AccessStyle = "bogus"
	if _, err := newS3Client(bad); err == nil {
		t.Errorf("newS3Client(style=bogus) should fail")
	}

	if _, err := newS3Client(S3Config{}); err == nil {
		t.Errorf("newS3Client(no endpoint) should fail")
	}
}
