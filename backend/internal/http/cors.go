package http

import "net/http"

// corsMiddleware allows the Next.js frontend (a different origin/port
// than this API in local development — e.g. http://localhost:3199
// calling http://localhost:8080) to read responses from this API in a
// browser. Root cause of the dashboard's "Failed to fetch" error: a
// browser's fetch() silently rejects a cross-origin response with no
// Access-Control-Allow-Origin header, even though the request itself
// succeeds at the network level (curl, which doesn't enforce CORS,
// showed a normal 200 response). Every route this API serves is either
// read-only and unauthenticated (GET) or already idempotent/safe to
// retry (the existing POST endpoints), and none of them rely on cookies
// or browser-managed credentials, so a permissive
// Access-Control-Allow-Origin: * is appropriate here — this is not an
// authenticated API where origin-restricted CORS would matter for
// security.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Razorpay-Signature, X-Razorpay-Event-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
