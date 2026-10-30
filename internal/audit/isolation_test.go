package audit

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestUnavailableAuditAndConcurrentCloseNeverBlockBusiness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked")
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	a, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ev, e := a.Start(context.Background(), "test", "")
				if e != nil {
					t.Error(e)
				}
				if e = a.Finish(ev, nil); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	a.Close()
	wg.Wait()
	if a.Dropped.Load() == 0 {
		t.Fatal("missing dropped-event observation")
	}
}
