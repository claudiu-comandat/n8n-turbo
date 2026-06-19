package nodes

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type SplitInBatches struct{}

type LoopOverItems struct{}

type Wait struct{}

type LoopState struct {
	AllItems     []dataplane.Item
	CurrentIndex int
	BatchSize    int
	LazyCSV      *lazyCSVLoopState
}

type lazyCSVLoopState struct {
	Sources       []*lazyCSVSource
	CurrentSource int
	OriginalItems []dataplane.Item
}

type lazyCSVSource struct {
	SourceItem      dataplane.Item
	SourceItemIndex int
	Params          extractParams
	Reader          io.ReadCloser
	Stream          *csvObjectStream
}

type loopStateStore struct {
	mu     sync.RWMutex
	states map[string]LoopState
}

var defaultLoopStateStore = &loopStateStore{states: map[string]LoopState{}}

func (SplitInBatches) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return executeLoopBatch(ctx, in)
}

func (LoopOverItems) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return executeLoopBatch(ctx, in)
}

func (Wait) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	output := dataplane.MainOutput(firstInput(in.InputData))
	now := time.Now().UTC()
	resumeAt, reason, err := waitResumeAt(in.Node.Parameters, now)
	if err != nil {
		return nil, err
	}
	if resumeAt.IsZero() || !resumeAt.After(now) {
		return output, nil
	}
	return output, &engine.SuspendError{ExecutionID: in.ExecutionID, NodeName: in.Node.Name, ResumeAt: resumeAt, Reason: reason, Output: output}
}

func executeLoopBatch(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	params := loopParams(in.Node.Parameters)
	key := loopStateKey(in)
	state, ok := defaultLoopStateStore.Get(key)
	items := firstInput(in.InputData)
	if !ok || params.Reset {
		closeLoopState(state)
		lazyState, err := newLazyCSVLoopState(ctx, in, items)
		if err != nil {
			return nil, err
		}
		if lazyState != nil {
			state = LoopState{BatchSize: params.BatchSize, LazyCSV: lazyState}
		} else {
			// Keep a reference to the current execution items instead of deep-cloning
			// the whole collection, which doubles memory usage for large batches.
			state = LoopState{AllItems: items, CurrentIndex: 0, BatchSize: params.BatchSize}
		}
		defaultLoopStateStore.Set(key, state)
	} else if state.LazyCSV == nil {
		state.CurrentIndex += state.BatchSize
	}
	if state.LazyCSV != nil {
		batch, done, err := state.LazyCSV.NextBatch(ctx, params.BatchSize)
		if err != nil {
			defaultLoopStateStore.Delete(key)
			return nil, err
		}
		if done {
			original := state.LazyCSV.OriginalItems
			defaultLoopStateStore.Delete(key)
			return dataplane.Output{[]dataplane.Item{}, original}, nil
		}
		defaultLoopStateStore.Set(key, state)
		return dataplane.Output{batch, []dataplane.Item{}}, nil
	}
	if len(state.AllItems) == 0 || state.CurrentIndex >= len(state.AllItems) {
		defaultLoopStateStore.Delete(key)
		return dataplane.Output{[]dataplane.Item{}, state.AllItems}, nil
	}
	end := state.CurrentIndex + state.BatchSize
	if end > len(state.AllItems) {
		end = len(state.AllItems)
	}
	defaultLoopStateStore.Set(key, state)
	return dataplane.Output{cloneItems(state.AllItems[state.CurrentIndex:end]), []dataplane.Item{}}, nil
}

type loopBatchParams struct {
	BatchSize int
	Reset     bool
}

func loopParams(params map[string]any) loopBatchParams {
	batchSize := intParam(params, "batchSize", 1)
	if batchSize <= 0 {
		batchSize = 1
	}
	if batchSize > 500 {
		batchSize = 500
	}
	options := mergeObject(params["options"])
	return loopBatchParams{BatchSize: batchSize, Reset: boolParam(options, "reset", boolParam(params, "reset", false))}
}

func loopStateKey(in engine.ExecuteInput) string {
	nodeKey := firstNonEmptyNode(in.Node.ID, in.Node.Name, in.Node.Type)
	return in.ExecutionID + ":" + nodeKey
}

func (s *loopStateStore) Get(key string) (LoopState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[key]
	return state, ok
}

func (s *loopStateStore) Set(key string, state LoopState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[key] = state
}

func (s *loopStateStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state, ok := s.states[key]; ok {
		closeLoopState(state)
	}
	delete(s.states, key)
}

func closeLoopState(state LoopState) {
	if state.LazyCSV != nil {
		state.LazyCSV.Close()
	}
}

func newLazyCSVLoopState(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item) (*lazyCSVLoopState, error) {
	if len(items) == 0 {
		return nil, nil
	}
	for _, item := range items {
		if !isLazyCSVPlaceholder(item) {
			return nil, nil
		}
	}
	sources := make([]*lazyCSVSource, 0, len(items))
	for _, item := range items {
		source, err := newLazyCSVSource(ctx, in.BinaryStore, item)
		if err != nil {
			for _, opened := range sources {
				opened.Close()
			}
			return nil, err
		}
		sources = append(sources, source)
	}
	return &lazyCSVLoopState{Sources: sources, OriginalItems: items}, nil
}

func isLazyCSVPlaceholder(item dataplane.Item) bool {
	_, ok := lazyCSVMeta(item)
	return ok
}

func lazyCSVMeta(item dataplane.Item) (map[string]any, bool) {
	raw, ok := item.JSON[lazyCSVMetaKey]
	if !ok {
		return nil, false
	}
	meta, ok := raw.(map[string]any)
	return meta, ok
}

func newLazyCSVSource(ctx context.Context, store binarydata.Store, item dataplane.Item) (*lazyCSVSource, error) {
	meta, ok := lazyCSVMeta(item)
	if !ok {
		return nil, fmt.Errorf("lazy csv metadata missing")
	}
	params := newExtractParams(meta)
	params.binaryProperty = stringParam(meta, "binaryProperty", "binaryPropertyName", "dataPropertyName")
	sourceItemIndex := intParam(meta, "sourceItemIndex", 0)
	binary, ok := item.Binary[params.binaryProperty]
	if !ok {
		return nil, fmt.Errorf("lazy csv binary property %s not found", params.binaryProperty)
	}
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return nil, err
	}
	decoded, err := decodedReader(reader, params.encoding)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	buffered := bufio.NewReader(decoded)
	preview, _ := buffered.Peek(64 * 1024)
	delimiter := detectCSVDelimiter(string(preview), params.delimiter)
	csvReader := csv.NewReader(buffered)
	csvReader.Comma = []rune(delimiter)[0]
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true
	csvReader.TrimLeadingSpace = params.trimLeadingSpace
	if params.commentChar != "" {
		csvReader.Comment = []rune(params.commentChar)[0]
	}
	return &lazyCSVSource{
		SourceItem:      item,
		SourceItemIndex: sourceItemIndex,
		Params:          params,
		Reader:          reader,
		Stream:          newCSVObjectStream(csvReader, params),
	}, nil
}

func (s *lazyCSVLoopState) NextBatch(ctx context.Context, batchSize int) ([]dataplane.Item, bool, error) {
	if s == nil {
		return nil, true, nil
	}
	if batchSize <= 0 {
		batchSize = 1
	}
	batch := make([]dataplane.Item, 0, batchSize)
	for len(batch) < batchSize && s.CurrentSource < len(s.Sources) {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		default:
		}
		source := s.Sources[s.CurrentSource]
		entry, err := source.Stream.Next()
		if err == io.EOF {
			source.Close()
			s.CurrentSource++
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if entry == nil {
			continue
		}
		batch = append(batch, extractedRowItem(source.SourceItem, source.SourceItemIndex, entry, source.Params.includeInputFields))
	}
	if len(batch) == 0 {
		return nil, true, nil
	}
	return batch, false, nil
}

func (s *lazyCSVLoopState) Close() {
	if s == nil {
		return
	}
	for _, source := range s.Sources {
		source.Close()
	}
}

func (s *lazyCSVSource) Close() {
	if s == nil || s.Reader == nil {
		return
	}
	_ = s.Reader.Close()
	s.Reader = nil
}

func waitResumeAt(params map[string]any, now time.Time) (time.Time, string, error) {
	resume := stringParam(params, "resume")
	if resume == "" {
		resume = stringParam(params, "resumeType")
	}
	if resume == "" {
		resume = "timeInterval"
	}
	switch resume {
	case "timeInterval", "afterTimeInterval", "after":
		duration, err := waitDuration(intParam(params, "amount", 1), stringParam(params, "unit"))
		if err != nil {
			return time.Time{}, "", err
		}
		return now.Add(duration), "timeInterval", nil
	case "specificTime", "atSpecifiedTime", "at":
		raw := stringParam(params, "dateTime")
		if raw == "" {
			raw = stringParam(params, "resumeAt")
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, raw)
		}
		if err != nil {
			return time.Time{}, "", fmt.Errorf("invalid wait dateTime %q", raw)
		}
		return parsed.UTC(), "specificTime", nil
	case "webhook", "form":
		return externalWaitResumeAt(params, now), resume, nil
	default:
		return time.Time{}, "", fmt.Errorf("unsupported wait resume mode %s", resume)
	}
}

func externalWaitResumeAt(params map[string]any, now time.Time) time.Time {
	if !boolParam(params, "limitWaitTime", false) {
		return time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	switch stringParam(params, "limitType") {
	case "atSpecifiedTime", "specificTime", "at":
		raw := stringParam(params, "maxDateAndTime", "dateTime", "resumeAt")
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, raw)
		}
		if err == nil {
			return parsed.UTC()
		}
	}
	amount := intParam(params, "limitAmount", intParam(params, "resumeAmount", 1))
	unit := stringParam(params, "limitUnit")
	if unit == "" {
		unit = stringParam(params, "resumeUnit")
	}
	duration, err := waitDuration(amount, unit)
	if err != nil {
		return time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return now.Add(duration)
}

func waitDuration(amount int, unit string) (time.Duration, error) {
	if amount <= 0 {
		amount = 1
	}
	switch unit {
	case "", "second", "seconds":
		return time.Duration(amount) * time.Second, nil
	case "millisecond", "milliseconds":
		return time.Duration(amount) * time.Millisecond, nil
	case "minute", "minutes":
		return time.Duration(amount) * time.Minute, nil
	case "hour", "hours":
		return time.Duration(amount) * time.Hour, nil
	case "day", "days":
		return time.Duration(amount) * 24 * time.Hour, nil
	case "week", "weeks":
		return time.Duration(amount) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported wait unit %s", unit)
	}
}
