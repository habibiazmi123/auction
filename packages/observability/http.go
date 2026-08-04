package observability

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const (
	requestIDKey     contextKey = "request_id"
	correlationIDKey contextKey = "correlation_id"
)

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func CorrelationID(ctx context.Context) string {
	value, _ := ctx.Value(correlationIDKey).(string)
	return value
}

func Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.NewString()
			}
			correlationID := r.Header.Get("X-Correlation-ID")
			if correlationID == "" {
				correlationID = requestID
			}
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			ctx = context.WithValue(ctx, correlationIDKey, correlationID)
			r = r.WithContext(ctx)
			w.Header().Set("X-Request-ID", requestID)
			w.Header().Set("X-Correlation-ID", correlationID)
			writer, status := wrapResponseWriter(w)
			started := time.Now()
			next.ServeHTTP(writer, r)
			logger.InfoContext(ctx, "http request",
				slog.String("request_id", requestID),
				slog.String("correlation_id", correlationID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status.status),
				slog.Duration("latency", time.Since(started)),
			)
		})
	}
}

func HTTPMiddleware(logger *slog.Logger) func(http.Handler) http.Handler { return Middleware(logger) }

type statusWriter struct {
	http.ResponseWriter
	status      int
	headerWrote bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.headerWrote {
		return
	}
	w.status = status
	w.headerWrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if !w.headerWrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func wrapResponseWriter(writer http.ResponseWriter) (http.ResponseWriter, *statusWriter) {
	status := &statusWriter{ResponseWriter: writer, status: http.StatusOK}
	flusher, hasFlusher := writer.(http.Flusher)
	hijacker, hasHijacker := writer.(http.Hijacker)
	pusher, hasPusher := writer.(http.Pusher)
	readerFrom, hasReaderFrom := writer.(io.ReaderFrom)
	mask := 0
	if hasFlusher {
		mask |= 1
	}
	if hasHijacker {
		mask |= 2
	}
	if hasPusher {
		mask |= 4
	}
	if hasReaderFrom {
		mask |= 8
	}
	flush := flusherCapability{status: status, flusher: flusher}
	hijack := hijackerCapability{hijacker: hijacker}
	push := pusherCapability{pusher: pusher}
	read := readerFromCapability{status: status, readerFrom: readerFrom}
	switch mask {
	case 1:
		return &statusWriterF{statusWriter: status, flusherCapability: flush}, status
	case 2:
		return &statusWriterH{statusWriter: status, hijackerCapability: hijack}, status
	case 3:
		return &statusWriterFH{statusWriter: status, flusherCapability: flush, hijackerCapability: hijack}, status
	case 4:
		return &statusWriterP{statusWriter: status, pusherCapability: push}, status
	case 5:
		return &statusWriterFP{statusWriter: status, flusherCapability: flush, pusherCapability: push}, status
	case 6:
		return &statusWriterHP{statusWriter: status, hijackerCapability: hijack, pusherCapability: push}, status
	case 7:
		return &statusWriterFHP{statusWriter: status, flusherCapability: flush, hijackerCapability: hijack, pusherCapability: push}, status
	case 8:
		return &statusWriterR{statusWriter: status, readerFromCapability: read}, status
	case 9:
		return &statusWriterFR{statusWriter: status, flusherCapability: flush, readerFromCapability: read}, status
	case 10:
		return &statusWriterHR{statusWriter: status, hijackerCapability: hijack, readerFromCapability: read}, status
	case 11:
		return &statusWriterFHR{statusWriter: status, flusherCapability: flush, hijackerCapability: hijack, readerFromCapability: read}, status
	case 12:
		return &statusWriterPR{statusWriter: status, pusherCapability: push, readerFromCapability: read}, status
	case 13:
		return &statusWriterFPR{statusWriter: status, flusherCapability: flush, pusherCapability: push, readerFromCapability: read}, status
	case 14:
		return &statusWriterHPR{statusWriter: status, hijackerCapability: hijack, pusherCapability: push, readerFromCapability: read}, status
	case 15:
		return &statusWriterFHPR{statusWriter: status, flusherCapability: flush, hijackerCapability: hijack, pusherCapability: push, readerFromCapability: read}, status
	default:
		return status, status
	}
}

type flusherCapability struct {
	status  *statusWriter
	flusher http.Flusher
}

func (w flusherCapability) Flush() {
	if !w.status.headerWrote {
		w.status.WriteHeader(http.StatusOK)
	}
	w.flusher.Flush()
}

type hijackerCapability struct{ hijacker http.Hijacker }

func (w hijackerCapability) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijacker.Hijack()
}

type pusherCapability struct{ pusher http.Pusher }

func (w pusherCapability) Push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}

type readerFromCapability struct {
	status     *statusWriter
	readerFrom io.ReaderFrom
}

func (w readerFromCapability) ReadFrom(reader io.Reader) (int64, error) {
	if !w.status.headerWrote {
		w.status.WriteHeader(http.StatusOK)
	}
	return w.readerFrom.ReadFrom(reader)
}

type statusWriterF struct {
	*statusWriter
	flusherCapability
}

type statusWriterH struct {
	*statusWriter
	hijackerCapability
}

type statusWriterFH struct {
	*statusWriter
	flusherCapability
	hijackerCapability
}

type statusWriterP struct {
	*statusWriter
	pusherCapability
}

type statusWriterFP struct {
	*statusWriter
	flusherCapability
	pusherCapability
}

type statusWriterHP struct {
	*statusWriter
	hijackerCapability
	pusherCapability
}

type statusWriterFHP struct {
	*statusWriter
	flusherCapability
	hijackerCapability
	pusherCapability
}

type statusWriterR struct {
	*statusWriter
	readerFromCapability
}

type statusWriterFR struct {
	*statusWriter
	flusherCapability
	readerFromCapability
}

type statusWriterHR struct {
	*statusWriter
	hijackerCapability
	readerFromCapability
}

type statusWriterFHR struct {
	*statusWriter
	flusherCapability
	hijackerCapability
	readerFromCapability
}

type statusWriterPR struct {
	*statusWriter
	pusherCapability
	readerFromCapability
}

type statusWriterFPR struct {
	*statusWriter
	flusherCapability
	pusherCapability
	readerFromCapability
}

type statusWriterHPR struct {
	*statusWriter
	hijackerCapability
	pusherCapability
	readerFromCapability
}

type statusWriterFHPR struct {
	*statusWriter
	flusherCapability
	hijackerCapability
	pusherCapability
	readerFromCapability
}
