package api

import (
	"net/netip"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback", "127.0.0.1", true},
		{"rfc1918-10", "10.0.0.1", true},
		{"rfc1918-192", "192.168.0.1", true},
		{"rfc1918-172", "172.16.0.1", true},
		{"link-local-metadata", "169.254.169.254", true},
		{"unspecified", "0.0.0.0", true},
		{"public", "8.8.8.8", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr, err := netip.ParseAddr(c.ip)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", c.ip, err)
			}
			if got := isBlockedIP(addr); got != c.want {
				t.Errorf("isBlockedIP(%q) = %v, want %v", c.ip, got, c.want)
			}
		})
	}
}

func TestIsBlockedIPAllowPrivate(t *testing.T) {
	t.Run("flag off: RFC1918 stays blocked", func(t *testing.T) {
		cases := []string{"10.0.0.1", "192.168.0.1", "172.16.0.1"}
		for _, ip := range cases {
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", ip, err)
			}
			if !isBlockedIP(addr) {
				t.Errorf("isBlockedIP(%q) = false, want true when WEBHOOK_ALLOW_PRIVATE_IPS is unset", ip)
			}
		}
	})

	t.Run("flag on: RFC1918 allowed, hard-blocks unchanged", func(t *testing.T) {
		t.Setenv("WEBHOOK_ALLOW_PRIVATE_IPS", "true")

		allowed := []string{"10.0.0.1", "192.168.0.1", "172.16.0.1"}
		for _, ip := range allowed {
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", ip, err)
			}
			if isBlockedIP(addr) {
				t.Errorf("isBlockedIP(%q) = true, want false when WEBHOOK_ALLOW_PRIVATE_IPS=true", ip)
			}
		}

		stillBlocked := []string{"127.0.0.1", "169.254.169.254", "0.0.0.0"}
		for _, ip := range stillBlocked {
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", ip, err)
			}
			if !isBlockedIP(addr) {
				t.Errorf("isBlockedIP(%q) = false, want true even with WEBHOOK_ALLOW_PRIVATE_IPS=true", ip)
			}
		}
	})
}

func TestValidateCallbackURLAllowPrivate(t *testing.T) {
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_IPS", "true")

	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"rfc1918 https allowed", "https://10.0.0.1/hook", false},
		{"http scheme still rejected (separate flag)", "http://10.0.0.1/hook", true},
		{"invalid url still rejected", "not-a-url", true},
		{"empty still rejected", "", true},
		{"loopback still blocked", "https://127.0.0.1/hook", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateCallbackURL(c.url)
			if (err != nil) != c.wantErr {
				t.Errorf("validateCallbackURL(%q) error = %v, wantErr %v", c.url, err, c.wantErr)
			}
		})
	}
}

// TestIsBlockedIPAllowPrivateInlineComment is the security proof: an inline
// "# comment" trailing WEBHOOK_ALLOW_PRIVATE_IPS=true must still enable the
// opt-in. Against the pre-change code (raw os.Getenv(...) == "true"), this
// case fails -- the comment defeats the flag and RFC1918 targets stay
// blocked.
func TestIsBlockedIPAllowPrivateInlineComment(t *testing.T) {
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_IPS", "true   # for staging")

	allowed := []string{"10.0.0.1", "192.168.0.1", "172.16.0.1"}
	for _, ip := range allowed {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			t.Fatalf("ParseAddr(%q): %v", ip, err)
		}
		if isBlockedIP(addr) {
			t.Errorf("isBlockedIP(%q) = true, want false when WEBHOOK_ALLOW_PRIVATE_IPS=%q", ip, "true   # for staging")
		}
	}

	stillBlocked := []string{"127.0.0.1", "169.254.169.254", "0.0.0.0"}
	for _, ip := range stillBlocked {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			t.Fatalf("ParseAddr(%q): %v", ip, err)
		}
		if !isBlockedIP(addr) {
			t.Errorf("isBlockedIP(%q) = false, want true even with WEBHOOK_ALLOW_PRIVATE_IPS=%q", ip, "true   # for staging")
		}
	}
}

// TestIsBlockedIPRejectsUnrecognizedFlagValues asserts that unrecognized
// values for WEBHOOK_ALLOW_PRIVATE_IPS never widen the allow-list: they fall
// back to the fail-closed default (false), so RFC1918 addresses stay blocked.
func TestIsBlockedIPRejectsUnrecognizedFlagValues(t *testing.T) {
	unrecognized := []string{"TRUE", "1", "yes", "true anything"}
	for _, raw := range unrecognized {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("WEBHOOK_ALLOW_PRIVATE_IPS", raw)
			addr, err := netip.ParseAddr("10.0.0.1")
			if err != nil {
				t.Fatalf("ParseAddr: %v", err)
			}
			if !isBlockedIP(addr) {
				t.Errorf("isBlockedIP(10.0.0.1) = false, want true (blocked) when WEBHOOK_ALLOW_PRIVATE_IPS=%q is unrecognized", raw)
			}
		})
	}
}

// TestValidateCallbackURLInsecureHTTPInlineComment mirrors the private-IP
// inline-comment proof above, for the WEBHOOK_ALLOW_INSECURE_HTTP flag: an
// inline "# comment" must still enable http:// callback URLs, and an
// unrecognized value must still reject them.
func TestValidateCallbackURLInsecureHTTPInlineComment(t *testing.T) {
	t.Run("inline comment enables http scheme", func(t *testing.T) {
		t.Setenv("WEBHOOK_ALLOW_INSECURE_HTTP", "true # staging")
		if err := validateCallbackURL("http://8.8.8.8/hook"); err != nil {
			t.Errorf("validateCallbackURL(http://8.8.8.8/hook) error = %v, want nil when WEBHOOK_ALLOW_INSECURE_HTTP=%q", err, "true # staging")
		}
	})

	t.Run("unrecognized value stays fail-closed", func(t *testing.T) {
		t.Setenv("WEBHOOK_ALLOW_INSECURE_HTTP", "yes")
		if err := validateCallbackURL("http://8.8.8.8/hook"); err == nil {
			t.Errorf("validateCallbackURL(http://8.8.8.8/hook) error = nil, want an error when WEBHOOK_ALLOW_INSECURE_HTTP=%q is unrecognized", "yes")
		}
	})
}

func TestValidateCallbackURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"loopback IP literal", "https://127.0.0.1/hook", true},
		{"cloud metadata endpoint", "https://169.254.169.254/latest/meta-data", true},
		{"insecure http rejected by default", "http://8.8.8.8/hook", true},
		{"public https IP literal allowed", "https://8.8.8.8/hook", false},
		{"empty", "", true},
		{"not a url", "not-a-url", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateCallbackURL(c.url)
			if (err != nil) != c.wantErr {
				t.Errorf("validateCallbackURL(%q) error = %v, wantErr %v", c.url, err, c.wantErr)
			}
		})
	}
}
