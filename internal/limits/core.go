package limits

// Keys of the limits of the core.
const (
	RequestGuestKB     = "core.request.guest_kb"
	RequestBodyKB      = "core.request.body_kb"
	RequestContentKB   = "core.request.content_kb"
	RequestSSOKB       = "core.request.sso_kb"
	RequestImportMB    = "core.request.import_mb"
	RequestUpload      = "core.request.upload" // marker: the route streams an upload (storage.max_upload_mb)
	LargeParallel      = "core.request.large_parallel"
	LargeWaitSeconds   = "core.request.large_wait_s"
	AuditValueGuest    = "core.audit.value_guest_bytes"
	AuditValue         = "core.audit.value_bytes"
	WaitNoticeSeconds  = "core.ui.wait_notice_s"
	RateUser           = "core.rate.user_per_second"
	RateUserBurst      = "core.rate.user_burst"
	RateGuest          = "core.rate.guest_per_second"
	RateGuestBurst     = "core.rate.guest_burst"
	RateExpensive      = "core.rate.expensive_per_second"
	RateExpensiveBurst = "core.rate.expensive_burst"
)

// LargeBody is the size from which a request counts as large (limited
// number in parallel).
const LargeBody = 1 << 20

func init() {
	MustRegister("core",
		Def{Key: RequestGuestKB, Group: "requests", Unit: "kb", Default: 64, Min: 4, Max: 4096,
			Label: "Request size without sign-in", Help: "Largest body of a request that carries no session and no API key (sign-in, forms of guests)."},
		Def{Key: RequestBodyKB, Group: "requests", Unit: "kb", Default: 1024, Min: 64, Max: 65536,
			Label: "Request size", Help: "Largest body of a request of a signed-in user. Routes that need more declare their own limit."},
		Def{Key: RequestContentKB, Group: "requests", Unit: "kb", Default: 4096, Min: 256, Max: 262144,
			Label: "Size of content", Help: "Largest body when saving pages, drafts, templates and previews."},
		Def{Key: RequestSSOKB, Group: "requests", Unit: "kb", Default: 1024, Min: 64, Max: 16384,
			Label: "Messages of identity providers", Help: "Largest SAML or OIDC message and SCIM request."},
		Def{Key: RequestImportMB, Group: "requests", Unit: "mb", Default: 64, Min: 1, Max: 4096,
			Label: "Size of imports", Help: "Largest file when importing categories or translations."},
		Def{Key: LargeParallel, Group: "requests", Unit: "count", Default: 4, Min: 1, Max: 256,
			Label: "Large requests at the same time", Help: "How many requests larger than 1 MB are processed at once (uploads are streamed and do not count). Further ones wait."},
		Def{Key: LargeWaitSeconds, Group: "requests", Unit: "seconds", Default: 30, Min: 1, Max: 600,
			Label: "Waiting time of large requests", Help: "How long a large request waits for a free place before it is answered with \"busy\" (503). Nothing is stored in that case; the sender repeats it."},
		Def{Key: AuditValueGuest, Group: "audit", Unit: "bytes", Default: 1024, Min: 128, Max: 65536,
			Label: "Audit values without sign-in", Help: "Longest value in the audit log for events without a signed-in user (e.g. failed sign-ins). Longer values are shortened; the original length is noted."},
		Def{Key: AuditValue, Group: "audit", Unit: "bytes", Default: 65536, Min: 1024, Max: 16777216,
			Label: "Audit values", Help: "Longest before/after value in the audit log for events of signed-in users."},
		Def{Key: WaitNoticeSeconds, Group: "requests", Unit: "seconds", Default: 5, Min: 1, Max: 300,
			Label: "Notice while waiting", Help: "After this time without an answer to a change the user is asked whether to keep waiting or to cancel. The entered content is kept in both cases."},
		Def{Key: RateUser, Group: "rate", Unit: "per_second", Default: 50, Min: 1, Max: 100000,
			Label: "Requests of an account", Help: "Requests per second a signed-in account may send in the long run. Beyond it the answer is 429 with the time to wait."},
		Def{Key: RateUserBurst, Group: "rate", Unit: "count", Default: 500, Min: 1, Max: 1000000,
			Label: "Reserve of an account", Help: "Requests an account may send at once before the rate per second applies (opening a page sends several)."},
		Def{Key: RateGuest, Group: "rate", Unit: "per_second", Default: 20, Min: 1, Max: 100000,
			Label: "Requests of an address without sign-in", Help: "Requests per second of all guests behind one address. Behind a reverse proxy the address of the client counts (server.trusted_proxies)."},
		Def{Key: RateGuestBurst, Group: "rate", Unit: "count", Default: 200, Min: 1, Max: 1000000,
			Label: "Reserve of an address without sign-in", Help: "Requests an address may send at once before the rate per second applies."},
		Def{Key: RateExpensive, Group: "rate", Unit: "per_second", Default: 5, Min: 1, Max: 10000,
			Label: "Expensive requests", Help: "Searches, previews, exports and uploads per second of an account or address, on top of the general limit."},
		Def{Key: RateExpensiveBurst, Group: "rate", Unit: "count", Default: 50, Min: 1, Max: 100000,
			Label: "Reserve for expensive requests", Help: "Expensive requests an account or address may send at once."},
	)
}
