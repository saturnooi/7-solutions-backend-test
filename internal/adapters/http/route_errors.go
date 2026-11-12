package http

import "net/http"

func jsonRouteErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern == "" {
			w = &routeErrorWriter{ResponseWriter: w}
		}
		mux.ServeHTTP(w, r)
	})
}

type routeErrorWriter struct {
	http.ResponseWriter
	replaced bool
}

func (w *routeErrorWriter) WriteHeader(status int) {
	switch status {
	case http.StatusNotFound:
		w.replaced = true
		writeError(w.ResponseWriter, status, "not_found", "no route matches this path")
		return
	case http.StatusMethodNotAllowed:
		w.replaced = true
		writeError(w.ResponseWriter, status, "method_not_allowed", "this path does not accept the method")
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *routeErrorWriter) Write(b []byte) (int, error) {
	if w.replaced {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *routeErrorWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
