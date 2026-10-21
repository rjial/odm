package youtube

// client is one Innertube client identity. Ported from a handful of the
// entries in yt-dlp's yt_dlp/extractor/youtube/_base.py INNERTUBE_CLIENTS
// (not the whole table) — only the ones whose GVS_PO_TOKEN_POLICY marks
// HLS as not requiring a PO Token, so a plain Innertube request plus the
// "n" solve in nsig.go is enough, no BotGuard involved.
type client struct {
	Name          string // for error messages, not sent
	ClientName    string
	ClientVersion string
	ClientNameID  string // X-Youtube-Client-Name / INNERTUBE_CONTEXT_CLIENT_NAME
	UserAgent     string
	DeviceMake    string
	DeviceModel   string
	OSName        string
	OSVersion     string
	AndroidSDK    int
}

func (c client) context() map[string]any {
	m := map[string]any{
		"clientName":    c.ClientName,
		"clientVersion": c.ClientVersion,
		"userAgent":     c.UserAgent,
	}
	if c.DeviceMake != "" {
		m["deviceMake"] = c.DeviceMake
	}
	if c.DeviceModel != "" {
		m["deviceModel"] = c.DeviceModel
	}
	if c.OSName != "" {
		m["osName"] = c.OSName
	}
	if c.OSVersion != "" {
		m["osVersion"] = c.OSVersion
	}
	if c.AndroidSDK != 0 {
		m["androidSdkVersion"] = c.AndroidSDK
	}
	return m
}

// hlsClients are tried in order by FetchPlayer. Last checked against
// yt-dlp's _base.py on 2026-10-20 (yt-dlp version with _video.py at 217KB,
// the jsc/pot split already in place).
//
// Known risk, not just theoretical: _base.py's own comment on android_vr
// notes Google started 403'ing even HLS for that client's clientVersion
// 1.65.10 on 2026-08-17 — two months before this was written. These client
// identities (version strings, user agents) are exactly the kind of thing
// that goes stale; update them from a current yt-dlp checkout, not by
// guessing.
var hlsClients = []client{
	{
		Name:          "tv",
		ClientName:    "TVHTML5",
		ClientVersion: "7.20260707.07.00",
		ClientNameID:  "7",
		UserAgent:     "Mozilla/5.0 (ChromiumStylePlatform) Cobalt/25.lts.30.1034943-gold (unlike Gecko), Unknown_TV_Unknown_0/Unknown (Unknown, Unknown)",
	},
	{
		Name:          "android",
		ClientName:    "ANDROID",
		ClientVersion: "21.26.364",
		ClientNameID:  "3",
		UserAgent:     "com.google.android.youtube/21.26.364 (Linux; U; Android 11) gzip",
		OSName:        "Android",
		OSVersion:     "11",
		AndroidSDK:    30,
	},
	{
		Name:          "ios",
		ClientName:    "IOS",
		ClientVersion: "21.26.4",
		ClientNameID:  "5",
		UserAgent:     "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)",
		DeviceMake:    "Apple",
		DeviceModel:   "iPhone16,2",
		OSName:        "iPhone",
		OSVersion:     "18.3.2.22D82",
	},
}
