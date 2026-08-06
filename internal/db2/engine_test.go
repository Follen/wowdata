package db2

import (
	"context"
	"errors"
	"testing"
)

type engineTestReader struct {
	rows     map[uint32]map[string]interface{}
	allCalls int
	rowCalls int
}

type blockingContextReader struct {
	engineTestReader
	started chan struct{}
}

type streamingEngineReader struct {
	engineTestReader
}

func (r *streamingEngineReader) StreamRowsContext(ctx context.Context, fields []string, filterFn func(map[string]interface{}) bool, limit int, yield func(map[string]interface{}) error) error {
	emitted := 0
	for id := uint32(1); id <= uint32(len(r.rows)); id++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := projectFields(r.rows[id], fields)
		if filterFn != nil && !filterFn(row) {
			continue
		}
		if err := yield(row); err != nil {
			return err
		}
		emitted++
		if limit > 0 && emitted >= limit {
			return nil
		}
	}
	return nil
}

func (r *blockingContextReader) ScanContext(ctx context.Context, _ []string, _ func(map[string]interface{}) bool, _ int) ([]map[string]interface{}, error) {
	close(r.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *engineTestReader) GetRow(id uint32) map[string]interface{} {
	r.rowCalls++
	return r.rows[id]
}

func (r *engineTestReader) GetAllRows() map[uint32]map[string]interface{} {
	r.allCalls++
	return r.rows
}

func (r *engineTestReader) Size() int { return len(r.rows) }

func TestEngineSnapshotMultiGetPreservesRequestOrder(t *testing.T) {
	reader := &engineTestReader{rows: map[uint32]map[string]interface{}{
		1: {"ID": uint32(1), "Name": "one"},
		2: {"ID": uint32(2), "Name": "two"},
	}}
	engine := NewEngine()
	if err := engine.Register("T", nil, reader); err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	defer snapshot.Close()
	result, stats, err := snapshot.Execute(context.Background(), QueryPlan{Table: "T", Mode: PlanRows, IDs: []uint32{2, 1, 2}, Fields: []string{"ID"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 3 || result.Rows[0]["ID"] != uint32(2) || result.Rows[1]["ID"] != uint32(1) || result.Rows[2]["ID"] != uint32(2) {
		t.Fatalf("unexpected rows: %#v", result.Rows)
	}
	if stats.BatchCount != 1 || reader.allCalls != 0 {
		t.Fatalf("expected batch point path without GetAllRows, stats=%+v allCalls=%d", stats, reader.allCalls)
	}
}

func TestEngineSnapshotCancelAndClose(t *testing.T) {
	engine := NewEngine()
	if err := engine.Register("T", nil, &engineTestReader{rows: map[uint32]map[string]interface{}{1: {"ID": uint32(1)}}}); err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := snapshot.Execute(ctx, QueryPlan{Table: "T", Mode: PlanRows, IDs: []uint32{1}}); err == nil {
		t.Fatal("cancelled query should return an error")
	}
	snapshot.Close()
	if _, _, err := snapshot.Execute(context.Background(), QueryPlan{Table: "T", Mode: PlanRows, IDs: []uint32{1}}); err == nil {
		t.Fatal("closed snapshot should return an error")
	}
}

func TestEngineCancellationReachesRunningScanOperator(t *testing.T) {
	reader := &blockingContextReader{started: make(chan struct{})}
	engine := NewEngine()
	if err := engine.Register("T", nil, reader); err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	defer snapshot.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := snapshot.Execute(ctx, QueryPlan{Table: "T", Mode: PlanSearch, SearchField: "Name", SearchQuery: "x"})
		done <- err
	}()
	<-reader.started
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("scan error = %v, want context.Canceled", err)
	}
}

func TestEngineStreamIsBoundedAndPropagatesYieldErrors(t *testing.T) {
	reader := &streamingEngineReader{engineTestReader: engineTestReader{rows: map[uint32]map[string]interface{}{
		1: {"ID": uint32(1), "Name": "one"},
		2: {"ID": uint32(2), "Name": "two"},
	}}}
	engine := NewEngine()
	if err := engine.Register("T", nil, reader); err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	defer snapshot.Close()
	var ids []uint32
	_, stats, err := snapshot.Execute(context.Background(), QueryPlan{Table: "T", Mode: PlanStream, Fields: []string{"ID"}, Yield: func(row map[string]interface{}) error {
		ids = append(ids, row["ID"].(uint32))
		return nil
	}})
	if err != nil || len(ids) != 2 || stats.OutputRows != 2 || stats.Physical != "stream-scan" || reader.allCalls != 0 {
		t.Fatalf("stream result ids=%v stats=%+v allCalls=%d err=%v", ids, stats, reader.allCalls, err)
	}
	wantErr := errors.New("stop")
	_, _, err = snapshot.Execute(context.Background(), QueryPlan{Table: "T", Mode: PlanStream, Yield: func(map[string]interface{}) error { return wantErr }})
	if !errors.Is(err, wantErr) {
		t.Fatalf("yield error = %v, want %v", err, wantErr)
	}
}
