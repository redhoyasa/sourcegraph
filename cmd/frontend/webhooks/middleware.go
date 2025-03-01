package webhooks

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/inconshreveable/log15"

	"github.com/sourcegraph/sourcegraph/internal/conf"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/types"
	"github.com/sourcegraph/sourcegraph/schema"
)

// SetExternalServiceID attaches a specific external service ID to the current
// webhook request for logging purposes.
func SetExternalServiceID(ctx context.Context, id int64) {
	if setter, ok := ctx.Value(setterContextKey).(contextFunc); ok {
		setter(id)
	} else {
		log15.Error("cannot get setter from context; this likely means that SetExternalServiceID has been called from outside a HTTP handler wrapped in LogMiddleware")
	}
}

// LogMiddleware tracks webhook request content and stores it for diagnostic
// purposes.
type LogMiddleware struct {
	store database.WebhookLogStore
}

// NewLogMiddleware instantiates a new LogMiddleware.
func NewLogMiddleware(store database.WebhookLogStore) *LogMiddleware {
	return &LogMiddleware{store}
}

func (mw *LogMiddleware) Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If logging is disabled, we'll immediately forward to the next
		// handler, turning this middleware into a no-op.
		if !loggingEnabled(conf.Get()) {
			next.ServeHTTP(w, r)
			return
		}

		// Split the body reader so we can also access it. We need to shim an
		// io.ReadCloser implementation around the TeeReader, since TeeReader
		// doesn't implement io.Closer, but Request.Body is required to be an
		// io.ReadCloser.
		type readCloser struct {
			io.Reader
			io.Closer
		}
		buf := &bytes.Buffer{}
		tee := io.TeeReader(r.Body, buf)
		r.Body = readCloser{tee, r.Body}

		// Set up a logging response writer so we can also store the response;
		// most importantly, the status code.
		writer := &responseWriter{
			ResponseWriter: w,
			statusCode:     200,
		}

		// The external service ID is looked up within the webhook handler, but
		// we need access to it to be able to store the webhook log with the
		// appropriate external service ID. To handle this, we'll put a setter
		// closure into the context that can then be used by
		// SetExternalServiceID to receive the external service ID from the
		// handler.
		var externalServiceID *int64
		var setter contextFunc = func(id int64) {
			externalServiceID = &id
		}
		ctx := context.WithValue(r.Context(), setterContextKey, setter)

		// Delegate to the next handler.
		next.ServeHTTP(writer, r.WithContext(ctx))

		// See if we have the requested URL.
		url := ""
		if u := r.URL; u != nil {
			url = u.String()
		}

		// Write the payload.
		if err := mw.store.Create(r.Context(), &types.WebhookLog{
			ExternalServiceID: externalServiceID,
			StatusCode:        writer.statusCode,
			Request: types.WebhookLogMessage{
				Header:  r.Header,
				Body:    buf.Bytes(),
				Method:  r.Method,
				URL:     url,
				Version: r.Proto,
			},
			Response: types.WebhookLogMessage{
				Header: writer.Header(),
				Body:   writer.buf.Bytes(),
			},
		}); err != nil {
			// This is non-fatal, but almost certainly indicates a significant
			// problem nonetheless.
			log15.Error("error writing webhook log", "err", err)
		}
	})
}

type responseWriter struct {
	http.ResponseWriter

	// We do need to retain a duplicate copy of the response body, but since the
	// webhook response bodies are always either empty or a simple error
	// message, this isn't a lot of overhead.
	buf        bytes.Buffer
	statusCode int
}

var _ http.ResponseWriter = &responseWriter{}

func (rw *responseWriter) Write(data []byte) (int, error) {
	rw.buf.Write(data)
	return rw.ResponseWriter.Write(data)
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func loggingEnabledByDefault(keys *schema.EncryptionKeys) bool {
	// If any encryption key is provided, then this is off by default.
	if keys != nil {
		return keys.BatchChangesCredentialKey == nil &&
			keys.ExternalServiceKey == nil &&
			keys.UserExternalAccountKey == nil &&
			keys.WebhookLogKey == nil
	}

	// There's no encryption enabled on the site, so let's log webhook payloads
	// by default.
	return true
}

func loggingEnabled(c *conf.Unified) bool {
	if logging := c.WebhookLogging; logging != nil && logging.Enabled != nil {
		return *logging.Enabled
	}

	return loggingEnabledByDefault(c.EncryptionKeys)
}

// Define the context key and value that we'll use to track the setter that the
// log middleware uses to save the external service ID.

type contextKey string

var setterContextKey = contextKey("webhook setter")

type contextFunc func(int64)
