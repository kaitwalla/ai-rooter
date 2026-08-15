package main

import (
	"bufio"
	"net"
	"net/http"
	"strings"
)

type sseFlushWriter struct { http.ResponseWriter }

func (w *sseFlushWriter) Write(p []byte) (int,error) {
	n,err:=w.ResponseWriter.Write(p)
	if err==nil && strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")),"text/event-stream") {
		if f,ok:=w.ResponseWriter.(http.Flusher);ok { f.Flush() }
	}
	return n,err
}
func (w *sseFlushWriter) Flush(){ if f,ok:=w.ResponseWriter.(http.Flusher);ok{f.Flush()} }
func (w *sseFlushWriter) Hijack()(net.Conn,*bufio.ReadWriter,error){ return w.ResponseWriter.(http.Hijacker).Hijack() }

func sseFlushMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ next.ServeHTTP(&sseFlushWriter{ResponseWriter:w},r) })
}
