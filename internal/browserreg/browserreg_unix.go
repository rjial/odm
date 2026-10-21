//go:build !windows

package browserreg

import (
	"os"
	"path/filepath"
	"runtime"
)

// hostDir is one browser's per-user NativeMessagingHosts folder and the
// profile folder whose existence means the browser is installed.
type hostDir struct {
	browser, profile, dir, manifest string
}

func hostDirs() []hostDir {
	home, _ := os.UserHomeDir()
	j := func(p ...string) string { return filepath.Join(append([]string{home}, p...)...) }
	if runtime.GOOS == "darwin" {
		as := j("Library", "Application Support")
		return []hostDir{
			{"Chrome", filepath.Join(as, "Google", "Chrome"), filepath.Join(as, "Google", "Chrome", "NativeMessagingHosts"), "chromium"},
			{"Chromium", filepath.Join(as, "Chromium"), filepath.Join(as, "Chromium", "NativeMessagingHosts"), "chromium"},
			{"Edge", filepath.Join(as, "Microsoft Edge"), filepath.Join(as, "Microsoft Edge", "NativeMessagingHosts"), "chromium"},
			{"Firefox", filepath.Join(as, "Mozilla"), filepath.Join(as, "Mozilla", "NativeMessagingHosts"), "firefox"},
		}
	}
	return []hostDir{
		{"Chrome", j(".config", "google-chrome"), j(".config", "google-chrome", "NativeMessagingHosts"), "chromium"},
		{"Chromium", j(".config", "chromium"), j(".config", "chromium", "NativeMessagingHosts"), "chromium"},
		{"Edge", j(".config", "microsoft-edge"), j(".config", "microsoft-edge", "NativeMessagingHosts"), "chromium"},
		// Helium (https://helium.computer): its profile directory is
		// "net.imput.helium", confirmed on a real install; its macOS/Windows
		// equivalents are not, so only this one is added for now.
		{"Helium", j(".config", "net.imput.helium"), j(".config", "net.imput.helium", "NativeMessagingHosts"), "chromium"},
		{"Firefox", j(".mozilla"), j(".mozilla", "native-messaging-hosts"), "firefox"},
	}
}

// register copies the manifest into each installed browser's folder; a
// browser with no profile folder is skipped rather than having one created.
func register(chromium, firefox string) []Result {
	var out []Result
	for _, h := range hostDirs() {
		if _, err := os.Stat(h.profile); err != nil {
			out = append(out, Result{Browser: h.browser, Skipped: true})
			continue
		}
		src := chromium
		if h.manifest == "firefox" {
			src = firefox
		}
		dst := filepath.Join(h.dir, HostName+".json")
		data, err := os.ReadFile(src)
		if err == nil {
			err = os.MkdirAll(h.dir, 0o755)
		}
		if err == nil {
			err = os.WriteFile(dst, data, 0o644)
		}
		out = append(out, Result{Browser: h.browser, Location: dst, Err: err})
	}
	return out
}

func check() []Status {
	var out []Status
	for _, h := range hostDirs() {
		st := Status{Browser: h.browser}
		if _, err := os.Stat(h.profile); err == nil {
			st.Installed = true
		}
		if exe, err := manifestExe(filepath.Join(h.dir, HostName+".json")); err == nil {
			st.Registered, st.Exe = true, exe
		}
		out = append(out, st)
	}
	return out
}

func samePath(a, b string) bool {
	return a != "" && filepath.Clean(a) == filepath.Clean(b)
}

func unregister() []Result {
	var out []Result
	for _, h := range hostDirs() {
		dst := filepath.Join(h.dir, HostName+".json")
		err := os.Remove(dst)
		if os.IsNotExist(err) {
			err = nil
		}
		out = append(out, Result{Browser: h.browser, Location: dst, Err: err})
	}
	return out
}
