package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var embeddingRequests = struct {
	sync.Mutex
	models map[string]string
}{models: map[string]string{}}

type embeddingChainKeyType struct{}

var embeddingChainKey = embeddingChainKeyType{}
var nextEmbeddingChainID atomic.Uint64

func init() {
	base := http.DefaultTransport
	if base == nil {
		base = &http.Transport{}
	}
	http.DefaultTransport = &embeddingCompatibilityTransport{base: base}
}

type embeddingCompatibilityTransport struct {
	base http.RoundTripper
}

func (t *embeddingCompatibilityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Body == nil || !isEmbeddingRequestPath(req.URL.Path) {
		return t.base.RoundTrip(req)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	model, err := embeddingRequestModel(body)
	if err != nil {
		return jsonTransportError(req, http.StatusBadRequest, "invalid_request_error", err.Error()), nil
	}
	if err := guardEmbeddingModel(req, model); err != nil {
		return jsonTransportError(req, http.StatusBadRequest, "embedding_model_mismatch", err.Error()), nil
	}

	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	clone.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return t.base.RoundTrip(clone)
}

func isEmbeddingRequestPath(path string) bool {
	path = strings.TrimRight(path, "/")
	return strings.HasSuffix(path, "/embeddings") || strings.HasSuffix(path, "/api/embed")
}

func embeddingRequestModel(body []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("embedding request needs a model")
	}
	return model, nil
}

func guardEmbeddingModel(req *http.Request, model string) error {
	ctx := req.Context()
	var key string
	if chainID, ok := ctx.Value(embeddingChainKey).(string); ok {
		key = chainID
	} else {
		key = fmt.Sprintf("chain_%d", nextEmbeddingChainID.Add(1))
		ctx = context.WithValue(ctx, embeddingChainKey, key)
		*req = *req.WithContext(ctx)
	}

	embeddingRequests.Lock()
	previous, ok := embeddingRequests.models[key]
	if !ok {
		embeddingRequests.models[key] = model
	}
	embeddingRequests.Unlock()

	if !ok {
		done := req.Context().Done()
		if done != nil {
			go func() {
				<-done
				embeddingRequests.Lock()
				delete(embeddingRequests.models, key)
				embeddingRequests.Unlock()
			}()
		} else {
			go func() {
				embeddingRequests.Lock()
				delete(embeddingRequests.models, key)
				embeddingRequests.Unlock()
			}()
		}
		return nil
	}
	if previous != model {
		return fmt.Errorf("embedding failover cannot change model from %q to %q; use the same embedding model on every chain step", previous, model)
	}
	return nil
}
