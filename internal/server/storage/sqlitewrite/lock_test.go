package sqlitewrite

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoSerializesWriters(t *testing.T) {
	var active int32
	var maxActive int32
	done := make(chan struct{}, 2)

	for i := 0; i < 2; i++ {
		go func() {
			err := Do(context.Background(), func() error {
				current := atomic.AddInt32(&active, 1)
				if current > atomic.LoadInt32(&maxActive) {
					atomic.StoreInt32(&maxActive, current)
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return nil
			})
			if err != nil {
				t.Errorf("Do: %v", err)
			}
			done <- struct{}{}
		}()
	}

	<-done
	<-done
	if maxActive != 1 {
		t.Fatalf("max active writers = %d, want 1", maxActive)
	}
}
