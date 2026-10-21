package browserreg

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Chrome derives an unpacked extension's ID from the manifest "key": the
// first 128 bits of SHA-256 over the DER public key, hex digits 0-f written
// as letters a-p. If the key in extension/manifest.json changes, the host
// manifest would allow the wrong origin; this keeps the two in step.
func TestChromeIDMatchesManifestKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "extension", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Key string `json:"key"`
		BSS struct {
			Gecko struct {
				ID string `json:"id"`
			} `json:"gecko"`
		} `json:"browser_specific_settings"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(m.Key)
	if err != nil {
		t.Fatalf("manifest key: %v", err)
	}
	sum := sha256.Sum256(der)
	id := []byte(hex.EncodeToString(sum[:16]))
	for i, c := range id {
		if c >= 'a' {
			id[i] = 'a' + 10 + (c - 'a')
		} else {
			id[i] = 'a' + (c - '0')
		}
	}
	if string(id) != ChromeExtensionID {
		t.Fatalf("manifest key gives ID %s, constant is %s", id, ChromeExtensionID)
	}
	if m.BSS.Gecko.ID != FirefoxExtensionID {
		t.Fatalf("gecko id %q, constant is %q", m.BSS.Gecko.ID, FirefoxExtensionID)
	}
}

func TestWriteManifests(t *testing.T) {
	dir := t.TempDir()
	c, f, err := writeManifests(dir, "/opt/odm/odm")
	if err != nil {
		t.Fatal(err)
	}
	var cm, fm manifest
	for p, dst := range map[string]*manifest{c: &cm, f: &fm} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, dst); err != nil {
			t.Fatal(err)
		}
	}
	if cm.Path != "/opt/odm/odm" || cm.Name != HostName || len(cm.AllowedExtensions) != 0 ||
		cm.AllowedOrigins[0] != "chrome-extension://"+ChromeExtensionID+"/" {
		t.Fatalf("chromium manifest %+v", cm)
	}
	if len(fm.AllowedOrigins) != 0 || fm.AllowedExtensions[0] != FirefoxExtensionID {
		t.Fatalf("firefox manifest %+v", fm)
	}
}

func TestRegisterUnixFolders(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("folder layout checked on Linux")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	_ = os.MkdirAll(filepath.Join(home, ".config", "google-chrome"), 0o755)    // Chrome installed
	_ = os.MkdirAll(filepath.Join(home, ".mozilla"), 0o755)                    // Firefox installed
	_ = os.MkdirAll(filepath.Join(home, ".config", "net.imput.helium"), 0o755) // Helium installed

	res := Register("/opt/odm/odm", filepath.Join(home, ".config", "odm"))
	got := map[string]Result{}
	for _, r := range res {
		got[r.Browser] = r
	}
	for _, b := range []string{"Chrome", "Firefox", "Helium"} {
		if got[b].Err != nil || got[b].Skipped {
			t.Fatalf("%s: %+v", b, got[b])
		}
		if _, err := os.Stat(got[b].Location); err != nil {
			t.Fatalf("%s manifest missing: %v", b, err)
		}
	}
	if !got["Chromium"].Skipped || !got["Edge"].Skipped {
		t.Fatal("browsers without a profile folder should be skipped")
	}
	ff, _ := os.ReadFile(got["Firefox"].Location)
	if !json.Valid(ff) || !strings.Contains(string(ff), FirefoxExtensionID) {
		t.Fatalf("firefox manifest content: %s", ff)
	}

	status := map[string]Status{}
	for _, s := range Check() {
		status[s.Browser] = s
	}
	if !status["Chrome"].Points("/opt/odm/odm") || !status["Firefox"].Points("/opt/odm/odm") || !status["Helium"].Points("/opt/odm/odm") {
		t.Fatalf("Check after Register: %+v", status)
	}
	if status["Chrome"].Points("/other/odm") {
		t.Fatal("Points matched a different executable")
	}
	if status["Edge"].Registered || status["Edge"].Installed {
		t.Fatalf("Edge without a profile: %+v", status["Edge"])
	}

	for _, r := range Unregister() {
		if r.Err != nil {
			t.Fatalf("unregister %s: %v", r.Browser, r.Err)
		}
	}
	for _, s := range Check() {
		if s.Registered {
			t.Fatalf("%s still registered after Unregister", s.Browser)
		}
	}
	if _, err := os.Stat(got["Chrome"].Location); !os.IsNotExist(err) {
		t.Fatal("chrome manifest not removed")
	}
}

func TestSummary(t *testing.T) {
	me := "/opt/odm/odm"
	all := func(exe string) []Status {
		var s []Status
		for _, b := range []string{"Chrome", "Edge", "Firefox"} {
			s = append(s, Status{Browser: b, Installed: true, Registered: exe != "", Exe: exe})
		}
		return s
	}
	if got := Summary(all(me), me); got != "Registered for Chrome, Edge, Firefox." {
		t.Fatalf("all here: %q", got)
	}
	if got := Summary(all(""), me); got != "Not registered: Chrome, Edge, Firefox." {
		t.Fatalf("none: %q", got)
	}
	mixed := []Status{
		{Browser: "Chrome", Installed: true, Registered: true, Exe: me},
		{Browser: "Edge", Installed: true, Registered: true, Exe: "/old/odm"},
		{Browser: "Firefox", Installed: true},
		{Browser: "Chromium"}, // not installed: not mentioned
	}
	want := "Registered for Chrome. Edge uses another ODM: /old/odm. Not registered for: Firefox."
	if got := Summary(mixed, me); got != want {
		t.Fatalf("mixed:\n got  %q\n want %q", got, want)
	}
}
