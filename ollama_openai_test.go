package main

import "testing"

func TestOllamaOpenAIEndpointURL(t *testing.T) {
	cases := map[string]string{
		"https://ollama.com/api":             "https://ollama.com/v1/responses",
		"http://localhost:11434/api":         "http://localhost:11434/v1/responses",
		"http://localhost:11434/v1":          "http://localhost:11434/v1/responses",
		"http://localhost:11434":             "http://localhost:11434/v1/responses",
		"http://localhost:11434/":            "http://localhost:11434/v1/responses",
		"http://localhost:11434/api?key=val": "http://localhost:11434/v1/responses",
	}
	for base, want := range cases {
		got, err := ollamaOpenAIEndpointURL(base, "responses")
		if err != nil {
			t.Errorf("%s: %v", base, err)
			continue
		}
		if got != want {
			t.Errorf("%s => %s want %s", base, got, want)
		}
	}
}
