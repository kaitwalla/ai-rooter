package main

import "net/http"

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
