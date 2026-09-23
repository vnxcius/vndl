package jobs

import (
	"testing"
	"time"

	"vndl/internal/config"
)

func TestPendingJobExpiresWithoutFileCall(t *testing.T) {
	mgr := NewManager(config.Config{JobTTL: 30 * time.Millisecond})
	j := mgr.Create("https://youtube.com/watch?v=x", "", false, "t", "mp4", "mp4")

	if _, ok := mgr.Get(j.ID); !ok {
		t.Fatal("job should exist immediately after creation")
	}

	time.Sleep(80 * time.Millisecond)

	if _, ok := mgr.Get(j.ID); ok {
		t.Fatal("job should have expired even though /file was never called")
	}
}

func TestSubscribeCapsPerJob(t *testing.T) {
	mgr := NewManager(config.Config{JobTTL: time.Minute})
	j := mgr.Create("https://youtube.com/watch?v=x", "", false, "t", "mp4", "mp4")

	var cancels []func()
	for i := 0; i < maxSubscribersPerJob; i++ {
		_, cancel, ok := j.Subscribe()
		if !ok {
			t.Fatalf("subscribe %d should have succeeded under the cap", i)
		}
		cancels = append(cancels, cancel)
	}

	if _, _, ok := j.Subscribe(); ok {
		t.Fatal("subscribe past the cap should be rejected")
	}

	cancels[0]()
	if _, _, ok := j.Subscribe(); !ok {
		t.Fatal("subscribe should succeed again once a slot frees up")
	}
}
