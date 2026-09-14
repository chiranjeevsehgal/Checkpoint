package extractor

import (
	"sort"
	"sync"

	"extraction-service/internal/llm"
	"extraction-service/internal/model"
)

// Extractor turns a batch of claimed jobs into structured output via the
// LLM. One batch always belongs to a single user — the batcher guarantees it.
//
// Adding a new extraction type (insights, summaries, ...) means:
//  1. implement this interface in a new file,
//  2. register it in the registry,
//  3. add its output table in a migration.
// Consumer, queue, claim, retry, and commit logic are shared and untouched.
type Extractor interface {
	// Type is the extraction_jobs.extraction_type value this extractor owns.
	Type() string
	// Messages builds the chat messages for one batch. item order is the
	// claim order; Parse receives the same slice to map answers back.
	Messages(items []model.Job) []llm.Message
	// Parse maps the LLM's JSON response onto the claimed jobs. Every
	// claimed job must yield a Result (possibly with empty output).
	Parse(content string, items []model.Job) ([]model.Result, error)
}

// Registry is the type-keyed set of extractors the worker runs.
type Registry struct {
	mu         sync.RWMutex
	extractors map[string]Extractor
}

func NewRegistry() *Registry {
	return &Registry{extractors: make(map[string]Extractor)}
}

func (r *Registry) Register(e Extractor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extractors[e.Type()] = e
}

func (r *Registry) Get(t string) (Extractor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.extractors[t]
	return e, ok
}

// Types returns the registered extraction types in stable order.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.extractors))
	for t := range r.extractors {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
